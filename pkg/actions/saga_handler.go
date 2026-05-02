package actions

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// ApplySagaRequest is the body for POST /actions/applySaga. Each item in
// Steps is a full ApplyRequest (with its own actionType + parameters).
// IdempotencyKey is optional; when set, repeating a request with the
// same key returns the prior SagaResult verbatim with replayed=true.
type ApplySagaRequest struct {
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	Steps          []ApplyRequest `json:"steps"`
}

// ApplySaga handles POST
// /api/v2/ontologies/{ontologyApiName}/actions/applySaga (US-369). The
// body declares an ordered list of ActionType+parameters tuples; the
// executor runs them as a saga with reverse-order compensation,
// idempotency-key dedupe, and DLQ enqueueing for failed compensators.
func (h *Handler) ApplySaga(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	var body ApplySagaRequest
	if err := httputil.ReadJSON(r, &body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody",
			map[string]string{"error": err.Error()}))
		return
	}
	if len(body.Steps) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingSteps",
			map[string]string{"field": "steps", "message": "at least one step is required"}))
		return
	}
	for i := range body.Steps {
		if body.Steps[i].ActionType == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionType",
				map[string]string{"index": strconv.Itoa(i)}))
			return
		}
	}

	opts := SagaOptions{IdempotencyKey: body.IdempotencyKey}
	if u := auth.UserFromContext(r.Context()); u != nil {
		opts.RequestedBy = u.ID
	}

	result, err := h.executor.ApplyBatchSagaWithOptions(r.Context(), ontologyRID, body.Steps, opts)
	if err != nil {
		// Write the SagaResult body alongside the structured error so SDK
		// callers see the per-step compensations and DLQ ids even on
		// failure. The HTTP status follows the BatchError envelope.
		apiErr := asBatchError(err)
		writeSagaErrorResponse(w, apiErr.StatusCode, result, apiErr)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// writeSagaErrorResponse mirrors the apierror.WriteJSON structure but
// embeds the SagaResult so SDK callers can recover compensations / DLQ
// ids even when the primary saga branch failed.
func writeSagaErrorResponse(w http.ResponseWriter, status int, result *SagaResult, apiErr *apierror.APIError) {
	body := struct {
		ErrorCode       string            `json:"errorCode"`
		ErrorName       string            `json:"errorName"`
		ErrorInstanceID string            `json:"errorInstanceId"`
		Parameters      map[string]string `json:"parameters"`
		Saga            *SagaResult       `json:"saga,omitempty"`
	}{
		ErrorCode:       apiErr.ErrorCode,
		ErrorName:       apiErr.ErrorName,
		ErrorInstanceID: apiErr.ErrorInstanceID,
		Parameters:      apiErr.Parameters,
		Saga:            result,
	}
	httputil.WriteJSON(w, status, body)
}

// ListSagaDLQ handles GET
// /api/v2/ontologies/{ontologyApiName}/actions/saga/dlq (US-369). Returns
// PENDING DLQ rows (default) or a status-filtered view via
// ?status=PENDING|RESOLVED|DROPPED.
func (h *Handler) ListSagaDLQ(w http.ResponseWriter, r *http.Request) {
	store := h.executor.SagaStore()
	if store == nil {
		httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"entries": []any{}})
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = SagaDLQStatusPending
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	entries, err := store.ListDLQ(r.Context(), status, limit)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SagaDLQListFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

// RetrySagaDLQ handles POST
// /api/v2/ontologies/{ontologyApiName}/actions/saga/dlq/{dlqId}/retry. The
// PENDING entry's edits_json is published as a fresh EditBatch; on
// success the DLQ row is transitioned to RESOLVED.
func (h *Handler) RetrySagaDLQ(w http.ResponseWriter, r *http.Request) {
	store := h.executor.SagaStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("SagaStoreNotConfigured", nil))
		return
	}
	dlqID := chi.URLParam(r, "dlqId")
	if dlqID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingDLQID", nil))
		return
	}
	entries, err := store.ListDLQ(r.Context(), "", 1000)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SagaDLQListFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	var entry *SagaDLQEntry
	for _, e := range entries {
		if e.DLQID == dlqID {
			entry = e
			break
		}
	}
	if entry == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("SagaDLQNotFound",
			map[string]string{"dlqId": dlqID}))
		return
	}
	if entry.Status != SagaDLQStatusPending {
		apierror.WriteJSON(w, apierror.NewConflict("SagaDLQNotPending",
			map[string]string{"dlqId": dlqID, "status": entry.Status}))
		return
	}

	if err := h.executor.RetrySagaDLQ(r.Context(), entry); err != nil {
		// Bump attempts + record failure; status stays PENDING.
		attempts := entry.Attempts + 1
		msg := err.Error()
		_ = store.UpdateDLQStatus(r.Context(), dlqID, SagaDLQUpdate{
			Attempts:       &attempts,
			FailureMessage: &msg,
		})
		apierror.WriteJSON(w, apierror.NewInternal("SagaDLQRetryFailed",
			map[string]string{"dlqId": dlqID, "error": err.Error()}))
		return
	}

	if err := store.UpdateDLQStatus(r.Context(), dlqID, SagaDLQUpdate{Status: SagaDLQStatusResolved}); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SagaDLQUpdateFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"dlqId":  dlqID,
		"status": SagaDLQStatusResolved,
	})
}

// DropSagaDLQ handles POST
// /api/v2/ontologies/{ontologyApiName}/actions/saga/dlq/{dlqId}/drop —
// the operator-dismissal path when retry is not appropriate.
func (h *Handler) DropSagaDLQ(w http.ResponseWriter, r *http.Request) {
	store := h.executor.SagaStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("SagaStoreNotConfigured", nil))
		return
	}
	dlqID := chi.URLParam(r, "dlqId")
	if dlqID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingDLQID", nil))
		return
	}
	if err := store.UpdateDLQStatus(r.Context(), dlqID, SagaDLQUpdate{Status: SagaDLQStatusDropped}); err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SagaDLQNotFound",
				map[string]string{"dlqId": dlqID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SagaDLQUpdateFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"dlqId":  dlqID,
		"status": SagaDLQStatusDropped,
	})
}
