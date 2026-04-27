package featureflags

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/admin/feature-flags/* admin endpoints.
//
//	GET    /api/admin/feature-flags
//	POST   /api/admin/feature-flags
//	GET    /api/admin/feature-flags/{name}
//	PUT    /api/admin/feature-flags/{name}
//	DELETE /api/admin/feature-flags/{name}
//
// The handler is gated on PermUserManage by the surrounding router;
// the auth.UserFromContext nil-check below is defence-in-depth so test
// routers that skip RequirePermission still refuse unauthenticated
// callers. Mirrors pkg/gdpr.Handler and pkg/compliance.Handler.
type Handler struct {
	store Store
}

// NewHandler returns an admin handler reading / writing through store.
// A nil store leaves every endpoint reporting FeatureFlagsUnavailable.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts every admin endpoint on r. Callers should
// wrap the call in auth.RequirePermission(auth.PermUserManage).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/admin/feature-flags", h.List)
	r.Post("/api/admin/feature-flags", h.Create)
	r.Get("/api/admin/feature-flags/{name}", h.Get)
	r.Put("/api/admin/feature-flags/{name}", h.Update)
	r.Delete("/api/admin/feature-flags/{name}", h.Delete)
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if auth.UserFromContext(r.Context()) == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return false
	}
	return true
}

func (h *Handler) requireStore(w http.ResponseWriter) bool {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagsUnavailable", map[string]string{
			"reason": "feature flags are not configured on this deployment",
		}))
		return false
	}
	return true
}

// createFlagRequest is the JSON body for POST. Defaults: empty
// description, Enabled=false, empty scopes.
type createFlagRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Realms      []string `json:"realms"`
	Users       []string `json:"users"`
}

// updateFlagRequest is the JSON body for PUT. Pointer fields carry
// the three-state "omit=preserve" semantic.
type updateFlagRequest struct {
	Description *string   `json:"description,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	Realms      *[]string `json:"realms,omitempty"`
	Users       *[]string `json:"users,omitempty"`
}

// listResponse matches the "{items: []}" envelope the rest of the admin
// API uses — callers grep for the plural key rather than ranging over
// a bare array.
type listResponse struct {
	Flags []*Flag `json:"flags"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	flags, err := h.store.ListFlags(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if flags == nil {
		flags = []*Flag{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{Flags: flags})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	var req createFlagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := ValidateFlagName(req.Name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlagName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}

	now := time.Now().UTC()
	flag := &Flag{
		Name:        req.Name,
		Description: req.Description,
		Enabled:     req.Enabled,
		Realms:      req.Realms,
		Users:       req.Users,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.store.CreateFlag(r.Context(), flag); err != nil {
		if errors.Is(err, ErrFlagAlreadyExists) {
			apierror.WriteJSON(w, apierror.NewConflict("FeatureFlagAlreadyExists", map[string]string{
				"name": req.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	// Return the round-tripped form so the client sees the stamped
	// timestamps and any normalised slices.
	stored, err := h.store.GetFlag(r.Context(), req.Name)
	if err != nil {
		stored = flag
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	name := chi.URLParam(r, "name")
	if err := ValidateFlagName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlagName", map[string]string{
			"reason": err.Error(),
			"name":   name,
		}))
		return
	}
	flag, err := h.store.GetFlag(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrFlagNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FeatureFlagNotFound", map[string]string{
				"name": name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, flag)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	name := chi.URLParam(r, "name")
	if err := ValidateFlagName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlagName", map[string]string{
			"reason": err.Error(),
			"name":   name,
		}))
		return
	}
	var req updateFlagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := FlagUpdate{
		Description: req.Description,
		Enabled:     req.Enabled,
		Realms:      req.Realms,
		Users:       req.Users,
	}
	if err := h.store.UpdateFlag(r.Context(), name, upd); err != nil {
		if errors.Is(err, ErrFlagNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FeatureFlagNotFound", map[string]string{
				"name": name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	flag, err := h.store.GetFlag(r.Context(), name)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, flag)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	name := chi.URLParam(r, "name")
	if err := ValidateFlagName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlagName", map[string]string{
			"reason": err.Error(),
			"name":   name,
		}))
		return
	}
	if err := h.store.DeleteFlag(r.Context(), name); err != nil {
		if errors.Is(err, ErrFlagNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FeatureFlagNotFound", map[string]string{
				"name": name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("FeatureFlagDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
