package rls

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

// CreateRequest is the POST body for /api/admin/row-policies. Either
// Predicate (legacy WhereClause) or CELExpression (US-487 CEL gate) must
// be supplied; both populated is allowed but the CEL gate is enforced as
// a strict additional filter on top of the predicate.
type CreateRequest struct {
	ObjectTypeRID string          `json:"objectTypeRid"`
	Predicate     json.RawMessage `json:"predicate,omitempty"`
	CELExpression string          `json:"celExpression,omitempty"`
	AppliesTo     AppliesTo       `json:"appliesTo"`
	Description   string          `json:"description,omitempty"`
}

// ListResponse is the GET /api/admin/row-policies envelope.
type ListResponse struct {
	Policies []*RowPolicy `json:"policies"`
}

// Handler implements the admin CRUD endpoints for row_policies.
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

// RegisterRoutes mounts CRUD under /api/admin/row-policies. Callers should
// wrap this in RequirePermission(PermUserManage) at the chi.Router group
// level so ACL enforcement stays consistent with the other admin surfaces.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/row-policies", h.Create)
	r.Get("/api/admin/row-policies", h.List)
	r.Get("/api/admin/row-policies/{rid}", h.Get)
	r.Patch("/api/admin/row-policies/{rid}", h.Update)
	r.Delete("/api/admin/row-policies/{rid}", h.Delete)
}

// Create handles POST /api/admin/row-policies.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeUnauthorized(w)
		return
	}
	var req CreateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRowPolicyRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.ObjectTypeRID = strings.TrimSpace(req.ObjectTypeRID)
	p := &RowPolicy{
		RID:           rid.New("rls", "main", "row-policy"),
		ObjectTypeRID: req.ObjectTypeRID,
		Predicate:     req.Predicate,
		CELExpression: strings.TrimSpace(req.CELExpression),
		AppliesTo:     req.AppliesTo,
		Description:   strings.TrimSpace(req.Description),
		CreatedBy:     u.ID,
	}
	if err := p.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRowPolicy", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	// US-487: reject invalid CEL up front (size / type / parse). The
	// engine also rejects on Reload, but failing fast at admin-create
	// gives the operator a clean 400 instead of a silent broken policy.
	if p.HasCEL() {
		if err := validateCELExpression(p.CELExpression); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRowPolicyCEL", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
	}
	if err := h.store.Create(r.Context(), p); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RowPolicyCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "row_policy_create",
			ResourceType: "RowPolicy",
			ResourceRID:  p.RID,
		})
	}
	writeJSON(w, http.StatusCreated, p)
}

// List handles GET /api/admin/row-policies (optionally filtered by
// ?objectType=<rid>).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		writeUnauthorized(w)
		return
	}
	otRID := strings.TrimSpace(r.URL.Query().Get("objectType"))
	var (
		rows []*RowPolicy
		err  error
	)
	if otRID != "" {
		rows, err = h.store.ListByObjectType(r.Context(), otRID)
	} else {
		rows, err = h.store.List(r.Context())
	}
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RowPolicyListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*RowPolicy{}
	}
	writeJSON(w, http.StatusOK, ListResponse{Policies: rows})
}

// Get handles GET /api/admin/row-policies/{rid}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	h.getFor(w, r, chi.URLParam(r, "rid"))
}

func (h *Handler) getFor(w http.ResponseWriter, r *http.Request, ridStr string) {
	if auth.UserFromContext(r.Context()) == nil {
		writeUnauthorized(w)
		return
	}
	if ridStr == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRowPolicyRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	p, err := h.store.Get(r.Context(), ridStr)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RowPolicyNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RowPolicyLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// Update handles PATCH /api/admin/row-policies/{rid}.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRowPolicyRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	var upd RowPolicyUpdate
	if err := httputil.ReadJSON(r, &upd); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRowPolicyUpdate", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if upd.CELExpression != nil {
		expr := strings.TrimSpace(*upd.CELExpression)
		if expr != "" {
			if err := validateCELExpression(expr); err != nil {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRowPolicyCEL", map[string]string{
					"reason": err.Error(),
				}))
				return
			}
		}
		// Re-bind to the trimmed value so storage never holds whitespace.
		upd.CELExpression = &expr
	}
	p, err := h.store.Update(r.Context(), ridStr, upd)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RowPolicyNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RowPolicyUpdateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "row_policy_update",
			ResourceType: "RowPolicy",
			ResourceRID:  p.RID,
		})
	}
	writeJSON(w, http.StatusOK, p)
}

// Delete handles DELETE /api/admin/row-policies/{rid}.
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRowPolicyRID", map[string]string{
			"reason": "rid path parameter is required",
		}))
		return
	}
	if err := h.store.Delete(r.Context(), ridStr); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RowPolicyNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RowPolicyDeleteFailed", map[string]string{"reason": err.Error()}))
		return
	}
	h.refreshEngine(r.Context())
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "row_policy_delete",
			ResourceType: "RowPolicy",
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

// validateCELExpression is the admin-create / admin-update CEL gatekeeper.
// Wraps pkg/cel.Validate so the handler can stay free of the package import
// cycle risk (handlers → cel → ...). Empty string is treated as "no CEL
// gate" and accepted; the caller is expected to TrimSpace beforehand.
func validateCELExpression(expression string) error {
	if expression == "" {
		return nil
	}
	return celValidate(expression)
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
