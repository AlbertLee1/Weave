package cellsec

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/rid"
)

// CreateRequest is the POST body for /api/admin/cell-masks.
type CreateRequest struct {
	ObjectTypeRID   string            `json:"objectTypeRid"`
	PrimaryKey      string            `json:"primaryKey"`
	PropertyAPIName string            `json:"propertyApiName"`
	MaskRule        masking.MaskRule  `json:"maskRule"`
	AppliesTo       masking.AppliesTo `json:"appliesTo"`
	Description     string            `json:"description,omitempty"`
}

// ListResponse is the GET /api/admin/cell-masks envelope.
type ListResponse struct {
	Masks []*CellMask `json:"masks"`
}

// Handler implements the admin CRUD endpoints for cell_masks.
type Handler struct {
	store      Store
	auditStore audit.Store
	engine     *Engine
}

// NewHandler constructs the admin handler. auditStore and engine may be nil;
// a nil engine skips the post-write cache refresh (CRUD still persists).
func NewHandler(store Store, auditStore audit.Store, engine *Engine) *Handler {
	return &Handler{store: store, auditStore: auditStore, engine: engine}
}

// RegisterRoutes mounts CRUD under /api/admin/cell-masks. Callers should
// wrap this in RequirePermission(PermUserManage) at the chi.Router group
// level so ACL enforcement stays consistent with the other admin surfaces.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/cell-masks", h.Create)
	r.Get("/api/admin/cell-masks", h.List)
	r.Get("/api/admin/cell-masks/{rid}", h.Get)
	r.Patch("/api/admin/cell-masks/{rid}", h.Update)
	r.Delete("/api/admin/cell-masks/{rid}", h.Delete)
}

// Create handles POST /api/admin/cell-masks.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeUnauthorized(w)
		return
	}
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCellMaskRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	m := &CellMask{
		RID:             rid.New("cellsec", "main", "cell-mask"),
		ObjectTypeRID:   strings.TrimSpace(req.ObjectTypeRID),
		PrimaryKey:      strings.TrimSpace(req.PrimaryKey),
		PropertyAPIName: strings.TrimSpace(req.PropertyAPIName),
		MaskRule:        req.MaskRule,
		AppliesTo:       req.AppliesTo,
		Description:     strings.TrimSpace(req.Description),
		CreatedBy:       u.ID,
	}
	if err := m.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCellMask", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := h.store.Create(r.Context(), m); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CellMaskCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "cell_mask_create",
			ResourceType: "CellMask",
			ResourceRID:  m.RID,
		})
	}
	writeJSON(w, http.StatusCreated, m)
}

// List handles GET /api/admin/cell-masks (optionally filtered by
// ?objectType=<rid>).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		writeUnauthorized(w)
		return
	}
	otRID := strings.TrimSpace(r.URL.Query().Get("objectType"))
	var (
		rows []*CellMask
		err  error
	)
	if otRID != "" {
		rows, err = h.store.ListByObjectType(r.Context(), otRID)
	} else {
		rows, err = h.store.List(r.Context())
	}
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CellMaskListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*CellMask{}
	}
	writeJSON(w, http.StatusOK, ListResponse{Masks: rows})
}

// Get handles GET /api/admin/cell-masks/{rid}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	h.getFor(w, r, chi.URLParam(r, "rid"))
}

func (h *Handler) getFor(w http.ResponseWriter, r *http.Request, ridStr string) {
	if auth.UserFromContext(r.Context()) == nil {
		writeUnauthorized(w)
		return
	}
	if ridStr == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingCellMaskRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	m, err := h.store.Get(r.Context(), ridStr)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CellMaskNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CellMaskLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// Update handles PATCH /api/admin/cell-masks/{rid}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	h.updateFor(w, r, chi.URLParam(r, "rid"))
}

func (h *Handler) updateFor(w http.ResponseWriter, r *http.Request, ridStr string) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeUnauthorized(w)
		return
	}
	if ridStr == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingCellMaskRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	var upd CellMaskUpdate
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCellMaskUpdate", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if upd.MaskRule != nil && !masking.IsKnownRule(*upd.MaskRule) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCellMask", map[string]string{
			"reason": ErrUnknownMaskRule.Error(),
		}))
		return
	}
	m, err := h.store.Update(r.Context(), ridStr, upd)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CellMaskNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CellMaskUpdateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "cell_mask_update",
			ResourceType: "CellMask",
			ResourceRID:  m.RID,
		})
	}
	writeJSON(w, http.StatusOK, m)
}

// Delete handles DELETE /api/admin/cell-masks/{rid}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	h.deleteFor(w, r, chi.URLParam(r, "rid"))
}

func (h *Handler) deleteFor(w http.ResponseWriter, r *http.Request, ridStr string) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeUnauthorized(w)
		return
	}
	if ridStr == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingCellMaskRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	if err := h.store.Delete(r.Context(), ridStr); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("CellMaskNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CellMaskDeleteFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "cell_mask_delete",
			ResourceType: "CellMask",
			ResourceRID:  ridStr,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) refreshEngine(ctx context.Context) {
	if h.engine == nil {
		return
	}
	_ = h.engine.Reload(ctx)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeUnauthorized(w http.ResponseWriter) {
	apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
		"reason": "no authenticated user in request context",
	}))
}
