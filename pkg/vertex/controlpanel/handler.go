package controlpanel

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler serves /api/vertex/v1/admin/control-panel under chi. It is composed
// over a Store so MemStore (tests, degraded boots) and the PG-backed store
// (production) plug in unchanged.
type Handler struct {
	store Store
}

// NewHandler wires a Handler over a Store. A nil store surfaces to callers
// as 500 ControlPanelStoreNotConfigured rather than panicking.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts the GET + PUT endpoints on r. Both live under
// /api/vertex/v1/admin/control-panel.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/vertex/v1/admin/control-panel", h.get)
	r.Put("/api/vertex/v1/admin/control-panel", h.put)
}

// get returns the current control-panel config. Open to any authenticated
// caller — every Vertex client needs to read the knobs to render correctly
// (polling interval, default window, etc.). When the store has never been
// written, Get returns DefaultConfig so the response is always populated.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ControlPanelStoreNotConfigured", nil))
		return
	}
	cfg, err := h.store.Get(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ControlPanelGetFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, cfg)
}

// putRequest captures the PUT body as a sparse map of optional fields. We
// merge it onto the current Config (or defaults when unset) before writing —
// callers may legitimately submit a single knob without restating every
// other field.
type putRequest struct {
	DefaultWindowDays       *int `json:"defaultWindowDays,omitempty"`
	PollingIntervalSec      *int `json:"pollingIntervalSec,omitempty"`
	SearchAroundMaxNodes    *int `json:"searchAroundMaxNodes,omitempty"`
	SearchAroundMaxDepth    *int `json:"searchAroundMaxDepth,omitempty"`
	MissingDataWarningHours *int `json:"missingDataWarningHours,omitempty"`
}

// put updates the control-panel config. Admin role required; anyone else
// (including anonymous callers) gets 403.
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ControlPanelStoreNotConfigured", nil))
		return
	}
	if !isAdmin(auth.UserFromContext(r.Context())) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("ControlPanelUpdateForbidden", nil))
		return
	}
	var req putRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJSON",
			map[string]string{"error": err.Error()}))
		return
	}
	current, err := h.store.Get(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ControlPanelGetFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	merged := mergeConfig(current, req)
	if err := h.store.Set(r.Context(), merged); err != nil {
		if errors.Is(err, ErrInvalidConfig) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidControlPanelConfig",
				map[string]string{"error": err.Error()}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ControlPanelSetFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, merged)
}

// mergeConfig overlays the optional fields in req onto base. Pointers in
// putRequest distinguish "field omitted" (nil) from "field set to zero".
func mergeConfig(base Config, req putRequest) Config {
	out := base
	if req.DefaultWindowDays != nil {
		out.DefaultWindowDays = *req.DefaultWindowDays
	}
	if req.PollingIntervalSec != nil {
		out.PollingIntervalSec = *req.PollingIntervalSec
	}
	if req.SearchAroundMaxNodes != nil {
		out.SearchAroundMaxNodes = *req.SearchAroundMaxNodes
	}
	if req.SearchAroundMaxDepth != nil {
		out.SearchAroundMaxDepth = *req.SearchAroundMaxDepth
	}
	if req.MissingDataWarningHours != nil {
		out.MissingDataWarningHours = *req.MissingDataWarningHours
	}
	return out
}

// isAdmin returns true when u carries the "admin" role. Nil user (anonymous)
// is never admin.
func isAdmin(u *auth.User) bool {
	if u == nil {
		return false
	}
	for _, role := range u.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

