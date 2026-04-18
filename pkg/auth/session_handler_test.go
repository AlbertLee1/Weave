package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// withURLParam injects a chi URL param into the request context so handlers
// that call chi.URLParam can be exercised without a full router.
func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func newSessionHandler(t *testing.T) (*SessionHandler, *MemorySessionStore) {
	t.Helper()
	store := NewMemorySessionStore()
	h := NewSessionHandler(SessionHandlerDeps{Sessions: store})
	return h, store
}

func TestSessionHandler_ListRequiresAuth(t *testing.T) {
	h, _ := newSessionHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestSessionHandler_ListReturnsOnlyCallerSessions(t *testing.T) {
	h, store := newSessionHandler(t)
	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "a1", UserID: "user:alice", IP: "1.1.1.1", UserAgent: "Mozilla", CreatedAt: time.Unix(100, 0), LastSeen: time.Unix(200, 0)})
	_ = store.Create(ctx, &SessionRecord{ID: "b1", UserID: "user:bob", IP: "2.2.2.2"})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice"}))
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp SessionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
	}
	got := resp.Sessions[0]
	if got.ID != "a1" || got.IP != "1.1.1.1" || got.UserAgent != "Mozilla" {
		t.Fatalf("unexpected session: %+v", got)
	}
	if got.UserID != "" {
		t.Fatalf("wire response must not leak user_id")
	}
}

func TestSessionHandler_ListMarksCurrent(t *testing.T) {
	h, store := newSessionHandler(t)
	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "a1", UserID: "user:alice"})
	_ = store.Create(ctx, &SessionRecord{ID: "a2", UserID: "user:alice"})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice", Attributes: map[string]any{"sessionID": "a2"}}))
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp SessionListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	var current bool
	for _, s := range resp.Sessions {
		if s.ID == "a2" && s.Current {
			current = true
		}
		if s.ID != "a2" && s.Current {
			t.Fatalf("unexpected current mark on %s", s.ID)
		}
	}
	if !current {
		t.Fatalf("expected current=true on a2")
	}
}

func TestSessionHandler_DeleteOwnerOnly(t *testing.T) {
	h, store := newSessionHandler(t)
	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "a1", UserID: "user:alice"})
	_ = store.Create(ctx, &SessionRecord{ID: "b1", UserID: "user:bob"})

	req := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/a1", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice"}))
	req = withURLParam(req, "sessionID", "a1")
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := store.Get(ctx, "a1"); err == nil {
		t.Fatalf("expected a1 deleted")
	}

	// Alice cannot delete Bob's session.
	req = httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/b1", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice"}))
	req = withURLParam(req, "sessionID", "b1")
	rec = httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestSessionHandler_DeleteRequiresAuth(t *testing.T) {
	h, _ := newSessionHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/x", nil)
	req = withURLParam(req, "sessionID", "x")
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestSessionHandler_DeleteMissingReturns404(t *testing.T) {
	h, _ := newSessionHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/missing", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice"}))
	req = withURLParam(req, "sessionID", "missing")
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSessionHandler_DeleteRevokesRefreshTokensForSelf(t *testing.T) {
	h, store := newSessionHandler(t)
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: time.Hour})
	h.deps.RefreshService = rs

	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "a1", UserID: "user:alice", RefreshTokenID: "rt-1"})
	rec0 := &RefreshTokenRecord{
		ID:        "rt-1",
		UserID:    "user:alice",
		TokenHash: "abcd",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := rs.store.Create(ctx, rec0); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/a1", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice"}))
	req = withURLParam(req, "sessionID", "a1")
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// The specific refresh token row must be revoked.
	got, err := rs.store.GetByHash(ctx, "abcd")
	if err != nil {
		t.Fatalf("lookup refresh: %v", err)
	}
	if !got.IsRevoked() {
		t.Fatalf("expected refresh revoked after session delete")
	}
}

func TestSessionHandler_ListReturnsEmptyArrayNotNull(t *testing.T) {
	h, _ := newSessionHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	req = req.WithContext(WithUser(req.Context(), &User{ID: "user:nobody"}))
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Must serialize sessions as [] not null so SDKs don't need null-check.
	if !strings.Contains(rec.Body.String(), `"sessions":[]`) {
		t.Fatalf("expected empty sessions array, got %s", rec.Body.String())
	}
}
