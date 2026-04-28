package watches

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/v2/watches/* endpoints (US-337).
//
//	GET    /api/v2/watches                          — list caller's watches
//	GET    /api/v2/watches/status?targetRid=…       — single boolean probe
//	POST   /api/v2/watches                          — body {targetRid}
//	DELETE /api/v2/watches?targetRid=…              — unwatch
//
// Every endpoint is scoped to the authenticated user — watches are
// strictly private. Cross-user reads are not exposed; the only way to
// observe another user's follow set is the activity-fanout consumer
// (US-338) which loads it server-side.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting WatchesUnavailable so degraded-mode test routers (no PG)
// can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers. Note: the /status route must be registered
// BEFORE the catch-all DELETE so chi dispatches the static segment
// first.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/watches", h.List)
	r.Get("/api/v2/watches/status", h.Status)
	r.Post("/api/v2/watches", h.Create)
	r.Delete("/api/v2/watches", h.Delete)
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) *auth.User {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return nil
	}
	return user
}

func (h *Handler) requireStore(w http.ResponseWriter) bool {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("WatchesUnavailable", map[string]string{
			"reason": "watches are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	TargetRID string `json:"targetRid"`
}

type listResponse struct {
	Watches []*Watch `json:"watches"`
}

type statusResponse struct {
	TargetRID string `json:"targetRid"`
	Watching  bool   `json:"watching"`
}

// Create POST /api/v2/watches. Idempotent — calling twice on the same
// target returns the same row.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := ValidateTargetRID(req.TargetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidWatchTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": req.TargetRID,
		}))
		return
	}
	row := &Watch{
		ID:        newWatchID(),
		UserID:    user.ID,
		TargetRID: req.TargetRID,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("WatchCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, row)
}

// List GET /api/v2/watches.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	rows, err := h.store.List(r.Context(), user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("WatchListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Watch{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{Watches: rows})
}

// Status GET /api/v2/watches/status?targetRid=… — probe one row.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	targetRID := r.URL.Query().Get("targetRid")
	if err := ValidateTargetRID(targetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidWatchTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": targetRID,
		}))
		return
	}
	watching, err := h.store.IsWatching(r.Context(), user.ID, targetRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("WatchLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, statusResponse{TargetRID: targetRID, Watching: watching})
}

// Delete DELETE /api/v2/watches?targetRid=…
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	targetRID := r.URL.Query().Get("targetRid")
	if err := ValidateTargetRID(targetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidWatchTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": targetRID,
		}))
		return
	}
	if err := h.store.Delete(r.Context(), user.ID, targetRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("WatchNotFound", map[string]string{
				"targetRid": targetRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("WatchDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newWatchID returns a uuid-shaped identifier for a new watch row.
// Mirrors comments.newCommentID — RFC4122 v4 layout via crypto/rand.
func newWatchID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
