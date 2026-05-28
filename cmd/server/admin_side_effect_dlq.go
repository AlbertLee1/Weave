package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/actions"
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
	GetSideEffectDLQRow(ctx context.Context, id int64) (*oms.SideEffectDLQRow, error)
	UpdateSideEffectDLQAfterReplay(ctx context.Context, id int64, outcome json.RawMessage, success bool) error
	GetActionLog(ctx context.Context, id int64) (*oms.ActionLog, error)
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

// adminSideEffectDLQReplayResponse is the wire shape for POST
// .../{id}/replay. Carries the per-attempt outcome plus the final
// row status so the admin UI can render "succeeded on 1st replay"
// or "still failing — 3rd replay also gave up".
type adminSideEffectDLQReplayResponse struct {
	ID          int64                     `json:"id"`
	Replayed    bool                      `json:"replayed"`
	Status      string                    `json:"status"`
	ReplayCount int                       `json:"replayCount"`
	Outcome     actions.SideEffectOutcome `json:"outcome"`
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

// NewAdminSideEffectDLQReplayHandler returns the POST
// /api/admin/side-effect-dlq/{id}/replay handler (Gap-A4 round 35).
//
// Flow:
//  1. Load the DLQ row (404 if missing).
//  2. Reject if status != 'pending' (409 — replay is only valid from
//     'pending'; replayed rows must not double-fire, abandoned rows
//     need an explicit un-abandon flow we don't expose here).
//  3. Load the linked action_logs row so we can reconstruct an
//     ActionResult shaped the same way the original dispatch saw —
//     ActionRID + Edits come straight from the row; BatchID is
//     stamped as "replay-<dlq.id>" so receivers can distinguish a
//     replayed payload from the original.
//  4. Call actions.ReplaySideEffect to fire the webhook through the
//     same round-30 retry loop the original dispatch used.
//  5. UpdateSideEffectDLQAfterReplay records the new outcome,
//     bumps replay_count, sets replayed_at, and flips replay_status
//     to 'replayed' on success. On failure replay_status stays
//     'pending' so the operator can try again.
//
// Returns 200 with the outcome regardless of whether the webhook
// itself succeeded — the operator needs to see the per-attempt
// result, which is encoded inside response.outcome.status. A
// non-200 response means the replay machinery itself broke (404,
// 409, 500); HTTP 200 + outcome.status="failed" means "the
// dispatcher ran but the webhook is still broken".
func NewAdminSideEffectDLQReplayHandler(deps AdminSideEffectDLQDeps) http.Handler {
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

		row, err := deps.Repo.GetSideEffectDLQRow(r.Context(), id)
		if err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("SideEffectDLQEntryNotFound", map[string]string{"id": raw}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("SideEffectDLQReplayFailed", map[string]string{
				"id": raw, "reason": err.Error(),
			}))
			return
		}
		if row.ReplayStatus != oms.SideEffectDLQStatusPending {
			apierror.WriteJSON(w, apierror.NewConflict("SideEffectDLQNotReplayable", map[string]string{
				"id":     raw,
				"status": row.ReplayStatus,
				"reason": "replay is only valid from 'pending' status",
			}))
			return
		}

		al, err := deps.Repo.GetActionLog(r.Context(), row.ActionLogID)
		if err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("SideEffectDLQActionLogMissing", map[string]string{
					"id":          raw,
					"actionLogId": strconv.FormatInt(row.ActionLogID, 10),
					"reason":      "linked action_log row not found (deleted?)",
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("SideEffectDLQReplayFailed", map[string]string{
				"id": raw, "reason": err.Error(),
			}))
			return
		}

		// Reconstruct ActionResult from the action_logs row. Edits is
		// the same JSON the original dispatch marshaled into its
		// webhook body. BatchID is stamped "replay-<dlq.id>" so the
		// receiver can tell this is a manual replay (the original
		// BatchID isn't stored).
		var edits interface{}
		if len(al.Edits) > 0 {
			edits = al.Edits
		}
		result := actions.ActionResult{
			ActionRID: al.ActionTypeRID,
			BatchID:   fmt.Sprintf("replay-%d", row.ID),
			Edits:     edits,
		}
		effect := actions.SideEffect{Type: row.EffectType, Config: row.EffectConfig}
		outcome := actions.ReplaySideEffect(effect, result)

		// Persist the outcome regardless of webhook success. On
		// success the row's status flips to 'replayed' (terminal);
		// on failure it stays 'pending' for a future retry.
		outcomeBytes, marshalErr := json.Marshal(outcome)
		if marshalErr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("SideEffectDLQReplayFailed", map[string]string{
				"id": raw, "reason": "marshal outcome: " + marshalErr.Error(),
			}))
			return
		}
		success := outcome.Status == actions.SideEffectStatusSuccess
		if updErr := deps.Repo.UpdateSideEffectDLQAfterReplay(r.Context(), id, outcomeBytes, success); updErr != nil {
			// Don't block on persistence failure — the webhook may have
			// actually succeeded; surface that to the operator.
			apierror.WriteJSON(w, apierror.NewInternal("SideEffectDLQReplayUpdateFailed", map[string]string{
				"id":            raw,
				"webhookStatus": outcome.Status,
				"reason":        updErr.Error(),
			}))
			return
		}

		newStatus := oms.SideEffectDLQStatusPending
		if success {
			newStatus = oms.SideEffectDLQStatusReplayed
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(adminSideEffectDLQReplayResponse{
			ID:          id,
			Replayed:    success,
			Status:      newStatus,
			ReplayCount: row.ReplayCount + 1,
			Outcome:     outcome,
		})
	})
}
