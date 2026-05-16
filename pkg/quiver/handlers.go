package quiver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// Handler implements the /api/v2/quiver/* endpoints (US-403).
//
//	POST   /api/v2/quiver/save               — create or update by rid
//	GET    /api/v2/quiver/dashboards         — list owner's dashboards
//	GET    /api/v2/quiver/dashboards/{rid}   — owner-scoped get
//	GET    /api/v2/quiver/dashboards/{rid}/view — read-only view (share)
//	DELETE /api/v2/quiver/dashboards/{rid}   — owner-only delete
//
// The /view endpoint is the share surface: any authenticated caller
// who knows the RID can read the row. Owner-only routes use the same
// `?rid=...` matcher under a shared chi route group.
type Handler struct {
	store    Store
	tsReader TimeSeriesReader
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting QuiverDashboardsUnavailable so degraded-mode test routers
// (no PG) can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// RegisterRoutes mounts every endpoint on r. The router is expected to
// already enforce auth presence; the requireAuth check below is
// defence-in-depth so test routers that skip middleware still refuse
// unauthenticated callers.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/quiver/save", h.Save)
	r.Get("/api/v2/quiver/dashboards", h.List)
	r.Get("/api/v2/quiver/dashboards/{rid}", h.Get)
	r.Get("/api/v2/quiver/dashboards/{rid}/view", h.View)
	// US-482: multi-series time-series fetch for a saved dashboard.
	// Share-link semantics — same authentication contract as /view.
	r.Get("/api/v2/quiver/dashboards/{rid}/data", h.Data)
	r.Delete("/api/v2/quiver/dashboards/{rid}", h.Delete)
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
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardsUnavailable", map[string]string{
			"reason": "quiver dashboards are not configured on this deployment",
		}))
		return false
	}
	return true
}

type saveRequest struct {
	RID    string          `json:"rid,omitempty"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type listResponse struct {
	Dashboards []*Dashboard `json:"dashboards"`
}

// Save POST /api/v2/quiver/save — creates a new dashboard if rid is
// empty, otherwise updates the existing row. Both branches return the
// final persisted dashboard so the SPA can pick up the assigned RID
// for fresh saves and the bumped updatedAt for re-saves.
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := ValidateName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidQuiverDashboardName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}
	cfg := req.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}

	if strings.TrimSpace(req.RID) == "" {
		// Create a fresh dashboard.
		now := time.Now().UTC()
		row := &Dashboard{
			RID:       rid.New("quiver", "main", "dashboard"),
			Name:      name,
			Owner:     user.ID,
			Config:    cfg,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.store.Save(r.Context(), row); err != nil {
			h.writeSaveError(w, err, name)
			return
		}
		stored, err := h.store.Get(r.Context(), row.RID, user.ID)
		if err != nil {
			stored = row
		}
		httputil.WriteJSON(w, http.StatusCreated, stored)
		return
	}

	// Update an existing dashboard. Only the owner may save.
	upd := Update{Name: &name, Config: &cfg}
	if err := h.store.Update(r.Context(), req.RID, user.ID, upd); err != nil {
		h.writeSaveError(w, err, name)
		return
	}
	row, err := h.store.Get(r.Context(), req.RID, user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

func (h *Handler) writeSaveError(w http.ResponseWriter, err error, name string) {
	if errors.Is(err, ErrNotFound) {
		apierror.WriteJSON(w, apierror.NewNotFound("QuiverDashboardNotFound", map[string]string{}))
		return
	}
	if errors.Is(err, ErrNameConflict) {
		apierror.WriteJSON(w, apierror.NewConflict("QuiverDashboardNameConflict", map[string]string{
			"name": name,
		}))
		return
	}
	apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardSaveFailed", map[string]string{
		"reason": err.Error(),
	}))
}

// List GET /api/v2/quiver/dashboards — returns every dashboard the
// caller owns. Sharing is RID-only, so other users' dashboards are
// never enumerated.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	rows, err := h.store.List(r.Context(), user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Dashboard{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{Dashboards: rows})
}

// Get GET /api/v2/quiver/dashboards/{rid} — owner-scoped lookup. Used
// by the SPA's editor when re-opening a saved dashboard for further
// edits. Non-owners receive 404.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	dashboardRID := chi.URLParam(r, "rid")
	row, err := h.store.Get(r.Context(), dashboardRID, user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QuiverDashboardNotFound", map[string]string{"rid": dashboardRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// View GET /api/v2/quiver/dashboards/{rid}/view — read-only share.
// Any authenticated caller who knows the RID can read the row. The
// /view suffix differentiates the share surface from the owner-scoped
// Get; both eventually emit the same `Dashboard` envelope so the SPA
// reuses one renderer.
func (h *Handler) View(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	_ = user
	dashboardRID := chi.URLParam(r, "rid")
	row, err := h.store.GetByRID(r.Context(), dashboardRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QuiverDashboardNotFound", map[string]string{"rid": dashboardRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Delete DELETE /api/v2/quiver/dashboards/{rid}. Owner only.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	dashboardRID := chi.URLParam(r, "rid")
	if err := h.store.Delete(r.Context(), dashboardRID, user.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QuiverDashboardNotFound", map[string]string{"rid": dashboardRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
