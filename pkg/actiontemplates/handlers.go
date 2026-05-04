package actiontemplates

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// TeammateResolver resolves the set of user ids who share at least
// one auth.Group with the supplied caller. Used by the handler to
// build a Visibility before delegating to Store. nil resolver means
// "single-user deployment / no group memberships wired" — TEAM-scoped
// rows from other owners stay invisible, mirroring the degraded-mode
// shape used elsewhere in the codebase.
type TeammateResolver interface {
	Teammates(ctx context.Context, callerID string) ([]string, error)
}

// Handler implements the /api/v2/action-templates/* CRUD endpoints.
//
//	GET    /api/v2/action-templates?ontology=&actionType=
//	POST   /api/v2/action-templates
//	GET    /api/v2/action-templates/{id}
//	PUT    /api/v2/action-templates/{id}
//	DELETE /api/v2/action-templates/{id}
//
// Read endpoints honour the row's Scope (PRIVATE | TEAM | PUBLIC);
// write endpoints (POST/PUT/DELETE) are always owner-only. Cross-
// owner private lookups surface as 404 ActionTemplateNotFound to
// avoid leaking ids.
type Handler struct {
	store     Store
	teammates TeammateResolver
}

// NewHandler constructs a Handler. nil store leaves every endpoint
// reporting ActionTemplatesUnavailable so degraded-mode test routers
// (no PG) can keep their /api/v2 prefix mounted without 500s.
func NewHandler(store Store) *Handler { return &Handler{store: store} }

// WithTeammateResolver wires the resolver responsible for translating
// a caller id into the set of group-mates used to evaluate
// Scope=TEAM. Returns the receiver for fluent wiring at boot.
func (h *Handler) WithTeammateResolver(r TeammateResolver) *Handler {
	h.teammates = r
	return h
}

// RegisterRoutes mounts every endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/action-templates", h.List)
	r.Post("/api/v2/action-templates", h.Create)
	r.Get("/api/v2/action-templates/{id}", h.Get)
	r.Put("/api/v2/action-templates/{id}", h.Update)
	r.Delete("/api/v2/action-templates/{id}", h.Delete)
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) *auth.User {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return nil
	}
	return user
}

func (h *Handler) requireStore(w http.ResponseWriter) bool {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplatesUnavailable", map[string]string{
			"reason": "action templates are not configured on this deployment",
		}))
		return false
	}
	return true
}

// visibilityFor builds the per-request Visibility — the caller is
// always self-visible; teammates are resolved through the optional
// TeammateResolver. A resolver error degrades to "no teammates" so a
// transient lookup failure can't widen visibility but also can't
// 5xx an otherwise-healthy List.
func (h *Handler) visibilityFor(ctx context.Context, callerID string) Visibility {
	vis := Visibility{CallerID: callerID}
	if h.teammates == nil {
		return vis
	}
	mates, err := h.teammates.Teammates(ctx, callerID)
	if err != nil {
		return vis
	}
	vis.Teammates = mates
	return vis
}

type createRequest struct {
	Name       string          `json:"name"`
	Ontology   string          `json:"ontology"`
	ActionType string          `json:"actionType"`
	Shared     *bool           `json:"shared,omitempty"`
	Scope      string          `json:"scope,omitempty"`
	Parameters json.RawMessage `json:"parameters"`
}

type updateRequest struct {
	Name       *string          `json:"name,omitempty"`
	Parameters *json.RawMessage `json:"parameters,omitempty"`
	Shared     *bool            `json:"shared,omitempty"`
	Scope      *string          `json:"scope,omitempty"`
}

type listResponse struct {
	ActionTemplates []*Template `json:"actionTemplates"`
}

// resolveScope picks the canonical scope from the wire shape: an
// explicit Scope wins, otherwise the legacy boolean is mapped, else
// PRIVATE. Wire-format errors surface as InvalidActionTemplateScope.
func resolveScope(rawScope string, shared *bool) (string, error) {
	if strings.TrimSpace(rawScope) != "" {
		return NormaliseScope(rawScope)
	}
	if shared != nil {
		return ScopeFromShared(*shared), nil
	}
	return ScopePrivate, nil
}

// Create POST /api/v2/action-templates.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := ValidateName(name); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidActionTemplateName", map[string]string{
			"reason": err.Error(),
			"name":   req.Name,
		}))
		return
	}
	if err := ValidateScope(req.Ontology, req.ActionType); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidActionTemplateScope", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	scope, err := resolveScope(req.Scope, req.Shared)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidActionTemplateScope", map[string]string{
			"reason": err.Error(),
			"scope":  req.Scope,
		}))
		return
	}
	params := req.Parameters
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	row := &Template{
		ID:         newTemplateID(),
		Name:       name,
		Ontology:   req.Ontology,
		ActionType: req.ActionType,
		CreatedBy:  user.ID,
		Scope:      scope,
		Shared:     SharedFromScope(scope),
		Parameters: params,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := h.store.Create(r.Context(), row); err != nil {
		if errors.Is(err, ErrNameConflict) {
			apierror.WriteJSON(w, apierror.NewConflict("ActionTemplateNameConflict", map[string]string{
				"name":       name,
				"actionType": req.ActionType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplateCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	vis := h.visibilityFor(r.Context(), user.ID)
	stored, err := h.store.Get(r.Context(), row.ID, vis)
	if err != nil {
		stored = row
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// List GET /api/v2/action-templates?ontology=&actionType=.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	ontology := r.URL.Query().Get("ontology")
	actionType := r.URL.Query().Get("actionType")
	vis := h.visibilityFor(r.Context(), user.ID)
	rows, err := h.store.List(r.Context(), vis, ontology, actionType)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplateListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []*Template{}
	}
	httputil.WriteJSON(w, http.StatusOK, listResponse{ActionTemplates: rows})
}

// Get GET /api/v2/action-templates/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	vis := h.visibilityFor(r.Context(), user.ID)
	row, err := h.store.Get(r.Context(), id, vis)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTemplateNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplateLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Update PUT /api/v2/action-templates/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := Update{}
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if err := ValidateName(trimmed); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidActionTemplateName", map[string]string{
				"reason": err.Error(),
				"name":   *req.Name,
			}))
			return
		}
		upd.Name = &trimmed
	}
	if req.Parameters != nil {
		upd.Parameters = req.Parameters
	}
	if req.Scope != nil {
		normalised, err := NormaliseScope(*req.Scope)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidActionTemplateScope", map[string]string{
				"reason": err.Error(),
				"scope":  *req.Scope,
			}))
			return
		}
		upd.Scope = &normalised
	}
	if req.Shared != nil {
		upd.Shared = req.Shared
	}
	if err := h.store.Update(r.Context(), id, user.ID, upd); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTemplateNotFound", map[string]string{"id": id}))
			return
		}
		if errors.Is(err, ErrNameConflict) {
			payload := map[string]string{}
			if upd.Name != nil {
				payload["name"] = *upd.Name
			}
			apierror.WriteJSON(w, apierror.NewConflict("ActionTemplateNameConflict", payload))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplateUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	vis := h.visibilityFor(r.Context(), user.ID)
	row, err := h.store.Get(r.Context(), id, vis)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplateLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, row)
}

// Delete DELETE /api/v2/action-templates/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, user.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ActionTemplateNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ActionTemplateDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// newTemplateID returns a UUID-shaped identifier for a new row.
// Mirrors savedsearches.newSavedSearchID — keeps the package free of
// the google/uuid dep for one call site.
func newTemplateID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
