package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_LoginRejectsAmbiguousJSONBody covers P2A-A001.
//
// Given a POST /api/auth/login whose body contains two concatenated JSON
// objects, when the handler decodes the request, then it responds 400
// InvalidLoginRequest and the single-value validation runs before any
// credential lookup or password verification.
func TestBDD_LoginRejectsAmbiguousJSONBody(t *testing.T) {
	h, repo, _ := newLoginHandlerHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice")

	// Two concatenated JSON objects: the second smuggles a different
	// password under the same field name.
	body := `{"email":"alice@example.com","password":"WRONG"}{"email":"alice@example.com","password":"letmein123!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for ambiguous body, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if resp.ErrorName != "InvalidLoginRequest" {
		t.Errorf("errorName: got %q, want InvalidLoginRequest", resp.ErrorName)
	}
	if !strings.Contains(strings.ToLower(resp.Parameters["reason"]), "single json value") {
		t.Errorf("expected reason to mention single JSON value, got %q", resp.Parameters["reason"])
	}
}

// TestBDD_LoginRejectsOversizedJSONBody covers P2A-A001.
//
// Given a POST /api/auth/login whose body exceeds httputil.MaxBodySize
// (1 MiB), when the handler decodes the request, then it responds 400
// InvalidLoginRequest without entering the credential lookup path.
func TestBDD_LoginRejectsOversizedJSONBody(t *testing.T) {
	h, repo, _ := newLoginHandlerHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice")

	// Padding pushes the payload well above the 1 MiB shared cap while
	// still being syntactically valid JSON.
	padding := strings.Repeat("x", 2*1024*1024)
	body := `{"email":"alice@example.com","password":"letmein123!","junk":"` + padding + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized body, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ErrorName string `json:"errorName"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if resp.ErrorName != "InvalidLoginRequest" {
		t.Errorf("errorName: got %q, want InvalidLoginRequest", resp.ErrorName)
	}
}

// TestBDD_LoginAcceptsWellFormedJSONBody covers P2A-A001 regression.
//
// Given a POST /api/auth/login with a valid single JSON object, when the
// handler decodes the request, then the existing happy path still issues
// tokens — confirming the hardening does not regress legitimate clients.
func TestBDD_LoginAcceptsWellFormedJSONBody(t *testing.T) {
	h, repo, _ := newLoginHandlerHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice", "editor")

	bs, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "letmein123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for well-formed body, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Errorf("expected access + refresh tokens, got %+v", resp)
	}
}
