package savedsearches

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

// Handler implements the /api/v2/saved-searches/* CRUD endpoints.
//
//	GET    /api/v2/saved-searches?ontology=&objectType=
//	POST   /api/v2/saved-searches
//	GET    /api/v2/saved-searches/{id}
//	PUT    /api/v2/saved-searches/{id}
//	DELETE /api/v2/saved-searches/{id}
//
// Every endpoint is scoped to the authenticated user — saved searches
// are private to their creator. Cross-user lookups surface as 404
// SavedSearchNotFound to avoid leaking ids.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting SavedSearchesUnavailable so degraded-mode test routers
// (no PG) can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/saved-searches", h.List)
	r.Post("/api/v2/saved-searches", h.Create)
	r.Get("/api/v2/saved-searches/{id}", h.Get)
	r.Put("/api/v2/saved-searches/{id}", h.Update)
	r.Delete("/api/v2/saved-searches/{id}", h.Delete)
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
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchesUnavailable", map[string]string{
			"reason": "saved searches are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	Name       string          `json:"name"`
	Ontology   string          `json:"ontology"`
	ObjectType string          `json:"objectType"`
	Definition json.RawMessage `json:"definition"`
}

type updateRequest struct {
	Name       *string          `json:"name,omitempty"`
	Definition *json.RawMessage `json:"definition,omitempty"`
}

type listResponse struct {
	SavedSearches []*SavedSearch `json:"savedSearches"`
}

// Create POST /api/v2/saved-searches.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidSavedSearchName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}
	if err := ValidateScope(req.Ontology, req.ObjectType); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidSavedSearchScope", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	def := req.Definition
	if len(def) == 0 {
		def = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	row := &SavedSearch{
		ID:         newSavedSearchID(),
		Name:       name,
		Ontology:   req.Ontology,
		ObjectType: req.ObjectType,
		CreatedBy:  user.ID,
		Definition: def,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		if errors.Is(err, ErrNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("SavedSearchNameConflict", map[string]string{
				"name": name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchCreateFailed", map[string]string{
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

// List GET /api/v2/saved-searches?ontology=&objectType= — returns the
// caller's rows. Empty ontology / objectType returns every saved
// search owned by the caller.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	ontology := r.URL.Query().Get("ontology")
	objectType := r.URL.Query().Get("objectType")
	rows, err := h.store.List(r.Context(), user.ID, ontology, objectType)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*SavedSearch{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{SavedSearches: rows})
}

// Get GET /api/v2/saved-searches/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	row, err := h.store.Get(r.Context(), id, user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SavedSearchNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Update PUT /api/v2/saved-searches/{id}.
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
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidSavedSearchName", map[string]string{
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
	if err := h.store.Update(r.Context(), id, user.ID, upd); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SavedSearchNotFound", map[string]string{"id": id}))
			return
		}
		if errors.Is(err, ErrNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("SavedSearchNameConflict", map[string]string{
				"name": *upd.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	row, err := h.store.Get(r.Context(), id, user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Delete DELETE /api/v2/saved-searches/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, user.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SavedSearchNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("SavedSearchDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newSavedSearchID returns a uuid-shaped identifier for a new row.
// Avoids pulling in google/uuid for one call site — the format mirrors
// other private id helpers in the repo (aip.newThreadID).
func newSavedSearchID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	// RFC4122 variant + version-4 marker so the column accepts the
	// value as a UUID even though we're hex-encoding manually.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
