package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

// UserRoleRequest is the POST /api/admin/users/{userId}/roles body.
type UserRoleRequest struct {
	Role string `json:"role"`
}

// UserRolesResponse is returned by GET /api/admin/users/{userId}/roles.
type UserRolesResponse struct {
	Roles []string `json:"roles"`
}

// UserRoleRevoker is the minimal repository surface needed to revoke a
// user's role grant. Separate from UserRepository so the admin handler can
// be constructed without widening the mock surface across every caller.
type UserRoleRevoker interface {
	RevokeUserRole(ctx context.Context, userID, role string) error
}

// UserRoleHandler implements the admin role-grant REST endpoints. The
// handler reads/writes via the existing UserRepository for grant+list and
// a narrow UserRoleRevoker for revoke — the method doesn't exist on the
// existing interface and adding it there would cascade into every mock.
type UserRoleHandler struct {
	users      UserRepository
	roles      RoleRepository
	revoker    UserRoleRevoker
	auditStore audit.Store
}

// NewUserRoleHandler constructs the handler. roles is used to validate
// that the requested role exists in the registry before the grant lands.
func NewUserRoleHandler(users UserRepository, roles RoleRepository, revoker UserRoleRevoker, auditStore audit.Store) *UserRoleHandler {
	return &UserRoleHandler{users: users, roles: roles, revoker: revoker, auditStore: auditStore}
}

// RegisterRoutes mounts the endpoints. Callers should wrap with
// RequirePermission(PermUserManage).
func (h *UserRoleHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/admin/users/{userId}/roles", h.ListRoles)
	r.Post("/api/admin/users/{userId}/roles", h.GrantRole)
	r.Delete("/api/admin/users/{userId}/roles/{role}", h.RevokeRole)
}

// ListRoles handles GET /api/admin/users/{userId}/roles.
func (h *UserRoleHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	h.listRolesFor(w, r, chi.URLParam(r, "userId"))
}

func (h *UserRoleHandler) listRolesFor(w http.ResponseWriter, r *http.Request, userID string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if userID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingUserID", map[string]string{
			"reason": "userId path parameter is required",
		}))
		return
	}
	if _, err := h.users.GetUserByID(r.Context(), userID); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("UserNotFound", map[string]string{"userId": userID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UserLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	roles, err := h.users.ListUserRoles(r.Context(), userID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UserRolesListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if roles == nil {
		roles = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(UserRolesResponse{Roles: roles})
}

// GrantRole handles POST /api/admin/users/{userId}/roles.
func (h *UserRoleHandler) GrantRole(w http.ResponseWriter, r *http.Request) {
	h.grantRoleFor(w, r, chi.URLParam(r, "userId"))
}

func (h *UserRoleHandler) grantRoleFor(w http.ResponseWriter, r *http.Request, userID string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if userID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingUserID", map[string]string{
			"reason": "userId path parameter is required",
		}))
		return
	}
	var req UserRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidUserRoleRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Role = strings.TrimSpace(req.Role)
	if err := ValidateRoleName(req.Role); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRoleName", map[string]string{
			"reason": "role must be a non-empty identifier",
		}))
		return
	}
	if _, err := h.users.GetUserByID(r.Context(), userID); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("UserNotFound", map[string]string{"userId": userID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UserLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if _, err := h.roles.Get(r.Context(), req.Role); err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RoleNotFound", map[string]string{"role": req.Role}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RoleLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if err := h.users.UpsertUserRole(r.Context(), userID, req.Role); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GrantRoleFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{"role": req.Role})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "user_role_grant",
			ResourceType: "User",
			ResourceRID:  userID,
			DiffJSON:     diff,
		})
	}
	roles, _ := h.users.ListUserRoles(r.Context(), userID)
	if roles == nil {
		roles = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(UserRolesResponse{Roles: roles})
}

// RevokeRole handles DELETE /api/admin/users/{userId}/roles/{role}.
func (h *UserRoleHandler) RevokeRole(w http.ResponseWriter, r *http.Request) {
	h.revokeRoleFor(w, r, chi.URLParam(r, "userId"), chi.URLParam(r, "role"))
}

func (h *UserRoleHandler) revokeRoleFor(w http.ResponseWriter, r *http.Request, userID, role string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if userID == "" || role == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingPathParameter", map[string]string{
			"reason": "userId and role are required",
		}))
		return
	}
	if h.revoker == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RevokeUnsupported", map[string]string{
			"reason": "user role revocation is not wired on this deployment",
		}))
		return
	}
	if err := h.revoker.RevokeUserRole(r.Context(), userID, role); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("RevokeRoleFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{"role": role})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "user_role_revoke",
			ResourceType: "User",
			ResourceRID:  userID,
			DiffJSON:     diff,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}
