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

// Apply handles POST /api/v2/ontologies/{ontologyApiName}/actions/apply
func (h *Handler) Apply(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var req ApplyRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ActionType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType", nil))
		return
	}

	result, err := h.executor.Apply(r.Context(), ontologyRID, &req)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ActionFailed", map[string]string{"error": err.Error()}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// ApplyBatch handles POST /api/v2/ontologies/{ontologyApiName}/actions/applyBatch.
//
// Semantics:
//   - Request body: { "actions": [...], "mode": "atomic"|"bestEffort" }.
//     "mode" is optional and defaults to "atomic".
//   - Atomic mode: Prepare every action, fail fast on the first error, and
//     (only on full success) publish exactly one combined EditBatch.
//     On failure the response body carries phase + failedActionIndex + actionType.
//   - Best-effort mode: Prepare every action, skip prepare failures, and
//     publish one combined EditBatch for the survivors. Failures are surfaced
//     alongside the committed results.
//
// The backwards-compatible response shape (a top-level "results" array) is
// preserved on success for callers that do not yet read the new fields.
func (h *Handler) ApplyBatch(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var reqs struct {
		Actions []ApplyRequest `json:"actions"`
		Mode    string         `json:"mode"`
	}
	if err := httputil.ReadJSON(r, &reqs); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	mode := strings.ToLower(strings.TrimSpace(reqs.Mode))
	var (
		result *BatchResult
		err    error
	)
	switch mode {
	case "", "atomic":
		result, err = h.executor.ApplyBatchAtomic(r.Context(), ontologyRID, reqs.Actions)
	case "besteffort":
		result, err = h.executor.ApplyBatchBestEffort(r.Context(), ontologyRID, reqs.Actions)
	default:
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMode", map[string]string{"mode": reqs.Mode}))
		return
	}

	if err != nil {
		apierror.WriteJSON(w, asBatchError(err))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
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
