package aip

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

// defaultCompletionTimeout caps how long Provider.Complete may take per
// SendMessage call. Picked to fit within the 60s WriteTimeout configured
// on the production *http.Server while leaving a small margin for the
// surrounding HTTP machinery.
const defaultCompletionTimeout = 50 * time.Second

// Handler implements the /api/v2/aip/threads/* CRUD + /messages
// endpoints. The handler is gated on auth presence — both inside the
// surrounding chi.Router (auth.RequirePermission) and via a defensive
// nil-check below.
type Handler struct {
	store    Store
	registry *Registry
}

// NewHandler constructs a Handler. A nil store leaves every endpoint
// reporting AIPThreadsUnavailable; a nil registry leaves SendMessage
// reporting AIPProviderNotConfigured.
func NewHandler(store Store, registry *Registry) *Handler {
	return &Handler{store: store, registry: registry}
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
	UserMessage      *Message `json:"userMessage"`
	AssistantMessage *Message `json:"assistantMessage"`
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
// assistant reply. Returns both messages so the SPA can render them
// together without an extra ListMessages round-trip.
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

	history, err := h.store.ListMessages(r.Context(), id)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPMessageListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	chatReq := buildChatRequest(thread, history, req.Temperature, req.MaxTokens)

	ctx, cancel := context.WithTimeout(r.Context(), defaultCompletionTimeout)
	defer cancel()
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

	assistantMsg := &Message{
		ThreadID:   id,
		Role:       RoleAssistant,
		Content:    resp.Content,
		TokenCount: resp.TokenCount,
	}
	if err := h.store.AppendMessage(r.Context(), assistantMsg); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AIPMessageAppendFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, sendMessageResponse{
		UserMessage:      userMsg,
		AssistantMessage: assistantMsg,
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
// is preserved verbatim.
func buildChatRequest(t *Thread, history []*Message, temperature float64, maxTokens int) ChatRequest {
	msgs := make([]ChatMessage, 0, len(history)+1)
	if strings.TrimSpace(t.SystemPrompt) != "" {
		msgs = append(msgs, ChatMessage{Role: RoleSystem, Content: t.SystemPrompt})
	}
	for _, m := range history {
		if m == nil {
			continue
		}
		msgs = append(msgs, ChatMessage{Role: m.Role, Content: m.Content})
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
