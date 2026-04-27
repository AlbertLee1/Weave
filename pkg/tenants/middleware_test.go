package tenants

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

func TestMiddleware_BypassesAnonymous(t *testing.T) {
	mgr := NewManager(NewMemoryStore())
	called := false
	h := Middleware(mgr)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !called {
		t.Errorf("anonymous request should pass through")
	}
	if w.Code != http.StatusOK {
		t.Errorf("anonymous: want 200, got %d", w.Code)
	}
}

func TestMiddleware_AllowsWithinBurst(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxQPS: 100, Burst: 5})
	mgr := NewManager(store)

	user := &auth.User{ID: "user:alice", Attributes: map[string]any{"realm": "acme"}}
	count := 0
	h := Middleware(mgr)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		count++
	}))
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r = r.WithContext(auth.WithUser(r.Context(), user))
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if count != 5 {
		t.Errorf("burst=5: want 5 calls, got %d", count)
	}
}

func TestMiddleware_Returns429AfterBurst(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxQPS: 1, Burst: 1})
	mgr := NewManager(store)

	user := &auth.User{ID: "user:bob", Attributes: map[string]any{"realm": "acme"}}
	h := Middleware(mgr)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// no-op
	}))

	// First call: passes.
	r1 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r1 = r1.WithContext(auth.WithUser(r1.Context(), user))
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: want 200, got %d", w1.Code)
	}

	// Second call: 429.
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r2 = r2.WithContext(auth.WithUser(r2.Context(), user))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second call: want 429, got %d (body: %s)", w2.Code, w2.Body.String())
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header on 429")
	}
}

func TestMiddleware_NilManagerPassesThrough(t *testing.T) {
	called := false
	h := Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	user := &auth.User{ID: "user:c", Attributes: map[string]any{"realm": "acme"}}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(auth.WithUser(r.Context(), user))
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !called {
		t.Errorf("nil manager should pass through")
	}
}

func TestMiddleware_TenantsAreIsolated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.CreateQuota(ctx, &Quota{Tenant: "acme", MaxQPS: 1, Burst: 1})
	_ = store.CreateQuota(ctx, &Quota{Tenant: "globex", MaxQPS: 1, Burst: 1})
	mgr := NewManager(store)

	h := Middleware(mgr)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	acme := &auth.User{ID: "user:1", Attributes: map[string]any{"realm": "acme"}}
	globex := &auth.User{ID: "user:2", Attributes: map[string]any{"realm": "globex"}}

	// Burn acme's burst.
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(auth.WithUser(r.Context(), acme))
	h.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(auth.WithUser(r.Context(), acme))
	wAcme := httptest.NewRecorder()
	h.ServeHTTP(wAcme, r)
	if wAcme.Code != http.StatusTooManyRequests {
		t.Errorf("acme second call should be 429, got %d", wAcme.Code)
	}

	// globex should be unaffected.
	r = httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(auth.WithUser(r.Context(), globex))
	wGlobex := httptest.NewRecorder()
	h.ServeHTTP(wGlobex, r)
	if wGlobex.Code != http.StatusOK {
		t.Errorf("globex first call should be 200, got %d", wGlobex.Code)
	}
}
