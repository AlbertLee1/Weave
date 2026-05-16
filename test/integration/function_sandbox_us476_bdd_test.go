//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

// US-476 — Function 沙箱配额（栈/内存/超时）(BDD).
//
// PRD acceptance verbatim:
//   - 8 层递归、100MB heap、1s 超时（可配）
//   - 负向测试：无限递归被 abort；超时函数被 cancel
//
// Three scenarios drive the end-to-end behaviour through the real chi
// router + the real OMSHandler /execute endpoint backed by a goja runtime
// constructed with RestrictedConfig(). Each scenario asserts the externally
// observable HTTP contract — status code, error code, and elapsed wall time
// — so a future regression that re-hardcodes the stack quota or silently
// drops the timeout watchdog surfaces as a failed assertion, not a slow
// or hung request.

// us476GojaExecutor adapts a *functions.Runtime onto oms.FunctionExecutor.
// Mirrors us475GojaExecutor but stays self-contained for the US-476 BDD so
// the test file compiles even when the US-475 file is excluded.
type us476GojaExecutor struct{ rt *functions.Runtime }

func (g *us476GojaExecutor) Execute(ctx context.Context, fn *oms.Function, params map[string]interface{}) (interface{}, error) {
	return g.rt.Execute(ctx, fn.SourceCode, map[string]interface{}{
		"parameters": params,
	})
}

func setupUS476SandboxFixture(t *testing.T, sourceCode, fnRIDSuffix, fnName string) (
	router *chi.Mux,
	fn *oms.Function,
	ontologyAPIName string,
) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.us476-bdd-" + fnRIDSuffix,
		APIName:     "us476-bdd-" + fnRIDSuffix,
		DisplayName: "US-476 BDD " + fnName,
	}
	if err := repo.CreateOntology(context.Background(), ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	fn = &oms.Function{
		RID:         "ri.ontology.main.function." + fnRIDSuffix,
		OntologyRID: ont.RID,
		Name:        fnName,
		Version:     "1.0.0",
		SourceCode:  sourceCode,
		Runtime:     "goja",
		Signature:   json.RawMessage(`{"params":[],"returns":{"type":"object"}}`),
	}
	if err := repo.CreateFunction(context.Background(), fn); err != nil {
		t.Fatalf("create function: %v", err)
	}

	handler := oms.NewOMSHandler(repo)
	// Restricted profile is the PRD US-476 acceptance criterion: 1s timeout,
	// 100MB heap, 8 stack frames.
	handler.SetFunctionExecutor(&us476GojaExecutor{rt: functions.NewRuntime(functions.RestrictedConfig())})

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)
	return r, fn, ont.APIName
}

// TestBDD_US476_Sandbox_AbortsInfiniteRecursion drives PRD literal "无限递归被
// abort": a function that recurses without a base case must surface as a
// failed Execute well under the 1s timeout window. RestrictedConfig pins
// MaxCallStackSize=8 so the goja stack-overflow trip is what aborts the
// run — NOT the timeout watchdog.
func TestBDD_US476_Sandbox_AbortsInfiniteRecursion(t *testing.T) {
	// Given a function that recurses without a base case
	router, fn, ontAPI := setupUS476SandboxFixture(t,
		`function main(input) {
		   function infinite() { return infinite(); }
		   return infinite();
		 }`,
		"recurse", "infiniteRecurse")

	// When the operator invokes /execute
	body, _ := json.Marshal(map[string]interface{}{"parameters": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/functions/"+fn.RID+"/execute",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	start := time.Now()
	router.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Then the runtime aborts via stack-overflow path well under the 1s
	// budget. The handler maps non-quota errors to 400 FunctionExecutionFailed
	// — but never to a 408 timeout (would mean the stack quota was not
	// enforced and the watchdog had to step in).
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-2xx for infinite recursion; got 200 body=%s", rec.Body.String())
	}
	if rec.Code == http.StatusRequestTimeout {
		t.Fatalf("infinite recursion fell through to timeout path (408) — stack quota not enforced; body=%s", rec.Body.String())
	}
	if elapsed > 800*time.Millisecond {
		t.Fatalf("infinite recursion aborted in %v — expected stack-quota trip in <800ms (timeout budget is 1s)", elapsed)
	}
	// The error body must be a structured FunctionExecutionFailed apierror
	// envelope (not an HTTP-level 500 or empty body) so SDK callers can
	// dispatch on the error code.
	var resp struct {
		ErrorCode string `json:"errorCode"`
		ErrorName string `json:"errorName"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !strings.Contains(resp.ErrorName, "FunctionExecutionFailed") &&
		!strings.Contains(resp.ErrorName, "FunctionExecution") {
		t.Fatalf("expected FunctionExecutionFailed-shaped error envelope; got code=%s name=%s", resp.ErrorCode, resp.ErrorName)
	}
}

// TestBDD_US476_Sandbox_CancelsTimeoutFunction drives PRD literal "超时函数被
// cancel": an infinite-loop function must be aborted at approximately the
// 1s deadline (RestrictedConfig.MaxExecutionTime) and surface as HTTP 408
// FunctionExecutionTimeout.
func TestBDD_US476_Sandbox_CancelsTimeoutFunction(t *testing.T) {
	// Given a function with an infinite tight loop
	router, fn, ontAPI := setupUS476SandboxFixture(t,
		`function main(input) {
		   while (true) {}
		 }`,
		"loop", "infiniteLoop")

	// When the operator invokes /execute
	body, _ := json.Marshal(map[string]interface{}{"parameters": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/functions/"+fn.RID+"/execute",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	start := time.Now()
	router.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// Then the runtime cancels at ~1s + watchdog overhead and emits the
	// FunctionExecutionTimeout apierror.
	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("expected HTTP 408 RequestTimeout for infinite loop; got %d body=%s", rec.Code, rec.Body.String())
	}
	// Handler enforces a 5s deadline outside the goja config; the inner
	// runtime should trip first at ~1s. Anything > 3s means the inner
	// quota isn't actually firing.
	if elapsed > 3*time.Second {
		t.Fatalf("timeout cancellation took %v — runtime watchdog likely missed; handler 5s fallback fired instead", elapsed)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("timeout fired in %v — expected ~1s (premature watchdog)", elapsed)
	}
	var resp struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !strings.Contains(resp.ErrorName, "FunctionExecutionTimeout") {
		t.Fatalf("expected FunctionExecutionTimeout error name; got %q", resp.ErrorName)
	}
}

// TestBDD_US476_Sandbox_BenignFunctionUnderQuota_Returns200 is the positive
// control: a function well under all three quotas (constant-time, no
// allocation, no recursion) must still complete cleanly through the
// restricted profile. Without this, a regression that accidentally aborts
// all functions would still pass the two negative tests.
func TestBDD_US476_Sandbox_BenignFunctionUnderQuota_Returns200(t *testing.T) {
	// Given a constant-time function with no recursion / allocation
	router, fn, ontAPI := setupUS476SandboxFixture(t,
		`function main(input) {
		   return { ok: true, depth: 1 };
		 }`,
		"benign", "benignReturn")

	// When the operator invokes /execute
	body, _ := json.Marshal(map[string]interface{}{"parameters": map[string]interface{}{}})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/functions/"+fn.RID+"/execute",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Then the call returns 200 with the expected result shape — proves the
	// restricted profile doesn't aborts everything.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for benign function; got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Result map[string]interface{} `json:"result"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if v, _ := resp.Result["ok"].(bool); !v {
		t.Fatalf("expected result.ok = true; got %v", resp.Result)
	}
}
