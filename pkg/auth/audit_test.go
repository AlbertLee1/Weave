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

	"github.com/liyang/weave/pkg/audit"
)

// newAuditLoginHarness builds a LoginHandler with an injected audit store.
func newAuditLoginHarness(t *testing.T) (*LoginHandler, *fakeUserRepo, *audit.MemoryStore) {
	t.Helper()
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	store := NewMemoryRefreshStore()
	rs := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	auditStore := audit.NewMemoryStore()

	h := NewLoginHandler(LoginHandlerDeps{
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		AuditStore:     auditStore,
	})
	return h, repo, auditStore
}

func TestAuthAuditTrail_LoginSuccess(t *testing.T) {
	h, repo, auditStore := newAuditLoginHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "pw123!", "Alice")

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "pw123!"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	events := auditStore.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != "login_success" {
		t.Errorf("Action: got %q, want login_success", evt.Action)
	}
	if evt.ActorID != "user:alice@example.com" {
		t.Errorf("ActorID: got %q", evt.ActorID)
	}
	if evt.ResourceType != "Session" {
		t.Errorf("ResourceType: got %q, want Session", evt.ResourceType)
	}
}

func TestAuthAuditTrail_LoginFailed(t *testing.T) {
	h, repo, auditStore := newAuditLoginHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "pw123!", "Alice")

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "WRONG"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	events := auditStore.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != "login_failed" {
		t.Errorf("Action: got %q, want login_failed", evt.Action)
	}
	if evt.ResourceType != "Session" {
		t.Errorf("ResourceType: got %q, want Session", evt.ResourceType)
	}
}

func TestAuthAuditTrail_TokenRefresh(t *testing.T) {
	// Build a login handler to get a valid refresh token first.
	lh, repo, _ := newAuditLoginHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "pw123!", "Alice")

	loginBody, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "pw123!"})
	loginRec := httptest.NewRecorder()
	lh.ServeHTTP(loginRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	var loginResp LoginResponse
	json.NewDecoder(loginRec.Body).Decode(&loginResp)

	// Now build a refresh handler with its own audit store.
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	refreshAuditStore := audit.NewMemoryStore()
	rh := NewRefreshHandler(RefreshHandlerDeps{
		Users:          repo,
		Resolver:       NewRoleResolver(repo, time.Minute),
		Signer:         signer,
		RefreshService: lh.deps.RefreshService,
		AuditStore:     refreshAuditStore,
	})

	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": loginResp.RefreshToken})
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(refreshBody))
	refreshRec := httptest.NewRecorder()
	rh.ServeHTTP(refreshRec, refreshReq)

	if refreshRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", refreshRec.Code, refreshRec.Body.String())
	}

	events := refreshAuditStore.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != "token_refresh" {
		t.Errorf("Action: got %q, want token_refresh", evt.Action)
	}
	if evt.ActorID != "user:alice@example.com" {
		t.Errorf("ActorID: got %q", evt.ActorID)
	}
	if evt.ResourceType != "Session" {
		t.Errorf("ResourceType: got %q, want Session", evt.ResourceType)
	}
}

func TestAuthAuditTrail_APIKeyCreate(t *testing.T) {
	auditStore := audit.NewMemoryStore()
	repo := newFakeAPIKeyRepo()
	h := NewAPIKeyHandler(repo, auditStore)

	body, _ := json.Marshal(map[string]any{"name": "ci-bot"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	events := auditStore.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != "api_key_create" {
		t.Errorf("Action: got %q, want api_key_create", evt.Action)
	}
	if evt.ActorID != "user:admin@example.com" {
		t.Errorf("ActorID: got %q", evt.ActorID)
	}
	if evt.ResourceType != "APIKey" {
		t.Errorf("ResourceType: got %q, want APIKey", evt.ResourceType)
	}
}

func TestAuthAuditTrail_APIKeyRevoke(t *testing.T) {
	auditStore := audit.NewMemoryStore()
	repo := newFakeAPIKeyRepo()
	h := NewAPIKeyHandler(repo, auditStore)

	// Create a key first.
	body, _ := json.Marshal(map[string]any{"name": "kill-me"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	var created APIKeyCreateResponse
	json.NewDecoder(rec.Body).Decode(&created)

	// Clear the create event.
	// Revoke the key.
	delReq := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/"+created.ID, nil))
	delRec := httptest.NewRecorder()
	h.DeleteFor(delRec, delReq, created.ID)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRec.Code)
	}

	events := auditStore.Events()
	// Should have 2 events: create + revoke
	var revokeEvents []audit.AuditEvent
	for _, e := range events {
		if e.Action == "api_key_revoke" {
			revokeEvents = append(revokeEvents, e)
		}
	}
	if len(revokeEvents) != 1 {
		t.Fatalf("expected 1 api_key_revoke event, got %d (total events: %d)", len(revokeEvents), len(events))
	}
	evt := revokeEvents[0]
	if evt.ActorID != "user:admin@example.com" {
		t.Errorf("ActorID: got %q", evt.ActorID)
	}
	if evt.ResourceType != "APIKey" {
		t.Errorf("ResourceType: got %q, want APIKey", evt.ResourceType)
	}
	if evt.ResourceRID != created.ID {
		t.Errorf("ResourceRID: got %q, want %q", evt.ResourceRID, created.ID)
	}
}

// TestAuthAuditTrail_LogoutSuccess verifies that logout records an audit event.
func TestAuthAuditTrail_LogoutSuccess(t *testing.T) {
	auditStore := audit.NewMemoryStore()
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	// Generate a valid refresh token.
	plain, _, err := rs.Generate(context.Background(), "user:alice@example.com", "")
	if err != nil {
		t.Fatal(err)
	}

	h := NewLogoutHandler(rs, auditStore)

	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader(body))
	req = req.WithContext(WithUser(req.Context(), &User{
		ID:    "user:alice@example.com",
		Email: "alice@example.com",
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	events := auditStore.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != "logout" {
		t.Errorf("Action: got %q, want logout", evt.Action)
	}
	if evt.ActorID != "user:alice@example.com" {
		t.Errorf("ActorID: got %q", evt.ActorID)
	}
	if evt.ResourceType != "Session" {
		t.Errorf("ResourceType: got %q, want Session", evt.ResourceType)
	}
}
