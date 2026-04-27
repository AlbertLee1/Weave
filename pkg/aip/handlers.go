package aip

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// defaultCompletionTimeout caps how long Provider.Complete may take per
// SendMessage call. Picked to fit within the 60s WriteTimeout configured
// on the production *http.Server while leaving a small margin for the
// surrounding HTTP machinery.
const defaultCompletionTimeout = 50 * time.Second

// MaxToolCallIterations caps how many Provider.Complete cycles a
// single SendMessage may trigger before forcing termination (US-284).
// One iteration covers one Provider call plus any tool execution it
// requested. The cap matches the PRD ("max 8 次") and protects the
// caller against pathological loops where the model repeatedly asks
// for more tool invocations without producing a final reply.
const MaxToolCallIterations = 8

// Handler implements the /api/v2/aip/threads/* CRUD + /messages
// endpoints. The handler is gated on auth presence — both inside the
// surrounding chi.Router (auth.RequirePermission) and via a defensive
// nil-check below.
//
// Tools (US-284) is the optional ToolRegistry the SendMessage loop
// resolves model-requested tool calls through. A nil registry means
// function-calling is disabled and the loop runs at most one
// Provider.Complete invocation (legacy single-turn behaviour).
type Handler struct {
	store    Store
	registry *Registry
	tools    *ToolRegistry
}

// NewHandler constructs a Handler. A nil store leaves every endpoint
// reporting AIPThreadsUnavailable; a nil registry leaves SendMessage
// reporting AIPProviderNotConfigured. Tool calling (US-284) is wired
// post-construction via SetToolRegistry so test deployments with no
// tools observe the legacy single-turn contract.
func NewHandler(store Store, registry *Registry) *Handler {
	return &Handler{store: store, registry: registry}
}

// SetToolRegistry attaches a ToolRegistry. Calling with nil disables
// function-calling for the handler. Safe to call before RegisterRoutes.
func (h *Handler) SetToolRegistry(tools *ToolRegistry) {
	if h == nil {
		return
	}
	h.tools = tools
}

// RegisterRoutes mounts every thread + messages endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/aip/threads", h.ListThreads)
	r.Post("/api/v2/aip/threads", h.CreateThread)
	r.Get("/api/v2/aip/threads/{threadId}", h.GetThread)
	r.Put("/api/v2/aip/threads/{threadId}", h.UpdateThread)
	r.Delete("/api/v2/aip/threads/{threadId}", h.DeleteThread)
	r.Get("/api/v2/aip/threads/{threadId}/messages", h.ListMessages)
	r.Post("/api/v2/aip/threads/{threadId}/messages", h.SendMessage)
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
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadsUnavailable", map[string]string{
			"reason": "AIP threads are not configured on this deployment",
		}))
		return false
	}
	return true
}

type createThreadRequest struct {
	ID           string `json:"id,omitempty"`
	Title        string `json:"title,omitempty"`
	Provider     string `json:"provider"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
}

type updateThreadRequest struct {
	Title        *string `json:"title,omitempty"`
	Model        *string `json:"model,omitempty"`
	SystemPrompt *string `json:"systemPrompt,omitempty"`
}

type sendMessageRequest struct {
	Content     string  `json:"content"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
}

type sendMessageResponse struct {
	UserMessage      *Message   `json:"userMessage"`
	AssistantMessage *Message   `json:"assistantMessage"`
	ToolMessages     []*Message `json:"toolMessages,omitempty"`
	Iterations       int        `json:"iterations,omitempty"`
}

type listThreadsResponse struct {
	Threads []*Thread `json:"threads"`
}

type listMessagesResponse struct {
	Messages []*Message `json:"messages"`
}

// CreateThread POST /api/v2/aip/threads.
func (h *Handler) CreateThread(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	var req createThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	provider := strings.TrimSpace(req.Provider)
	if err := ValidateProvider(provider); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidProvider", map[string]string{
			"reason":   err.Error(),
			"provider": req.Provider,
		}))
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = newThreadID()
	} else if err := ValidateThreadID(id); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidThreadID", map[string]string{
			"reason": err.Error(),
			"id":     req.ID,
		}))
		return
	}
	now := time.Now().UTC()
	thread := &Thread{
		ID:           id,
		Title:        req.Title,
		Provider:     provider,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		CreatedBy:    user.ID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.store.CreateThread(r.Context(), thread); err != nil {
		if errors.Is(err, ErrThreadAlreadyExists) {
			apierror.WriteJSON(w, apierror.NewConflict("AIPThreadAlreadyExists", map[string]string{
				"id": id,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	stored, err := h.store.GetThread(r.Context(), id)
	if err != nil {
		stored = thread
	}
	httputil.WriteJSON(w, http.StatusCreated, stored)
}

// ListThreads GET /api/v2/aip/threads. Scoped to the authenticated
// user's own threads.
func (h *Handler) ListThreads(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	threads, err := h.store.ListThreads(r.Context(), user.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if threads == nil {
		threads = []*Thread{}
	}
	httputil.WriteJSON(w, http.StatusOK, listThreadsResponse{Threads: threads})
}

// GetThread GET /api/v2/aip/threads/{threadId}.
func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	thread, ok := h.lookupThreadOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	httputil.WriteJSON(w, http.StatusOK, thread)
}

// UpdateThread PUT /api/v2/aip/threads/{threadId}. Partial update;
// pointer fields preserve omitted keys.
func (h *Handler) UpdateThread(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupThreadOwned(r.Context(), w, id, user); !ok {
		return
	}
	var req updateThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	upd := ThreadUpdate{
		Title:        req.Title,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
	}
	if err := h.store.UpdateThread(r.Context(), id, upd); err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPThreadNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadUpdateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	thread, err := h.store.GetThread(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, thread)
}

// DeleteThread DELETE /api/v2/aip/threads/{threadId}. Cascades into
// messages via the FK ON DELETE CASCADE.
func (h *Handler) DeleteThread(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupThreadOwned(r.Context(), w, id, user); !ok {
		return
	}
	if err := h.store.DeleteThread(r.Context(), id); err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPThreadNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMessages GET /api/v2/aip/threads/{threadId}/messages.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupThreadOwned(r.Context(), w, id, user); !ok {
		return
	}
	msgs, err := h.store.ListMessages(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPThreadNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPMessageListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if msgs == nil {
		msgs = []*Message{}
	}
	httputil.WriteJSON(w, http.StatusOK, listMessagesResponse{Messages: msgs})
}

// SendMessage POST /api/v2/aip/threads/{threadId}/messages — appends a
// user message, dispatches to the thread's Provider, and persists the
// assistant reply. When a tool registry is wired (US-284) the handler
// runs a function-calling loop: each model-requested tool is invoked
// in-process and its result appended as a RoleTool message before the
// next Provider.Complete call. The loop is capped at
// MaxToolCallIterations so a misbehaving model cannot pin the request.
//
// Response carries the original user message, every RoleTool result
// that was produced (toolMessages, in invocation order), and the final
// assistant reply (assistantMessage). The SPA can render them together
// without an extra ListMessages round-trip.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	thread, ok := h.lookupThreadOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMessageContent", map[string]string{
			"reason": "content is required",
		}))
		return
	}

	provider, perr := h.providerForThread(thread)
	if perr != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPProviderNotConfigured", map[string]string{
			"reason":   perr.Error(),
			"provider": thread.Provider,
		}))
		return
	}

	userMsg := &Message{
		ThreadID: id,
		Role:     RoleUser,
		Content:  content,
	}
	if err := h.store.AppendMessage(r.Context(), userMsg); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPMessageAppendFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	tools := h.tools.Definitions()
	ctx, cancel := context.WithTimeout(r.Context(), defaultCompletionTimeout)
	defer cancel()

	var (
		assistantMsg *Message
		toolMessages []*Message
		iter         int
	)
	for iter = 0; iter < MaxToolCallIterations; iter++ {
		history, err := h.store.ListMessages(ctx, id)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("AIPMessageListFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		chatReq := buildChatRequest(thread, history, req.Temperature, req.MaxTokens)
		chatReq.Tools = tools

		resp, err := provider.Complete(ctx, chatReq)
		if err != nil {
			if errors.Is(err, ErrProviderNotConfigured) {
				apierror.WriteJSON(w, apierror.NewInternal("AIPProviderNotConfigured", map[string]string{
					"reason":   err.Error(),
					"provider": thread.Provider,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("AIPCompletionFailed", map[string]string{
				"reason":   err.Error(),
				"provider": thread.Provider,
			}))
			return
		}

		// Always persist the assistant turn — even when ToolCalls is
		// non-empty — so the thread history reflects every model step.
		assistantMsg = &Message{
			ThreadID:   id,
			Role:       RoleAssistant,
			Content:    resp.Content,
			TokenCount: resp.TokenCount,
			ToolCalls:  resp.ToolCalls,
		}
		if err := h.store.AppendMessage(ctx, assistantMsg); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("AIPMessageAppendFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}

		if len(resp.ToolCalls) == 0 {
			break
		}

		// Resolve and execute every requested tool, then append a
		// RoleTool result message per call so the next iteration's
		// history carries them back to the model.
		if h.tools == nil {
			apierror.WriteJSON(w, apierror.NewInternal("AIPToolsNotConfigured", map[string]string{
				"reason":   "model requested tool invocation but no tool registry is wired",
				"provider": thread.Provider,
			}))
			return
		}
		for _, call := range resp.ToolCalls {
			handler, lookupErr := h.tools.Get(call.Name)
			if lookupErr != nil {
				apierror.WriteJSON(w, apierror.NewInternal("AIPToolNotFound", map[string]string{
					"reason": lookupErr.Error(),
					"tool":   call.Name,
				}))
				return
			}
			result, execErr := handler.Execute(ctx, call.Arguments)
			toolMsg := &Message{
				ThreadID:   id,
				Role:       RoleTool,
				ToolCallID: call.ID,
				ToolName:   call.Name,
			}
			if execErr != nil {
				toolMsg.Content = fmt.Sprintf("error: %s", execErr.Error())
			} else {
				toolMsg.Content = result
			}
			if err := h.store.AppendMessage(ctx, toolMsg); err != nil {
				apierror.WriteJSON(w, apierror.NewInternal("AIPMessageAppendFailed", map[string]string{
					"reason": err.Error(),
				}))
				return
			}
			toolMessages = append(toolMessages, toolMsg)
		}
	}

	if iter >= MaxToolCallIterations {
		apierror.WriteJSON(w, apierror.NewInternal("AIPToolLoopExceeded", map[string]string{
			"reason":     fmt.Sprintf("function-calling loop exceeded %d iterations", MaxToolCallIterations),
			"iterations": fmt.Sprintf("%d", iter),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, sendMessageResponse{
		UserMessage:      userMsg,
		AssistantMessage: assistantMsg,
		ToolMessages:     toolMessages,
		Iterations:       iter + 1,
	})
}

// threadIDParam extracts and validates the {threadId} URL segment.
func (h *Handler) threadIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "threadId")
	if err := ValidateThreadID(id); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidThreadID", map[string]string{
			"reason": err.Error(),
			"id":     id,
		}))
		return "", false
	}
	return id, true
}

// lookupThreadOwned loads a thread and rejects callers who don't own
// it. Returns (nil, false) on miss / forbidden / store error and writes
// the appropriate apierror response. The caller must abort on false.
func (h *Handler) lookupThreadOwned(ctx context.Context, w http.ResponseWriter, id string, user *auth.User) (*Thread, bool) {
	thread, err := h.store.GetThread(ctx, id)
	if err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPThreadNotFound", map[string]string{"id": id}))
			return nil, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPThreadLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return nil, false
	}
	if thread.CreatedBy != "" && user.ID != thread.CreatedBy && !userHasAdminRole(user) {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("AIPThreadForbidden", map[string]string{
			"id": id,
		}))
		return nil, false
	}
	return thread, true
}

// providerForThread resolves the thread's provider via the registry.
func (h *Handler) providerForThread(t *Thread) (Provider, error) {
	if h.registry == nil {
		return nil, errors.New("aip: provider registry not wired")
	}
	return h.registry.Get(t.Provider)
}

// buildChatRequest converts the persisted system prompt + message history
// into the canonical ChatRequest the Provider expects. The system prompt
// is hoisted to the head when set; otherwise the existing message order
// is preserved verbatim. Assistant tool_calls and RoleTool result fields
// (US-284) are propagated so providers that support function-calling
// can rebuild the model-side context faithfully across iterations.
func buildChatRequest(t *Thread, history []*Message, temperature float64, maxTokens int) ChatRequest {
	msgs := make([]ChatMessage, 0, len(history)+1)
	if strings.TrimSpace(t.SystemPrompt) != "" {
		msgs = append(msgs, ChatMessage{Role: RoleSystem, Content: t.SystemPrompt})
	}
	for _, m := range history {
		if m == nil {
			continue
		}
		cm := ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
		}
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
		}
		msgs = append(msgs, cm)
	}
	return ChatRequest{
		Model:       t.Model,
		Messages:    msgs,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
}

// newThreadID returns a fresh random thread identifier of the form
// "thr_<32-hex-chars>". Validates against threadIDRE so any future
// tightening is caught by ValidateThreadID round-trip tests.
func newThreadID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "thr_" + hex.EncodeToString(buf[:])
}

// userHasAdminRole reports whether u carries the global admin role.
// auth.User has no IsAdmin helper, and existing handlers either lean on
// auth.RequirePermission middleware or check Roles directly — we mirror
// that pattern locally to avoid widening the auth surface for one site.
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
