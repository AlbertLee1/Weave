package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/functions/fnerrors"
	"github.com/liyang/weave/pkg/oms"
)

// US-218: POST /functions/{rid}/execute enforces a 5s CPU budget, a 128MB
// memory ceiling (delegated to pkg/functions runtime), and a per-realm
// per-minute call quota. The tests below pin the HTTP status mapping —
// 408 for timeout, 429 for memory-limit and quota-exceeded — plus the
// quota-enforcement trigger path.

type boundedLimiter struct {
	allowed atomic.Int32
	seen    []string
}

func (b *boundedLimiter) Allow(key string) bool {
	b.seen = append(b.seen, key)
	return b.allowed.Add(-1) >= 0
}

type slowExecutor struct {
	delay  time.Duration
	err    error
	seenCx context.Context
}

func (s *slowExecutor) Execute(ctx context.Context, _ *oms.Function, _ map[string]interface{}) (interface{}, error) {
	s.seenCx = ctx
	if s.err != nil {
		return nil, s.err
	}
	select {
	case <-time.After(s.delay):
		return "late", nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestExecuteFunction_QuotaExceededReturns429(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: "ok"}

	handler := oms.NewOMSHandler(repo)
	handler.SetFunctionExecutor(exec)

	limiter := &boundedLimiter{}
	limiter.allowed.Store(1) // permit exactly one call
	handler.SetFunctionQuotaLimiter(limiter)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)

	// First call — under the quota.
	w1 := doExecute(t, r, `{"parameters":{"a":1}}`)
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second call — quota exhausted.
	w2 := doExecute(t, r, `{"parameters":{"a":1}}`)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second call: expected 429, got %d: %s", w2.Code, w2.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["errorName"] != "FunctionQuotaExceeded" {
		t.Errorf("expected errorName=FunctionQuotaExceeded, got %+v", body)
	}
	params, _ := body["parameters"].(map[string]interface{})
	if params["realm"] != "main" {
		t.Errorf("expected realm=main (from fn RID), got %+v", params)
	}
}

func TestExecuteFunction_QuotaKeyedByRealm(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: "ok"}

	handler := oms.NewOMSHandler(repo)
	handler.SetFunctionExecutor(exec)

	limiter := &boundedLimiter{}
	limiter.allowed.Store(10)
	handler.SetFunctionQuotaLimiter(limiter)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)

	w := doExecute(t, r, `{"parameters":{"a":1}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(limiter.seen) != 1 || limiter.seen[0] != "main" {
		t.Errorf("expected limiter keyed by RID realm 'main', got %+v", limiter.seen)
	}
}

func TestExecuteFunction_TimeoutSentinelReturns408(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{err: errors.New("wrap: " + fnerrors.ErrTimeout.Error())}
	// errors.Is unwrap requires the sentinel on the chain; use the typed
	// helper directly rather than a re-formatted string.
	exec.err = fnerrorsWrap(fnerrors.ErrTimeout, "timed out while running")

	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("expected 408, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["errorName"] != "FunctionExecutionTimeout" {
		t.Errorf("expected errorName=FunctionExecutionTimeout, got %+v", body)
	}
	params, _ := body["parameters"].(map[string]interface{})
	if params["timeout"] == "" {
		t.Errorf("expected timeout parameter echoed, got %+v", params)
	}
}

func TestExecuteFunction_MemoryLimitSentinelReturns429(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{}
	exec.err = fnerrorsWrap(fnerrors.ErrMemoryLimit, "heap exploded")

	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["errorName"] != "FunctionMemoryLimitExceeded" {
		t.Errorf("expected errorName=FunctionMemoryLimitExceeded, got %+v", body)
	}
}

func TestExecuteFunction_ContextDeadlineReturns408(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	// Executor that blocks until ctx fires, then returns ctx.Err() —
	// simulates a well-behaved FunctionExecutor that honours ctx but does
	// NOT explicitly wrap fnerrors.ErrTimeout. The handler must still
	// detect the deadline condition via execCtx.Err().
	exec := &slowExecutor{delay: 10 * time.Second}
	handler := oms.NewOMSHandler(repo)
	handler.SetFunctionExecutor(exec)
	// Override the handler-side timeout via limiter-unrelated field —
	// rely on the production default (5s) being longer than this test's
	// patience is not an option, so we temporarily shorten it by having
	// the OUTER request context hold a shorter deadline.
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions/add/execute", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("expected 408 via context deadline propagation, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecuteFunction_LimiterNotConfiguredDoesNotBlock(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: "ok"}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	for i := 0; i < 10; i++ {
		w := doExecute(t, router, `{"parameters":{"a":1}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d without limiter wired should 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
}

// fnerrorsWrap returns an error that wraps the sentinel so errors.Is can
// traverse the chain. It intentionally mirrors the wrapping used inside
// pkg/functions.wrapGojaError — if that call-site's format string
// changes, callers that rely on errors.Is still work because unwrap
// finds the sentinel via the %w verb.
func fnerrorsWrap(sentinel error, message string) error {
	return &wrappedErr{sentinel: sentinel, message: message}
}

type wrappedErr struct {
	sentinel error
	message  string
}

func (w *wrappedErr) Error() string { return w.sentinel.Error() + ": " + w.message }
func (w *wrappedErr) Unwrap() error { return w.sentinel }
