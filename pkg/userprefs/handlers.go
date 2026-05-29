package userprefs

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/v2/user-preferences endpoints.
//
//	GET /api/v2/user-preferences  — return the caller's persisted prefs
//	                                 (or a virtual default row when none
//	                                 have been written yet).
//	PUT /api/v2/user-preferences  — partial-update the caller's row.
//
// Every endpoint is scoped to the authenticated user — preferences are
// private and self-managed only.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. Nil store leaves every endpoint
// reporting UserPreferencesUnavailable so degraded-mode test routers
// (no PG) can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/user-preferences", h.Get)
	r.Put("/api/v2/user-preferences", h.Put)
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
		apierror.WriteJSON(w, apierror.NewInternal("UserPreferencesUnavailable", map[string]string{
			"reason": "user preferences are not configured on this deployment",
		}))
		return false
	}
	return true
}

// virtualDefault returns the zero-value Preferences row exposed when a
// user fetches their prefs before ever PUT-ing. UI treats this as
// "use OS / localStorage defaults".
func virtualDefault(userID string) *Preferences {
	return &Preferences{
		UserID:        userID,
		Theme:         "",
		Language:      "",
		Notifications: append(json.RawMessage(nil), DefaultPayload...),
		Hotkeys:       append(json.RawMessage(nil), DefaultPayload...),
	}
}

// Get GET /api/v2/user-preferences. Returns the caller's row or a
// virtual default when no row has been persisted yet.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	row, err := h.store.Get(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.WriteJSON(w, http.StatusOK, virtualDefault(user.ID))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UserPreferencesLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

type updateRequest struct {
	Theme         *string          `json:"theme,omitempty"`
	Language      *string          `json:"language,omitempty"`
	Notifications *json.RawMessage `json:"notifications,omitempty"`
	Hotkeys       *json.RawMessage `json:"hotkeys,omitempty"`
}

// Put PUT /api/v2/user-preferences. Partial update — only fields
// supplied in the body are mutated; the rest are preserved.
func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req updateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if req.Theme != nil {
		t := NormaliseTheme(*req.Theme)
		if err := ValidateTheme(t); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidUserPreferenceTheme", map[string]string{
				"reason": err.Error(),
				"theme":  *req.Theme,
			}))
			return
		}
	}
	if req.Language != nil {
		l := NormaliseLanguage(*req.Language)
		if err := ValidateLanguage(l); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidUserPreferenceLanguage", map[string]string{
				"reason":   err.Error(),
				"language": *req.Language,
			}))
			return
		}
	}
	row, err := h.store.Upsert(r.Context(), user.ID, Update{
		Theme:         req.Theme,
		Language:      req.Language,
		Notifications: req.Notifications,
		Hotkeys:       req.Hotkeys,
	})
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UserPreferencesUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}
