package logic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
)

// defaultExecuteTimeout caps how long a single Execute call can run.
// Picked to fit comfortably inside the 60s WriteTimeout configured on
// the production *http.Server while leaving room for HTTP overhead.
const defaultExecuteTimeout = 50 * time.Second

// Handler exposes the /api/v2/aip/logic-flows/* endpoints. The handler
// is gated on auth presence both by the surrounding auth.Middleware and
// a defensive nil-check below.
type Handler struct {
	store    Store
	executor *Executor
}

// NewHandler wires a Handler. Either argument may be nil — a nil store
// makes every endpoint return AIPLogicFlowsUnavailable; a nil executor
// disables /execute (returns AIPLogicFlowExecutorUnavailable).
func NewHandler(store Store, executor *Executor) *Handler {
	return &Handler{store: store, executor: executor}
}

// RegisterRoutes mounts every handler endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/aip/logic-flows", h.ListFlows)
	r.Post("/api/v2/aip/logic-flows", h.CreateFlow)
	r.Get("/api/v2/aip/logic-flows/{flowId}", h.GetFlow)
	r.Put("/api/v2/aip/logic-flows/{flowId}", h.UpdateFlow)
	r.Delete("/api/v2/aip/logic-flows/{flowId}", h.DeleteFlow)
	r.Post("/api/v2/aip/logic-flows/{flowId}/execute", h.ExecuteFlow)
	r.Post("/api/v2/aip/logic-flows/{flowId}/dry-run-node", h.DryRunNode)
	r.Get("/api/v2/aip/logic-flows/{flowId}/runs", h.ListRuns)
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
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowsUnavailable", map[string]string{
			"reason": "AIP Logic Flows are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createFlowRequest struct {
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
	FallbackModel string `json:"fallbackModel,omitempty"`
	MaxRetries    int    `json:"maxRetries,omitempty"`
}

type updateFlowRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Nodes         *[]Node `json:"nodes,omitempty"`
	Edges         *[]Edge `json:"edges,omitempty"`
	FallbackModel *string `json:"fallbackModel,omitempty"`
	MaxRetries    *int    `json:"maxRetries,omitempty"`
}

type executeFlowRequest struct {
	Input map[string]any `json:"input,omitempty"`
}

// dryRunNodeRequest carries an in-flight node spec plus an arbitrary
// state map so the editor can preview a node's output without saving the
// flow. The state map is plumbed straight through to the executor's
// per-node dispatcher; "input" and prior-node outputs may be provided
// by name (e.g. {"input": {...}, "n1": {...}}).
type dryRunNodeRequest struct {
	Node  Node           `json:"node"`
	State map[string]any `json:"state,omitempty"`
}

type dryRunNodeResponse struct {
	Trace TraceEntry `json:"trace"`
}

type listFlowsResponse struct {
	Flows []*Flow `json:"flows"`
}

type listRunsResponse struct {
	Runs []*Run `json:"runs"`
}

// CreateFlow POST /api/v2/aip/logic-flows.
func (h *Handler) CreateFlow(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req createFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = newFlowID()
	} else if err := ValidateFlowID(id); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlowID", map[string]string{
			"reason": err.Error(),
			"id":     req.ID,
		}))
		return
	}
	now := time.Now().UTC()
	flow := &Flow{
		ID:            id,
		Name:          req.Name,
		Description:   req.Description,
		Nodes:         req.Nodes,
		Edges:         req.Edges,
		FallbackModel: req.FallbackModel,
		MaxRetries:    req.MaxRetries,
		CreatedBy:     user.ID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := flow.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlowDefinition", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := h.store.CreateFlow(r.Context(), flow); err != nil {
		if errors.Is(err, ErrFlowAlreadyExists) {
			apierror.WriteJSON(w, apierror.NewConflict("AIPLogicFlowAlreadyExists", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.GetFlow(r.Context(), id)
	if err != nil {
		stored = flow
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// ListFlows GET /api/v2/aip/logic-flows. Scoped to the authenticated
// user's own flows (admins see everything).
func (h *Handler) ListFlows(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	scope := user.ID
	if userHasAdminRole(user) {
		scope = ""
	}
	flows, err := h.store.ListFlows(r.Context(), scope)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if flows == nil {
		flows = []*Flow{}
	}
	httputil.WriteJSON(w, http.StatusOK, listFlowsResponse{Flows: flows})
}

// GetFlow GET /api/v2/aip/logic-flows/{flowId}.
func (h *Handler) GetFlow(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.flowIDParam(w, r)
	if !ok {
		return
	}
	flow, ok := h.lookupFlowOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	httputil.WriteJSON(w, http.StatusOK, flow)
}

// UpdateFlow PUT /api/v2/aip/logic-flows/{flowId}.
func (h *Handler) UpdateFlow(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.flowIDParam(w, r)
	if !ok {
		return
	}
	current, ok := h.lookupFlowOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	var req updateFlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	// Validate proposed final state by applying the partial update to a
	// copy of the current flow before persisting.
	proposed := *current
	if req.Name != nil {
		proposed.Name = *req.Name
	}
	if req.Description != nil {
		proposed.Description = *req.Description
	}
	if req.Nodes != nil {
		proposed.Nodes = *req.Nodes
	}
	if req.Edges != nil {
		proposed.Edges = *req.Edges
	}
	if req.FallbackModel != nil {
		proposed.FallbackModel = *req.FallbackModel
	}
	if req.MaxRetries != nil {
		proposed.MaxRetries = *req.MaxRetries
	}
	if err := proposed.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlowDefinition", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := FlowUpdate{
		Name:          req.Name,
		Description:   req.Description,
		Nodes:         req.Nodes,
		Edges:         req.Edges,
		FallbackModel: req.FallbackModel,
		MaxRetries:    req.MaxRetries,
	}
	if err := h.store.UpdateFlow(r.Context(), id, upd); err != nil {
		if errors.Is(err, ErrFlowNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPLogicFlowNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.GetFlow(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, stored)
}

// DeleteFlow DELETE /api/v2/aip/logic-flows/{flowId}.
func (h *Handler) DeleteFlow(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.flowIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupFlowOwned(r.Context(), w, id, user); !ok {
		return
	}
	if err := h.store.DeleteFlow(r.Context(), id); err != nil {
		if errors.Is(err, ErrFlowNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPLogicFlowNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ExecuteFlow POST /api/v2/aip/logic-flows/{flowId}/execute. Runs the
// flow with the provided input map; persists a Run row and returns the
// run record (output / trace / status / error).
func (h *Handler) ExecuteFlow(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	if h.executor == nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowExecutorUnavailable", map[string]string{
			"reason": "executor is not wired",
		}))
		return
	}
	id, ok := h.flowIDParam(w, r)
	if !ok {
		return
	}
	flow, ok := h.lookupFlowOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	var req executeFlowRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultExecuteTimeout)
	defer cancel()
	run, err := h.executor.Execute(ctx, flow, req.Input)
	if run == nil {
		run = &Run{FlowID: id, Status: RunStatusFailed}
	}
	run.CreatedBy = user.ID
	if appendErr := h.store.AppendRun(ctx, run); appendErr != nil {
		// Don't fail the request because we couldn't persist the audit
		// row — return the in-memory run so the caller still sees the
		// outcome. Operators see the persistence failure in the log.
		_ = appendErr
	}
	if err != nil {
		httputil.WriteJSON(w, http.StatusUnprocessableEntity, run)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, run)
}

// DryRunNode POST /api/v2/aip/logic-flows/{flowId}/dry-run-node. Runs a
// single in-flight node spec against a caller-supplied state map and
// returns its TraceEntry. No Run row is persisted — this is the editor
// preview path the SPA uses to show "what would this node output?"
// without forcing the author to save the whole flow first. Ownership is
// gated on the parent flow so dry-run can't be used to evaluate nodes
// for a flow the caller cannot already read.
func (h *Handler) DryRunNode(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	if h.executor == nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowExecutorUnavailable", map[string]string{
			"reason": "executor is not wired",
		}))
		return
	}
	id, ok := h.flowIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupFlowOwned(r.Context(), w, id, user); !ok {
		return
	}
	var req dryRunNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if strings.TrimSpace(req.Node.ID) == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidNode", map[string]string{
			"reason": "node.id is required",
		}))
		return
	}
	if !IsKnownNodeType(req.Node.Type) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidNode", map[string]string{
			"reason": "node.type is unknown",
			"type":   req.Node.Type,
		}))
		return
	}
	if err := validateNodeConfig(req.Node); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidNode", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	state := req.State
	if state == nil {
		state = map[string]any{}
	}
	var input map[string]any
	if v, ok := state["input"].(map[string]any); ok {
		input = v
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultExecuteTimeout)
	defer cancel()
	entry, _, runErr := h.executor.runNode(ctx, req.Node, state, input)
	if runErr != nil {
		// Preview surfaces failures via the trace entry, not as a
		// transport error — return 200 so the SPA can render the
		// failure inline instead of the error envelope.
		entry.Status = TraceStatusFailed
		if entry.Error == "" {
			entry.Error = runErr.Error()
		}
	}
	httputil.WriteJSON(w, http.StatusOK, dryRunNodeResponse{Trace: entry})
}

// ListRuns GET /api/v2/aip/logic-flows/{flowId}/runs.
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.flowIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupFlowOwned(r.Context(), w, id, user); !ok {
		return
	}
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed < 500 {
			limit = parsed
		}
	}
	runs, err := h.store.ListRuns(r.Context(), id, limit)
	if err != nil {
		if errors.Is(err, ErrFlowNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPLogicFlowNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowRunListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if runs == nil {
		runs = []*Run{}
	}
	httputil.WriteJSON(w, http.StatusOK, listRunsResponse{Runs: runs})
}

func (h *Handler) flowIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "flowId")
	if err := ValidateFlowID(id); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFlowID", map[string]string{
			"reason": err.Error(),
			"id":     id,
		}))
		return "", false
	}
	return id, true
}

func (h *Handler) lookupFlowOwned(ctx context.Context, w http.ResponseWriter, id string, user *auth.User) (*Flow, bool) {
	flow, err := h.store.GetFlow(ctx, id)
	if err != nil {
		if errors.Is(err, ErrFlowNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPLogicFlowNotFound", map[string]string{"id": id}))
			return nil, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPLogicFlowLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return nil, false
	}
	if flow.CreatedBy != "" && user.ID != flow.CreatedBy && !userHasAdminRole(user) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("AIPLogicFlowForbidden", map[string]string{
			"id": id,
		}))
		return nil, false
	}
	return flow, true
}

// newFlowID returns a fresh random flow identifier of the form
// "flow_<32-hex-chars>".
func newFlowID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "flow_" + hex.EncodeToString(buf[:])
}

// userHasAdminRole reports whether u carries the global admin role.
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
