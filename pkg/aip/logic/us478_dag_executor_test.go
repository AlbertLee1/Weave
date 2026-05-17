package logic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// US-478: DAG Executor enhancements
//   - Executor 拓扑排序 + 并发执行同层节点
//   - 节点 retry policy（指数退避，maxAttempts=3）
//   - 节点 fallback：失败时跳转 fallbackNodeId
//   - 测试：菱形 DAG、含 retry 节点、含 fallback

// barrierTool blocks until exactly wantN concurrent Invoke calls are
// in-flight, then unblocks all of them. If the bar is not reached
// within timeout, every still-blocked Invoke returns an error so the
// test fails fast rather than hanging. Used to prove same-layer
// concurrency: two distinct tool nodes that both call barrier(2) cannot
// possibly complete unless the executor dispatches them in parallel.
type barrierTool struct {
	name    string
	started atomic.Int64
	wantN   int64
	timeout time.Duration
	done    chan struct{}
	once    sync.Once
}

func newBarrierTool(name string, wantN int64, timeout time.Duration) *barrierTool {
	return &barrierTool{
		name:    name,
		wantN:   wantN,
		timeout: timeout,
		done:    make(chan struct{}),
	}
}

func (t *barrierTool) Name() string { return t.name }
func (t *barrierTool) Invoke(ctx context.Context, params map[string]any) (map[string]any, error) {
	n := t.started.Add(1)
	if n == t.wantN {
		t.once.Do(func() { close(t.done) })
	}
	select {
	case <-t.done:
		return map[string]any{"id": params["id"], "started": n}, nil
	case <-time.After(t.timeout):
		return nil, fmt.Errorf("barrier %q timeout after %v: only %d/%d concurrent calls",
			t.name, t.timeout, t.started.Load(), t.wantN)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestUS478_DiamondDAG_SameLayerNodesRunConcurrently(t *testing.T) {
	// Diamond: A → B, A → C, B → D, C → D.
	// B and C share a barrier(2) — they MUST execute concurrently to
	// resolve the barrier within the deadline. A sequential executor
	// would hang at the barrier and time out.
	barrier := newBarrierTool("barrier2", 2, 750*time.Millisecond)
	reg := NewMapToolRegistry()
	reg.Register(barrier)

	exec := NewExecutor(nil, reg)
	flow := &Flow{
		ID: "flow_us478_diamond",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeTool, Config: map[string]any{
				"tool":   "echo",
				"params": map[string]any{"text": "start"},
			}},
			{ID: "b", Type: NodeTypeTool, Config: map[string]any{
				"tool":   "barrier2",
				"params": map[string]any{"id": "B"},
			}},
			{ID: "c", Type: NodeTypeTool, Config: map[string]any{
				"tool":   "barrier2",
				"params": map[string]any{"id": "C"},
			}},
			{ID: "d", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"b.id", "c.id"},
			}},
		},
		Edges: []Edge{
			{From: "a", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "d"},
		},
	}
	start := time.Now()
	run, err := exec.Execute(context.Background(), flow, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected success, got err=%v run=%+v elapsed=%v", err, run, elapsed)
	}
	if run.Status != RunStatusSuccess {
		t.Fatalf("status=%q err=%q", run.Status, run.Error)
	}
	if got, _ := run.Output["b.id"].(string); got != "B" {
		t.Errorf("output[b.id]=%v want B", run.Output)
	}
	if got, _ := run.Output["c.id"].(string); got != "C" {
		t.Errorf("output[c.id]=%v want C", run.Output)
	}
	if elapsed > 700*time.Millisecond {
		t.Errorf("diamond completion took %v — likely sequential (barrier timeout 750ms)", elapsed)
	}
}

func TestUS478_TopoLayers_DiamondGroupsBCInSameLayer(t *testing.T) {
	flow := &Flow{
		ID: "f",
		Nodes: []Node{
			{ID: "a", Type: NodeTypeOutput, Config: map[string]any{}},
			{ID: "b", Type: NodeTypeOutput, Config: map[string]any{}},
			{ID: "c", Type: NodeTypeOutput, Config: map[string]any{}},
			{ID: "d", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{
			{From: "a", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "d"},
		},
	}
	layers, err := flow.TopoLayers()
	if err != nil {
		t.Fatalf("TopoLayers: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %v", len(layers), layers)
	}
	if len(layers[0]) != 1 || layers[0][0] != "a" {
		t.Errorf("layer[0]=%v want [a]", layers[0])
	}
	got := map[string]bool{}
	for _, id := range layers[1] {
		got[id] = true
	}
	if !got["b"] || !got["c"] || len(got) != 2 {
		t.Errorf("layer[1]=%v want {b,c}", layers[1])
	}
	if len(layers[2]) != 1 || layers[2][0] != "d" {
		t.Errorf("layer[2]=%v want [d]", layers[2])
	}
}

// flakyTool fails the first failUntil-1 invocations, succeeds afterwards.
type flakyTool struct {
	name      string
	failUntil int64
	calls     atomic.Int64
}

func (t *flakyTool) Name() string { return t.name }
func (t *flakyTool) Invoke(_ context.Context, params map[string]any) (map[string]any, error) {
	n := t.calls.Add(1)
	if n < t.failUntil {
		return nil, fmt.Errorf("flaky-tool fail #%d", n)
	}
	return map[string]any{"text": "ok"}, nil
}

func TestUS478_Retry_ExponentialBackoff_DoublesBetweenAttempts(t *testing.T) {
	// PRD acceptance gate: "节点 retry policy（指数退避，maxAttempts=3）".
	// Three attempts with 30ms base + exponential strategy ⇒
	// expected sleeps: 0ms, 30ms, 60ms ⇒ total ≥ 80ms (a little slack).
	// Fixed backoff would total 30+30 = 60ms — the lower bound
	// distinguishes "exponential really took effect" from "still fixed".
	flaky := &flakyTool{name: "flaky", failUntil: 3}
	reg := NewMapToolRegistry()
	reg.Register(flaky)
	exec := NewExecutor(nil, reg)
	flow := &Flow{
		ID: "flow_us478_expo",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeTool, Config: map[string]any{
				"tool": "flaky",
				"retry": map[string]any{
					"maxAttempts": 3,
					"backoffMs":   30,
					"strategy":    "exponential",
				},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{"n1.text"}}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	start := time.Now()
	run, err := exec.Execute(context.Background(), flow, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got, _ := run.Output["n1.text"].(string); got != "ok" {
		t.Errorf("expected ok, got %v", run.Output)
	}
	if flaky.calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", flaky.calls.Load())
	}
	if elapsed < 80*time.Millisecond {
		t.Errorf("exponential backoff under-slept: %v want ≥ 80ms (sleeps 30+60)", elapsed)
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("exponential backoff over-slept: %v want < 400ms", elapsed)
	}
}

func TestUS478_Retry_FixedBackoff_StaysConstantBetweenAttempts(t *testing.T) {
	// Sanity: fixed strategy (default) totals base * (attempts-1).
	flaky := &flakyTool{name: "flaky2", failUntil: 3}
	reg := NewMapToolRegistry()
	reg.Register(flaky)
	exec := NewExecutor(nil, reg)
	flow := &Flow{
		ID: "flow_us478_fixed",
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeTool, Config: map[string]any{
				"tool": "flaky2",
				"retry": map[string]any{
					"maxAttempts": 3,
					"backoffMs":   30,
				},
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{"n1.text"}}},
		},
		Edges: []Edge{{From: "n1", To: "out"}},
	}
	start := time.Now()
	_, err := exec.Execute(context.Background(), flow, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// Fixed totals exactly 60ms (30 + 30); exponential would total 90ms.
	if elapsed > 80*time.Millisecond {
		t.Errorf("fixed backoff over-slept (looks exponential): %v", elapsed)
	}
}

func TestUS478_FallbackNodeId_RoutesOnPrimaryFailureAndProducesOutput(t *testing.T) {
	// PRD: "节点 fallback：失败时跳转 fallbackNodeId"
	// nodeA references a missing tool ⇒ guaranteed failure.
	// nodeA.config.fallbackNodeId = "nodeB" ⇒ executor invokes nodeB
	// once and uses its output as nodeA's. Trace records the routing.
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_us478_fallback",
		Nodes: []Node{
			{ID: "nodeA", Type: NodeTypeTool, Config: map[string]any{
				"tool":           "doesnotexist",
				"retry":          map[string]any{"maxAttempts": 1},
				"fallbackNodeId": "nodeB",
			}},
			{ID: "nodeB", Type: NodeTypeTool, Config: map[string]any{
				"tool":         "echo",
				"params":       map[string]any{"text": "rescue"},
				"fallbackOnly": true,
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{
				"keys": []any{"nodeA.text"},
			}},
		},
		Edges: []Edge{{From: "nodeA", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err != nil {
		t.Fatalf("expected success via fallback, got err=%v run=%+v", err, run)
	}
	if run.Status != RunStatusSuccess {
		t.Fatalf("status=%q err=%q", run.Status, run.Error)
	}
	if got, _ := run.Output["nodeA.text"].(string); got != "rescue" {
		t.Errorf("expected rescue, got %v", run.Output)
	}
	var primary TraceEntry
	for _, te := range run.Trace {
		if te.NodeID == "nodeA" {
			primary = te
		}
	}
	if !primary.UsedFallbackNode {
		t.Errorf("trace[nodeA].UsedFallbackNode=false want true; trace=%+v", run.Trace)
	}
	if primary.FallbackNodeID != "nodeB" {
		t.Errorf("trace[nodeA].FallbackNodeID=%q want nodeB", primary.FallbackNodeID)
	}
	if primary.Status != TraceStatusSuccess {
		t.Errorf("primary trace after successful fallback should be success; got %q", primary.Status)
	}
}

func TestUS478_FallbackNodeId_PropagatesWhenFallbackAlsoFails(t *testing.T) {
	// When fallback itself fails, the flow still fails; trace records
	// the routing attempt so authors can see what was tried.
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_us478_fallback_fails",
		Nodes: []Node{
			{ID: "nodeA", Type: NodeTypeTool, Config: map[string]any{
				"tool":           "missingPrimary",
				"retry":          map[string]any{"maxAttempts": 1},
				"fallbackNodeId": "nodeB",
			}},
			{ID: "nodeB", Type: NodeTypeTool, Config: map[string]any{
				"tool":         "missingFallback",
				"fallbackOnly": true,
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{}},
		},
		Edges: []Edge{{From: "nodeA", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err == nil {
		t.Fatalf("expected failure when fallback also fails; got run=%+v", run)
	}
	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
	if run.Status != RunStatusFailed {
		t.Errorf("status=%q want failed", run.Status)
	}
	var primary TraceEntry
	for _, te := range run.Trace {
		if te.NodeID == "nodeA" {
			primary = te
		}
	}
	if !primary.UsedFallbackNode {
		t.Errorf("trace[nodeA].UsedFallbackNode=false; want true even when fallback failed")
	}
	if primary.FallbackNodeID != "nodeB" {
		t.Errorf("trace[nodeA].FallbackNodeID=%q want nodeB", primary.FallbackNodeID)
	}
}

func TestUS478_Validate_RejectsFallbackNodeIdToUnknownNode(t *testing.T) {
	flow := &Flow{
		ID: "flow_us478_bad_fallback",
		Nodes: []Node{
			{ID: "nodeA", Type: NodeTypeTool, Config: map[string]any{
				"tool":           "echo",
				"fallbackNodeId": "missingNode",
			}},
		},
	}
	err := flow.Validate()
	if err == nil {
		t.Fatalf("expected validation failure for unknown fallbackNodeId")
	}
	if !contains(err.Error(), "fallbackNodeId") {
		t.Errorf("error should mention fallbackNodeId; got %v", err)
	}
}

func TestUS478_Validate_RejectsFallbackNodeIdSelfReference(t *testing.T) {
	flow := &Flow{
		ID: "flow_us478_self_fallback",
		Nodes: []Node{
			{ID: "nodeA", Type: NodeTypeTool, Config: map[string]any{
				"tool":           "echo",
				"fallbackNodeId": "nodeA",
			}},
		},
	}
	if err := flow.Validate(); err == nil {
		t.Fatalf("expected validation failure for self-referential fallbackNodeId")
	}
}

func TestUS478_FallbackOnly_SkipsNodeDuringNormalTopo(t *testing.T) {
	// A node with config.fallbackOnly=true must not produce a regular
	// trace entry — it exists solely as a fallback target.
	exec := NewExecutor(nil, NewMapToolRegistry())
	flow := &Flow{
		ID: "flow_us478_fallbackonly",
		Nodes: []Node{
			{ID: "primary", Type: NodeTypeTool, Config: map[string]any{
				"tool":   "echo",
				"params": map[string]any{"text": "ok"},
			}},
			{ID: "backup", Type: NodeTypeTool, Config: map[string]any{
				"tool":         "echo",
				"params":       map[string]any{"text": "rescue"},
				"fallbackOnly": true,
			}},
			{ID: "out", Type: NodeTypeOutput, Config: map[string]any{"keys": []any{"primary.text"}}},
		},
		Edges: []Edge{{From: "primary", To: "out"}},
	}
	run, err := exec.Execute(context.Background(), flow, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	// fallbackOnly nodes still appear in the trace as Skipped so the
	// run record acknowledges them — but they MUST NOT execute their
	// handler (status success/failed would mean they did run).
	for _, te := range run.Trace {
		if te.NodeID == "backup" && te.Status != TraceStatusSkipped {
			t.Errorf("backup node ran during normal topo: status=%q trace=%+v",
				te.Status, run.Trace)
		}
	}
	if _, has := run.Output["backup"]; has {
		t.Errorf("backup output leaked: %v", run.Output)
	}
}

// contains is a tiny strings.Contains helper for assertions that doesn't
// import the strings package twice in this file.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
