package dashboards

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/v2/dashboards/* CRUD endpoints (US-329).
//
//	GET    /api/v2/dashboards
//	POST   /api/v2/dashboards
//	GET    /api/v2/dashboards/{id}
//	PUT    /api/v2/dashboards/{id}
//	DELETE /api/v2/dashboards/{id}
//
// Reads (GET single) succeed across users when the row is marked
// IsPublic — that's how share links work. List + mutating routes stay
// owner-scoped.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting DashboardsUnavailable so degraded-mode test routers
// (no PG) can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/dashboards", h.List)
	r.Post("/api/v2/dashboards", h.Create)
	r.Get("/api/v2/dashboards/{id}", h.Get)
	r.Put("/api/v2/dashboards/{id}", h.Update)
	r.Delete("/api/v2/dashboards/{id}", h.Delete)
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
		apierror.WriteJSON(w, apierror.NewInternal("DashboardsUnavailable", map[string]string{
			"reason": "dashboards are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
	IsPublic   bool            `json:"isPublic"`
}

type updateRequest struct {
	Name       *string          `json:"name,omitempty"`
	Definition *json.RawMessage `json:"definition,omitempty"`
	IsPublic   *bool            `json:"isPublic,omitempty"`
}

type listResponse struct {
	Dashboards []*Dashboard `json:"dashboards"`
}

// Create POST /api/v2/dashboards.
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
	name := strings.TrimSpace(req.Name)
	if err := ValidateName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidDashboardName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}
	def := req.Definition
	if len(def) == 0 {
		def = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	row := &Dashboard{
		ID:         newDashboardID(),
		Name:       name,
		CreatedBy:  user.ID,
		IsPublic:   req.IsPublic,
		Definition: def,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		if errors.Is(err, ErrNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("DashboardNameConflict", map[string]string{
				"name": name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DashboardCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.Get(r.Context(), row.ID, user.ID)
	if err != nil {
		stored = row
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// List GET /api/v2/dashboards — returns every dashboard the caller
// owns. Public dashboards owned by other users are NOT included; share
// links convey the id explicitly.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	rows, err := h.store.List(r.Context(), user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DashboardListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Dashboard{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{Dashboards: rows})
}

// Get GET /api/v2/dashboards/{id}. Public dashboards are readable by
// any authenticated caller; private rows return 404 to non-owners.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	row, err := h.store.Get(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DashboardNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DashboardLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Update PUT /api/v2/dashboards/{id}. Owner only — non-owners receive
// 404 even when the dashboard is public.
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
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if err := ValidateName(trimmed); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidDashboardName", map[string]string{
				"reason": err.Error(),
				"name":   *req.Name,
			}))
			return
		}
		upd.Name = &trimmed
	}
	if req.Definition != nil {
		upd.Definition = req.Definition
	}
	if req.IsPublic != nil {
		upd.IsPublic = req.IsPublic
	}
	if err := h.store.Update(r.Context(), id, user.ID, upd); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DashboardNotFound", map[string]string{"id": id}))
			return
		}
		if errors.Is(err, ErrNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("DashboardNameConflict", map[string]string{
				"name": *upd.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DashboardUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	row, err := h.store.Get(r.Context(), id, user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DashboardLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Delete DELETE /api/v2/dashboards/{id}. Owner only.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, user.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DashboardNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DashboardDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newDashboardID returns a uuid-shaped identifier for a new row.
// Mirrors savedsearches.newSavedSearchID — same byte-level RFC4122
// shape so the column accepts the value as a UUID without pulling in
// google/uuid.
func newDashboardID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
