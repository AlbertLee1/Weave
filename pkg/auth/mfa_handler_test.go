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

	"github.com/pquerna/otp/totp"
)

func newMFAHarness(t *testing.T) (*MFAHandler, *fakeUserRepo, *MFAChallengeStore, *RefreshService) {
	t.Helper()
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{AccessTokenTTL: 15 * time.Minute})
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 24 * time.Hour})
	store := NewMFAChallengeStore(time.Minute)
	h := NewMFAHandler(MFAHandlerDeps{
		Users:          repo,
		MFAStore:       repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		MFAChallenges:  store,
	})
	return h, repo, store, rs
}

func authedRequest(t *testing.T, method, path string, body any, userID string) *http.Request {
	t.Helper()
	var r *http.Request
	if body != nil {
		bs, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(bs))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if userID != "" {
		r = r.WithContext(WithUser(r.Context(), &User{ID: userID, Email: "alice@example.com"}))
	}
	return r
}

func TestMFAHandler_Setup_PersistsSecretButNotEnabled(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com"}

	rec := httptest.NewRecorder()
	h.Setup(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/setup", nil, "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp MFASetupResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret == "" {
		t.Error("expected secret")
	}
	if resp.OTPAuth == "" || resp.QRPNG == "" {
		t.Errorf("expected otpauth_url + qr_png_base64, got url=%q qr-len=%d", resp.OTPAuth, len(resp.QRPNG))
	}
	if resp.Activated {
		t.Error("setup must NOT activate enforcement")
	}
	if repo.users["alice"].MFASecret != resp.Secret {
		t.Errorf("secret not persisted: got %q want %q", repo.users["alice"].MFASecret, resp.Secret)
	}
	if repo.users["alice"].MFAEnabled {
		t.Error("MFAEnabled must remain false after /setup")
	}
}

func TestMFAHandler_Setup_RotatesSecretAndDisablesIfEnabled(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: "OLDSECRET", MFAEnabled: true}

	rec := httptest.NewRecorder()
	h.Setup(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/setup", nil, "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.users["alice"].MFAEnabled {
		t.Error("re-running /setup must disable enforcement until /enable")
	}
	if repo.users["alice"].MFASecret == "OLDSECRET" {
		t.Error("expected secret to rotate")
	}
}

func TestMFAHandler_Setup_RequiresAuth(t *testing.T) {
	h, _, _, _ := newMFAHarness(t)
	rec := httptest.NewRecorder()
	h.Setup(rec, httptest.NewRequest(http.MethodPost, "/api/auth/mfa/setup", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMFAHandler_Enable_Success(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret()}

	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	rec := httptest.NewRecorder()
	h.Enable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/enable", map[string]string{"code": code}, "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if !repo.users["alice"].MFAEnabled {
		t.Error("expected MFAEnabled=true after /enable")
	}
}

func TestMFAHandler_Enable_RejectsBadCode(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret()}

	rec := httptest.NewRecorder()
	h.Enable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/enable", map[string]string{"code": "000000"}, "alice"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 InvalidMFACode, got %d", rec.Code)
	}
	if repo.users["alice"].MFAEnabled {
		t.Error("expected MFAEnabled to remain false after bad code")
	}
}

func TestMFAHandler_Enable_RejectsWhenNotEnrolled(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com"} // no secret

	rec := httptest.NewRecorder()
	h.Enable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/enable", map[string]string{"code": "123456"}, "alice"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 MFANotEnrolled, got %d", rec.Code)
	}
}

func TestMFAHandler_Enable_RequiresCode(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: "JBSWY3DPEHPK3PXP"}

	rec := httptest.NewRecorder()
	h.Enable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/enable", map[string]string{"code": ""}, "alice"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 MissingMFACode, got %d", rec.Code)
	}
}

func TestMFAHandler_Disable_ClearsSecretAndFlag(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret(), MFAEnabled: true}

	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	rec := httptest.NewRecorder()
	h.Disable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/disable", map[string]string{"code": code}, "alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.users["alice"].MFASecret != "" {
		t.Errorf("expected MFASecret cleared, got %q", repo.users["alice"].MFASecret)
	}
	if repo.users["alice"].MFAEnabled {
		t.Error("expected MFAEnabled=false")
	}
}

func TestMFAHandler_Disable_NoOpWhenNotEnrolled(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com"}
	rec := httptest.NewRecorder()
	h.Disable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/disable", map[string]string{}, "alice"))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 idempotent disable, got %d", rec.Code)
	}
}

func TestMFAHandler_Disable_RejectsBadCode(t *testing.T) {
	h, repo, _, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret(), MFAEnabled: true}

	rec := httptest.NewRecorder()
	h.Disable(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/disable", map[string]string{"code": "000000"}, "alice"))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 InvalidMFACode, got %d", rec.Code)
	}
	if !repo.users["alice"].MFAEnabled {
		t.Error("expected MFAEnabled to remain true after bad disable code")
	}
}

func TestMFAHandler_Verify_Success(t *testing.T) {
	h, repo, store, rs := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{
		ID:         "alice",
		Email:      "alice@example.com",
		Name:       "Alice",
		MFASecret:  key.Secret(),
		MFAEnabled: true,
	}
	repo.roles["alice"] = []string{"editor"}

	tok, _ := store.Issue("alice")
	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	body := map[string]string{"challenge_token": tok, "code": code}

	rec := httptest.NewRecorder()
	h.Verify(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/verify", body, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Error("expected access + refresh tokens")
	}
	if resp.User.ID != "alice" {
		t.Errorf("user.id: got %q", resp.User.ID)
	}
	if len(resp.User.Roles) != 1 || resp.User.Roles[0] != "editor" {
		t.Errorf("user.roles: got %v", resp.User.Roles)
	}
	// Refresh token must persist in the store.
	if _, err := rs.Lookup(context.Background(), resp.RefreshToken); err != nil {
		t.Errorf("refresh lookup: %v", err)
	}
	// Challenge must be single-use.
	if _, err := store.Consume(tok); err == nil {
		t.Error("expected challenge to be consumed")
	}
}

func TestMFAHandler_Verify_RejectsBadCode(t *testing.T) {
	h, repo, store, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret(), MFAEnabled: true}
	tok, _ := store.Issue("alice")

	rec := httptest.NewRecorder()
	h.Verify(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/verify", map[string]string{"challenge_token": tok, "code": "000000"}, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMFAHandler_Verify_RejectsUnknownChallenge(t *testing.T) {
	h, _, _, _ := newMFAHarness(t)
	rec := httptest.NewRecorder()
	h.Verify(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/verify", map[string]string{"challenge_token": "garbage", "code": "123456"}, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 InvalidMFAChallenge, got %d", rec.Code)
	}
}

func TestMFAHandler_Verify_RejectsExpiredChallenge(t *testing.T) {
	h, repo, store, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret(), MFAEnabled: true}

	clock := time.Now()
	store.SetNowFunc(func() time.Time { return clock })
	tok, _ := store.Issue("alice")
	clock = clock.Add(10 * time.Minute) // past TTL

	code, _ := totp.GenerateCode(key.Secret(), clock)
	rec := httptest.NewRecorder()
	h.Verify(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/verify", map[string]string{"challenge_token": tok, "code": code}, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 InvalidMFAChallenge for expired token, got %d", rec.Code)
	}
}

func TestMFAHandler_Verify_RejectsWhenMFADisabledMidFlight(t *testing.T) {
	h, repo, store, _ := newMFAHarness(t)
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com", MFASecret: key.Secret(), MFAEnabled: true}

	tok, _ := store.Issue("alice")
	// Admin disables MFA between login and verify.
	repo.users["alice"].MFAEnabled = false
	repo.users["alice"].MFASecret = ""

	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	rec := httptest.NewRecorder()
	h.Verify(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/verify", map[string]string{"challenge_token": tok, "code": code}, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 MFANotEnrolled, got %d", rec.Code)
	}
}

func TestMFAHandler_Verify_MissingFields(t *testing.T) {
	h, _, _, _ := newMFAHarness(t)
	rec := httptest.NewRecorder()
	h.Verify(rec, authedRequest(t, http.MethodPost, "/api/auth/mfa/verify", map[string]string{}, ""))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
