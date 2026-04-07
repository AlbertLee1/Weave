package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// handler is a simple test handler that writes the user from context as JSON.
func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(u)
	})
}

func TestDevMode_NoToken(t *testing.T) {
	t.Setenv("AUTH_MODE", "dev")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if u.ID != "dev-user" {
		t.Errorf("expected user ID 'dev-user', got %q", u.ID)
	}
}

func TestDevMode_WithToken(t *testing.T) {
	t.Setenv("AUTH_MODE", "dev")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer custom-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if u.ID != "custom-token" {
		t.Errorf("expected user ID 'custom-token', got %q", u.ID)
	}
}

func TestDevMode_DefaultRoles(t *testing.T) {
	t.Setenv("AUTH_MODE", "dev")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "admin" {
		t.Errorf("expected roles [admin], got %v", u.Roles)
	}
}

func TestProdMode_NoHeader(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestProdMode_EmptyBearer(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestProdMode_InvalidScheme(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestProdMode_ValidToken(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if u.ID != "my-secret-token" {
		t.Errorf("expected user ID 'my-secret-token', got %q", u.ID)
	}
}

func TestProdMode_UserPrefixToken(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer user:alice")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if u.ID != "alice" {
		t.Errorf("expected user ID 'alice', got %q", u.ID)
	}
}

func TestProdMode_ResponseFormat(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")

	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	// Verify Palantir-style error format fields
	requiredKeys := []string{"errorCode", "errorName", "errorInstanceId", "parameters"}
	for _, key := range requiredKeys {
		if _, ok := body[key]; !ok {
			t.Errorf("missing required field %q in error response", key)
		}
	}

	if body["errorCode"] != "UNAUTHORIZED" {
		t.Errorf("expected errorCode 'UNAUTHORIZED', got %v", body["errorCode"])
	}
}

func TestUserFromContext_Nil(t *testing.T) {
	ctx := context.Background()
	u := UserFromContext(ctx)
	if u != nil {
		t.Errorf("expected nil user from empty context, got %+v", u)
	}
}

func TestUser_OntologyRolesField(t *testing.T) {
	u := &User{
		ID:    "alice",
		Roles: []string{"editor"},
		OntologyRoles: map[string]string{
			"ri.ontology.main.ontology.northwind": "ontology-owner",
		},
	}
	if u.OntologyRoles["ri.ontology.main.ontology.northwind"] != "ontology-owner" {
		t.Errorf("expected ontology-owner, got %v", u.OntologyRoles)
	}
}

func TestDevMode_OntologyRolesEmpty(t *testing.T) {
	t.Setenv("AUTH_MODE", "dev")
	mw := Middleware()
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	// Dev mode is global admin: OntologyRoles is allowed to be nil/empty
	// because the global admin role grants everything.
	if len(u.OntologyRoles) != 0 {
		t.Errorf("expected dev mode OntologyRoles to be empty, got %v", u.OntologyRoles)
	}
}

// ---- Tier 2.4: API key middleware tests ----------------------------------
//
// In jwt mode the middleware MUST also accept "wvk_..." bearer tokens by
// looking the prefix up in the api keys repo, constant-time comparing the
// SHA-256 hash, and populating User from the owning user's roles.

// fakeAPIKeyRepo is an in-memory APIKeyRepository for middleware tests.
type fakeAPIKeyRepo struct {
	byPrefix     map[string]*APIKeyRecord
	byID         map[string]*APIKeyRecord
	touchedIDs   []string
	touchedTimes []time.Time
}

func newFakeAPIKeyRepo() *fakeAPIKeyRepo {
	return &fakeAPIKeyRepo{
		byPrefix: map[string]*APIKeyRecord{},
		byID:     map[string]*APIKeyRecord{},
	}
}

func (f *fakeAPIKeyRepo) Create(_ context.Context, k *APIKeyRecord) error {
	if k.ID == "" {
		k.ID = "key-" + k.KeyPrefix
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	cp := *k
	f.byPrefix[k.KeyPrefix] = &cp
	f.byID[k.ID] = &cp
	return nil
}

func (f *fakeAPIKeyRepo) GetByPrefix(_ context.Context, prefix string) (*APIKeyRecord, error) {
	rec, ok := f.byPrefix[prefix]
	if !ok || rec.IsRevoked() {
		return nil, ErrAPIKeyNotFound
	}
	cp := *rec
	return &cp, nil
}

func (f *fakeAPIKeyRepo) GetByID(_ context.Context, id string) (*APIKeyRecord, error) {
	rec, ok := f.byID[id]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	cp := *rec
	return &cp, nil
}

func (f *fakeAPIKeyRepo) ListByUser(_ context.Context, userID string) ([]*APIKeyRecord, error) {
	var out []*APIKeyRecord
	for _, rec := range f.byID {
		if rec.UserID == userID && !rec.IsRevoked() {
			cp := *rec
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeAPIKeyRepo) Revoke(_ context.Context, id string) error {
	rec, ok := f.byID[id]
	if !ok {
		return nil
	}
	now := time.Now()
	rec.RevokedAt = &now
	return nil
}

func (f *fakeAPIKeyRepo) TouchLastUsed(_ context.Context, id string, when time.Time) error {
	rec, ok := f.byID[id]
	if !ok {
		return nil
	}
	rec.LastUsedAt = &when
	f.touchedIDs = append(f.touchedIDs, id)
	f.touchedTimes = append(f.touchedTimes, when)
	return nil
}

// Compile-time check that the fake satisfies the interface.
var _ APIKeyRepository = (*fakeAPIKeyRepo)(nil)

// seedAPIKey installs a fresh key into the fake repo, owned by ownerID.
func seedAPIKey(t *testing.T, repo *fakeAPIKeyRepo, ownerID string) (raw string, prefix string) {
	t.Helper()
	raw, prefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	rec := &APIKeyRecord{
		KeyHash:   HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    ownerID,
		Name:      "test",
	}
	if err := repo.Create(context.Background(), rec); err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	return raw, prefix
}

// newAPIKeyMiddlewareHarness builds a JWT-mode middleware with API keys
// enabled, plus a UserRepository pre-populated with the owning user.
func newAPIKeyMiddlewareHarness(t *testing.T) (func(http.Handler) http.Handler, *fakeAPIKeyRepo, *fakeUserRepo) {
	t.Helper()
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	users := newFakeUserRepo()
	users.users["user:bot@example.com"] = &UserRecord{
		ID:    "user:bot@example.com",
		Email: "bot@example.com",
		Name:  "Bot",
	}
	users.roles["user:bot@example.com"] = []string{"editor"}
	apiKeys := newFakeAPIKeyRepo()
	resolver := NewRoleResolver(users, time.Minute)
	mw := MiddlewareWithAPIKeys(signer, apiKeys, users, resolver)
	return mw, apiKeys, users
}

func TestMiddleware_JWTMode_APIKey_Accepted(t *testing.T) {
	mw, apiKeys, _ := newAPIKeyMiddlewareHarness(t)
	raw, _ := seedAPIKey(t, apiKeys, "user:bot@example.com")

	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u.ID != "user:bot@example.com" {
		t.Errorf("ID: got %q, want user:bot@example.com", u.ID)
	}
}

func TestMiddleware_JWTMode_InvalidAPIKey_401(t *testing.T) {
	mw, _, _ := newAPIKeyMiddlewareHarness(t)
	srv := mw(handler())

	// A token whose prefix exists in NO row.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wvk_AAAAAAAA_garbagegarbagegarbagegarbagegarbage")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_JWTMode_InvalidAPIKey_HashMismatch_401(t *testing.T) {
	mw, apiKeys, _ := newAPIKeyMiddlewareHarness(t)
	// Seed a real key, then construct a request with the SAME prefix but a
	// different random suffix so the hash compare fails.
	_, prefix := seedAPIKey(t, apiKeys, "user:bot@example.com")
	tampered := "wvk_" + prefix + "_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on tampered hash, got %d", rec.Code)
	}
}

func TestMiddleware_JWTMode_ExpiredAPIKey_401(t *testing.T) {
	mw, apiKeys, _ := newAPIKeyMiddlewareHarness(t)

	raw, prefix, _ := GenerateAPIKey()
	past := time.Now().Add(-1 * time.Hour)
	rec := &APIKeyRecord{
		KeyHash:   HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:bot@example.com",
		Name:      "test",
		ExpiresAt: &past,
	}
	if err := apiKeys.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 expired, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMiddleware_JWTMode_RevokedAPIKey_401(t *testing.T) {
	mw, apiKeys, _ := newAPIKeyMiddlewareHarness(t)
	raw, prefix := seedAPIKey(t, apiKeys, "user:bot@example.com")
	// Revoke the row directly.
	if err := apiKeys.Revoke(context.Background(), "key-"+prefix); err != nil {
		t.Fatal(err)
	}

	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 revoked, got %d", rec.Code)
	}
}

func TestMiddleware_JWTMode_APIKeyPopulatesUserFromOwner(t *testing.T) {
	mw, apiKeys, _ := newAPIKeyMiddlewareHarness(t)
	raw, _ := seedAPIKey(t, apiKeys, "user:bot@example.com")

	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var u User
	json.NewDecoder(rec.Body).Decode(&u)
	if u.Email != "bot@example.com" {
		t.Errorf("Email: got %q", u.Email)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "editor" {
		t.Errorf("Roles: got %v, expected [editor] from owning user", u.Roles)
	}
}

func TestMiddleware_JWTMode_APIKey_StillAcceptsJWT(t *testing.T) {
	// With API keys enabled, real JWTs must still authenticate.
	mw, _, _ := newAPIKeyMiddlewareHarness(t)
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	tok, _ := signer.Sign(SignInput{
		UserID: "user:human@example.com",
		Email:  "human@example.com",
		Roles:  []string{"viewer"},
	})

	// Re-build middleware with the SAME signer used to sign.
	users := newFakeUserRepo()
	apiKeys := newFakeAPIKeyRepo()
	resolver := NewRoleResolver(users, time.Minute)
	t.Setenv("AUTH_MODE", "jwt")
	mw = MiddlewareWithAPIKeys(signer, apiKeys, users, resolver)

	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for valid JWT, got %d", rec.Code)
	}
}

func TestMiddleware_LegacyConstructorStillWorks(t *testing.T) {
	// The legacy Middleware(signers...) constructor must still produce a
	// working middleware so existing call sites do not have to migrate.
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	tok, _ := signer.Sign(SignInput{UserID: "user:legacy@example.com"})

	mw := Middleware(signer)
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
