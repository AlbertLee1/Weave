package actions

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

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
		Actions []ApplyRequest    `json:"actions"`
		Options *BatchApplyOptions `json:"options,omitempty"`
		Mode    string            `json:"mode"` // old field — rejected if present
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

	// Always atomic.
	result, err := h.executor.ApplyBatchAtomic(r.Context(), ontologyRID, reqs.Actions)
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
// ActionFailed.
func asBatchError(err error) *apierror.APIError {
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
