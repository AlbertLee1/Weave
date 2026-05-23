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

// ServiceAccountRequest is the POST /api/admin/service-accounts body.
type ServiceAccountRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	ExpiresAt   string   `json:"expiresAt,omitempty"`
}

// ServiceAccountUpdateRequest is the PATCH body.
//
// All fields are pointer-typed so a caller's omit (nil) is distinguishable
// from an explicit clear (non-nil, zero value). Mirrors the pointer-field
// convention used by UpdateLinkTypeRequest and UpdateActionTypeRequest.
//
//   - Description: nil = preserve, non-nil = replace
//   - Scopes:      nil = preserve, non-nil = replace with supplied slice
//   - ExpiresAt:   nil = preserve, empty-string = clear expiry, RFC3339 = set
type ServiceAccountUpdateRequest struct {
	Description *string   `json:"description,omitempty"`
	Scopes      *[]string `json:"scopes,omitempty"`
	ExpiresAt   *string   `json:"expiresAt,omitempty"`
}

// ServiceAccountResponse is the wire shape returned by every CRUD endpoint.
type ServiceAccountResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	OwnerUserID string     `json:"ownerUserId"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	DisabledAt  *time.Time `json:"disabledAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// ServiceAccountListResponse is the wire shape returned by GET /api/admin/service-accounts.
type ServiceAccountListResponse struct {
	ServiceAccounts []ServiceAccountResponse `json:"serviceAccounts"`
}

func toServiceAccountResponse(sa *ServiceAccount) ServiceAccountResponse {
	scopes := sa.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return ServiceAccountResponse{
		ID:          sa.ID,
		Name:        sa.Name,
		Description: sa.Description,
		OwnerUserID: sa.OwnerUserID,
		Scopes:      scopes,
		ExpiresAt:   sa.ExpiresAt,
		DisabledAt:  sa.DisabledAt,
		CreatedAt:   sa.CreatedAt,
		UpdatedAt:   sa.UpdatedAt,
	}
}

// ServiceAccountHandler implements the admin REST endpoints for service accounts.
type ServiceAccountHandler struct {
	repo       ServiceAccountRepository
	auditStore audit.Store
}

// NewServiceAccountHandler constructs the admin handler around a repository.
// auditStore may be nil to disable audit logging.
func NewServiceAccountHandler(repo ServiceAccountRepository, auditStore audit.Store) *ServiceAccountHandler {
	return &ServiceAccountHandler{repo: repo, auditStore: auditStore}
}

// RegisterRoutes mounts the CRUD endpoints under /api/admin/service-accounts.
// Callers should wrap this with RequirePermission(PermUserManage) at
// registration time so only admin-level callers can manage service accounts.
func (h *ServiceAccountHandler) RegisterRoutes(r chi.Router) {
	r.Post("/api/admin/service-accounts", h.Create)
	r.Get("/api/admin/service-accounts", h.List)
	r.Get("/api/admin/service-accounts/{id}", h.Get)
	r.Patch("/api/admin/service-accounts/{id}", h.Update)
	r.Delete("/api/admin/service-accounts/{id}", h.Delete)
}

// Create handles POST /api/admin/service-accounts.
func (h *ServiceAccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}

	var req ServiceAccountRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidServiceAccountRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := ValidateServiceAccountName(req.Name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidServiceAccountName", map[string]string{
			"reason": "name must be 1-128 chars, starting with alphanumeric, containing only [A-Za-z0-9._-]",
		}))
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidExpiresAt", map[string]string{
				"reason": "expiresAt must be RFC3339",
			}))
			return
		}
		expiresAt = &t
	}

	scopes := req.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	sa := &ServiceAccount{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		OwnerUserID: u.ID,
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
	}
	if err := h.repo.Create(r.Context(), sa); err != nil {
		if errors.Is(err, ErrServiceAccountNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("ServiceAccountNameConflict", map[string]string{
				"name": req.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ServiceAccountCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "service_account_create",
			ResourceType: "ServiceAccount",
			ResourceRID:  sa.ID,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toServiceAccountResponse(sa))
}

// List handles GET /api/admin/service-accounts.
func (h *ServiceAccountHandler) List(w http.ResponseWriter, r *http.Request) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}

	rows, err := h.repo.ListActive(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ServiceAccountListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := ServiceAccountListResponse{ServiceAccounts: make([]ServiceAccountResponse, 0, len(rows))}
	for _, sa := range rows {
		out.ServiceAccounts = append(out.ServiceAccounts, toServiceAccountResponse(sa))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// Get handles GET /api/admin/service-accounts/{id}.
func (h *ServiceAccountHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.getFor(w, r, id)
}

func (h *ServiceAccountHandler) getFor(w http.ResponseWriter, r *http.Request, id string) {
	if u := UserFromContext(r.Context()); u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingServiceAccountID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	sa, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrServiceAccountNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ServiceAccountNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ServiceAccountLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toServiceAccountResponse(sa))
}

// Update handles PATCH /api/admin/service-accounts/{id}.
func (h *ServiceAccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.updateFor(w, r, id)
}

func (h *ServiceAccountHandler) updateFor(w http.ResponseWriter, r *http.Request, id string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingServiceAccountID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	var req ServiceAccountUpdateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidServiceAccountUpdate", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	upd := ServiceAccountUpdate{}
	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		upd.Description = &trimmed
	}
	if req.Scopes != nil {
		scopes := *req.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		upd.Scopes = &scopes
	}
	if req.ExpiresAt != nil {
		raw := *req.ExpiresAt
		if raw == "" {
			var clear *time.Time
			upd.ExpiresAt = &clear
		} else {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidExpiresAt", map[string]string{
					"reason": "expiresAt must be RFC3339 or empty string to clear",
				}))
				return
			}
			tp := &t
			upd.ExpiresAt = &tp
		}
	}

	sa, err := h.repo.Update(r.Context(), id, upd)
	if err != nil {
		if errors.Is(err, ErrServiceAccountNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ServiceAccountNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ServiceAccountUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "service_account_update",
			ResourceType: "ServiceAccount",
			ResourceRID:  sa.ID,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toServiceAccountResponse(sa))
}

// Delete handles DELETE /api/admin/service-accounts/{id}. Soft-disables the
// service account; repeated deletes are idempotent.
func (h *ServiceAccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.deleteFor(w, r, id)
}

func (h *ServiceAccountHandler) deleteFor(w http.ResponseWriter, r *http.Request, id string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingServiceAccountID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		if errors.Is(err, ErrServiceAccountNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ServiceAccountNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ServiceAccountLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := h.repo.Disable(r.Context(), id); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ServiceAccountDisableFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "service_account_disable",
			ResourceType: "ServiceAccount",
			ResourceRID:  id,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}
