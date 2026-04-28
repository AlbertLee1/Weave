package comments

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/v2/comments/* CRUD endpoints (US-334).
//
//	GET    /api/v2/comments?targetRid=&parentId=&limit=&offset=
//	POST   /api/v2/comments
//	GET    /api/v2/comments/{id}
//	PUT    /api/v2/comments/{id}
//	DELETE /api/v2/comments/{id}    (soft-delete)
//
// Read endpoints are open to any authenticated user — comments are not
// per-user private (cf savedsearches). Mutation endpoints (PUT/DELETE)
// are gated on Author == caller.UserID; cross-user attempts get a 403.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting CommentsUnavailable so degraded-mode test routers (no PG)
// can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/comments", h.List)
	r.Post("/api/v2/comments", h.Create)
	r.Get("/api/v2/comments/{id}", h.Get)
	r.Put("/api/v2/comments/{id}", h.Update)
	r.Delete("/api/v2/comments/{id}", h.Delete)
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
		apierror.WriteJSON(w, apierror.NewInternal("CommentsUnavailable", map[string]string{
			"reason": "comments are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	TargetRID string `json:"targetRid"`
	Body      string `json:"body"`
	ParentID  string `json:"parentId,omitempty"`
}

type updateRequest struct {
	Body *string `json:"body,omitempty"`
}

type listResponse struct {
	Comments []*Comment `json:"comments"`
	Total    int        `json:"total"`
	Limit    int        `json:"limit"`
	Offset   int        `json:"offset"`
}

// Create POST /api/v2/comments.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommentTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": req.TargetRID,
		}))
		return
	}
	if err := ValidateBody(req.Body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommentBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	now := time.Now().UTC()
	row := &Comment{
		ID:        newCommentID(),
		TargetRID: req.TargetRID,
		Body:      strings.TrimRight(req.Body, "\n"),
		Author:    user.ID,
		ParentID:  strings.TrimSpace(req.ParentID),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		if errors.Is(err, ErrInvalidParent) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommentParent", map[string]string{
				"parentId":  req.ParentID,
				"targetRid": req.TargetRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CommentCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.Get(r.Context(), row.ID)
	if err != nil {
		stored = row
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// List GET /api/v2/comments?targetRid=&parentId=&limit=&offset=.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	q := r.URL.Query()
	targetRID := q.Get("targetRid")
	if err := ValidateTargetRID(targetRID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommentTarget", map[string]string{
			"reason":    err.Error(),
			"targetRid": targetRID,
		}))
		return
	}
	limit := DefaultPageLimit
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	offset := 0
	if raw := q.Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	rows, total, err := h.store.List(r.Context(), ListQuery{
		TargetRID: targetRID,
		ParentID:  q.Get("parentId"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CommentListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Comment{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{
		Comments: rows,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// Get GET /api/v2/comments/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	row, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CommentNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CommentLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Update PUT /api/v2/comments/{id}. Only the author may edit.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := Update{}
	if req.Body != nil {
		if err := ValidateBody(*req.Body); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCommentBody", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		body := strings.TrimRight(*req.Body, "\n")
		upd.Body = &body
	}
	if err := h.store.Update(r.Context(), id, user.ID, upd); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CommentNotFound", map[string]string{"id": id}))
			return
		}
		if errors.Is(err, ErrForbidden) {
			apierror.WriteJSON(w, apierror.NewPermissionDenied("CommentForbidden", map[string]string{
				"id":     id,
				"reason": "only the author can edit this comment",
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CommentUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	row, err := h.store.Get(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CommentLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Delete DELETE /api/v2/comments/{id}. Only the author may delete; the
// row is soft-deleted (Body redacted, DeletedAt set) so reply chains
// keep their parent reference.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, user.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CommentNotFound", map[string]string{"id": id}))
			return
		}
		if errors.Is(err, ErrForbidden) {
			apierror.WriteJSON(w, apierror.NewPermissionDenied("CommentForbidden", map[string]string{
				"id":     id,
				"reason": "only the author can delete this comment",
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CommentDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newCommentID returns a uuid-shaped identifier for a new comment row.
// Mirrors savedsearches.newSavedSearchID — RFC4122 v4 layout via crypto/rand.
func newCommentID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
