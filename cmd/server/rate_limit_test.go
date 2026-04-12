package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
)

func TestRateLimitMiddleware_BlocksAfterBurst(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/login", RPS: 2, Burst: 2, KeyBy: KeyByIP},
	}
	mw := NewRateLimitMiddleware(rules)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 requests (burst) should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: expected 429, got %d", w.Code)
	}

	// Verify Retry-After header is present and is a positive number
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("missing Retry-After header on 429 response")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil || secs <= 0 {
		t.Fatalf("Retry-After should be a positive integer, got %q", retryAfter)
	}

	// Verify JSON error body
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("expected JSON error body: %v", err)
	}
	if body["errorCode"] != "TooManyRequests" {
		t.Errorf("errorCode: got %q, want %q", body["errorCode"], "TooManyRequests")
	}
}

func TestRateLimitMiddleware_DifferentIPsGetSeparateBuckets(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/login", RPS: 1, Burst: 1, KeyBy: KeyByIP},
	}
	mw := NewRateLimitMiddleware(rules)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP 1: first request succeeds
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("IP1 request: expected 200, got %d", w1.Code)
	}

	// IP 2: first request also succeeds (separate bucket)
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("IP2 request: expected 200, got %d", w2.Code)
	}

	// IP 1: second request should be rate limited
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req3.RemoteAddr = "10.0.0.1:12345"
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("IP1 second request: expected 429, got %d", w3.Code)
	}
}

func TestRateLimitMiddleware_KeyByUser(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", RPS: 1, Burst: 1, KeyBy: KeyByUser},
	}
	mw := NewRateLimitMiddleware(rules)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Build a chi router so URL params get resolved
	router := chi.NewRouter()
	router.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithUser(r.Context(), &auth.User{ID: "user-1"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}).With(mw).Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", inner.ServeHTTP)

	// First request succeeds
	req1 := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/actions/doSomething/apply", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("user-1 first request: expected 200, got %d", w1.Code)
	}

	// Second request from same user should be rate limited
	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/actions/doSomething/apply", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("user-1 second request: expected 429, got %d", w2.Code)
	}
}

func TestRateLimitMiddleware_KeyByOntology(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", RPS: 1, Burst: 1, KeyBy: KeyByOntology},
	}
	mw := NewRateLimitMiddleware(rules)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router := chi.NewRouter()
	router.With(mw).Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", inner.ServeHTTP)

	// First request succeeds
	req1 := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/streams/orders/ingest", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Second request to same ontology should be rate limited
	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/streams/orders/ingest", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w2.Code)
	}

	// Request to DIFFERENT ontology should succeed (separate bucket)
	req3 := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/chinook/streams/orders/ingest", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("different ontology: expected 200, got %d", w3.Code)
	}
}

func TestRateLimitMiddleware_UnmatchedRoutePassesThrough(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/login", RPS: 1, Burst: 1, KeyBy: KeyByIP},
	}
	mw := NewRateLimitMiddleware(rules)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// GET to a different path should not be rate limited
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d to unmatched route: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimitMiddleware_MethodMismatchPassesThrough(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/login", RPS: 1, Burst: 1, KeyBy: KeyByIP},
	}
	mw := NewRateLimitMiddleware(rules)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// GET to the same path but different method should not be rate limited
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestDefaultRateLimitRules(t *testing.T) {
	rules, defaultRule := DefaultRateLimitRules()

	// Verify the 4 explicit endpoint rules exist
	if len(rules) != 4 {
		t.Fatalf("expected 4 explicit rules, got %d", len(rules))
	}

	// login: 5 rps/ip
	login := rules[0]
	if login.Method != "POST" || login.Pattern != "/api/auth/login" || login.RPS != 5 || login.KeyBy != KeyByIP {
		t.Errorf("login rule mismatch: %+v", login)
	}

	// refresh: 10 rps/user
	refresh := rules[1]
	if refresh.Method != "POST" || refresh.Pattern != "/api/auth/refresh" || refresh.RPS != 10 || refresh.KeyBy != KeyByUser {
		t.Errorf("refresh rule mismatch: %+v", refresh)
	}

	// apply: 100 rps/user
	apply := rules[2]
	if apply.Method != "POST" || apply.Pattern != "/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply" || apply.RPS != 100 || apply.KeyBy != KeyByUser {
		t.Errorf("apply rule mismatch: %+v", apply)
	}

	// ingest: 1000 rps/ontology
	ingest := rules[3]
	if ingest.Method != "POST" || ingest.Pattern != "/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest" || ingest.RPS != 1000 || ingest.KeyBy != KeyByOntology {
		t.Errorf("ingest rule mismatch: %+v", ingest)
	}

	// default: 200 rps/user
	if defaultRule == nil {
		t.Fatal("expected a non-nil default rule")
	}
	if defaultRule.RPS != 200 || defaultRule.KeyBy != KeyByUser {
		t.Errorf("default rule mismatch: %+v", defaultRule)
	}
}

func TestRateLimitMiddleware_DefaultFallbackRule(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/login", RPS: 1, Burst: 1, KeyBy: KeyByIP},
	}
	defaultRule := &RateLimitRule{RPS: 1, Burst: 1, KeyBy: KeyByUser}
	mw := NewRateLimitMiddlewareWithDefault(rules, defaultRule)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// An unmatched route should still be rate-limited by the default rule
	req1 := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first unmatched request: expected 200, got %d", w1.Code)
	}

	// Second request from same IP (falls back to IP since no user context)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v2/ontologies", nil)
	req2.RemoteAddr = "10.0.0.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second unmatched request: expected 429, got %d", w2.Code)
	}
}

func TestRateLimitMiddleware_UserFallbackToIP(t *testing.T) {
	rules := []RateLimitRule{
		{Method: "POST", Pattern: "/api/auth/refresh", RPS: 1, Burst: 1, KeyBy: KeyByUser},
	}
	mw := NewRateLimitMiddleware(rules)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No user in context — should fall back to IP-based keying
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req1.RemoteAddr = "10.0.0.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req2.RemoteAddr = "10.0.0.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w2.Code)
	}
}
