package aip

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// ToolCatalogHandler exposes the /api/v2/aip/tools admin CRUD endpoints
// (US-285). The handler is gated on auth presence — the surrounding
// chi.Router applies auth middleware AND the requireAuth helper below
// rejects unauthenticated requests defensively.
//
// registry is optional. When set, every successful CreateTool /
// UpdateTool / DeleteTool call refreshes the in-process ToolRegistry so
// the next SendMessage iteration sees the catalog change without a
// process restart. invoker is captured at construction so newly added
// catalog rows can dispatch through the same FunctionExecutor wiring as
// the bootstrap-time load.
type ToolCatalogHandler struct {
	catalog  ToolCatalog
	registry *ToolRegistry
	invoker  FunctionInvoker
}

// NewToolCatalogHandler wires a handler. catalog may be nil — every
// endpoint then returns AIPToolCatalogUnavailable. registry / invoker
// may be nil so degraded test rigs that don't care about hot-reload can
// skip the registry plumbing.
func NewToolCatalogHandler(catalog ToolCatalog, registry *ToolRegistry, invoker FunctionInvoker) *ToolCatalogHandler {
	return &ToolCatalogHandler{
		catalog:  catalog,
		registry: registry,
		invoker:  invoker,
	}
}

// RegisterRoutes mounts the tools CRUD endpoints on r.
func (h *ToolCatalogHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/aip/tools", h.ListTools)
	r.Post("/api/v2/aip/tools", h.CreateTool)
	r.Get("/api/v2/aip/tools/{toolName}", h.GetTool)
	r.Put("/api/v2/aip/tools/{toolName}", h.UpdateTool)
	r.Delete("/api/v2/aip/tools/{toolName}", h.DeleteTool)
}

func (h *ToolCatalogHandler) requireAuth(w http.ResponseWriter, r *http.Request) *auth.User {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return nil
	}
	return user
}

func (h *ToolCatalogHandler) requireCatalog(w http.ResponseWriter) bool {
	if h.catalog == nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolCatalogUnavailable", map[string]string{
			"reason": "AIP tool catalog is not configured on this deployment",
		}))
		return false
	}
	return true
}

type createToolRequest struct {
	Name               string          `json:"name"`
	Description        string          `json:"description,omitempty"`
	Parameters         json.RawMessage `json:"parameters,omitempty"`
	HandlerFunctionRID string          `json:"handlerFunctionRid,omitempty"`
	Enabled            *bool           `json:"enabled,omitempty"`
}

type updateToolRequest struct {
	Description        *string          `json:"description,omitempty"`
	Parameters         *json.RawMessage `json:"parameters,omitempty"`
	HandlerFunctionRID *string          `json:"handlerFunctionRid,omitempty"`
	Enabled            *bool            `json:"enabled,omitempty"`
}

type listToolsResponse struct {
	Tools []*ToolRecord `json:"tools"`
}

// ListTools GET /api/v2/aip/tools.
func (h *ToolCatalogHandler) ListTools(w http.ResponseWriter, r *http.Request) {
	if h.requireAuth(w, r) == nil || !h.requireCatalog(w) {
		return
	}
	tools, err := h.catalog.ListTools(r.Context())
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if tools == nil {
		tools = []*ToolRecord{}
	}
	httputil.WriteJSON(w, http.StatusOK, listToolsResponse{Tools: tools})
}

// CreateTool POST /api/v2/aip/tools.
func (h *ToolCatalogHandler) CreateTool(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireCatalog(w) {
		return
	}
	var req createToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := ValidateToolName(req.Name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidToolName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rec := &ToolRecord{
		Name:               req.Name,
		Description:        req.Description,
		Parameters:         req.Parameters,
		HandlerFunctionRID: req.HandlerFunctionRID,
		Enabled:            enabled,
		CreatedBy:          user.ID,
	}
	if err := h.catalog.CreateTool(r.Context(), rec); err != nil {
		if errors.Is(err, ErrToolAlreadyExists) {
			apierror.WriteJSON(w, apierror.NewConflict("AIPToolAlreadyExists", map[string]string{
				"name": req.Name,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.catalog.GetTool(r.Context(), req.Name)
	if err != nil {
		stored = rec
	}
	h.refreshRegistry(stored)
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// GetTool GET /api/v2/aip/tools/{toolName}.
func (h *ToolCatalogHandler) GetTool(w http.ResponseWriter, r *http.Request) {
	if h.requireAuth(w, r) == nil || !h.requireCatalog(w) {
		return
	}
	name, ok := h.toolNameParam(w, r)
	if !ok {
		return
	}
	rec, err := h.catalog.GetTool(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrToolRecordNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPToolNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, rec)
}

// UpdateTool PUT /api/v2/aip/tools/{toolName}. Partial update.
func (h *ToolCatalogHandler) UpdateTool(w http.ResponseWriter, r *http.Request) {
	if h.requireAuth(w, r) == nil || !h.requireCatalog(w) {
		return
	}
	name, ok := h.toolNameParam(w, r)
	if !ok {
		return
	}
	var req updateToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := ToolUpdate{
		Description:        req.Description,
		Parameters:         req.Parameters,
		HandlerFunctionRID: req.HandlerFunctionRID,
		Enabled:            req.Enabled,
	}
	if err := h.catalog.UpdateTool(r.Context(), name, upd); err != nil {
		if errors.Is(err, ErrToolRecordNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPToolNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	rec, err := h.catalog.GetTool(r.Context(), name)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.refreshRegistry(rec)
	httputil.WriteJSON(w, http.StatusOK, rec)
}

// DeleteTool DELETE /api/v2/aip/tools/{toolName}.
func (h *ToolCatalogHandler) DeleteTool(w http.ResponseWriter, r *http.Request) {
	if h.requireAuth(w, r) == nil || !h.requireCatalog(w) {
		return
	}
	name, ok := h.toolNameParam(w, r)
	if !ok {
		return
	}
	if err := h.catalog.DeleteTool(r.Context(), name); err != nil {
		if errors.Is(err, ErrToolRecordNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPToolNotFound", map[string]string{"name": name}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if h.registry != nil {
		h.registry.Unregister(name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// toolNameParam extracts and validates the {toolName} URL segment.
func (h *ToolCatalogHandler) toolNameParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := chi.URLParam(r, "toolName")
	if err := ValidateToolName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidToolName", map[string]string{
			"reason": err.Error(),
			"name":   name,
		}))
		return "", false
	}
	return name, true
}

// refreshRegistry re-registers (or unregisters when disabled) rec in the
// live ToolRegistry so the next SendMessage iteration sees the catalog
// change without a process restart. No-op when no registry is wired.
func (h *ToolCatalogHandler) refreshRegistry(rec *ToolRecord) {
	if h.registry == nil || rec == nil {
		return
	}
	if rec.Enabled {
		h.registry.Register(NewFunctionToolHandler(rec, h.invoker))
		return
	}
	h.registry.Unregister(rec.Name)
}
