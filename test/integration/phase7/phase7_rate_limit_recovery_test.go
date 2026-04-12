//go:build integration

package phase7_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"golang.org/x/time/rate"
)

// -----------------------------------------------------------------------
// Minimal rate-limit middleware reproduced from cmd/server/rate_limit.go
// so the integration test can exercise the full HTTP pipeline without
// importing package main.
// -----------------------------------------------------------------------

type rlCompiledRule struct {
	segments []string
	rps      float64
	burst    int
	limiters sync.Map
}

func (cr *rlCompiledRule) matchPath(path string) bool {
	segs := rlSplitPath(path)
	if len(segs) != len(cr.segments) {
		return false
	}
	for i, s := range cr.segments {
		if strings.HasPrefix(s, "{") {
			continue
		}
		if s != segs[i] {
			return false
		}
	}
	return true
}

func (cr *rlCompiledRule) getLimiter(key string) *rate.Limiter {
	if v, ok := cr.limiters.Load(key); ok {
		return v.(*rate.Limiter)
	}
	lim := rate.NewLimiter(rate.Limit(cr.rps), cr.burst)
	actual, _ := cr.limiters.LoadOrStore(key, lim)
	return actual.(*rate.Limiter)
}

func rlSplitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func rlClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// buildLoginRateLimitMW returns middleware that rate-limits POST /api/auth/login
// by client IP with the given rps and burst parameters.
func buildLoginRateLimitMW(rps float64, burst int) func(http.Handler) http.Handler {
	rule := &rlCompiledRule{
		segments: rlSplitPath("/api/auth/login"),
		rps:      rps,
		burst:    burst,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && rule.matchPath(r.URL.Path) {
				key := "ip:" + rlClientIP(r)
				lim := rule.getLimiter(key)
				if !lim.Allow() {
					retryAfter := int(math.Ceil(1.0 / rule.rps))
					if retryAfter < 1 {
						retryAfter = 1
					}
					w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					json.NewEncoder(w).Encode(map[string]any{
						"errorCode":    "TooManyRequests",
						"errorName":    "TooManyRequests",
						"errorMessage": "Rate limit exceeded. Please retry after the indicated period.",
					})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TestPhase7_RateLimitRecovery is a cross-US integration test (US-077) that
// exercises the full login rate-limit lifecycle:
//
//  1. Hammer POST /api/auth/login with wrong password → 429 after 5 attempts
//  2. Parse Retry-After header
//  3. Wait the indicated period
//  4. Next login attempt returns 401 (normal auth failure, not rate-limited)
func TestPhase7_RateLimitRecovery(t *testing.T) {
	ctx := context.Background()

	// ---- infrastructure: real PostgreSQL ----
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	// ---- seed a real user ----
	userRepo := auth.NewPGUserRepository(pg.Pool)
	hash, err := auth.HashPassword("correctPassword1!")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := userRepo.CreateUser(ctx, &auth.UserRecord{
		ID:           "user:rate-limit-test",
		Email:        "ratelimit@example.com",
		Name:         "Rate Limit Test",
		PasswordHash: hash,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// ---- build login handler (handler-level rate limit disabled) ----
	resolver := auth.NewRoleResolver(userRepo, time.Minute)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	signer, err := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create JWT signer: %v", err)
	}
	refreshStore := auth.NewMemoryRefreshStore()
	rs := auth.NewRefreshService(refreshStore, auth.RefreshServiceOptions{
		AbsoluteTTL: 7 * 24 * time.Hour,
	})
	auditStore := audit.NewPGStore(pg.Pool)

	loginHandler := auth.NewLoginHandler(auth.LoginHandlerDeps{
		Users:          userRepo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		RateLimit:      0, // handler-level rate limit disabled; middleware handles it
		AuditStore:     auditStore,
	})

	// Use a low RPS (0.5 req/sec) so the token bucket refill is negligible
	// between sequential requests (~100ms each due to bcrypt), while burst=5
	// matches the acceptance criteria of "429 after 5 attempts".
	// Retry-After = ceil(1/0.5) = 2 seconds.
	r := chi.NewRouter()
	r.Use(buildLoginRateLimitMW(0.5, 5))
	r.Post("/api/auth/login", loginHandler.ServeHTTP)

	wrongBody := func() *bytes.Reader {
		b, _ := json.Marshal(map[string]string{
			"email":    "ratelimit@example.com",
			"password": "WRONG_PASSWORD",
		})
		return bytes.NewReader(b)
	}

	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", wrongBody())
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.168.1.100:12345"
		return req
	}

	// ---- Phase 1: Send 5 wrong-password requests → all should be 401 ----
	for i := 1; i <= 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, makeReq())
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: expected 401, got %d (body: %s)", i, w.Code, w.Body.String())
		}
	}

	// ---- Phase 2: 6th request → 429 with Retry-After ----
	w := httptest.NewRecorder()
	r.ServeHTTP(w, makeReq())
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: expected 429, got %d (body: %s)", w.Code, w.Body.String())
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("429 response missing Retry-After header")
	}
	t.Logf("Retry-After: %s seconds", retryAfter)

	var waitSec int
	if _, err := fmt.Sscanf(retryAfter, "%d", &waitSec); err != nil {
		t.Fatalf("parse Retry-After %q: %v", retryAfter, err)
	}
	if waitSec < 1 {
		waitSec = 1
	}

	// ---- Phase 3: Wait Retry-After period for bucket refill ----
	time.Sleep(time.Duration(waitSec)*time.Second + 200*time.Millisecond)

	// ---- Phase 4: Next request → 401 (normal auth failure, not 429) ----
	w = httptest.NewRecorder()
	r.ServeHTTP(w, makeReq())
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("post-recovery request: expected 401 (normal), got %d (body: %s)", w.Code, w.Body.String())
	}
}
