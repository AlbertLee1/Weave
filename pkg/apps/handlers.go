package apps

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// Handler implements the /api/v2/apps/* CRUD + version history
// endpoints (US-391, US-396).
//
//	GET    /api/v2/apps                          — list owned Apps
//	POST   /api/v2/apps                          — create
//	GET    /api/v2/apps/{rid}                    — fetch live row
//	PUT    /api/v2/apps/{rid}                    — partial update (bumps version)
//	DELETE /api/v2/apps/{rid}                    — delete + cascade history
//	GET    /api/v2/apps/{rid}/versions           — list history newest-first
//	GET    /api/v2/apps/{rid}/versions/{version} — fetch one history row
//	POST   /api/v2/apps/{rid}/publish            — owner pin current version (US-396)
//	POST   /api/v2/apps/{rid}/unpublish          — owner clear publish state (US-396)
//	GET    /api/v2/apps/{rid}/view               — viewer fetch published snapshot (US-396)
//	POST   /api/v2/apps/{rid}/versions/{version}/rollback — owner restore from history (US-398)
type Handler struct {
	store Store
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting AppsUnavailable so degraded-mode test routers (no PG) can
// keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/apps", h.List)
	r.Post("/api/v2/apps", h.Create)
	r.Get("/api/v2/apps/{rid}", h.Get)
	r.Put("/api/v2/apps/{rid}", h.Update)
	r.Delete("/api/v2/apps/{rid}", h.Delete)
	r.Get("/api/v2/apps/{rid}/versions", h.ListVersions)
	r.Get("/api/v2/apps/{rid}/versions/{version}", h.GetVersion)
	r.Post("/api/v2/apps/{rid}/publish", h.Publish)
	r.Post("/api/v2/apps/{rid}/unpublish", h.Unpublish)
	r.Get("/api/v2/apps/{rid}/view", h.View)
	r.Post("/api/v2/apps/{rid}/versions/{version}/rollback", h.Rollback)
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
		apierror.WriteJSON(w, apierror.NewInternal("AppsUnavailable", map[string]string{
			"reason": "apps are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createRequest struct {
	Name       string          `json:"name"`
	LayoutJSON json.RawMessage `json:"layoutJson"`
}

type updateRequest struct {
	Name       *string          `json:"name,omitempty"`
	LayoutJSON *json.RawMessage `json:"layoutJson,omitempty"`
}

type listResponse struct {
	Apps []*App `json:"apps"`
}

type listVersionsResponse struct {
	Versions []*AppVersion `json:"versions"`
}

// Create POST /api/v2/apps.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAppName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}
	if err := ValidateLayout(req.LayoutJSON); err != nil {
		writeLayoutError(w, err)
		return
	}
	row := &App{
		RID:        rid.New("app", "main", "app"),
		Name:       name,
		OwnerID:    user.ID,
		LayoutJSON: req.LayoutJSON,
	}
	if err := h.store.Create(r.Context(), row, user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	stored, err := h.store.Get(r.Context(), row.RID, user.ID)
	if err != nil {
		stored = row
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// List GET /api/v2/apps — every App owned by the caller.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	rows, err := h.store.List(r.Context(), user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AppListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*App{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{Apps: rows})
}

// Get GET /api/v2/apps/{rid}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	row, err := h.store.Get(r.Context(), id, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Update PUT /api/v2/apps/{rid}. Owner-only.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
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
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAppName", map[string]string{
				"reason": err.Error(),
				"name":   *req.Name,
			}))
			return
		}
		upd.Name = &trimmed
	}
	if req.LayoutJSON != nil {
		if err := ValidateLayout(*req.LayoutJSON); err != nil {
			writeLayoutError(w, err)
			return
		}
		upd.LayoutJSON = req.LayoutJSON
	}
	if err := h.store.Update(r.Context(), id, user.ID, upd, user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	row, err := h.store.Get(r.Context(), id, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Delete DELETE /api/v2/apps/{rid}. Owner-only.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	if err := h.store.Delete(r.Context(), id, user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListVersions GET /api/v2/apps/{rid}/versions — newest first.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	versions, err := h.store.ListVersions(r.Context(), id, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if versions == nil {
		versions = []*AppVersion{}
	}
	httputil.WriteJSON(w, http.StatusOK, listVersionsResponse{Versions: versions})
}

// GetVersion GET /api/v2/apps/{rid}/versions/{version}.
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 1 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAppVersion", map[string]string{
			"reason":  "version must be a positive integer",
			"version": versionStr,
		}))
		return
	}
	v, err := h.store.GetVersion(r.Context(), id, version, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, v)
}

// Publish POST /api/v2/apps/{rid}/publish. Owner-only.
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	view, err := h.store.Publish(r.Context(), id, user.ID, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, view)
}

// Unpublish POST /api/v2/apps/{rid}/unpublish. Owner-only.
func (h *Handler) Unpublish(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	if err := h.store.Unpublish(r.Context(), id, user.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Rollback POST /api/v2/apps/{rid}/versions/{version}/rollback. Owner-only.
//
// Restores Name + LayoutJSON from the targeted history row, bumping
// Version (so the rollback itself is recorded as a new history row
// attributed to the caller). The response is the live App row after
// the rollback lands — same shape as Get/Update so the SPA can swap it
// straight into its editor state.
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 1 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAppVersion", map[string]string{
			"reason":  "version must be a positive integer",
			"version": versionStr,
		}))
		return
	}
	row, err := h.store.Rollback(r.Context(), id, version, user.ID, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// View GET /api/v2/apps/{rid}/view. Any authenticated user.
//
// The viewer surface intentionally does NOT consult ownership — once
// an App has been published it is readable by every authenticated
// viewer in the deployment. Unpublished Apps return 404
// AppNotPublished so viewers cannot enumerate draft RIDs.
func (h *Handler) View(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "rid")
	view, err := h.store.GetPublished(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, view)
}

// writeStoreError translates store-layer sentinel errors into the
// canonical API error envelope. Unrecognised errors fall through to a
// 500 internal envelope.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotPublished):
		apierror.WriteJSON(w, apierror.NewNotFound("AppNotPublished", nil))
	case errors.Is(err, ErrNotFound):
		apierror.WriteJSON(w, apierror.NewNotFound("AppNotFound", nil))
	case errors.Is(err, ErrNameConflict):
		apierror.WriteJSON(w, apierror.NewConflict("AppNameConflict", nil))
	case errors.Is(err, ErrInvalidLayout):
		writeLayoutError(w, err)
	default:
		apierror.WriteJSON(w, apierror.NewInternal("AppStoreFailed", map[string]string{
			"reason": err.Error(),
		}))
	}
}

func writeLayoutError(w http.ResponseWriter, err error) {
	params := map[string]string{"reason": err.Error()}
	var le *LayoutError
	if errors.As(err, &le) && le.Path != "" {
		params["path"] = le.Path
	}
	apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAppLayout", params))
}
