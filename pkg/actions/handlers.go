package actions

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// staleObjectAPIError converts an Executor-level *StaleObjectError into the
// Palantir-wire-format 409 Conflict response used by US-023 optimistic
// concurrency. Returns nil when err is not a *StaleObjectError so the
// caller can fall through to its existing error translation path.
func staleObjectAPIError(err error) *apierror.APIError {
	var stale *StaleObjectError
	if !errors.As(err, &stale) {
		return nil
	}
	return apierror.NewConflict("StaleObject", map[string]string{
		"objectType":      stale.ObjectType,
		"primaryKey":      stale.PrimaryKey,
		"expectedVersion": strconv.Itoa(stale.ExpectedVersion),
		"currentVersion":  strconv.FormatInt(stale.CurrentVersion, 10),
	})
}

// typedAPIError unwraps a chained *apierror.APIError (e.g. WEAVE_VALIDATION_ENUM
// from US-208 ValueType constraint enforcement) so the handler surfaces the
// pre-built status code + parameters instead of collapsing it into a generic
// 400 ActionFailed. Returns nil when no typed error is present.
func typedAPIError(err error) *apierror.APIError {
	var apiErr *apierror.APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

// Handler handles action HTTP requests.
type Handler struct {
	executor *Executor
}

// NewHandler creates a new action handler.
func NewHandler(executor *Executor) *Handler {
	return &Handler{executor: executor}
}

// Apply handles POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/apply.
//
// The action API name lives in the URL (Foundry OSv2 shape). Any
// actionType field in the body is ignored — the path is the single
// source of truth. An empty {action} path segment is rejected with
// MissingActionType so malformed URLs surface a clean 400.
//
// Foundry OSv2 options:
//   - options.mode: VALIDATE_ONLY | VALIDATE_AND_EXECUTE (default)
//   - options.returnEdits: ALL | ALL_V2_WITH_DELETIONS | NONE (default ALL)
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	action := chi.URLParam(r, "action")

	if action == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	var req ApplyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}
	req.ActionType = action

	// Resolve options with defaults.
	mode := "VALIDATE_AND_EXECUTE"
	returnEdits := "ALL"
	if req.Options != nil {
		if req.Options.Mode != "" {
			mode = strings.ToUpper(req.Options.Mode)
		}
		if req.Options.ReturnEdits != "" {
			returnEdits = strings.ToUpper(req.Options.ReturnEdits)
		}
	}

	// Validate mode enum.
	if mode != "VALIDATE_ONLY" && mode != "VALIDATE_AND_EXECUTE" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMode",
			map[string]string{"mode": mode, "allowed": "VALIDATE_ONLY, VALIDATE_AND_EXECUTE"}))
		return
	}

	// VALIDATE_ONLY: run Prepare only, return validation result.
	if mode == "VALIDATE_ONLY" {
		_, err := h.executor.Prepare(r.Context(), ontologyRID, &req)
		if err != nil {
			httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
				Validation: &ValidationResult{Result: "INVALID"},
			})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
			Validation: &ValidationResult{Result: "VALID"},
		})
		return
	}

	// VALIDATE_AND_EXECUTE: normal execution.
	result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
	if err != nil {
		if staleErr := staleObjectAPIError(err); staleErr != nil {
			apierror.WriteJSON(w, staleErr)
			return
		}
		if apiErr := typedAPIError(err); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()}))
		return
	}

	// Build SyncApplyActionResponseV2 envelope.
	resp := &SyncApplyActionResponseV2{
		OperationID: result.BatchID,
	}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.Edits)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ApplyActionOverrides is the Foundry OSv2 override envelope. In Foundry this
// carries uniqueIdentifier and currentTime knobs used to make auto-generated
// parameters deterministic. Weave does not currently auto-generate parameters,
// so the only meaningful override today is an explicit parameter override map
// which is merged into the wrapped request's parameters (overrides win).
type ApplyActionOverrides struct {
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// applyWithOverridesEnvelope is the Foundry OSv2 request body for
// POST .../actions/{action}/applyWithOverrides.
type applyWithOverridesEnvelope struct {
	Request   *ApplyRequest         `json:"request"`
	Overrides *ApplyActionOverrides `json:"overrides,omitempty"`
}

// ApplyWithOverrides handles POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/applyWithOverrides.
//
// The request body wraps an ApplyActionRequestV2 in a `request` field and an
// ApplyActionOverrides in an `overrides` field. Overrides.parameters are
// merged into request.parameters (overrides win), then the resulting request
// is routed through the same Apply code path so options.mode and
// options.returnEdits behave identically to the plain apply endpoint.
func (h *Handler) ApplyWithOverrides(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	action := chi.URLParam(r, "action")

	if action == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	var env applyWithOverridesEnvelope
	if err := httputil.ReadJSON(r, &env); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}
	if env.Request == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRequest",
			map[string]string{"field": "request", "message": "request field is required"}))
		return
	}

	req := *env.Request
	req.ActionType = action

	// Merge overrides into parameters. Overrides win on key collision.
	if env.Overrides != nil && len(env.Overrides.Parameters) > 0 {
		if req.Parameters == nil {
			req.Parameters = make(map[string]interface{}, len(env.Overrides.Parameters))
		}
		for k, v := range env.Overrides.Parameters {
			req.Parameters[k] = v
		}
	}

	// Resolve options with defaults (same semantics as Apply).
	mode := "VALIDATE_AND_EXECUTE"
	returnEdits := "ALL"
	if req.Options != nil {
		if req.Options.Mode != "" {
			mode = strings.ToUpper(req.Options.Mode)
		}
		if req.Options.ReturnEdits != "" {
			returnEdits = strings.ToUpper(req.Options.ReturnEdits)
		}
	}

	if mode != "VALIDATE_ONLY" && mode != "VALIDATE_AND_EXECUTE" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMode",
			map[string]string{"mode": mode, "allowed": "VALIDATE_ONLY, VALIDATE_AND_EXECUTE"}))
		return
	}

	if mode == "VALIDATE_ONLY" {
		if _, err := h.executor.Prepare(r.Context(), ontologyRID, &req); err != nil {
			httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
				Validation: &ValidationResult{Result: "INVALID"},
			})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, &ValidateOnlyResponse{
			Validation: &ValidationResult{Result: "VALID"},
		})
		return
	}

	result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
	if err != nil {
		if staleErr := staleObjectAPIError(err); staleErr != nil {
			apierror.WriteJSON(w, staleErr)
			return
		}
		if apiErr := typedAPIError(err); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &SyncApplyActionResponseV2{
		OperationID: result.BatchID,
	}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.Edits)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// ApplyBatch handles POST /api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch.
//
// In Foundry OSv2 a batch is one-action-many-parameter-sets: the action
// API name sits in the path and every body item is only a parameter
// payload for that same action. Weave enforces this by stamping the
// path's action onto every request in the body, ignoring any actionType
// a client may still be sending.
//
// Foundry OSv2 semantics (PR-03):
//   - Request body: { "actions": [...], "options": { "returnEdits": "ALL"|"NONE" } }
//   - Batch is always atomic (all-or-nothing).
//   - The old Weave "mode" field (atomic/bestEffort) is rejected with 400.
//   - options.returnEdits controls whether edits appear in the response (default ALL).
func (h *Handler) ApplyBatch(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	action := chi.URLParam(r, "action")

	if action == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	var reqs struct {
		Actions []ApplyRequest     `json:"actions"`
		Options *BatchApplyOptions `json:"options,omitempty"`
		Mode    string             `json:"mode"` // old field — rejected if present
	}
	if err := httputil.ReadJSON(r, &reqs); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	// Reject the old Weave mode field.
	if reqs.Mode != "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DeprecatedField",
			map[string]string{
				"field":   "mode",
				"message": "The 'mode' field has been removed. Use 'options.returnEdits' instead.",
			}))
		return
	}

	// Resolve returnEdits option with default.
	returnEdits := "ALL"
	if reqs.Options != nil && reqs.Options.ReturnEdits != "" {
		returnEdits = strings.ToUpper(reqs.Options.ReturnEdits)
	}
	if returnEdits != "ALL" && returnEdits != "NONE" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReturnEdits",
			map[string]string{"returnEdits": returnEdits, "allowed": "ALL, NONE"}))
		return
	}

	// Foundry batch is same-action-many-parameter-sets. Stamp the path's
	// action onto every item so the executor resolves one action type
	// per batch regardless of what the client put in the body.
	for i := range reqs.Actions {
		reqs.Actions[i].ActionType = action
	}

	// US-238: opt-in PG-transaction commit via ?atomic=true. The default
	// path is the existing best-effort-commit atomic batch — it prepares
	// all-or-nothing but writes action_logs outside a tx. Setting
	// atomic=true routes through the tx-wrapped commit so PG state rolls
	// back together on failure and NATS publish happens post-commit.
	var (
		result *BatchResult
		err    error
	)
	if r.URL.Query().Get("atomic") == "true" {
		result, err = h.executor.ApplyBatchAtomicTx(r.Context(), ontologyRID, reqs.Actions)
	} else {
		result, err = h.executor.ApplyBatchAtomic(r.Context(), ontologyRID, reqs.Actions)
	}
	if err != nil {
		apierror.WriteJSON(w, asBatchError(err))
		return
	}

	// Build BatchApplyActionResponseV2 envelope.
	resp := &BatchApplyActionResponseV2{}
	if returnEdits != "NONE" {
		resp.Edits = countEdits(result.AppliedEdits)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// asBatchError converts an error returned by ApplyBatchAtomic / CommitBatch
// into a structured API error response. A *BatchError surfaces its phase,
// failedActionIndex, and actionType; everything else is treated as a generic
// ActionFailed. US-208: when the BatchError wraps a typed *apierror.APIError
// (e.g. WEAVE_VALIDATION_ENUM from constraint validation) the typed error is
// surfaced verbatim with its original status code + parameters.
func asBatchError(err error) *apierror.APIError {
	if apiErr := typedAPIError(err); apiErr != nil {
		return apiErr
	}
	var be *BatchError
	if errors.As(err, &be) {
		return apierror.NewInvalidParameter("ActionFailed", map[string]string{
			"phase":             be.Phase,
			"failedActionIndex": strconv.Itoa(be.FailedActionIndex),
			"actionType":        be.ActionType,
			"error":             be.Message,
		})
	}
	return apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()})
}

// revertRequest is the JSON body for POST .../actions/revert.
type revertRequest struct {
	ActionLogID int64 `json:"actionLogId"`
}

// Revert handles POST /api/v2/ontologies/{ontologyApiName}/actions/revert.
//
// Accepts { actionLogId } and reverses the action's edits by publishing a
// reverse EditBatch. Returns 409 Conflict if the action has already been
// reverted.
func (h *Handler) Revert(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var req revertRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}
	if req.ActionLogID == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionLogId", nil))
		return
	}

	result, err := h.executor.Revert(r.Context(), ontologyRID, req.ActionLogID)
	if err != nil {
		var alreadyReverted *AlreadyRevertedError
		if errors.As(err, &alreadyReverted) {
			apierror.WriteJSON(w, apierror.NewConflict("AlreadyReverted", map[string]string{
				"actionLogId": strconv.FormatInt(alreadyReverted.ActionLogID, 10),
			}))
			return
		}
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionLogNotFound", map[string]string{
				"actionLogId": strconv.FormatInt(req.ActionLogID, 10),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RevertFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &SyncApplyActionResponseV2{
		OperationID: result.BatchID,
		Edits:       countEdits(result.Edits),
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}
