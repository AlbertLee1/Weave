package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
