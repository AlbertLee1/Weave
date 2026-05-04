package tenants

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler implements the /api/admin/tenant-quotas/* admin endpoints.
//
//	GET    /api/admin/tenant-quotas
//	POST   /api/admin/tenant-quotas
//	GET    /api/admin/tenant-quotas/{tenant}
//	PUT    /api/admin/tenant-quotas/{tenant}
//	DELETE /api/admin/tenant-quotas/{tenant}
//
// The router gates this group on PermUserManage; the auth nil-check
// below is defence-in-depth so test routers without RequirePermission
// still refuse unauthenticated callers.
type Handler struct {
	store   Store
	manager *Manager
}

// NewHandler returns an admin handler reading / writing through store.
// Pass the same Manager the Middleware uses so writes invalidate cached
// limiters via mgr.Reload(). nil Manager is safe — admin writes still
// land but cached limiters won't be invalidated until next restart.
func NewHandler(store Store, mgr *Manager) *Handler {
	return &Handler{store: store, manager: mgr}
}

// RegisterRoutes mounts every admin endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/admin/tenant-quotas", h.List)
	r.Post("/api/admin/tenant-quotas", h.Create)
	r.Get("/api/admin/tenant-quotas/{tenant}", h.Get)
	r.Put("/api/admin/tenant-quotas/{tenant}", h.Update)
	r.Delete("/api/admin/tenant-quotas/{tenant}", h.Delete)

	// US-438 — usage + alerts surfaces. Mounted unconditionally so the
	// SPA can render the panel even when only quotas are wired; the
	// handlers degrade to 503 Unavailable when no UsageStore is present.
	r.Get("/api/admin/tenant-usage", h.ListUsage)
	r.Get("/api/admin/tenant-usage/{tenant}", h.GetUsage)
	r.Post("/api/admin/tenant-usage/{tenant}/{metric}", h.AddUsage)
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if auth.UserFromContext(r.Context()) == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return false
	}
	return true
}

func (h *Handler) requireStore(w http.ResponseWriter) bool {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotasUnavailable", map[string]string{
			"reason": "tenant quotas are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createQuotaRequest struct {
	Tenant      string  `json:"tenant"`
	MaxObjects  int64   `json:"maxObjects"`
	MaxStorage  int64   `json:"maxStorage"`
	MaxQPS      float64 `json:"maxQPS"`
	Burst       int     `json:"burst"`
	Description string  `json:"description"`
}

type updateQuotaRequest struct {
	MaxObjects  *int64   `json:"maxObjects,omitempty"`
	MaxStorage  *int64   `json:"maxStorage,omitempty"`
	MaxQPS      *float64 `json:"maxQPS,omitempty"`
	Burst       *int     `json:"burst,omitempty"`
	Description *string  `json:"description,omitempty"`
}

type listResponse struct {
	Quotas []*Quota `json:"quotas"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	rows, err := h.store.ListQuotas(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotaListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Quota{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{Quotas: rows})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	var req createQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := ValidateTenant(req.Tenant); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTenant", map[string]string{
			"reason": err.Error(),
			"tenant": req.Tenant,
		}))
		return
	}
	now := time.Now().UTC()
	q := &Quota{
		Tenant:      req.Tenant,
		MaxObjects:  req.MaxObjects,
		MaxStorage:  req.MaxStorage,
		MaxQPS:      req.MaxQPS,
		Burst:       req.Burst,
		Description: req.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.store.CreateQuota(r.Context(), q); err != nil {
		if errors.Is(err, ErrQuotaAlreadyExists) {
			apierror.WriteJSON(w, apierror.NewConflict("TenantQuotaAlreadyExists", map[string]string{
				"tenant": req.Tenant,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotaCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.GetQuota(r.Context(), req.Tenant)
	if err != nil {
		stored = q
	}
	if h.manager != nil {
		h.manager.Reload()
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	tenant := chi.URLParam(r, "tenant")
	if err := ValidateTenant(tenant); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTenant", map[string]string{
			"reason": err.Error(),
			"tenant": tenant,
		}))
		return
	}
	q, err := h.store.GetQuota(r.Context(), tenant)
	if err != nil {
		if errors.Is(err, ErrQuotaNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TenantQuotaNotFound", map[string]string{
				"tenant": tenant,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotaLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, q)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	tenant := chi.URLParam(r, "tenant")
	if err := ValidateTenant(tenant); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTenant", map[string]string{
			"reason": err.Error(),
			"tenant": tenant,
		}))
		return
	}
	var req updateQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := QuotaUpdate{
		MaxObjects:  req.MaxObjects,
		MaxStorage:  req.MaxStorage,
		MaxQPS:      req.MaxQPS,
		Burst:       req.Burst,
		Description: req.Description,
	}
	if err := h.store.UpdateQuota(r.Context(), tenant, upd); err != nil {
		if errors.Is(err, ErrQuotaNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TenantQuotaNotFound", map[string]string{
				"tenant": tenant,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotaUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	q, err := h.store.GetQuota(r.Context(), tenant)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotaLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if h.manager != nil {
		h.manager.Reload()
	}
	httputil.WriteJSON(w, http.StatusOK, q)
}

// requireUsage is the US-438 counterpart of requireStore — gates the
// /api/admin/tenant-usage handlers when the manager has no UsageStore
// wired (degraded-mode boots without PG).
func (h *Handler) requireUsage(w http.ResponseWriter) bool {
	if h.manager == nil || h.manager.usage == nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantUsageUnavailable", map[string]string{
			"reason": "tenant usage tracking is not configured on this deployment",
		}))
		return false
	}
	return true
}

// usageListResponse and usageRowResponse — wire-format envelopes so the
// SPA can rely on a stable {usage: [...]} shape independent of how the
// manager wires its underlying stores.
type usageListResponse struct {
	Usage []*MonthlyUsage `json:"usage"`
}

type addUsageRequest struct {
	Delta int64 `json:"delta"`
}

type addUsageResponse struct {
	Usage  []*MonthlyUsage `json:"usage"`
	Fired  []Alert         `json:"firedAlerts"`
}

// ListUsage renders the current calendar month's usage rows for every
// configured tenant — one row per (tenant, metric).
func (h *Handler) ListUsage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireUsage(w) {
		return
	}
	rows, err := h.manager.usage.ListMonthlyUsage(r.Context(), MonthStart(h.manager.Now()))
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantUsageListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*MonthlyUsage{}
	}
	httputil.WriteJSON(w, http.StatusOK, usageListResponse{Usage: rows})
}

// GetUsage renders one tenant's per-metric usage rows for the current
// calendar month.
func (h *Handler) GetUsage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireUsage(w) {
		return
	}
	tenant := chi.URLParam(r, "tenant")
	if err := ValidateTenant(tenant); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTenant", map[string]string{
			"reason": err.Error(),
			"tenant": tenant,
		}))
		return
	}
	rows, err := h.manager.MonthlyUsageFor(r.Context(), tenant)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantUsageLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*MonthlyUsage{}
	}
	httputil.WriteJSON(w, http.StatusOK, usageListResponse{Usage: rows})
}

// AddUsage atomically increments the (tenant, metric) counter by
// `delta`. The handler returns the post-increment usage rows AND the
// alerts that fired on this call (if any) so the SPA can surface a
// "warning sent" badge on operator-driven adjustments.
func (h *Handler) AddUsage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireUsage(w) {
		return
	}
	tenant := chi.URLParam(r, "tenant")
	if err := ValidateTenant(tenant); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTenant", map[string]string{
			"reason": err.Error(),
			"tenant": tenant,
		}))
		return
	}
	metric := chi.URLParam(r, "metric")
	if !IsValidMetric(metric) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidMetric", map[string]string{
			"metric": metric,
			"reason": "must be one of: objects, storage, requests",
		}))
		return
	}
	var req addUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	fired, err := h.manager.RecordUsage(r.Context(), tenant, metric, req.Delta)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantUsageRecordFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	rows, err := h.manager.MonthlyUsageFor(r.Context(), tenant)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("TenantUsageLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*MonthlyUsage{}
	}
	if fired == nil {
		fired = []Alert{}
	}
	httputil.WriteJSON(w, http.StatusOK, addUsageResponse{Usage: rows, Fired: fired})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.requireAuth(w, r) || !h.requireStore(w) {
		return
	}
	tenant := chi.URLParam(r, "tenant")
	if err := ValidateTenant(tenant); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTenant", map[string]string{
			"reason": err.Error(),
			"tenant": tenant,
		}))
		return
	}
	if err := h.store.DeleteQuota(r.Context(), tenant); err != nil {
		if errors.Is(err, ErrQuotaNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TenantQuotaNotFound", map[string]string{
				"tenant": tenant,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("TenantQuotaDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if h.manager != nil {
		h.manager.Reload()
	}
	w.WriteHeader(http.StatusNoContent)
}
