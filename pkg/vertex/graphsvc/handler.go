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
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// Handler serves /api/vertex/v1/graphs/* under chi. It is composed over a
// graphsvc.Repo (graphs + version history) and a TemplateStore (save-as-
// template). Both dependencies are interfaces so PG implementations and
// in-memory test fakes plug into the same handler unchanged.
//
// The optional PayloadValidator (set via SetPayloadValidator, VTX-011) gates
// POST + PUT writes against the embedded JSON Schema (400) and OMS reference
// existence checks (422). When unset, writes are forwarded unchanged — the
// degraded-mode boot path keeps that on so the routes stay discoverable.
type Handler struct {
	repo       Repo
	templates  TemplateStore
	validator  *PayloadValidator
	shareLinks ShareLinkStore
}

// NewHandler wires a Handler over a Repo + TemplateStore. Either may be nil
// in tests; nil deps surface to callers as 500 RepoNotConfigured rather than
// panicking.
func NewHandler(repo Repo, templates TemplateStore) *Handler {
	return &Handler{repo: repo, templates: templates}
}

// SetPayloadValidator installs (or clears, when v == nil) the structural +
// reference validator the create / update handlers run before delegating to
// the repo. cmd/server wires this from *oms.PGRepository when an OMS pool is
// available.
func (h *Handler) SetPayloadValidator(v *PayloadValidator) {
	h.validator = v
}

// SetShareLinkStore installs (or clears, when s == nil) the VTX-013 share-link
// store. When unset, /share-links endpoints surface 500 ShareLinksUnavailable
// so degraded-mode boots keep the route discoverable.
func (h *Handler) SetShareLinkStore(s ShareLinkStore) {
	h.shareLinks = s
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
	r.Post("/api/vertex/v1/templates/{rid}/instantiate", h.instantiate)
	// VTX-013: share link surface.
	r.Post("/api/vertex/v1/graphs/{rid}/share-links", h.createShareLink)
	r.Delete("/api/vertex/v1/share-links/{token}", h.revokeShareLink)
	r.Get("/api/vertex/v1/share-links/{token}/graph", h.getViaShareLink)
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
	if h.validator != nil {
		if err := h.validator.Validate(r.Context(), req.Payload); err != nil {
			writePayloadValidationError(w, err)
			return
		}
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
	if !canReadGraph(r, g) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("GraphReadForbidden",
			map[string]string{"rid": ridStr}))
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
	if h.validator != nil {
		if err := h.validator.Validate(r.Context(), req.Payload); err != nil {
			writePayloadValidationError(w, err)
			return
		}
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

// instantiateRequest is the body shape for POST
// /api/vertex/v1/templates/{rid}/instantiate. Parameters are captured as raw
// JSON so values keep their type (strings, arrays, objects all flow through
// to the Instantiate helper unchanged).
type instantiateRequest struct {
	Parameters map[string]json.RawMessage `json:"parameters,omitempty"`
}

func (h *Handler) instantiate(w http.ResponseWriter, r *http.Request) {
	if h.templates == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	tmpl, err := h.templates.Get(r.Context(), ridStr)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("TemplateNotFound", map[string]string{"rid": ridStr}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetTemplateFailed", map[string]string{"error": err.Error()}))
		return
	}
	var req instantiateRequest
	if r.ContentLength != 0 {
		if err := httputil.ReadJSON(r, &req); err != nil {
			apierror.WriteJSON(w, apierror.NewBadRequest("InvalidJSON", map[string]string{"error": err.Error()}))
			return
		}
	}
	if req.Parameters == nil {
		req.Parameters = map[string]json.RawMessage{}
	}
	instantiated, err := Instantiate(tmpl.Payload, tmpl.ParameterizedFields, req.Parameters)
	if err != nil {
		var iferr *InvalidTemplateFieldError
		if errors.As(err, &iferr) {
			apierror.WriteJSON(w, apierror.NewBadRequest("InvalidTemplateField", map[string]string{
				"field":  iferr.Field,
				"reason": iferr.Reason,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("InstantiateFailed", map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"sourceTemplateRid": tmpl.RID,
		"sourceGraphRid":    tmpl.SourceGraphRID,
		"name":              tmpl.Name,
		"payload":           json.RawMessage(instantiated),
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

// canReadGraph returns true if the caller is allowed to read g via the
// authenticated principal alone (i.e. ignoring share links).
//
// VTX-013 simple owner-based ACL:
//   - Ownerless graphs (CreatedBy == "") are public — preserves legacy
//     pre-VTX-013 test fixtures and degraded-mode boots.
//   - Otherwise the caller's user.ID must match CreatedBy, OR the caller
//     must hold the "admin" role.
//
// This is intentionally minimal: project-level RBAC + marking-based RLS live
// in pkg/auth / pkg/security and are not in scope for VTX-013, which focuses
// on the share-link masking flow.
func canReadGraph(r *http.Request, g *Graph) bool {
	if g.CreatedBy == "" {
		return true
	}
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return false
	}
	if u.ID == g.CreatedBy {
		return true
	}
	for _, role := range u.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

// createShareLink mints a new share link for a graph. Owner-only — non-owners
// get 403. The opaque random token is returned in the 201 response body; the
// caller surfaces it in a URL `/api/vertex/v1/share-links/{token}/graph`.
func (h *Handler) createShareLink(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	if h.shareLinks == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ShareLinksUnavailable",
			map[string]string{"reason": "share link store is not configured"}))
		return
	}
	ridStr := chi.URLParam(r, "rid")
	g, err := h.repo.Get(r.Context(), ridStr)
	if err != nil {
		writeRepoError(w, err, ridStr)
		return
	}
	u := auth.UserFromContext(r.Context())
	if !canManageShareLinks(u, g) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("ShareLinkManageForbidden",
			map[string]string{"rid": ridStr}))
		return
	}
	token, err := newShareToken()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ShareLinkTokenFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	createdBy := g.CreatedBy
	if u != nil && u.ID != "" {
		createdBy = u.ID
	}
	link := &ShareLink{
		Token:     token,
		GraphRID:  g.RID,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.shareLinks.Create(r.Context(), link); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateShareLinkFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]any{
		"token":     link.Token,
		"graphRid":  link.GraphRID,
		"createdBy": link.CreatedBy,
		"createdAt": link.CreatedAt,
	})
}

// revokeShareLink marks a share link revoked. Owner-only — only the user
// who originally minted the link (or an admin) can revoke. Already-revoked
// links return 204 idempotently so retries are safe.
func (h *Handler) revokeShareLink(w http.ResponseWriter, r *http.Request) {
	if h.shareLinks == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ShareLinksUnavailable",
			map[string]string{"reason": "share link store is not configured"}))
		return
	}
	token := chi.URLParam(r, "token")
	link, err := h.shareLinks.Get(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrShareLinkNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ShareLinkNotFound",
				map[string]string{"token": token}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetShareLinkFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	u := auth.UserFromContext(r.Context())
	if !canRevokeShareLink(u, link) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("ShareLinkRevokeForbidden",
			map[string]string{"token": token}))
		return
	}
	if err := h.shareLinks.Revoke(r.Context(), token); err != nil {
		if errors.Is(err, ErrShareLinkNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ShareLinkNotFound",
				map[string]string{"token": token}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("RevokeShareLinkFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getViaShareLink fetches a graph by share-link token. Unknown token → 404;
// revoked token → 410 Gone (distinct so the recipient sees the owner shut it
// down, not that the link never existed); valid token → 200 with the graph
// structure but layer property values masked to "***".
func (h *Handler) getViaShareLink(w http.ResponseWriter, r *http.Request) {
	if h.shareLinks == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ShareLinksUnavailable",
			map[string]string{"reason": "share link store is not configured"}))
		return
	}
	if h.repo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("RepoNotConfigured", nil))
		return
	}
	token := chi.URLParam(r, "token")
	link, err := h.shareLinks.Get(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrShareLinkNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ShareLinkNotFound",
				map[string]string{"token": token}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetShareLinkFailed",
			map[string]string{"error": err.Error()}))
		return
	}
	if link.Revoked {
		apierror.WriteJSON(w, apierror.NewGone("ShareLinkRevoked",
			map[string]string{"token": token, "reason": "share link has been revoked"}))
		return
	}
	g, err := h.repo.Get(r.Context(), link.GraphRID)
	if err != nil {
		writeRepoError(w, err, link.GraphRID)
		return
	}
	masked := cloneGraph(g)
	masked.Payload = maskLayerPropertyValues(masked.Payload)
	writeGraph(w, http.StatusOK, masked)
}

// canManageShareLinks decides whether u may mint a share link for g. Owner or
// admin role. Nil user is rejected; ownerless graphs are NOT auto-public for
// share-link management (otherwise anonymous callers could spam new tokens).
func canManageShareLinks(u *auth.User, g *Graph) bool {
	if u == nil {
		return false
	}
	if g.CreatedBy != "" && u.ID == g.CreatedBy {
		return true
	}
	for _, role := range u.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

// canRevokeShareLink decides whether u may revoke link. The link's CreatedBy
// is the owner-at-mint-time; admins can override.
func canRevokeShareLink(u *auth.User, link *ShareLink) bool {
	if u == nil {
		return false
	}
	if link.CreatedBy != "" && u.ID == link.CreatedBy {
		return true
	}
	for _, role := range u.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
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

// writePayloadValidationError translates the typed VTX-011 PayloadValidator
// error into the right APIError shape. Schema (400) failures use INVALID_ARGUMENT
// so existing clients keep their 400 handling; reference (422) failures use
// WEAVE_VALIDATION_SCHEMA so they slot into the same 422 surface as the
// ActionType parameter validator. Anything else (network, repo) falls through
// to 500.
func writePayloadValidationError(w http.ResponseWriter, err error) {
	var pve *PayloadValidationError
	if !errors.As(err, &pve) {
		apierror.WriteJSON(w, apierror.NewInternal("PayloadValidatorError", map[string]string{"error": err.Error()}))
		return
	}
	params := map[string]string{"reason": pve.Reason}
	if pve.Field != "" {
		params["field"] = pve.Field
	}
	switch pve.Code {
	case PayloadCodeUnknownObjectType, PayloadCodeUnknownLinkType:
		apierror.WriteJSON(w, apierror.NewValidationSchema(payloadErrorName(pve.Code), params))
	default:
		apierror.WriteJSON(w, apierror.NewInvalidParameter(payloadErrorName(pve.Code), params))
	}
}

// payloadErrorName maps PayloadCode* discriminators to wire ErrorName values.
// Stable strings so SDK consumers can switch on them without parsing reasons.
func payloadErrorName(code string) string {
	switch code {
	case PayloadCodeUnknownObjectType:
		return "UnknownObjectType"
	case PayloadCodeUnknownLinkType:
		return "UnknownLinkType"
	default:
		return "InvalidGraphPayload"
	}
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
