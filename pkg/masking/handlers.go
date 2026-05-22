package masking

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
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// CreateRequest is the POST body for /api/admin/column-masks.
type CreateRequest struct {
	ObjectTypeRID   string    `json:"objectTypeRid"`
	PropertyAPIName string    `json:"propertyApiName"`
	MaskRule        MaskRule  `json:"maskRule"`
	AppliesTo       AppliesTo `json:"appliesTo"`
	Description     string    `json:"description,omitempty"`
}

// ListResponse is the GET /api/admin/column-masks envelope.
type ListResponse struct {
	Masks []*ColumnMask `json:"masks"`
}

// Handler implements the admin CRUD endpoints for column_masks.
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

// RegisterRoutes mounts CRUD under /api/admin/column-masks. Callers should
// wrap this in RequirePermission(PermUserManage) at the chi.Router group
// level so ACL enforcement stays consistent with the other admin surfaces.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/column-masks", h.Create)
	r.Get("/api/admin/column-masks", h.List)
	r.Get("/api/admin/column-masks/{rid}", h.Get)
	r.Patch("/api/admin/column-masks/{rid}", h.Update)
	r.Delete("/api/admin/column-masks/{rid}", h.Delete)
}

// Create handles POST /api/admin/column-masks.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeUnauthorized(w)
		return
	}
	var req CreateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidColumnMaskRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	m := &ColumnMask{
		RID:             rid.New("masking", "main", "column-mask"),
		ObjectTypeRID:   strings.TrimSpace(req.ObjectTypeRID),
		PropertyAPIName: strings.TrimSpace(req.PropertyAPIName),
		MaskRule:        req.MaskRule,
		AppliesTo:       req.AppliesTo,
		Description:     strings.TrimSpace(req.Description),
		CreatedBy:       u.ID,
	}
	if err := m.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidColumnMask", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := h.store.Create(r.Context(), m); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ColumnMaskCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "column_mask_create",
			ResourceType: "ColumnMask",
			ResourceRID:  m.RID,
		})
	}
	writeJSON(w, http.StatusCreated, m)
}

// List handles GET /api/admin/column-masks (optionally filtered by
// ?objectType=<rid>).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		writeUnauthorized(w)
		return
	}
	otRID := strings.TrimSpace(r.URL.Query().Get("objectType"))
	var (
		rows []*ColumnMask
		err  error
	)
	if otRID != "" {
		rows, err = h.store.ListByObjectType(r.Context(), otRID)
	} else {
		rows, err = h.store.List(r.Context())
	}
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ColumnMaskListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*ColumnMask{}
	}
	writeJSON(w, http.StatusOK, ListResponse{Masks: rows})
}

// Get handles GET /api/admin/column-masks/{rid}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	h.getFor(w, r, chi.URLParam(r, "rid"))
}

func (h *Handler) getFor(w http.ResponseWriter, r *http.Request, ridStr string) {
	if auth.UserFromContext(r.Context()) == nil {
		writeUnauthorized(w)
		return
	}
	if ridStr == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingColumnMaskRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	m, err := h.store.Get(r.Context(), ridStr)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ColumnMaskNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ColumnMaskLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// Update handles PATCH /api/admin/column-masks/{rid}.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingColumnMaskRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	var upd ColumnMaskUpdate
	if err := httputil.ReadJSON(r, &upd); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidColumnMaskUpdate", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if upd.MaskRule != nil && !IsKnownRule(*upd.MaskRule) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidColumnMask", map[string]string{
			"reason": ErrUnknownMaskRule.Error(),
		}))
		return
	}
	m, err := h.store.Update(r.Context(), ridStr, upd)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ColumnMaskNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ColumnMaskUpdateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "column_mask_update",
			ResourceType: "ColumnMask",
			ResourceRID:  m.RID,
		})
	}
	writeJSON(w, http.StatusOK, m)
}

// Delete handles DELETE /api/admin/column-masks/{rid}.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingColumnMaskRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	if err := h.store.Delete(r.Context(), ridStr); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ColumnMaskNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ColumnMaskDeleteFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "column_mask_delete",
			ResourceType: "ColumnMask",
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
