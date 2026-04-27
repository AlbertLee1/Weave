package aip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// scriptedProvider drives the function-calling loop with a pre-baked
// list of responses. Each Complete call consumes one item; once the
// list is exhausted Complete returns an error so test assertions catch
// runaway loops cleanly.
type scriptedProvider struct {
	mu        sync.Mutex
	responses []ChatResponse
	calls     []ChatRequest
}

func (p *scriptedProvider) Name() string { return ProviderMock }

func (p *scriptedProvider) Complete(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, req)
	if len(p.responses) == 0 {
		return nil, errors.New("scriptedProvider: no scripted response")
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return &resp, nil
}

func (p *scriptedProvider) Calls() []ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ChatRequest, len(p.calls))
	copy(out, p.calls)
	return out
}

// counterTool counts how many times it was invoked.
type counterTool struct {
	def   ToolDef
	count atomic.Int32
	reply func(args json.RawMessage) (string, error)
}

func (c *counterTool) Definition() ToolDef { return c.def }
func (c *counterTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	c.count.Add(1)
	if c.reply != nil {
		return c.reply(args)
	}
	return "ok", nil
}

func newScriptedHandler(t *testing.T, sp *scriptedProvider, tools *ToolRegistry) (*Handler, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	reg := NewRegistry()
	reg.Register(sp)
	h := NewHandler(store, reg)
	h.SetToolRegistry(tools)
	return h, store
}

func sendMessage(t *testing.T, h *Handler, threadID, userID, content string) (*httptest.ResponseRecorder, *sendMessageResponse) {
	t.Helper()
	r := newRouter(h)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/aip/threads/"+threadID+"/messages",
		strings.NewReader(`{"content":`+jsonString(content)+`}`))
	req = withAuthContext(req, userID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		return w, nil
	}
	var resp sendMessageResponse
	decodeJSON(t, w.Body.Bytes(), &resp)
	return w, &resp
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestSendMessage_FunctionCallChain_OneToolCall(t *testing.T) {
	tools := NewToolRegistry()
	echo := &counterTool{def: ToolDef{Name: "echo"}, reply: func(_ json.RawMessage) (string, error) {
		return "tool-echo-result", nil
	}}
	tools.Register(echo)

	sp := &scriptedProvider{
		responses: []ChatResponse{
			{
				Model: "scripted",
				ToolCalls: []ToolCall{{
					ID:        "call_1",
					Name:      "echo",
					Arguments: json.RawMessage(`{"text":"foo"}`),
				}},
			},
			{
				Content: "final answer based on tool",
				Model:   "scripted",
			},
		},
	}
	h, store := newScriptedHandler(t, sp, tools)
	_ = store.CreateThread(context.Background(), &Thread{
		ID: "thr_chain", Provider: ProviderMock, CreatedBy: "user:alice",
	})

	w, resp := sendMessage(t, h, "thr_chain", "user:alice", "please run echo")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if resp.AssistantMessage == nil || resp.AssistantMessage.Content != "final answer based on tool" {
		t.Fatalf("expected final assistant message; got %+v", resp.AssistantMessage)
	}
	if len(resp.ToolMessages) != 1 {
		t.Fatalf("expected 1 tool message; got %d", len(resp.ToolMessages))
	}
	if resp.ToolMessages[0].Content != "tool-echo-result" {
		t.Errorf("tool result content = %q", resp.ToolMessages[0].Content)
	}
	if resp.ToolMessages[0].ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q", resp.ToolMessages[0].ToolCallID)
	}
	if resp.ToolMessages[0].ToolName != "echo" {
		t.Errorf("ToolName = %q", resp.ToolMessages[0].ToolName)
	}
	if resp.Iterations != 2 {
		t.Errorf("Iterations = %d want 2", resp.Iterations)
	}
	if echo.count.Load() != 1 {
		t.Errorf("echo invoked %d times, want 1", echo.count.Load())
	}

	// Persistence: user + assistant(tool_calls) + tool + assistant(final)
	msgs, err := store.ListMessages(context.Background(), "thr_chain")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 persisted messages, got %d (%+v)", len(msgs), msgs)
	}
	wantRoles := []string{RoleUser, RoleAssistant, RoleTool, RoleAssistant}
	for i, want := range wantRoles {
		if msgs[i].Role != want {
			t.Errorf("msgs[%d].Role = %q want %q", i, msgs[i].Role, want)
		}
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Errorf("first assistant message should carry tool_calls; got %+v", msgs[1])
	}
	if msgs[2].ToolName != "echo" || msgs[2].ToolCallID != "call_1" {
		t.Errorf("tool message metadata wrong: %+v", msgs[2])
	}
}

func TestSendMessage_FunctionCallChain_ToolHistoryFedBack(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(&counterTool{def: ToolDef{Name: "echo"}, reply: func(_ json.RawMessage) (string, error) {
		return "the answer is 42", nil
	}})

	sp := &scriptedProvider{
		responses: []ChatResponse{
			{ToolCalls: []ToolCall{{ID: "c1", Name: "echo"}}},
			{Content: "done"},
		},
	}
	h, store := newScriptedHandler(t, sp, tools)
	_ = store.CreateThread(context.Background(), &Thread{
		ID: "thr_back", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	_, _ = sendMessage(t, h, "thr_back", "user:alice", "go")

	calls := sp.Calls()
	if len(calls) != 2 {
		t.Fatalf("provider call count = %d want 2", len(calls))
	}
	// Second call must include the RoleTool message produced from the
	// first call's tool execution.
	found := false
	for _, m := range calls[1].Messages {
		if m.Role == RoleTool && m.Content == "the answer is 42" && m.ToolCallID == "c1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("second Provider.Complete call did not include tool result; got %+v", calls[1].Messages)
	}
}

func TestSendMessage_FunctionCallChain_StopsAtMaxIterations(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(&counterTool{def: ToolDef{Name: "loop"}, reply: func(_ json.RawMessage) (string, error) {
		return "more", nil
	}})

	// Always request another tool call — the loop must terminate after
	// MaxToolCallIterations.
	sp := &scriptedProvider{}
	for i := 0; i < MaxToolCallIterations+2; i++ {
		sp.responses = append(sp.responses, ChatResponse{
			ToolCalls: []ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "loop"}},
		})
	}
	h, store := newScriptedHandler(t, sp, tools)
	_ = store.CreateThread(context.Background(), &Thread{
		ID: "thr_inf", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	w, _ := sendMessage(t, h, "thr_inf", "user:alice", "go forever")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["errorName"] != "AIPToolLoopExceeded" {
		t.Errorf("errorName = %v want AIPToolLoopExceeded (body=%s)", body["errorName"], w.Body.String())
	}
	if calls := sp.Calls(); len(calls) != MaxToolCallIterations {
		t.Errorf("provider was called %d times, want %d", len(calls), MaxToolCallIterations)
	}
}

func TestSendMessage_FunctionCallChain_UnknownToolReturnsAIPToolNotFound(t *testing.T) {
	tools := NewToolRegistry() // empty registry
	sp := &scriptedProvider{
		responses: []ChatResponse{
			{ToolCalls: []ToolCall{{ID: "c1", Name: "ghost"}}},
		},
	}
	h, store := newScriptedHandler(t, sp, tools)
	_ = store.CreateThread(context.Background(), &Thread{
		ID: "thr_ghost", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	w, _ := sendMessage(t, h, "thr_ghost", "user:alice", "go")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["errorName"] != "AIPToolNotFound" {
		t.Errorf("errorName = %v want AIPToolNotFound", body["errorName"])
	}
}

func TestSendMessage_NoToolRegistry_StillSingleTurn(t *testing.T) {
	sp := &scriptedProvider{
		responses: []ChatResponse{
			{Content: "plain reply", Model: "scripted"},
		},
	}
	store := NewMemoryStore()
	reg := NewRegistry()
	reg.Register(sp)
	h := NewHandler(store, reg) // no SetToolRegistry
	_ = store.CreateThread(context.Background(), &Thread{
		ID: "thr_plain", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	w, resp := sendMessage(t, h, "thr_plain", "user:alice", "hi")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if resp.AssistantMessage.Content != "plain reply" {
		t.Errorf("Content = %q", resp.AssistantMessage.Content)
	}
	if resp.Iterations != 1 {
		t.Errorf("Iterations = %d want 1", resp.Iterations)
	}
	if len(resp.ToolMessages) != 0 {
		t.Errorf("ToolMessages should be empty; got %v", resp.ToolMessages)
	}
}

func TestSendMessage_ToolRegistryWired_DefinitionsForwarded(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(&counterTool{def: ToolDef{Name: "alpha"}})
	tools.Register(&counterTool{def: ToolDef{Name: "beta"}})

	sp := &scriptedProvider{
		responses: []ChatResponse{
			{Content: "no tool needed", Model: "scripted"},
		},
	}
	h, store := newScriptedHandler(t, sp, tools)
	_ = store.CreateThread(context.Background(), &Thread{
		ID: "thr_def", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	_, _ = sendMessage(t, h, "thr_def", "user:alice", "hi")

	calls := sp.Calls()
	if len(calls) != 1 {
		t.Fatalf("call count = %d", len(calls))
	}
	got := []string{}
	for _, td := range calls[0].Tools {
		got = append(got, td.Name)
	}
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("ChatRequest.Tools names = %v want [alpha beta]", got)
	}
}

func TestMockProvider_ToolCallingRoundTrip(t *testing.T) {
	p := NewMockProvider()
	tools := []ToolDef{{Name: "echo"}}

	// First call with a user message → should request a tool call.
	resp, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}},
		Tools:    tools,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "echo" {
		t.Fatalf("expected tool call to echo; got %+v", resp.ToolCalls)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content on tool-call turn; got %q", resp.Content)
	}

	// Second call with a RoleTool result → should produce final text.
	resp, err = p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, ToolCalls: resp.ToolCalls},
			{Role: RoleTool, ToolCallID: "x", ToolName: "echo", Content: "hi"},
		},
		Tools: tools,
	})
	if err != nil {
		t.Fatalf("Complete second: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("second turn should not request tools; got %+v", resp.ToolCalls)
	}
	if !strings.Contains(resp.Content, "tool-result") {
		t.Errorf("expected tool-result text; got %q", resp.Content)
	}
}
