package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler exposes the /api/v2/pipelines/* endpoints (US-287). The
// handler is gated on auth presence both by the surrounding
// auth.Middleware and a defensive nil-check below. The handler is
// scoped to admin / authoring users; non-admin callers see only the
// pipelines they themselves created — matches the AIP Logic Flow
// scoping rule.
type Handler struct {
	store     Store
	scheduler *Scheduler
}

// NewHandler wires a Handler. store may be nil — every endpoint then
// returns PipelinesUnavailable so degraded-mode deployments don't
// silently 404.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// SetScheduler attaches an optional cron Scheduler (US-289). When non-nil,
// every successful Create / Update / Delete keeps the in-process registry
// in sync via Register / Unregister so schedule edits take effect without
// a server restart. Tests and degraded-mode deployments may leave it
// unset — the handler then short-circuits the propagation.
func (h *Handler) SetScheduler(s *Scheduler) {
	h.scheduler = s
}

// RegisterRoutes mounts every handler endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/pipelines", h.ListPipelines)
	r.Post("/api/v2/pipelines", h.CreatePipeline)
	r.Get("/api/v2/pipelines/{pipelineId}", h.GetPipeline)
	r.Put("/api/v2/pipelines/{pipelineId}", h.UpdatePipeline)
	r.Delete("/api/v2/pipelines/{pipelineId}", h.DeletePipeline)

	// US-298 — Pipeline 执行历史 API.
	r.Get("/api/v2/pipelines/{pipelineId}/runs", h.ListPipelineRuns)
	r.Get("/api/v2/pipelines/{pipelineId}/runs/{runId}", h.GetPipelineRun)
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
		apierror.WriteJSON(w, apierror.NewInternal("PipelinesUnavailable", map[string]string{
			"reason": "pipelines are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createPipelineRequest struct {
	ID          string      `json:"id,omitempty"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	Inputs      []Input     `json:"inputs"`
	Transforms  []Transform `json:"transforms,omitempty"`
	Outputs     []Output    `json:"outputs"`
	Schedule    string      `json:"schedule,omitempty"`
	Enabled     *bool       `json:"enabled,omitempty"`
}

type updatePipelineRequest struct {
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	Inputs      *[]Input     `json:"inputs,omitempty"`
	Transforms  *[]Transform `json:"transforms,omitempty"`
	Outputs     *[]Output    `json:"outputs,omitempty"`
	Schedule    *string      `json:"schedule,omitempty"`
	Enabled     *bool        `json:"enabled,omitempty"`
}

type listPipelinesResponse struct {
	Pipelines []*Pipeline `json:"pipelines"`
}

// CreatePipeline POST /api/v2/pipelines.
func (h *Handler) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req createPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = newPipelineID()
	} else if err := ValidatePipelineID(id); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineID", map[string]string{
			"reason": err.Error(),
			"id":     req.ID,
		}))
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	p := &Pipeline{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		Inputs:      req.Inputs,
		Transforms:  req.Transforms,
		Outputs:     req.Outputs,
		Schedule:    req.Schedule,
		Enabled:     enabled,
		CreatedBy:   user.ID,
	}
	if err := p.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineDefinition", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := h.store.CreatePipeline(r.Context(), p); err != nil {
		if errors.Is(err, ErrPipelineAlreadyExists) {
			apierror.WriteJSON(w, apierror.NewConflict("PipelineAlreadyExists", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PipelineCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.GetPipeline(r.Context(), id)
	if err != nil {
		stored = p
	}
	h.syncScheduler(stored)
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// ListPipelines GET /api/v2/pipelines. Scoped to the authenticated
// user's own pipelines (admins see everything).
func (h *Handler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	scope := user.ID
	if userHasAdminRole(user) {
		scope = ""
	}
	pipelines, err := h.store.ListPipelines(r.Context(), scope)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PipelineListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if pipelines == nil {
		pipelines = []*Pipeline{}
	}
	httputil.WriteJSON(w, http.StatusOK, listPipelinesResponse{Pipelines: pipelines})
}

// GetPipeline GET /api/v2/pipelines/{pipelineId}.
func (h *Handler) GetPipeline(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.pipelineIDParam(w, r)
	if !ok {
		return
	}
	pipeline, ok := h.lookupPipelineOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	httputil.WriteJSON(w, http.StatusOK, pipeline)
}

// UpdatePipeline PUT /api/v2/pipelines/{pipelineId}.
func (h *Handler) UpdatePipeline(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.pipelineIDParam(w, r)
	if !ok {
		return
	}
	current, ok := h.lookupPipelineOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	var req updatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	// Validate proposed final state by applying the partial update to a
	// copy of the current pipeline before persisting.
	proposed := *current
	proposed.Inputs = current.Inputs
	proposed.Transforms = current.Transforms
	proposed.Outputs = current.Outputs
	if req.Name != nil {
		proposed.Name = *req.Name
	}
	if req.Description != nil {
		proposed.Description = *req.Description
	}
	if req.Inputs != nil {
		proposed.Inputs = *req.Inputs
	}
	if req.Transforms != nil {
		proposed.Transforms = *req.Transforms
	}
	if req.Outputs != nil {
		proposed.Outputs = *req.Outputs
	}
	if req.Schedule != nil {
		proposed.Schedule = *req.Schedule
	}
	if req.Enabled != nil {
		proposed.Enabled = *req.Enabled
	}
	if err := proposed.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineDefinition", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := PipelineUpdate{
		Name:        req.Name,
		Description: req.Description,
		Inputs:      req.Inputs,
		Transforms:  req.Transforms,
		Outputs:     req.Outputs,
		Schedule:    req.Schedule,
		Enabled:     req.Enabled,
	}
	if err := h.store.UpdatePipeline(r.Context(), id, upd); err != nil {
		if errors.Is(err, ErrPipelineNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PipelineNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PipelineUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.GetPipeline(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PipelineLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	h.syncScheduler(stored)
	httputil.WriteJSON(w, http.StatusOK, stored)
}

// DeletePipeline DELETE /api/v2/pipelines/{pipelineId}.
func (h *Handler) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.pipelineIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupPipelineOwned(r.Context(), w, id, user); !ok {
		return
	}
	if err := h.store.DeletePipeline(r.Context(), id); err != nil {
		if errors.Is(err, ErrPipelineNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PipelineNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PipelineDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if h.scheduler != nil {
		h.scheduler.Unregister(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// syncScheduler propagates the latest pipeline state into the cron
// scheduler when one is wired. Errors are intentionally swallowed —
// returning 5xx after the persistence-layer write has already succeeded
// would mislead callers about the state of their pipeline. The next
// admin write or scheduler.Reload() will reconcile.
func (h *Handler) syncScheduler(p *Pipeline) {
	if h.scheduler == nil || p == nil {
		return
	}
	_ = h.scheduler.Register(p)
}

func (h *Handler) pipelineIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "pipelineId")
	if err := ValidatePipelineID(id); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineID", map[string]string{
			"reason": err.Error(),
			"id":     id,
		}))
		return "", false
	}
	return id, true
}

func (h *Handler) lookupPipelineOwned(ctx context.Context, w http.ResponseWriter, id string, user *auth.User) (*Pipeline, bool) {
	pipeline, err := h.store.GetPipeline(ctx, id)
	if err != nil {
		if errors.Is(err, ErrPipelineNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PipelineNotFound", map[string]string{"id": id}))
			return nil, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("PipelineLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return nil, false
	}
	if pipeline.CreatedBy != "" && user.ID != pipeline.CreatedBy && !userHasAdminRole(user) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("PipelineForbidden", map[string]string{
			"id": id,
		}))
		return nil, false
	}
	return pipeline, true
}

// newPipelineID returns a fresh random pipeline identifier of the form
// "pipeline_<32-hex-chars>".
func newPipelineID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "pipeline_" + hex.EncodeToString(buf[:])
}

// userHasAdminRole reports whether u carries the global admin role.
// Mirrors logic.userHasAdminRole rather than importing it to keep the
// pipeline package free of an aip dependency.
func userHasAdminRole(u *auth.User) bool {
	if u == nil {
		return false
	}
	for _, r := range u.Roles {
		if r == auth.RoleAdmin {
			return true
		}
	}
	return false
}

// listPipelineRunsResponse is the wire shape for the run-history list
// endpoint. NextCursor is a string (encoded run id) so SDK callers can
// treat it as opaque — the handler keeps the rule that "non-empty means
// more pages remain".
type listPipelineRunsResponse struct {
	Runs       []*PipelineRun `json:"runs"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

// ListPipelineRuns GET /api/v2/pipelines/{pipelineId}/runs.
func (h *Handler) ListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.pipelineIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupPipelineOwned(r.Context(), w, id, user); !ok {
		return
	}
	limit, ok := parseRunLimit(w, r.URL.Query().Get("limit"))
	if !ok {
		return
	}
	cursor, ok := parseRunCursor(w, r.URL.Query().Get("cursor"))
	if !ok {
		return
	}
	page, err := h.store.ListPipelineRuns(r.Context(), id, ListRunsOptions{Limit: limit, Cursor: cursor})
	if err != nil {
		if errors.Is(err, ErrPipelineNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PipelineNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PipelineRunListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	resp := listPipelineRunsResponse{Runs: page.Runs}
	if resp.Runs == nil {
		resp.Runs = []*PipelineRun{}
	}
	if page.NextCursor != 0 {
		resp.NextCursor = strconv.FormatInt(page.NextCursor, 10)
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// GetPipelineRun GET /api/v2/pipelines/{pipelineId}/runs/{runId}.
func (h *Handler) GetPipelineRun(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.pipelineIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupPipelineOwned(r.Context(), w, id, user); !ok {
		return
	}
	runID, ok := parseRunID(w, chi.URLParam(r, "runId"))
	if !ok {
		return
	}
	run, err := h.store.GetPipelineRun(r.Context(), id, runID)
	if err != nil {
		if errors.Is(err, ErrPipelineRunNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("PipelineRunNotFound", map[string]string{
				"pipelineId": id,
				"runId":      strconv.FormatInt(runID, 10),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("PipelineRunLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, run)
}

// parseRunLimit parses an optional ?limit= query parameter. Empty string
// = use the store-level default; out-of-range = typed 400.
func parseRunLimit(w http.ResponseWriter, raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 || v > maxRunPageSize {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineRunLimit", map[string]string{
			"reason": "limit must be a non-negative integer no greater than " + strconv.Itoa(maxRunPageSize),
			"limit":  raw,
		}))
		return 0, false
	}
	return v, true
}

// parseRunCursor parses an optional ?cursor= query parameter. Empty
// string = no cursor; non-numeric or non-positive = typed 400.
func parseRunCursor(w http.ResponseWriter, raw string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineRunCursor", map[string]string{
			"reason": "cursor must be a positive integer",
			"cursor": raw,
		}))
		return 0, false
	}
	return v, true
}

// parseRunID parses the path-bound {runId} segment. Non-numeric or
// non-positive = typed 400.
func parseRunID(w http.ResponseWriter, raw string) (int64, bool) {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPipelineRunID", map[string]string{
			"reason": "runId must be a positive integer",
			"runId":  raw,
		}))
		return 0, false
	}
	return v, true
}
