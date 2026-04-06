package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newRefreshHandlerHarness(t *testing.T) (*RefreshHandler, *fakeUserRepo, *RefreshService) {
	t.Helper()
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	h := NewRefreshHandler(RefreshHandlerDeps{
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
	})
	return h, repo, rs
}

func postRefresh(t *testing.T, h *RefreshHandler, body any) *httptest.ResponseRecorder {
	t.Helper()
	bs, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRefreshHandler_HappyPath(t *testing.T) {
	h, repo, rs := newRefreshHandlerHarness(t)
	repo.users["user:alice"] = &UserRecord{ID: "user:alice", Email: "alice@example.com", Name: "Alice"}
	repo.roles["user:alice"] = []string{"editor"}

	plain, _, err := rs.Generate(context.Background(), "user:alice", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := postRefresh(t, h, map[string]string{"refresh_token": plain})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token")
	}
	if resp.RefreshToken == "" || resp.RefreshToken == plain {
		t.Errorf("expected new refresh token, got %q", resp.RefreshToken)
	}
}

func TestRefreshHandler_OldTokenIsRevoked(t *testing.T) {
	h, repo, rs := newRefreshHandlerHarness(t)
	repo.users["user:alice"] = &UserRecord{ID: "user:alice", Email: "alice@example.com"}

	plainOld, _, _ := rs.Generate(context.Background(), "user:alice", "")
	rec := postRefresh(t, h, map[string]string{"refresh_token": plainOld})
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	// Re-using the old token must fail (reuse detection).
	rec2 := postRefresh(t, h, map[string]string{"refresh_token": plainOld})
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on reuse, got %d", rec2.Code)
	}
}

func TestRefreshHandler_InvalidToken(t *testing.T) {
	h, _, _ := newRefreshHandlerHarness(t)
	rec := postRefresh(t, h, map[string]string{"refresh_token": "garbage"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRefreshHandler_MissingToken(t *testing.T) {
	h, _, _ := newRefreshHandlerHarness(t)
	rec := postRefresh(t, h, map[string]string{})
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 400/401, got %d", rec.Code)
	}
}

func TestRefreshHandler_FromCookieWhenBodyMissing(t *testing.T) {
	h, repo, rs := newRefreshHandlerHarness(t)
	repo.users["user:alice"] = &UserRecord{ID: "user:alice", Email: "alice@example.com"}

	plain, _, _ := rs.Generate(context.Background(), "user:alice", "")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: plain})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from cookie, got %d body=%s", rec.Code, rec.Body.String())
	}
}
