package aip

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

// newTestHandler wires a Handler against an in-memory store and a
// registry containing only the mock provider.
func newTestHandler() (*Handler, *MemoryStore) {
	store := NewMemoryStore()
	reg := NewRegistry()
	reg.Register(NewMockProvider())
	return NewHandler(store, reg), store
}

// withAuthContext attaches a fake authenticated user to r.Context().
func withAuthContext(r *http.Request, userID string, roles ...string) *http.Request {
	user := &auth.User{ID: userID, Roles: roles}
	ctx := auth.WithUser(r.Context(), user)
	return r.WithContext(ctx)
}

// newRouter mounts every aip endpoint inside a chi router for tests.
func newRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func decodeJSON(t *testing.T, body []byte, dst interface{}) {
	t.Helper()
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decodeJSON: %v (body=%s)", err, string(body))
	}
}

func TestHandler_CreateThread_Success(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)

	body := `{"provider":"mock","title":"hello","model":"weave-mock-llm-v1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var thread Thread
	decodeJSON(t, w.Body.Bytes(), &thread)
	if thread.ID == "" || !strings.HasPrefix(thread.ID, "thr_") {
		t.Errorf("expected thr_-prefixed id, got %q", thread.ID)
	}
	if thread.CreatedBy != "user:alice" {
		t.Errorf("CreatedBy = %q, want user:alice", thread.CreatedBy)
	}
	got, err := store.GetThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatalf("GetThread after create: %v", err)
	}
	if got.Provider != "mock" {
		t.Errorf("Provider stored as %q", got.Provider)
	}
}

func TestHandler_CreateThread_RejectsInvalidProvider(t *testing.T) {
	h, _ := newTestHandler()
	r := newRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads",
		strings.NewReader(`{"provider":"OpenAI!"}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestHandler_CreateThread_NoAuth(t *testing.T) {
	h, _ := newTestHandler()
	r := newRouter(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads",
		strings.NewReader(`{"provider":"mock"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandler_ListThreads_OnlyOwnerVisible(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)

	for _, owner := range []string{"user:alice", "user:bob"} {
		tr := &Thread{ID: "thr_" + strings.Replace(owner, ":", "_", -1), Provider: ProviderMock, CreatedBy: owner}
		_ = store.CreateThread(context.Background(), tr)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp listThreadsResponse
	decodeJSON(t, w.Body.Bytes(), &resp)
	if len(resp.Threads) != 1 || resp.Threads[0].CreatedBy != "user:alice" {
		t.Errorf("expected only alice's thread; got %+v", resp.Threads)
	}
}

func TestHandler_GetThread_OwnershipEnforced(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	_ = store.CreateThread(context.Background(), &Thread{ID: "thr_alice", Provider: ProviderMock, CreatedBy: "user:alice"})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads/thr_alice", nil)
	req = withAuthContext(req, "user:bob")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner status = %d body = %s", w.Code, w.Body.String())
	}

	// Admin role can see other users' threads.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads/thr_alice", nil)
	req = withAuthContext(req, "user:admin", auth.RoleAdmin)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestHandler_UpdateThread_Partial(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	_ = store.CreateThread(context.Background(), &Thread{ID: "thr_u", Provider: ProviderMock, CreatedBy: "user:alice", SystemPrompt: "old"})

	req := httptest.NewRequest(http.MethodPut, "/api/v2/aip/threads/thr_u",
		strings.NewReader(`{"title":"renamed"}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var got Thread
	decodeJSON(t, w.Body.Bytes(), &got)
	if got.Title != "renamed" {
		t.Errorf("Title = %q want renamed", got.Title)
	}
	if got.SystemPrompt != "old" {
		t.Errorf("SystemPrompt should be preserved; got %q", got.SystemPrompt)
	}
}

func TestHandler_DeleteThread_NoContent(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	_ = store.CreateThread(context.Background(), &Thread{ID: "thr_d", Provider: ProviderMock, CreatedBy: "user:alice"})

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/aip/threads/thr_d", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := store.GetThread(context.Background(), "thr_d"); err == nil {
		t.Errorf("thread should be gone")
	}
}

func TestHandler_SendMessage_PersistsBoth(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	_ = store.CreateThread(context.Background(), &Thread{ID: "thr_chat", Provider: ProviderMock, CreatedBy: "user:alice", SystemPrompt: "be brief"})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_chat/messages",
		strings.NewReader(`{"content":"hello there"}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp sendMessageResponse
	decodeJSON(t, w.Body.Bytes(), &resp)
	if resp.UserMessage == nil || resp.UserMessage.Role != RoleUser {
		t.Errorf("UserMessage missing or wrong role: %+v", resp.UserMessage)
	}
	if resp.AssistantMessage == nil || resp.AssistantMessage.Role != RoleAssistant {
		t.Errorf("AssistantMessage missing or wrong role: %+v", resp.AssistantMessage)
	}
	if !strings.Contains(resp.AssistantMessage.Content, "hello there") {
		t.Errorf("expected echo of user content; got %q", resp.AssistantMessage.Content)
	}

	// Both should be persisted.
	msgs, err := store.ListMessages(context.Background(), "thr_chat")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 persisted messages, got %d", len(msgs))
	}
}

func TestHandler_SendMessage_RejectsEmptyContent(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	_ = store.CreateThread(context.Background(), &Thread{ID: "thr_e", Provider: ProviderMock, CreatedBy: "user:alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_e/messages",
		strings.NewReader(`{"content":"  "}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandler_SendMessage_UnknownProvider(t *testing.T) {
	store := NewMemoryStore()
	reg := NewRegistry() // empty
	h := NewHandler(store, reg)
	r := newRouter(h)
	_ = store.CreateThread(context.Background(), &Thread{ID: "thr_x", Provider: "openai", CreatedBy: "user:alice"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_x/messages",
		strings.NewReader(`{"content":"hi"}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["errorName"] != "AIPProviderNotConfigured" {
		t.Errorf("errorName = %v want AIPProviderNotConfigured (body=%s)", body["errorName"], w.Body.String())
	}
}

func TestHandler_NilStore_ReturnsAIPThreadsUnavailable(t *testing.T) {
	h := NewHandler(nil, nil)
	r := newRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "AIPThreadsUnavailable") {
		t.Errorf("expected AIPThreadsUnavailable in body; got %s", w.Body.String())
	}
}

func TestBuildChatRequest_PrependsSystemPrompt(t *testing.T) {
	t1 := &Thread{SystemPrompt: "be brief", Model: "gpt"}
	hist := []*Message{{Role: RoleUser, Content: "hi"}}
	req := buildChatRequest(t1, hist, 0.5, 0)
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages; got %d", len(req.Messages))
	}
	if req.Messages[0].Role != RoleSystem {
		t.Errorf("first message should be system; got %s", req.Messages[0].Role)
	}
	if req.Model != "gpt" {
		t.Errorf("Model = %q want gpt", req.Model)
	}

	// Without system prompt the array is just history.
	t2 := &Thread{}
	req = buildChatRequest(t2, hist, 0, 0)
	if len(req.Messages) != 1 {
		t.Errorf("expected 1 message when no system prompt; got %d", len(req.Messages))
	}
}

// roundTripperFunc is a tiny adapter used by other tests if needed.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHandler_RoutesRegistered(t *testing.T) {
	h, _ := newTestHandler()
	r := newRouter(h)
	// Walk to confirm all expected paths are mounted.
	want := map[string]bool{
		"GET /api/v2/aip/threads":                            false,
		"POST /api/v2/aip/threads":                           false,
		"GET /api/v2/aip/threads/{threadId}":                 false,
		"PUT /api/v2/aip/threads/{threadId}":                 false,
		"DELETE /api/v2/aip/threads/{threadId}":              false,
		"GET /api/v2/aip/threads/{threadId}/messages":        false,
		"POST /api/v2/aip/threads/{threadId}/messages":       false,
	}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + strings.TrimSuffix(route, "/*")
		if _, ok := want[key]; ok {
			want[key] = true
		}
		return nil
	})
	for k, ok := range want {
		if !ok {
			t.Errorf("expected route %s to be registered", k)
		}
	}
	// Sanity: ensure handler can serve a basic Get without panicking.
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads", bytes.NewReader(nil))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}
