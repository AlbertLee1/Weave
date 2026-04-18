package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-221: POST /functions/{rid}/execute consults a per-process LRU+TTL
// result cache for Functions flagged `pure=true`. Cached entries are keyed
// on `rid@version + hash(params)` so two calls only collide when both the
// function build AND the input are byte-identical (US-217 semver +
// canonical params hash). The tests below pin the cache-gating semantics:
// cache-miss falls through to the executor, repeat call hits the cache,
// impure functions never cache, no-cache-wired falls through, different
// params produce distinct entries, errors are never cached.

type recordingCache struct {
	mu      sync.Mutex
	entries map[string]interface{}
	gets    atomic.Int32
	puts    atomic.Int32
}

func newRecordingCache() *recordingCache {
	return &recordingCache{entries: map[string]interface{}{}}
}

func (c *recordingCache) Get(key string) (interface{}, bool) {
	c.gets.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *recordingCache) Put(key string, value interface{}) {
	c.puts.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = value
}

func (c *recordingCache) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.entries))
	for k := range c.entries {
		out = append(out, k)
	}
	return out
}

type countingExecutor struct {
	calls  atomic.Int32
	result interface{}
	err    error
}

func (e *countingExecutor) Execute(_ context.Context, _ *oms.Function, _ map[string]interface{}) (interface{}, error) {
	e.calls.Add(1)
	if e.err != nil {
		return nil, e.err
	}
	return e.result, nil
}

func newPureFixtureRepo(pure bool) *mockRepo {
	return &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{{
			RID:         "ri.ontology.main.function.add",
			OntologyRID: "ri.ontology.main.ontology.o1",
			Name:        "add",
			Version:     "1.0.0",
			SourceCode:  "function main(input){ return input.parameters.a + input.parameters.b }",
			Runtime:     "goja",
			Pure:        pure,
			Signature:   json.RawMessage(`{"params":[{"name":"a","type":"integer","required":true},{"name":"b","type":"integer","required":true}]}`),
		}},
	}
}

func setupCacheRouter(repo oms.Repository, exec oms.FunctionExecutor, c oms.FunctionResultCache) *chi.Mux {
	handler := oms.NewOMSHandler(repo)
	if exec != nil {
		handler.SetFunctionExecutor(exec)
	}
	if c != nil {
		handler.SetFunctionResultCache(c)
	}
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)
	return r
}

func TestExecuteFunction_PureCacheMissThenHit(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{result: float64(7)}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	w1 := doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	if exec.calls.Load() != 1 {
		t.Fatalf("first call should hit executor exactly once, got %d", exec.calls.Load())
	}
	if cache.puts.Load() != 1 {
		t.Fatalf("first call should write one cache entry, got puts=%d", cache.puts.Load())
	}

	// Second call with the same params — must not hit the executor again.
	w2 := doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if exec.calls.Load() != 1 {
		t.Fatalf("second call should be served from cache (executor calls=%d)", exec.calls.Load())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["result"] != float64(7) {
		t.Errorf("expected cached result=7, got %+v", body)
	}
	if body["cached"] != true {
		t.Errorf("expected cached=true on cache hit, got %+v", body)
	}
}

func TestExecuteFunction_ImpureNeverCaches(t *testing.T) {
	repo := newPureFixtureRepo(false)
	exec := &countingExecutor{result: float64(7)}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	for i := 0; i < 3; i++ {
		w := doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	if exec.calls.Load() != 3 {
		t.Fatalf("impure function should re-run every call (got %d)", exec.calls.Load())
	}
	if cache.gets.Load() != 0 || cache.puts.Load() != 0 {
		t.Errorf("impure function must not touch the cache (gets=%d puts=%d)", cache.gets.Load(), cache.puts.Load())
	}
}

func TestExecuteFunction_NoCacheWiredFallsThrough(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{result: float64(42)}
	router := setupCacheRouter(repo, exec, nil)

	for i := 0; i < 3; i++ {
		w := doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	if exec.calls.Load() != 3 {
		t.Fatalf("no cache wired ⇒ pure function still re-runs (got %d)", exec.calls.Load())
	}
}

func TestExecuteFunction_DifferentParamsDistinctEntries(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{result: float64(0)}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	doExecute(t, router, `{"parameters":{"a":1,"b":2}}`)
	doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
	doExecute(t, router, `{"parameters":{"a":1,"b":2}}`) // should hit cache from call #1

	if exec.calls.Load() != 2 {
		t.Fatalf("expected 2 distinct executor invocations (call #3 cached), got %d", exec.calls.Load())
	}
	keys := cache.keys()
	if len(keys) != 2 {
		t.Errorf("expected 2 cache entries for the 2 distinct param sets, got %d (%+v)", len(keys), keys)
	}
}

func TestExecuteFunction_ParamMapOrderInsensitive(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{result: float64(7)}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
	doExecute(t, router, `{"parameters":{"b":4,"a":3}}`) // same content, different key order

	if exec.calls.Load() != 1 {
		t.Fatalf("identical params with different key order must hit the same cache entry (executor calls=%d)", exec.calls.Load())
	}
}

func TestExecuteFunction_CacheKeyIncludesRIDAndVersion(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{result: float64(7)}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
	keys := cache.keys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(keys))
	}
	got := keys[0]
	if !strings.HasPrefix(got, "ri.ontology.main.function.add@1.0.0#") {
		t.Errorf("cache key should be `<rid>@<version>#<hash>`, got %q", got)
	}
}

func TestExecuteFunction_ErrorsNeverCached(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{err: errExecutor}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	for i := 0; i < 3; i++ {
		w := doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("call %d: expected 400, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	if exec.calls.Load() != 3 {
		t.Fatalf("failures must NOT cache — every call should re-run (got %d)", exec.calls.Load())
	}
	if cache.puts.Load() != 0 {
		t.Errorf("expected zero cache writes on failure, got puts=%d", cache.puts.Load())
	}
}

func TestExecuteFunction_StreamingSkipsCache(t *testing.T) {
	repo := newPureFixtureRepo(true)
	exec := &countingExecutor{result: []interface{}{1, 2, 3}}
	cache := newRecordingCache()
	router := setupCacheRouter(repo, exec, cache)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions/add/execute?stream=1", bytes.NewBufferString(`{"parameters":{"a":3,"b":4}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	if exec.calls.Load() != 2 {
		t.Fatalf("streaming responses should not cache — every stream re-runs (got %d)", exec.calls.Load())
	}
	if cache.puts.Load() != 0 {
		t.Errorf("expected zero cache writes on streaming, got puts=%d", cache.puts.Load())
	}
}

// errExecutor is a sentinel error used to drive the failure-path test.
var errExecutor = stringError("boom")

type stringError string

func (s stringError) Error() string { return string(s) }
