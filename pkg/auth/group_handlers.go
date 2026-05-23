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
	"github.com/liyang/weave/pkg/httputil"
)

// GroupRequest is the POST /api/admin/groups body.
type GroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// GroupUpdateRequest is the PATCH body. All fields pointer-typed so omit
// is distinguishable from explicit clear.
type GroupUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// GroupResponse is the wire shape returned by every CRUD endpoint.
type GroupResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// GroupListResponse is the wire shape returned by GET /api/admin/groups.
type GroupListResponse struct {
	Groups []GroupResponse `json:"groups"`
}

// GroupMemberRequest is the POST /api/admin/groups/{id}/members body.
type GroupMemberRequest struct {
	UserID string `json:"userId"`
}

// GroupMembersResponse is the GET list shape.
type GroupMembersResponse struct {
	Members []string `json:"members"`
}

func toGroupResponse(g *Group) GroupResponse {
	return GroupResponse{
		ID:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}
}

// GroupHandler implements the admin REST endpoints for groups.
type GroupHandler struct {
	repo       GroupRepository
	auditStore audit.Store
}

// NewGroupHandler constructs the admin handler around a repository.
// auditStore may be nil to disable audit logging.
func NewGroupHandler(repo GroupRepository, auditStore audit.Store) *GroupHandler {
	return &GroupHandler{repo: repo, auditStore: auditStore}
}

// RegisterRoutes mounts the CRUD + membership endpoints. Callers should
// wrap this with RequirePermission(PermUserManage) at registration time.
func (h *GroupHandler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/groups", h.Create)
	r.Get("/api/admin/groups", h.List)
	r.Get("/api/admin/groups/{id}", h.Get)
	r.Patch("/api/admin/groups/{id}", h.Update)
	r.Delete("/api/admin/groups/{id}", h.Delete)
	r.Get("/api/admin/groups/{id}/members", h.ListMembers)
	r.Post("/api/admin/groups/{id}/members", h.AddMember)
	r.Delete("/api/admin/groups/{id}/members/{userId}", h.RemoveMember)
}

// Create handles POST /api/admin/groups.
func (h *GroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	var req GroupRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidGroupRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := ValidateGroupName(req.Name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidGroupName", map[string]string{
			"reason": "name must be 1-128 chars, starting with alphanumeric, containing only [A-Za-z0-9._-]",
		}))
		return
	}
	g := &Group{Name: req.Name, Description: strings.TrimSpace(req.Description)}
	if err := h.repo.Create(r.Context(), g); err != nil {
		if errors.Is(err, ErrGroupNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("GroupNameConflict", map[string]string{"name": req.Name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GroupCreateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "group_create",
			ResourceType: "Group",
			ResourceRID:  g.ID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toGroupResponse(g))
}

// List handles GET /api/admin/groups.
func (h *GroupHandler) List(w http.ResponseWriter, r *http.Request) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	rows, err := h.repo.List(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GroupListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	out := GroupListResponse{Groups: make([]GroupResponse, 0, len(rows))}
	for _, g := range rows {
		out.Groups = append(out.Groups, toGroupResponse(g))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// Get handles GET /api/admin/groups/{id}.
func (h *GroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.getFor(w, r, chi.URLParam(r, "id"))
}

func (h *GroupHandler) getFor(w http.ResponseWriter, r *http.Request, id string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingGroupID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	g, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("GroupNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GroupLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toGroupResponse(g))
}

// Update handles PATCH /api/admin/groups/{id}.
func (h *GroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.updateFor(w, r, chi.URLParam(r, "id"))
}

func (h *GroupHandler) updateFor(w http.ResponseWriter, r *http.Request, id string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingGroupID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	var req GroupUpdateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidGroupUpdate", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := GroupUpdate{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if err := ValidateGroupName(name); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidGroupName", map[string]string{
				"reason": "name must be 1-128 chars, starting with alphanumeric, containing only [A-Za-z0-9._-]",
			}))
			return
		}
		upd.Name = &name
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		upd.Description = &desc
	}
	g, err := h.repo.Update(r.Context(), id, upd)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("GroupNotFound", map[string]string{"id": id}))
			return
		}
		if errors.Is(err, ErrGroupNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("GroupNameConflict", map[string]string{
				"name": *upd.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GroupUpdateFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "group_update",
			ResourceType: "Group",
			ResourceRID:  g.ID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toGroupResponse(g))
}

// Delete handles DELETE /api/admin/groups/{id}.
func (h *GroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.deleteFor(w, r, chi.URLParam(r, "id"))
}

func (h *GroupHandler) deleteFor(w http.ResponseWriter, r *http.Request, id string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingGroupID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("GroupNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GroupDeleteFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "group_delete",
			ResourceType: "Group",
			ResourceRID:  id,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMembers handles GET /api/admin/groups/{id}/members.
func (h *GroupHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	h.listMembersFor(w, r, chi.URLParam(r, "id"))
}

func (h *GroupHandler) listMembersFor(w http.ResponseWriter, r *http.Request, id string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("GroupNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GroupLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	members, err := h.repo.ListMembers(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GroupMembersListFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if members == nil {
		members = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(GroupMembersResponse{Members: members})
}

// AddMember handles POST /api/admin/groups/{id}/members.
func (h *GroupHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	h.addMemberFor(w, r, chi.URLParam(r, "id"))
}

func (h *GroupHandler) addMemberFor(w http.ResponseWriter, r *http.Request, id string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	var req GroupMemberRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMemberRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingUserID", map[string]string{
			"reason": "userId is required",
		}))
		return
	}
	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("GroupNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GroupLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if err := h.repo.AddMember(r.Context(), id, req.UserID); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GroupAddMemberFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{"userId": req.UserID})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "group_add_member",
			ResourceType: "Group",
			ResourceRID:  id,
			DiffJSON:     diff,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveMember handles DELETE /api/admin/groups/{id}/members/{userId}.
func (h *GroupHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	h.removeMemberFor(w, r, chi.URLParam(r, "id"), chi.URLParam(r, "userId"))
}

func (h *GroupHandler) removeMemberFor(w http.ResponseWriter, r *http.Request, id, userID string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" || userID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingPathParameter", map[string]string{
			"reason": "id and userId are required",
		}))
		return
	}
	if err := h.repo.RemoveMember(r.Context(), id, userID); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GroupRemoveMemberFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if h.auditStore != nil {
		diff, _ := json.Marshal(map[string]string{"userId": userID})
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "group_remove_member",
			ResourceType: "Group",
			ResourceRID:  id,
			DiffJSON:     diff,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}
