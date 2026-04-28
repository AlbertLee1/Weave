// US-317: Action 执行历史. The handler returns paginated ActionLog rows
// scoped to a single ontology, with optional filters (actionType / status /
// userId / since / until) for the /actions/history UI page.
//
// The detail endpoint serves a single row with parameters / edits /
// prevEdits intact so the front-end can render full input + result audit
// without a second query.
package actions

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// ActionLogStore is the narrow read-side interface the History handlers
// require. Implemented directly by *oms.PGRepository — kept separate from
// oms.Repository so the ~15 in-memory mocks scattered across the test tree
// do not need new stub methods (Codebase Patterns #18).
type ActionLogStore interface {
	ListActionLogsByOntology(ctx context.Context, ontologyRID string, q oms.ActionLogQuery) ([]oms.ActionLog, error)
	CountActionLogsByOntology(ctx context.Context, ontologyRID string, q oms.ActionLogQuery) (int, error)
	GetActionLogByOntology(ctx context.Context, ontologyRID string, id int64) (*oms.ActionLog, error)
}

// ActionHistoryResponse is the wire shape returned by GET .../actions/history.
type ActionHistoryResponse struct {
	Data       []oms.ActionLog `json:"data"`
	Total      int             `json:"total"`
	Limit      int             `json:"limit"`
	Offset     int             `json:"offset"`
	NextOffset *int            `json:"nextOffset,omitempty"`
}

// ListHistory handles GET /api/v2/ontologies/{ontologyApiName}/actions/history.
//
// Query parameters (all optional):
//   - actionType: ActionType apiName — resolved to RID via ListActionTypes.
//     Unknown names return an empty page rather than 404 so the UI's filter
//     dropdown can render "no results" without a separate failure path.
//   - status: SUCCESS | FAILED (case-insensitive). Anything else 400.
//   - userId: exact match on action_logs.user_id.
//   - since / until: RFC3339 timestamps. Half-open: since INCLUSIVE, until
//     EXCLUSIVE — matches the [valid_from, valid_to) convention used by
//     pkg/oms time-travel rows.
//   - limit: 1..500, default 50.
//   - offset: ≥0, default 0.
//
// Degraded mode: when no ActionLogStore is wired the endpoint returns an
// empty page rather than 500, matching the GetJob / ListApprovals contracts.
func (h *Handler) ListHistory(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	q := r.URL.Query()

	store := h.executor.ActionLogStore()
	if store == nil {
		httputil.WriteJSON(w, http.StatusOK, &ActionHistoryResponse{
			Data: []oms.ActionLog{},
		})
		return
	}

	filter := oms.ActionLogQuery{}
	if s := strings.TrimSpace(q.Get("status")); s != "" {
		switch strings.ToUpper(s) {
		case "SUCCESS", "FAILED":
			filter.Status = strings.ToUpper(s)
		default:
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidStatus",
				map[string]string{"status": s, "allowed": "SUCCESS, FAILED"}))
			return
		}
	}
	if u := strings.TrimSpace(q.Get("userId")); u != "" {
		filter.UserID = u
	}
	if v := strings.TrimSpace(q.Get("since")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidSince",
				map[string]string{"since": v, "expected": "RFC3339"}))
			return
		}
		filter.Since = t
	}
	if v := strings.TrimSpace(q.Get("until")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidUntil",
				map[string]string{"until": v, "expected": "RFC3339"}))
			return
		}
		filter.Until = t
	}
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > oms.MaxActionHistoryLimit {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLimit",
				map[string]string{"limit": v, "allowed": "1..500"}))
			return
		}
		filter.Limit = n
	}
	if v := strings.TrimSpace(q.Get("offset")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidOffset",
				map[string]string{"offset": v}))
			return
		}
		filter.Offset = n
	}

	// Translate actionType apiName → action_type_rid via the executor's repo
	// (already used by ResolveActionType). When the apiName matches no action
	// type return an empty page — the Filter dropdown can show "no results"
	// without distinguishing between "wrong name" and "no executions yet".
	if name := strings.TrimSpace(q.Get("actionType")); name != "" {
		at, err := h.executor.ResolveActionType(r.Context(), ontologyRID, name)
		if err != nil || at == nil {
			httputil.WriteJSON(w, http.StatusOK, &ActionHistoryResponse{
				Data:   []oms.ActionLog{},
				Limit:  oms.DefaultActionHistoryLimit,
				Offset: filter.Offset,
			})
			return
		}
		filter.ActionTypeRID = at.RID
	}

	logs, err := store.ListActionLogsByOntology(r.Context(), ontologyRID, filter)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionHistoryListFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	total, err := store.CountActionLogsByOntology(r.Context(), ontologyRID, filter)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionHistoryCountFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	resp := &ActionHistoryResponse{
		Data:   logs,
		Total:  total,
		Limit:  effectiveLimit(filter.Limit),
		Offset: filter.Offset,
	}
	if logs == nil {
		resp.Data = []oms.ActionLog{}
	}
	if filter.Offset+len(logs) < total {
		next := filter.Offset + len(logs)
		resp.NextOffset = &next
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// GetHistoryEntry handles GET .../actions/history/{logId} and returns a
// single ActionLog row — including PrevEdits + parameters + edits — so the
// detail panel can render "what was submitted" / "what was applied" / "what
// was the prior state" without paging the whole list.
func (h *Handler) GetHistoryEntry(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	raw := strings.TrimSpace(chi.URLParam(r, "logId"))
	if raw == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingLogId", nil))
		return
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidLogId",
			map[string]string{"logId": raw}))
		return
	}
	store := h.executor.ActionLogStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ActionLogNotFound",
			map[string]string{"logId": raw}))
		return
	}
	log, err := store.GetActionLogByOntology(r.Context(), ontologyRID, id)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionLogNotFound",
				map[string]string{"logId": raw}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionHistoryGetFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, log)
}

// effectiveLimit mirrors the clamp logic in pkg/oms.clampActionHistoryLimit
// so the wire response advertises the limit the store actually applied (not
// the raw request value). Keeps the SDK's "if (resp.limit < requested) {…}"
// path honest.
func effectiveLimit(n int) int {
	if n <= 0 {
		return oms.DefaultActionHistoryLimit
	}
	if n > oms.MaxActionHistoryLimit {
		return oms.MaxActionHistoryLimit
	}
	return n
}
