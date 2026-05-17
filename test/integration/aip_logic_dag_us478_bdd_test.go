//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/aip/logic"
	"github.com/liyang/weave/pkg/auth"
)

// US-478 — AIP Logic Block DAG 执行器（topological + retry + fallback）— BDD.
//
// Three scenarios drive the PRD acceptance verbatim against the real HTTP
// surface (chi router + logic.Handler + logic.Executor + MemoryStore):
//
// Scenario A — DiamondConcurrentDispatch: a diamond-shaped flow whose
// two middle nodes share a barrier(2) tool. The barrier resolves iff
// both nodes are dispatched concurrently; a sequential executor times
// out at the barrier. Hits POST /execute and asserts 200 + both
// branches contribute output. This proves the PRD "并发执行同层节点"
// gate through the wire surface.
//
// Scenario B — NodeFallbackRoutes: a flow whose primary tool node names
// a missing tool (guaranteed failure) and points at a sibling via
// config.fallbackNodeId. The executor routes the primary's failure
// through the sibling and the wire response surfaces UsedFallbackNode +
// FallbackNodeID on the trace entry. Proves the PRD "失败时跳转
// fallbackNodeId" gate end-to-end.
//
// Scenario C — RetryExponentialBackoff: a flow whose primary tool fails
// twice then succeeds, with retry.strategy=exponential and backoffMs=30.
// Two retries sleep 30 + 60 = 90ms; a fixed-strategy regression would
// total 60ms. Asserting elapsed ≥ 80ms (with slack) through HTTP locks
// in the exponential semantics. Plus the flaky tool's call count = 3
// confirms maxAttempts=3 was honoured.

func setupUS478LogicRouter(t *testing.T) (*chi.Mux, *logic.MapToolRegistry) {
	t.Helper()
	store := logic.NewMemoryStore()
	tools := logic.NewMapToolRegistry()
	exec := logic.NewExecutor(nil, tools)
	h := logic.NewHandler(store, exec)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, tools
}

func bddUS478Request(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	ctx := auth.WithUser(req.Context(), &auth.User{ID: "user:bdd478"})
	return req.WithContext(ctx)
}

func bddUS478CreateFlow(t *testing.T, r *chi.Mux, body string) *logic.Flow {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, bddUS478Request(http.MethodPost, "/api/v2/aip/logic-flows", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create flow: status=%d body=%s", w.Code, w.Body.String())
	}
	var f logic.Flow
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatalf("decode flow: %v body=%s", err, w.Body.String())
	}
	return &f
}

func bddUS478ExecuteFlow(t *testing.T, r *chi.Mux, flowID, inputJSON string, wantStatus int) *logic.Run {
	t.Helper()
	body := "{}"
	if inputJSON != "" {
		body = inputJSON
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, bddUS478Request(http.MethodPost,
		"/api/v2/aip/logic-flows/"+flowID+"/execute", body))
	if w.Code != wantStatus {
		t.Fatalf("execute: status=%d want %d body=%s", w.Code, wantStatus, w.Body.String())
	}
	var run logic.Run
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v body=%s", err, w.Body.String())
	}
	return &run
}

// bddBarrierTool: only completes once wantN concurrent Invoke calls
// arrive. Used by scenario A to prove concurrent layer dispatch.
type bddBarrierTool struct {
	name    string
	started atomic.Int64
	wantN   int64
	timeout time.Duration
	done    chan struct{}
	once    sync.Once
}

func (t *bddBarrierTool) Name() string { return t.name }
func (t *bddBarrierTool) Invoke(ctx context.Context, params map[string]any) (map[string]any, error) {
	n := t.started.Add(1)
	if n == t.wantN {
		t.once.Do(func() { close(t.done) })
	}
	select {
	case <-t.done:
		return map[string]any{"id": params["id"], "n": n}, nil
	case <-time.After(t.timeout):
		return nil, fmt.Errorf("barrier %q timeout: %d/%d", t.name, t.started.Load(), t.wantN)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// bddFlakyTool fails the first failUntil-1 calls then succeeds.
type bddFlakyTool struct {
	name      string
	failUntil int64
	calls     atomic.Int64
}

func (t *bddFlakyTool) Name() string { return t.name }
func (t *bddFlakyTool) Invoke(_ context.Context, _ map[string]any) (map[string]any, error) {
	n := t.calls.Add(1)
	if n < t.failUntil {
		return nil, fmt.Errorf("flaky #%d", n)
	}
	return map[string]any{"text": "recovered"}, nil
}

func TestBDD_US478_DiamondDAG_DispatchesSameLayerConcurrently(t *testing.T) {
	router, tools := setupUS478LogicRouter(t)
	tools.Register(&bddBarrierTool{name: "bdd-barrier-2", wantN: 2, timeout: 1500 * time.Millisecond, done: make(chan struct{})})

	flowJSON := `{
		"id":"flow_bdd_us478_diamond",
		"name":"diamond",
		"nodes":[
			{"id":"a","type":"tool","config":{"tool":"echo","params":{"text":"start"}}},
			{"id":"b","type":"tool","config":{"tool":"bdd-barrier-2","params":{"id":"B"}}},
			{"id":"c","type":"tool","config":{"tool":"bdd-barrier-2","params":{"id":"C"}}},
			{"id":"out","type":"output","config":{"keys":["b.id","c.id"]}}
		],
		"edges":[
			{"from":"a","to":"b"},
			{"from":"a","to":"c"},
			{"from":"b","to":"out"},
			{"from":"c","to":"out"}
		]
	}`
	flow := bddUS478CreateFlow(t, router, flowJSON)

	start := time.Now()
	run := bddUS478ExecuteFlow(t, router, flow.ID, "{}", http.StatusOK)
	elapsed := time.Since(start)
	if run.Status != logic.RunStatusSuccess {
		t.Fatalf("status=%q err=%q", run.Status, run.Error)
	}
	if got, _ := run.Output["b.id"].(string); got != "B" {
		t.Errorf("output[b.id]=%v want B (full=%v)", run.Output["b.id"], run.Output)
	}
	if got, _ := run.Output["c.id"].(string); got != "C" {
		t.Errorf("output[c.id]=%v want C", run.Output["c.id"])
	}
	// Concurrent dispatch resolves the barrier almost immediately;
	// sequential dispatch would timeout at ~1500ms. 1s is a safe upper
	// bound that still catches "didn't actually parallelize".
	if elapsed > 1*time.Second {
		t.Errorf("diamond elapsed %v — likely serial, want < 1s", elapsed)
	}
}

func TestBDD_US478_NodeFallbackRoutes_OnPrimaryFailure(t *testing.T) {
	router, _ := setupUS478LogicRouter(t)

	flowJSON := `{
		"id":"flow_bdd_us478_fallback",
		"name":"fallback",
		"nodes":[
			{"id":"primary","type":"tool","config":{
				"tool":"doesnotexist",
				"retry":{"maxAttempts":1},
				"fallbackNodeId":"backup"
			}},
			{"id":"backup","type":"tool","config":{
				"tool":"echo",
				"params":{"text":"rescue"},
				"fallbackOnly":true
			}},
			{"id":"out","type":"output","config":{"keys":["primary.text"]}}
		],
		"edges":[{"from":"primary","to":"out"}]
	}`
	flow := bddUS478CreateFlow(t, router, flowJSON)
	run := bddUS478ExecuteFlow(t, router, flow.ID, "{}", http.StatusOK)
	if run.Status != logic.RunStatusSuccess {
		t.Fatalf("status=%q err=%q", run.Status, run.Error)
	}
	if got, _ := run.Output["primary.text"].(string); got != "rescue" {
		t.Errorf("output[primary.text]=%v want rescue", run.Output)
	}
	var primary logic.TraceEntry
	for _, te := range run.Trace {
		if te.NodeID == "primary" {
			primary = te
		}
	}
	if !primary.UsedFallbackNode {
		t.Errorf("primary.UsedFallbackNode=false, trace=%+v", run.Trace)
	}
	if primary.FallbackNodeID != "backup" {
		t.Errorf("primary.FallbackNodeID=%q want backup", primary.FallbackNodeID)
	}
}

func TestBDD_US478_RetryExponentialBackoff_HonouredOverHTTP(t *testing.T) {
	router, tools := setupUS478LogicRouter(t)
	flaky := &bddFlakyTool{name: "bdd-flaky-us478", failUntil: 3}
	tools.Register(flaky)

	flowJSON := `{
		"id":"flow_bdd_us478_expo",
		"name":"expo",
		"nodes":[
			{"id":"n1","type":"tool","config":{
				"tool":"bdd-flaky-us478",
				"retry":{"maxAttempts":3,"backoffMs":30,"strategy":"exponential"}
			}},
			{"id":"out","type":"output","config":{"keys":["n1.text"]}}
		],
		"edges":[{"from":"n1","to":"out"}]
	}`
	flow := bddUS478CreateFlow(t, router, flowJSON)

	start := time.Now()
	run := bddUS478ExecuteFlow(t, router, flow.ID, "{}", http.StatusOK)
	elapsed := time.Since(start)
	if run.Status != logic.RunStatusSuccess {
		t.Fatalf("status=%q err=%q", run.Status, run.Error)
	}
	if got, _ := run.Output["n1.text"].(string); got != "recovered" {
		t.Errorf("output[n1.text]=%v want recovered", run.Output)
	}
	if flaky.calls.Load() != 3 {
		t.Errorf("flaky tool called %d times, want 3", flaky.calls.Load())
	}
	if elapsed < 80*time.Millisecond {
		t.Errorf("expected ≥80ms (30+60 exponential), got %v", elapsed)
	}
	if elapsed > 600*time.Millisecond {
		t.Errorf("retry elapsed %v exceeds sane upper bound 600ms", elapsed)
	}
}
