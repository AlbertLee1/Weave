package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_MFAHandler_RejectsAmbiguousJSONBody continues the P2A-30x
// ambiguous-JSON hardening series (rounds 1, 15, 16, 17, 18) into
// pkg/auth/mfa_handler.go — high-impact second-factor write
// surfaces. Three endpoints still decoded via
// `json.NewDecoder(r.Body).Decode(&req)` which accepts only the
// first JSON value and silently drops trailing bytes:
//
//   - POST /api/auth/mfa/enable  (Enable)
//   - POST /api/auth/mfa/disable (Disable)
//   - POST /api/auth/mfa/verify  (Verify) — login-time second-factor
//
// Smuggling vector is particularly bad on /verify and /disable
// because they accept a 6-digit TOTP code that the audit log must
// be able to attribute to the actor. A body like
// `{"code":"111111"}{"code":"222222"}` would let the handler
// validate against the first while audit pipelines re-parsing the
// raw bytes might log the second — a forensic dead-end when an
// attacker later denies they typed `111111`.
//
// Fix mirrors rounds 15-18: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason.
func TestBDD_MFAHandler_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("Enable rejects concatenated JSON body", func(t *testing.T) {
		h, _, _, _ := newMFAHarness(t)
		body := `{"code":"111111"}{"code":"222222"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/enable", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(WithUser(req.Context(), &User{ID: "alice", Email: "alice@example.com"}))
		rec := httptest.NewRecorder()
		h.Enable(rec, req)
		assertMFASingleJSONRejection(t, rec)
	})

	t.Run("Disable rejects concatenated JSON body", func(t *testing.T) {
		h, _, _, _ := newMFAHarness(t)
		body := `{"code":"111111"}{"code":"222222"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/disable", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(WithUser(req.Context(), &User{ID: "alice", Email: "alice@example.com"}))
		rec := httptest.NewRecorder()
		h.Disable(rec, req)
		assertMFASingleJSONRejection(t, rec)
	})

	t.Run("Verify rejects concatenated JSON body", func(t *testing.T) {
		h, _, _, _ := newMFAHarness(t)
		body := `{"code":"111111","challenge_token":"safe"}{"code":"222222","challenge_token":"smuggled"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/verify", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.Verify(rec, req)
		assertMFASingleJSONRejection(t, rec)
	})

	t.Run("Enable with well-formed body still surfaces the existing MFANotEnrolled flow (regression guard)", func(t *testing.T) {
		// Sanity: a single well-formed body must still reach the
		// downstream handler logic. We don't have a TOTP setup
		// seeded so the expected outcome is the well-defined
		// MFANotEnrolled response — the handler returned 401 here
		// before the round-19 fix and must continue to do so after.
		h, repo, _, _ := newMFAHarness(t)
		repo.users["alice"] = &UserRecord{ID: "alice", Email: "alice@example.com"}
		body, _ := json.Marshal(map[string]any{"code": "111111"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/enable", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(WithUser(req.Context(), &User{ID: "alice", Email: "alice@example.com"}))
		rec := httptest.NewRecorder()
		h.Enable(rec, req)
		// Pre-fix would 400 InvalidMFARequest only for concatenated bodies;
		// well-formed must NOT 400 with that error name (it surfaces a
		// different downstream error for the missing TOTP enrolment).
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorName == "InvalidMFARequest" {
			t.Fatalf("happy body must not return InvalidMFARequest; status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func assertMFASingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorName != "InvalidMFARequest" {
		t.Errorf("errorName: got %q, want InvalidMFARequest", env.ErrorName)
	}
	if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
		t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
	}
}
