package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

// US-491 admin handler unit tests. We mount the handler under a chi router
// so chi.URLParam(r, "jti") sees its named param the same way main.go wires
// the production route.

func newRevokeRouter(deps AdminAuthRevokeDeps) http.Handler {
	r := chi.NewRouter()
	r.Method(http.MethodPost, "/api/auth/tokens/{jti}/revoke", NewAdminAuthRevokeHandler(deps))
	return r
}

func TestUS491_AdminAuthRevoke_Success_InsertsAndInvalidatesCache(t *testing.T) {
	store := auth.NewMemoryRevocationStore()
	checker := auth.NewCachedRevocationChecker(store, time.Minute)
	// Pre-populate cache with "not revoked" so we can detect the invalidate.
	if revoked, _ := checker.IsRevoked(context.Background(), "jti-abc"); revoked {
		t.Fatal("seed: should not be revoked")
	}

	srv := newRevokeRouter(AdminAuthRevokeDeps{Store: store, Checker: checker})
	body := []byte(`{"userId":"user:alice","reason":"logout"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/tokens/jti-abc/revoke", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp AdminAuthRevokeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.JTI != "jti-abc" {
		t.Errorf("jti: got %q, want jti-abc", resp.JTI)
	}
	if resp.RevokedAt == "" {
		t.Error("revokedAt should be set")
	}

	yes, err := store.IsRevoked(context.Background(), "jti-abc")
	if err != nil || !yes {
		t.Fatalf("store should have jti-abc revoked, yes=%v err=%v", yes, err)
	}
	// Cached "false" must have been flushed so this call goes back to the
	// store and now returns true.
	yes2, _ := checker.IsRevoked(context.Background(), "jti-abc")
	if !yes2 {
		t.Fatal("cache should have been invalidated; checker still reports not-revoked")
	}
}

func TestUS491_AdminAuthRevoke_BlankJTI_400(t *testing.T) {
	store := auth.NewMemoryRevocationStore()
	// Invoke the handler directly with a RouteContext whose {jti} is blank
	// (chi already prevents this via path matching for the production route,
	// but defense in depth covers any future router wiring that allows it).
	h := NewAdminAuthRevokeHandler(AdminAuthRevokeDeps{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/tokens/x/revoke", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jti", "   ")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank jti, got %d", rec.Code)
	}
}

func TestUS491_AdminAuthRevoke_NoStore_503(t *testing.T) {
	srv := newRevokeRouter(AdminAuthRevokeDeps{Store: nil})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/tokens/jti-1/revoke", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when store unwired, got %d", rec.Code)
	}
}

func TestUS491_AdminAuthRevoke_BadExpiresAt_400(t *testing.T) {
	store := auth.NewMemoryRevocationStore()
	srv := newRevokeRouter(AdminAuthRevokeDeps{Store: store})
	body := []byte(`{"expiresAt":"yesterday"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/tokens/jti-1/revoke", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-RFC3339 expiresAt, got %d", rec.Code)
	}
}
