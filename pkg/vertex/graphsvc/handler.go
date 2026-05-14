package graphsvc

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// Handler serves /api/vertex/v1/graphs/* under chi. It is composed over a
// graphsvc.Repo (graphs + version history) and a TemplateStore (save-as-
// template). Both dependencies are interfaces so PG implementations and
// in-memory test fakes plug into the same handler unchanged.
type Handler struct {
	repo      Repo
	templates TemplateStore
}

// NewHandler wires a Handler over a Repo + TemplateStore. Either may be nil
// in tests; nil deps surface to callers as 500 RepoNotConfigured rather than
// panicking.
func NewHandler(repo Repo, templates TemplateStore) *Handler {
	return &Handler{repo: repo, templates: templates}
}

// RegisterRoutes mounts all VTX-009 endpoints on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/vertex/v1/graphs", h.create)
	r.Get("/api/vertex/v1/graphs/{rid}", h.get)
	r.Put("/api/vertex/v1/graphs/{rid}", h.update)
	r.Patch("/api/vertex/v1/graphs/{rid}/layout", h.patchLayout)
	r.Post("/api/vertex/v1/graphs/{rid}/duplicate", h.duplicate)
	r.Post("/api/vertex/v1/graphs/{rid}/save-as-template", h.saveAsTemplate)
	r.Get("/api/vertex/v1/graphs/{rid}/history", h.history)
	r.Get("/api/vertex/v1/graphs/{rid}/versions/{version}", h.getVersion)
}

// createRequest is the body shape for POST /api/vertex/v1/graphs. Payload is
// captured as json.RawMessage so we forward exactly what the client sent —
// schema validation belongs to VTX-011.
type createRequest struct {
	OntologyRID string          `json:"ontologyRid"`
	Name        string          `json:"name"`
	Versioned   *bool           `json:"versioned,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedBy   string          `json:"createdBy,omitempty"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	var req createRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJSON", map[string]string{"error": err.Error()}))
		return
	}
	if strings.TrimSpace(req.OntologyRID) == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingOntologyRid", nil))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingName", nil))
		return
	}
	versioned := true
	if req.Versioned != nil {
		versioned = *req.Versioned
	}
	g, err := h.repo.Create(r.Context(), req.OntologyRID, req.Name, req.CreatedBy, req.Payload, versioned)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateGraphFailed", map[string]string{"error": err.Error()}))
		return
	}
	writeGraph(w, http.StatusCreated, g)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	g, err := h.repo.Get(r.Context(), ridStr)
	if err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	writeGraph(w, http.StatusOK, g)
}

type updateRequest struct {
	Payload json.RawMessage `json:"payload"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	var req updateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJSON", map[string]string{"error": err.Error()}))
		return
	}
	if len(req.Payload) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingPayload", nil))
		return
	}
	g, err := h.repo.Update(r.Context(), ridStr, req.Payload)
	if err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	writeGraph(w, http.StatusOK, g)
}

type patchLayoutRequest struct {
	Positions json.RawMessage `json:"positions"`
}

func (h *Handler) patchLayout(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	var req patchLayoutRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJSON", map[string]string{"error": err.Error()}))
		return
	}
	if len(req.Positions) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingPositions", nil))
		return
	}
	if err := h.repo.UpdateLayout(r.Context(), ridStr, req.Positions); err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"rid": ridStr})
}

func (h *Handler) duplicate(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	g, err := h.repo.Duplicate(r.Context(), ridStr)
	if err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	writeGraph(w, http.StatusCreated, g)
}

type saveAsTemplateRequest struct {
	Name                string   `json:"name"`
	ParameterizedFields []string `json:"parameterizedFields,omitempty"`
}

func (h *Handler) saveAsTemplate(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil || h.templates == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	var req saveAsTemplateRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJSON", map[string]string{"error": err.Error()}))
		return
	}
	src, err := h.repo.Get(r.Context(), ridStr)
	if err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = src.Name + " (template)"
	}
	tmpl := &GraphTemplate{
		RID:                 rid.New("vertex", "main", "graph-template"),
		SourceGraphRID:      src.RID,
		Name:                name,
		Payload:             src.Payload,
		ParameterizedFields: req.ParameterizedFields,
		Parameters:          json.RawMessage(`{}`),
		CreatedBy:           src.CreatedBy,
		CreatedAt:           time.Now().UTC(),
	}
	if err := h.templates.Create(r.Context(), tmpl); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateTemplateFailed", map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{
		"rid":                 tmpl.RID,
		"sourceGraphRid":      tmpl.SourceGraphRID,
		"name":                tmpl.Name,
		"parameterizedFields": tmpl.ParameterizedFields,
	})
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	if _, err := h.repo.Get(r.Context(), ridStr); err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	versions, err := h.repo.ListVersions(r.Context(), ridStr)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListVersionsFailed", map[string]string{"error": err.Error()}))
		return
	}
	out := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		out = append(out, map[string]any{
			"version":   v.Version,
			"createdAt": v.CreatedAt,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"rid":      ridStr,
		"versions": out,
	})
}

func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 1 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidVersion", map[string]string{"version": versionStr}))
		return
	}
	g, err := h.repo.GetVersion(r.Context(), ridStr, version)
	if err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	writeGraph(w, http.StatusOK, g)
}

// writeGraph encodes a Graph in the wire shape callers expect. Payload is
// emitted as a JSON value (not a string) so clients can index into it
// without re-parsing.
func writeGraph(w http.ResponseWriter, status int, g *Graph) {
	payload := json.RawMessage(g.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage(`null`)
	}
	httputil.WriteJSON(w, status, map[string]any{
		"rid":         g.RID,
		"ontologyRid": g.OntologyRID,
		"name":        g.Name,
		"version":     g.Version,
		"versioned":   g.Versioned,
		"payload":     payload,
		"createdBy":   g.CreatedBy,
		"createdAt":   g.CreatedAt,
		"updatedAt":   g.UpdatedAt,
	})
}

// writeRepoError maps repo sentinel errors to the right HTTP status + APIError
// name. Falls through to 500 for unknown errors.
func writeRepoError(w http.ResponseWriter, err error, ridStr string) {
	switch {
	case errors.Is(err, ErrGraphNotFound):
		apierror.WriteJSON(w, apierror.NewNotFound("GraphNotFound", map[string]string{"rid": ridStr}))
	case errors.Is(err, ErrVersionNotFound):
		apierror.WriteJSON(w, apierror.NewNotFound("GraphVersionNotFound", map[string]string{"rid": ridStr}))
	case errors.Is(err, ErrInvalidPositions):
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingPositions", nil))
	default:
		apierror.WriteJSON(w, apierror.NewInternal("GraphRepoError", map[string]string{"error": err.Error()}))
	}
}
