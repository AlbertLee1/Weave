package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/oms"
)

// PRD-V2 Gap-A4 round 34 admin endpoints expose the action_log_side_
// effect_dlq table (round 33) to operators for inspection + dismissal.
// Two routes:
//
//   - GET  /api/admin/side-effect-dlq            — list pending entries
//   - POST /api/admin/side-effect-dlq/{id}/abandon
//                                                — flip to abandoned
//
// Replay is intentionally NOT in scope for this round — re-dispatching
// a failed webhook needs payload reconstruction from the action_logs
// row (Edits, etc.) and careful handling of the case where the
// ActionType was edited or deleted between the original failure and
// the manual replay. Deferred to a follow-up round.
//
// Both routes are gated behind PermUserManage at the router level so
// only admin-level principals can call them. Degraded-mode bootstraps
// without a wired Repo leave deps.Repo nil and each handler short-
// circuits to 503 SideEffectDLQNotConfigured.

// AdminSideEffectDLQDeps captures the OMS read+write surface the admin
// side-effect DLQ routes require. Repo nil => 503 on every route.
type AdminSideEffectDLQDeps struct {
	Repo SideEffectDLQRepo
}

// SideEffectDLQRepo is the narrow subset of oms.Repository the admin
// surface depends on. Carved out so degraded-mode test routers can
// plug a minimal fake without dragging in the full Repository.
type SideEffectDLQRepo interface {
	ListPendingSideEffectDLQRows(ctx context.Context, limit int) ([]oms.SideEffectDLQRow, error)
	MarkSideEffectDLQAbandoned(ctx context.Context, id int64) error
}

// adminSideEffectDLQListResponse is the wire shape for GET
// /api/admin/side-effect-dlq.
type adminSideEffectDLQListResponse struct {
	Entries []oms.SideEffectDLQRow `json:"entries"`
}

// adminSideEffectDLQAbandonResponse is the wire shape for POST
// .../{id}/abandon.
type adminSideEffectDLQAbandonResponse struct {
	ID        int64  `json:"id"`
	Abandoned bool   `json:"abandoned"`
	Status    string `json:"status"`
}

// defaultSideEffectDLQListLimit caps the per-call list to 100 when the
// caller omits `?limit=`. Hard cap below prevents pathological scans.
const (
	defaultSideEffectDLQListLimit = 100
	maxSideEffectDLQListLimit     = 1000
)

// NewAdminSideEffectDLQListHandler returns the GET
// /api/admin/side-effect-dlq handler. Returns pending rows ordered
// newest-first.
func NewAdminSideEffectDLQListHandler(deps AdminSideEffectDLQDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Repo == nil {
			writeUnavailable(w, "SideEffectDLQNotConfigured")
			return
		}
		limit := defaultSideEffectDLQListLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:limit", map[string]string{
					"parameter": "limit",
					"reason":    "limit must be a positive integer",
				}))
				return
			}
			if n > maxSideEffectDLQListLimit {
				n = maxSideEffectDLQListLimit
			}
			limit = n
		}

		entries, err := deps.Repo.ListPendingSideEffectDLQRows(r.Context(), limit)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("SideEffectDLQListFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		if entries == nil {
			entries = []oms.SideEffectDLQRow{}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(adminSideEffectDLQListResponse{Entries: entries})
	})
}

// NewAdminSideEffectDLQAbandonHandler returns the POST
// /api/admin/side-effect-dlq/{id}/abandon handler. Idempotent on rows
// already in 'abandoned' status. Returns 409 Conflict when the row is
// already 'replayed' (can't abandon a successfully replayed row —
// would mask the dispatch).
func NewAdminSideEffectDLQAbandonHandler(deps AdminSideEffectDLQDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Repo == nil {
			writeUnavailable(w, "SideEffectDLQNotConfigured")
			return
		}
		raw := chi.URLParam(r, "id")
		if raw == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:id", map[string]string{
				"parameter": "id",
				"reason":    "id is required",
			}))
			return
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:id", map[string]string{
				"parameter": "id",
				"reason":    "id must be a positive integer",
			}))
			return
		}
		if err := deps.Repo.MarkSideEffectDLQAbandoned(r.Context(), id); err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("SideEffectDLQEntryNotFound", map[string]string{
					"id": raw,
				}))
				return
			}
			if errors.Is(err, oms.ErrInvalidState) {
				apierror.WriteJSON(w, apierror.NewConflict("SideEffectDLQCannotAbandonReplayed", map[string]string{
					"id":     raw,
					"reason": "row is in 'replayed' status; can't abandon a row that already replayed successfully",
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("SideEffectDLQAbandonFailed", map[string]string{
				"id":     raw,
				"reason": err.Error(),
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(adminSideEffectDLQAbandonResponse{
			ID:        id,
			Abandoned: true,
			Status:    oms.SideEffectDLQStatusAbandoned,
		})
	})
}
