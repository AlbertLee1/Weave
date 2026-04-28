package reactions

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

// Handler implements the /api/v2/reactions/* endpoints (US-342).
//
//	GET    /api/v2/reactions?targetRid=…    — aggregate counts + mine flag
//	POST   /api/v2/reactions                — body {targetRid, emoji}
//	DELETE /api/v2/reactions?targetRid=&emoji=
//
// The aggregate endpoint is the SPA's render path for the ReactionBar;
// POST / DELETE drive the toggle. Auth is required for all three
// because Mine relies on the caller's identity even on the read path.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting ReactionsUnavailable so degraded-mode test routers (no PG)
// can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/reactions", h.Aggregate)
	r.Post("/api/v2/reactions", h.Create)
	r.Delete("/api/v2/reactions", h.Delete)
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
		apierror.WriteJSON(w, apierror.NewInternal("ReactionsUnavailable", map[string]string{
			"reason": "reactions are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	TargetRID string `json:"targetRid"`
	Emoji     string `json:"emoji"`
}

// Aggregate GET /api/v2/reactions?targetRid=…
func (h *Handler) Aggregate(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	targetRID := r.URL.Query().Get("targetRid")
	if err := ValidateTargetRID(targetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReactionTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": targetRID,
		}))
		return
	}
	buckets, err := h.store.AggregateForTarget(r.Context(), user.ID, targetRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ReactionAggregateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if buckets == nil {
		buckets = []EmojiCount{}
	}
	httputil.WriteJSON(w, http.StatusOK, Summary{TargetRID: targetRID, Emojis: buckets})
}

// Create POST /api/v2/reactions. Idempotent — calling twice with the
// same (target, emoji) returns the same row.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReactionTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": req.TargetRID,
		}))
		return
	}
	if err := ValidateEmoji(req.Emoji); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReactionEmoji", map[string]string{
			"reason": err.Error(),
			"emoji":  req.Emoji,
		}))
		return
	}
	row := &Reaction{
		ID:        newReactionID(),
		UserID:    user.ID,
		TargetRID: req.TargetRID,
		Emoji:     req.Emoji,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ReactionCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, row)
}

// Delete DELETE /api/v2/reactions?targetRid=&emoji=
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	targetRID := r.URL.Query().Get("targetRid")
	emoji := r.URL.Query().Get("emoji")
	if err := ValidateTargetRID(targetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReactionTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": targetRID,
		}))
		return
	}
	if err := ValidateEmoji(emoji); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidReactionEmoji", map[string]string{
			"reason": err.Error(),
			"emoji":  emoji,
		}))
		return
	}
	if err := h.store.Delete(r.Context(), user.ID, targetRID, emoji); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ReactionNotFound", map[string]string{
				"targetRid": targetRID,
				"emoji":     emoji,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ReactionDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newReactionID returns a uuid-shaped identifier for a new reaction
// row. Mirrors watches.newWatchID — RFC4122 v4 layout via crypto/rand.
func newReactionID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
