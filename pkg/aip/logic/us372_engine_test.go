package logic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/aip"
)

// flakyProvider fails the first failUntil-1 calls then succeeds. Used to
// drive the executor's per-node retry budget without dragging in a real
// LLM.
type flakyProvider struct {
	name      string
	reply     string
	failUntil int
	calls     atomic.Int64
}

func (p *flakyProvider) Name() string { return p.name }
func (p *flakyProvider) Complete(_ context.Context, req aip.ChatRequest) (*aip.ChatResponse, error) {
	n := int(p.calls.Add(1))
	if n < p.failUntil {
		return nil, fmt.Errorf("flaky #%d", n)
	}
	return &aip.ChatResponse{Content: p.reply, Model: req.Model, TokenCount: 1}, nil
}

// modelAwareProvider returns a different reply per model so fallback
// tests can confirm the swap actually happened. It also accepts an
// "always-fail" model name so tests can assert primary-model failure.
type modelAwareProvider struct {
	name        string
	failOnModel string
	calls       []aip.ChatRequest
	mu          sync.Mutex
}

func (p *modelAwareProvider) Name() string { return p.name }
func (p *modelAwareProvider) Complete(_ context.Context, req aip.ChatRequest) (*aip.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, req)
	if req.Model == p.failOnModel {
		return nil, fmt.Errorf("model %q is sad", req.Model)
	}
	return &aip.ChatResponse{Content: "via " + req.Model, Model: req.Model, TokenCount: 1}, nil
}

// multiResolver routes lookups by provider name to one of N providers.
// Tests use it when an LLM node and the fallback both go through the
// same provider but different models. Mirrors aip.Registry's surface
// without the registration ceremony.
type multiResolver struct {
	providers map[string]aip.Provider
}

func (r *multiResolver) Get(name string) (aip.Provider, error) {
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("no provider %q", name)
}

func TestUS372_Iterate_RunsBodyForEachItem_RespectsCap(t *testing.T) {
	tools := NewMapToolRegistry()
	exec := NewExecutor(nil, tools)
	flow := &Flow{
		ID: "flow_iter_ok",
		Nodes: []Node{
			{ID: "loop", Type: NodeTypeIterate, Config: map[string]any{
				"forEach": "input.items",
				"body": map[string]any{
					"id":   "step",
					"type": NodeTypeTool,
					"config": map[string]any{
						"tool": "echo",
						"params": map[string]any{
							"text": "x={{iterate.step.item}}",
						},
					},
				},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"loop.results", "loop.iterations"},
			}},
		},
		Edges: []Edge{{From: "loop", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow,
		map[string]any{"items": []any{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	results, _ := run.Output["loop.results"].([]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 iterations, got %d (%v)", len(results), run.Output)
	}
	for i, want := range []string{"x=a", "x=b", "x=c"} {
		got, _ := results[i].(map[string]any)["text"].(string)
		if got != want {
			t.Errorf("iteration[%d]: got %q want %q", i, got, want)
		}
	}
	iter, _ := run.Output["loop.iterations"].(int)
	if iter != 3 {
		t.Errorf("iterations counter: got %v want 3", run.Output["loop.iterations"])
	}
}

func TestUS372_Iterate_OverCapAborts(t *testing.T) {
	tools := NewMapToolRegistry()
	exec := NewExecutor(nil, tools)
	items := make([]any, MaxIterateItems+1)
	for i := range items {
		items[i] = i
	}
	flow := &Flow{
		ID: "flow_iter_over",
		Nodes: []Node{
			{ID: "loop", Type: NodeTypeIterate, Config: map[string]any{
				"forEach": "input.items",
				"body": map[string]any{
					"id":   "step",
					"type": NodeTypeTool,
					"config": map[string]any{
						"tool": "echo", "params": map[string]any{"text": "x"},
					},
				},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "loop", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow,
		map[string]any{"items": items})
	if err == nil {
		t.Fatalf("expected ErrIterateLimitExceeded, got nil (run=%+v)", run)
	}
	if !errors.Is(err, ErrIterateLimitExceeded) {
		t.Errorf("error chain: got %v, want ErrIterateLimitExceeded", err)
	}
	if run.Status != RunStatusFailed {
		t.Errorf("expected failed status, got %q", run.Status)
	}
}

func TestUS372_Iterate_AcceptsTemplateWrappedForEach(t *testing.T) {
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_iter_tpl",
		Nodes: []Node{
			{ID: "loop", Type: NodeTypeIterate, Config: map[string]any{
				"forEach": "{{input.items}}",
				"body": map[string]any{
					"id": "step", "type": NodeTypeTool,
					"config": map[string]any{
						"tool": "echo", "params": map[string]any{"text": "ok"},
					},
				},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"loop.iterations"},
			}},
		},
		Edges: []Edge{{From: "loop", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow,
		map[string]any{"items": []any{"a", "b"}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if iter, _ := run.Output["loop.iterations"].(int); iter != 2 {
		t.Errorf("iterations: got %v want 2", run.Output)
	}
}

func TestUS372_Iterate_RejectsNestedIterate(t *testing.T) {
	flow := &Flow{
		ID: "flow_iter_nested",
		Nodes: []Node{
			{ID: "loop", Type: NodeTypeIterate, Config: map[string]any{
				"forEach": "input.items",
				"body": map[string]any{
					"id": "inner", "type": NodeTypeIterate,
					"config": map[string]any{
						"forEach": "input.x",
						"body": map[string]any{
							"id": "inmost", "type": NodeTypeTool,
							"config": map[string]any{"tool": "echo"},
						},
					},
				},
			}},
		},
	}
	if err := flow.Validate(); err == nil {
		t.Fatalf("expected nested-iterate rejection")
	}
}

func TestUS372_Retry_NodeLevelMaxAttempts_RecoversAfterFailures(t *testing.T) {
	prov := &flakyProvider{name: "mock", reply: "ok", failUntil: 3}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_retry_node",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock", "promptTemplate": "x",
				"retry": map[string]any{"maxAttempts": 3},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"n1.content"},
			}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got, _ := run.Output["n1.content"].(string); got != "ok" {
		t.Errorf("expected ok, got %v", run.Output)
	}
	if prov.calls.Load() != 3 {
		t.Errorf("expected 3 LLM calls (2 retries), got %d", prov.calls.Load())
	}
	var attempts int
	for _, te := range run.Trace {
		if te.NodeID == "n1" {
			attempts = te.Attempts
		}
	}
	if attempts != 3 {
		t.Errorf("trace attempts=%d want 3", attempts)
	}
}

func TestUS372_Retry_ExhaustionPropagates(t *testing.T) {
	prov := &flakyProvider{name: "mock", reply: "ok", failUntil: 99}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_retry_exhaust",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock", "promptTemplate": "x",
				"retry": map[string]any{"maxAttempts": 2},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if prov.calls.Load() != 2 {
		t.Errorf("expected 2 attempts before giving up, got %d", prov.calls.Load())
	}
	if run.Status != RunStatusFailed {
		t.Errorf("expected failed, got %q", run.Status)
	}
}

func TestUS372_Retry_FlowLevelMaxRetriesDefault(t *testing.T) {
	prov := &flakyProvider{name: "mock", reply: "ok", failUntil: 2}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())
	flow := &Flow{
		ID:         "flow_retry_default",
		MaxRetries: 2, // total attempts = 3
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock", "promptTemplate": "x",
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != RunStatusSuccess {
		t.Fatalf("expected success: %q", run.Error)
	}
	if prov.calls.Load() != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 success), got %d", prov.calls.Load())
	}
}

func TestUS372_FallbackModel_SwitchesAfterRetries(t *testing.T) {
	prov := &modelAwareProvider{name: "mock", failOnModel: "primary-v1"}
	exec := NewExecutor(&multiResolver{providers: map[string]aip.Provider{"mock": prov}}, NewMapToolRegistry())
	flow := &Flow{
		ID:            "flow_fallback",
		FallbackModel: "fallback-v1",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock", "model": "primary-v1",
				"promptTemplate": "hello",
				"retry":          map[string]any{"maxAttempts": 2},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"n1.content"},
			}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := run.Output["n1.content"].(string)
	if got != "via fallback-v1" {
		t.Errorf("expected fallback content, got %q (output=%v)", got, run.Output)
	}
	// Two primary attempts then one fallback call = 3 total.
	if len(prov.calls) != 3 {
		t.Fatalf("expected 3 provider calls, got %d", len(prov.calls))
	}
	if prov.calls[2].Model != "fallback-v1" {
		t.Errorf("third call should target fallback model, got %q", prov.calls[2].Model)
	}
	var ufb bool
	for _, te := range run.Trace {
		if te.NodeID == "n1" {
			ufb = te.UsedFallback
		}
	}
	if !ufb {
		t.Errorf("trace should record UsedFallback=true")
	}
}

func TestUS372_FallbackModel_NoFallbackForToolNode(t *testing.T) {
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_tool_no_fallback", FallbackModel: "fallback-v1",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeTool, Config: map[string]any{
				"tool":  "doesnotexist",
				"retry": map[string]any{"maxAttempts": 2},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err == nil {
		t.Fatalf("expected error, got nil (run=%+v)", run)
	}
	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
	for _, te := range run.Trace {
		if te.UsedFallback {
			t.Errorf("tool node should never trip the LLM fallback policy")
		}
	}
}

func TestUS372_E2E_LLM_Tool_If_Flow(t *testing.T) {
	// PRD acceptance gate: "3 节点 LLM→tool→conditional 流程跑通".
	prov := &fakeProvider{name: "mock", reply: "long content"}
	tools := NewMapToolRegistry()
	exec := NewExecutor(&stubProviderResolver{provider: prov}, tools)
	flow := &Flow{
		ID: "flow_e2e_llm_tool_if",
		Nodes: []Node{
			{ID: "summary", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock", "promptTemplate": "Summarize {{input.text}}",
			}},
			{ID: "fmt", Type: NodeTypeTool, Config: map[string]any{
				"tool": "echo",
				"params": map[string]any{
					"text": "result={{summary.content}}",
				},
			}},
			{ID: "decide", Type: NodeTypeIf, Config: map[string]any{
				// "long content" length > threshold so true branch fires
				"condition": "{{summary.content}} contains long",
			}},
			{ID: "yes", Type: NodeTypeTool, Config: map[string]any{
				"tool":   "echo",
				"params": map[string]any{"text": "long-flow"},
			}},
			{ID: "no", Type: NodeTypeTool, Config: map[string]any{
				"tool":   "echo",
				"params": map[string]any{"text": "short-flow"},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"fmt.text", "yes.text", "no.text"},
			}},
		},
		Edges: []Edge{
			{From: "summary", To: "fmt"},
			{From: "fmt", To: "decide"},
			{From: "decide", To: "yes", Branch: BranchTrue},
			{From: "decide", To: "no", Branch: BranchFalse},
			{From: "yes", To: "out"},
			{From: "no", To: "out"},
		},
	}
	run, err := exec.Execute(context.Background(), flow,
		map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.Status != RunStatusSuccess {
		t.Fatalf("status=%q err=%q", run.Status, run.Error)
	}
	if got, _ := run.Output["fmt.text"].(string); got != "result=long content" {
		t.Errorf("fmt.text: got %q", got)
	}
	if got, _ := run.Output["yes.text"].(string); got != "long-flow" {
		t.Errorf("yes branch should fire, got %v", run.Output)
	}
	if _, ok := run.Output["no.text"]; ok {
		t.Errorf("no branch should be skipped, output=%v", run.Output)
	}
	// Trace shows 6 entries — all 6 nodes visited (false branch
	// reaches the executor as a Skipped record).
	if len(run.Trace) != 6 {
		t.Errorf("expected 6 trace entries, got %d (%+v)", len(run.Trace), run.Trace)
	}
}

func TestUS372_Validate_RejectsRetryOutOfRange(t *testing.T) {
	f := &Flow{
		ID: "flow_retry_bad",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock",
				"retry":    map[string]any{"maxAttempts": MaxRetryAttempts + 1},
			}},
		},
	}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "maxAttempts") {
		t.Fatalf("expected maxAttempts error, got %v", err)
	}
}

func TestUS372_Validate_RejectsFlowMaxRetriesOutOfRange(t *testing.T) {
	f := &Flow{
		ID:         "flow_mr_bad",
		MaxRetries: MaxRetryAttempts + 1,
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{"provider": "mock"}},
		},
	}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "maxRetries") {
		t.Fatalf("expected maxRetries error, got %v", err)
	}
}

func TestUS372_Retry_BackoffDelayHonoured(t *testing.T) {
	prov := &flakyProvider{name: "mock", reply: "ok", failUntil: 2}
	exec := NewExecutor(&stubProviderResolver{provider: prov}, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_backoff",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeLLM, Config: map[string]any{
				"provider": "mock", "promptTemplate": "x",
				"retry": map[string]any{"maxAttempts": 2, "backoffMs": 25},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	start := time.Now()
	if _, err := exec.Execute(context.Background(), flow, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("expected at least 20ms backoff, got %v", elapsed)
	}
}

func TestUS372_Iterate_NotAnArrayFails(t *testing.T) {
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_iter_bad_type",
		Nodes: []Node{
			{ID: "loop", Type: NodeTypeIterate, Config: map[string]any{
				"forEach": "input.items",
				"body": map[string]any{
					"id": "step", "type": NodeTypeTool,
					"config": map[string]any{"tool": "echo"},
				},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "loop", To: "out"}},
	}
	_, err := exec.Execute(context.Background(), flow,
		map[string]any{"items": "not-an-array"})
	if err == nil || !strings.Contains(err.Error(), "not an array") {
		t.Fatalf("expected array-type error, got %v", err)
	}
}

func TestUS372_HandlerCreate_AcceptsFallbackAndMaxRetries(t *testing.T) {
	h, store := newHandlerWithMockProvider()
	r := newRouter(h)
	body := `{
		"id":"flow_handler_us372",
		"name":"x",
		"fallbackModel":"backup",
		"maxRetries":2,
		"nodes":[
			{"id":"n1","type":"llm","config":{"provider":"mock","promptTemplate":"hi"}},
			{"id":"out","type":"output","config":{}}
		],
		"edges":[{"from":"n1","to":"out"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/logic-flows", strings.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	got, err := store.GetFlow(context.Background(), "flow_handler_us372")
	if err != nil {
		t.Fatalf("GetFlow: %v", err)
	}
	if got.FallbackModel != "backup" || got.MaxRetries != 2 {
		t.Errorf("flow round-trip dropped US-372 fields: %+v", got)
	}
}
