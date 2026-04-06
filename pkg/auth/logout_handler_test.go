package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newLogoutHandlerHarness(t *testing.T) (*LogoutHandler, *RefreshService) {
	t.Helper()
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	h := NewLogoutHandler(rs)
	return h, rs
}

func TestLogoutHandler_RevokesRefreshToken(t *testing.T) {
	h, rs := newLogoutHandlerHarness(t)
	plain, _, _ := rs.Generate(context.Background(), "user:alice", "")

	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}

	// Token should now be revoked.
	rec2, _ := rs.Lookup(context.Background(), plain)
	if rec2 == nil || rec2.RevokedAt == nil {
		t.Error("expected token revoked after logout")
	}
}

func TestLogoutHandler_IdempotentOnUnknownToken(t *testing.T) {
	h, _ := newLogoutHandlerHarness(t)
	body, _ := json.Marshal(map[string]string{"refresh_token": "garbage"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 even for unknown token, got %d", rec.Code)
	}
}

func TestLogoutHandler_FromCookie(t *testing.T) {
	h, rs := newLogoutHandlerHarness(t)
	plain, _, _ := rs.Generate(context.Background(), "user:alice", "")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: plain})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
	rec2, _ := rs.Lookup(context.Background(), plain)
	if rec2.RevokedAt == nil {
		t.Error("expected token revoked from cookie path")
	}
}

func TestLogoutHandler_ClearsCookie(t *testing.T) {
	h, rs := newLogoutHandlerHarness(t)
	plain, _, _ := rs.Generate(context.Background(), "user:alice", "")

	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == RefreshCookieName {
			found = true
			if c.MaxAge >= 0 && c.Value != "" {
				t.Errorf("expected cookie cleared, got value=%q maxAge=%d", c.Value, c.MaxAge)
			}
		}
	}
	if !found {
		t.Error("expected logout to set clearing cookie")
	}
}
