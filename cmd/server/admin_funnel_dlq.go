package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/metrics"
)

// US-470 admin endpoints expose the funnel DLQ (OBJECT_EDITS_DLQ JetStream
// stream) to operators for inspection + replay. Three routes:
//   - GET  /api/admin/funnel/dlq                   — list pending entries
//   - POST /api/admin/funnel/dlq/{id}/replay       — republish + delete
//   - POST /api/admin/funnel/dlq/{id}/discard      — delete without replay
//
// All three are gated behind PermUserManage at the router level so only
// admin-level principals can call them. Degraded-mode bootstraps without
// NATS leave deps.Reader nil and the routes still mount, but return 503
// FunnelDLQNotConfigured so the OpenAPI surface stays consistent.

// AdminFunnelDLQDeps captures the read + replay capability the admin DLQ
// routes require. Reader nil => 503 list/replay/discard. Publish nil =>
// 503 replay only (list + discard still work).
type AdminFunnelDLQDeps struct {
	Reader  funnel.DLQReader
	Publish funnel.DLQPublishFunc
}

// adminFunnelDLQListResponse is the wire shape for GET /api/admin/funnel/dlq.
// Size is the authoritative DLQ depth (may exceed len(entries) when limit
// caps the page).
type adminFunnelDLQListResponse struct {
	Entries []funnel.DLQEntry `json:"entries"`
	Size    int64             `json:"size"`
}

// adminFunnelDLQReplayResponse is the wire shape for the replay route.
type adminFunnelDLQReplayResponse struct {
	ID              string `json:"id"`
	OriginalSubject string `json:"originalSubject"`
}

// adminFunnelDLQDiscardResponse is the wire shape for the discard route.
type adminFunnelDLQDiscardResponse struct {
	ID      string `json:"id"`
	Dropped bool   `json:"dropped"`
}

// defaultFunnelDLQListLimit caps the per-call list to 100 entries when the
// caller omits `?limit=`. The hard cap below (1000) prevents pathological
// scans of huge stream tails.
const (
	defaultFunnelDLQListLimit = 100
	maxFunnelDLQListLimit     = 1000
)

func writeUnavailable(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"errorCode": "SERVICE_UNAVAILABLE",
		"errorName": name,
	})
}

// NewAdminFunnelDLQListHandler returns the GET /api/admin/funnel/dlq handler.
func NewAdminFunnelDLQListHandler(deps AdminFunnelDLQDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Reader == nil {
			writeUnavailable(w, "FunnelDLQNotConfigured")
			return
		}
		limit := defaultFunnelDLQListLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:limit", map[string]string{
					"parameter": "limit",
					"reason":    "limit must be a positive integer",
				}))
				return
			}
			if n > maxFunnelDLQListLimit {
				n = maxFunnelDLQListLimit
			}
			limit = n
		}

		entries, err := deps.Reader.ListPending(r.Context(), limit)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("FunnelDLQListFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		size, err := deps.Reader.Size(r.Context())
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("FunnelDLQSizeFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		// Push a fresh gauge observation so /metrics stays in sync without
		// waiting for the next poll tick — the admin UI typically reads
		// the gauge after listing.
		metrics.SetFunnelDLQSize(float64(size))

		if entries == nil {
			entries = []funnel.DLQEntry{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(adminFunnelDLQListResponse{
			Entries: entries,
			Size:    size,
		})
	})
}

// NewAdminFunnelDLQReplayHandler returns the POST
// /api/admin/funnel/dlq/{id}/replay handler.
func NewAdminFunnelDLQReplayHandler(deps AdminFunnelDLQDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Reader == nil {
			writeUnavailable(w, "FunnelDLQNotConfigured")
			return
		}
		if deps.Publish == nil {
			writeUnavailable(w, "FunnelDLQPublisherNotConfigured")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:id", map[string]string{
				"parameter": "id",
				"reason":    "id is required",
			}))
			return
		}

		subject, err := funnel.ReplayDLQEntry(r.Context(), deps.Reader, id, deps.Publish)
		if err != nil {
			if errors.Is(err, funnel.ErrDLQEntryNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("FunnelDLQEntryNotFound", map[string]string{
					"id": id,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("FunnelDLQReplayFailed", map[string]string{
				"id":     id,
				"reason": err.Error(),
			}))
			return
		}

		// Refresh the gauge so dashboards reflect the post-replay decrement.
		if size, sizeErr := deps.Reader.Size(r.Context()); sizeErr == nil {
			metrics.SetFunnelDLQSize(float64(size))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(adminFunnelDLQReplayResponse{
			ID:              id,
			OriginalSubject: subject,
		})
	})
}

// NewAdminFunnelDLQDiscardHandler returns the POST
// /api/admin/funnel/dlq/{id}/discard handler. Discard is the "operator
// dismiss" path — the row is dropped from the DLQ stream without
// republishing the payload.
func NewAdminFunnelDLQDiscardHandler(deps AdminFunnelDLQDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.Reader == nil {
			writeUnavailable(w, "FunnelDLQNotConfigured")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:id", map[string]string{
				"parameter": "id",
				"reason":    "id is required",
			}))
			return
		}
		if err := deps.Reader.DeleteByID(r.Context(), id); err != nil {
			if errors.Is(err, funnel.ErrDLQEntryNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("FunnelDLQEntryNotFound", map[string]string{
					"id": id,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("FunnelDLQDiscardFailed", map[string]string{
				"id":     id,
				"reason": err.Error(),
			}))
			return
		}
		if size, sizeErr := deps.Reader.Size(r.Context()); sizeErr == nil {
			metrics.SetFunnelDLQSize(float64(size))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(adminFunnelDLQDiscardResponse{
			ID:      id,
			Dropped: true,
		})
	})
}

