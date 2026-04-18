package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

// RoleRequest is the POST /api/admin/roles body.
type RoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// RoleUpdateRequest is the PATCH /api/admin/roles/{name} body.
type RoleUpdateRequest struct {
	Description *string `json:"description,omitempty"`
}

// RolePermissionsRequest is the PUT /api/admin/roles/{name}/permissions body.
type RolePermissionsRequest struct {
	Permissions []string `json:"permissions"`
}

// RoleResponse is the wire shape.
type RoleResponse struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Builtin     bool      `json:"builtin"`
	CreatedAt   time.Time `json:"createdAt"`
	Permissions []string  `json:"permissions"`
}

// RoleListResponse is returned by GET /api/admin/roles.
type RoleListResponse struct {
	Roles []RoleResponse `json:"roles"`
}

// RolePermissionsResponse is returned by GET /permissions.
type RolePermissionsResponse struct {
	Permissions []string `json:"permissions"`
}

func toRoleResponse(role *Role, perms []string) RoleResponse {
	if perms == nil {
		perms = []string{}
	}
	return RoleResponse{
		Name:        role.Name,
		Description: role.Description,
		Builtin:     role.Builtin,
		CreatedAt:   role.CreatedAt,
		Permissions: perms,
	}
}

// RoleHandler implements the admin REST endpoints for roles.
//
// For built-in roles the permission list returned by GET always falls back
// to the static matrix in permissions.go — migration 000051 seeds the role
// identity but does not pre-populate role_permissions for built-ins, so
// listing "admin" must return the 29 permissions the resolver actually
// honours rather than an empty slice.
type RoleHandler struct {
	repo       RoleRepository
	auditStore audit.Store
}

// NewRoleHandler constructs the admin handler around a repository.
// auditStore may be nil to disable audit logging.
func NewRoleHandler(repo RoleRepository, auditStore audit.Store) *RoleHandler {
	return &RoleHandler{repo: repo, auditStore: auditStore}
}

// RegisterRoutes mounts the CRUD endpoints. Callers should wrap this with
// RequirePermission(PermUserManage) at registration time.
func (h *RoleHandler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/roles", h.Create)
	r.Get("/api/admin/roles", h.List)
	r.Get("/api/admin/roles/{name}", h.Get)
	r.Patch("/api/admin/roles/{name}", h.Update)
	r.Delete("/api/admin/roles/{name}", h.Delete)
	r.Get("/api/admin/roles/{name}/permissions", h.GetPermissions)
	r.Put("/api/admin/roles/{name}/permissions", h.SetPermissions)
}

// Create handles POST /api/admin/roles.
func (h *RoleHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	var req RoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRoleRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := ValidateRoleName(req.Name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRoleName", map[string]string{
			"reason": "name must be 1-128 chars, starting with alphanumeric, containing only [A-Za-z0-9._-]",
		}))
		return
	}
	role := &Role{Name: req.Name, Description: strings.TrimSpace(req.Description), Builtin: false}
	if err := h.repo.Create(r.Context(), role); err != nil {
		if errors.Is(err, ErrRoleConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("RoleConflict", map[string]string{"name": req.Name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleCreateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if len(req.Permissions) > 0 {
		if err := h.repo.SetPermissions(r.Context(), role.Name, req.Permissions); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("RolePermissionsFailed", map[string]string{"reason": err.Error()}))
			return
		}
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "role_create",
			ResourceType: "Role",
			ResourceRID:  role.Name,
		})
	}
	perms, _ := h.repo.ListPermissions(r.Context(), role.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toRoleResponse(role, perms))
}

// List handles GET /api/admin/roles.
func (h *RoleHandler) List(w http.ResponseWriter, r *http.Request) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	rows, err := h.repo.List(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RoleListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	out := RoleListResponse{Roles: make([]RoleResponse, 0, len(rows))}
	for _, role := range rows {
		perms := permissionsForRole(r, h.repo, role)
		out.Roles = append(out.Roles, toRoleResponse(role, perms))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// Get handles GET /api/admin/roles/{name}.
func (h *RoleHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.getFor(w, r, chi.URLParam(r, "name"))
}

func (h *RoleHandler) getFor(w http.ResponseWriter, r *http.Request, name string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRoleName", map[string]string{
			"reason": "name path parameter is required",
		}))
		return
	}
	role, err := h.repo.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	perms := permissionsForRole(r, h.repo, role)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toRoleResponse(role, perms))
}

// Update handles PATCH /api/admin/roles/{name}.
func (h *RoleHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.updateFor(w, r, chi.URLParam(r, "name"))
}

func (h *RoleHandler) updateFor(w http.ResponseWriter, r *http.Request, name string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRoleName", map[string]string{
			"reason": "name path parameter is required",
		}))
		return
	}
	var req RoleUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRoleUpdate", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if req.Description == nil {
		// No-op PATCH — return current.
		role, err := h.repo.Get(r.Context(), name)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"name": name}))
			return
		}
		perms := permissionsForRole(r, h.repo, role)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(toRoleResponse(role, perms))
		return
	}
	role, err := h.repo.UpdateDescription(r.Context(), name, strings.TrimSpace(*req.Description))
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleUpdateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "role_update",
			ResourceType: "Role",
			ResourceRID:  role.Name,
		})
	}
	perms := permissionsForRole(r, h.repo, role)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toRoleResponse(role, perms))
}

// Delete handles DELETE /api/admin/roles/{name}. Built-in roles are
// protected and return 409 ErrBuiltinRoleProtected.
func (h *RoleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.deleteFor(w, r, chi.URLParam(r, "name"))
}

func (h *RoleHandler) deleteFor(w http.ResponseWriter, r *http.Request, name string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	role, err := h.repo.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if role.Builtin {
		apierror.WriteJSON(w, apierror.NewConflict("BuiltinRoleProtected", map[string]string{
			"name":   name,
			"reason": "built-in roles cannot be deleted",
		}))
		return
	}
	if err := h.repo.Delete(r.Context(), name); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RoleDeleteFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "role_delete",
			ResourceType: "Role",
			ResourceRID:  name,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetPermissions handles GET /api/admin/roles/{name}/permissions.
func (h *RoleHandler) GetPermissions(w http.ResponseWriter, r *http.Request) {
	h.getPermissionsFor(w, r, chi.URLParam(r, "name"))
}

func (h *RoleHandler) getPermissionsFor(w http.ResponseWriter, r *http.Request, name string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	role, err := h.repo.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	perms := permissionsForRole(r, h.repo, role)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(RolePermissionsResponse{Permissions: perms})
}

// SetPermissions handles PUT /api/admin/roles/{name}/permissions. Built-in
// roles are protected here — their authoritative permissions list lives
// in the static matrix and cannot be overridden at runtime without a
// resolver rewrite (out of scope for US-251).
func (h *RoleHandler) SetPermissions(w http.ResponseWriter, r *http.Request) {
	h.setPermissionsFor(w, r, chi.URLParam(r, "name"))
}

func (h *RoleHandler) setPermissionsFor(w http.ResponseWriter, r *http.Request, name string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	role, err := h.repo.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if role.Builtin {
		apierror.WriteJSON(w, apierror.NewConflict("BuiltinRoleProtected", map[string]string{
			"name":   name,
			"reason": "built-in role permissions are defined by the static matrix and cannot be overridden",
		}))
		return
	}
	var req RolePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPermissionsRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := h.repo.SetPermissions(r.Context(), name, req.Permissions); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RolePermissionsFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "role_set_permissions",
			ResourceType: "Role",
			ResourceRID:  name,
		})
	}
	perms, _ := h.repo.ListPermissions(r.Context(), name)
	if perms == nil {
		perms = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(RolePermissionsResponse{Permissions: perms})
}

// permissionsForRole returns the effective permission list for a role.
// Built-ins without rows in role_permissions fall back to the static
// RolePermissions matrix so the wire response matches the resolver.
func permissionsForRole(r *http.Request, repo RoleRepository, role *Role) []string {
	perms, _ := repo.ListPermissions(r.Context(), role.Name)
	if role.Builtin && len(perms) == 0 {
		perms = RolePermissions(role.Name)
	}
	if perms == nil {
		perms = []string{}
	}
	return perms
}
