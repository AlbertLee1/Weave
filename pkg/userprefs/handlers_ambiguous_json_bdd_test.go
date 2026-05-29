package userprefs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_UserPrefs_Put_RejectsAmbiguousJSONBody is part of the
// round-23 P2A-30x close-out — completes the ambiguous-JSON
// hardening series for the last 3 sites in the repo. A body like
// `{"theme":"dark"}{"language":"en-GB"}` writes the dark theme
// while audit pipelines re-parsing the raw bytes might log a
// language change — confusing observability over what actually
// landed in the user's preferences.
//
// Fix mirrors rounds 15-22: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason.
func TestBDD_UserPrefs_Put_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("Put rejects concatenated JSON without persisting either change", func(t *testing.T) {
		store := NewMemoryStore()
		r, user := newTestRouter(t, store)

		body := `{"theme":"dark"}{"language":"en-GB"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v2/user-preferences", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertUserPrefsSingleJSONRejection(t, rec)

		// Non-mutation snapshot: stored preferences should remain at
		// their pre-Put state (i.e. nothing exists yet — Put was the
		// only attempt). ErrNotFound is the expected outcome.
		_, err := store.Get(context.Background(), user.ID)
		if err == nil {
			t.Errorf("ambiguous Put must not persist any preferences; got a row")
		}
	})

	t.Run("Put with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		store := NewMemoryStore()
		r, _ := newTestRouter(t, store)
		body, _ := json.Marshal(map[string]any{"theme": "dark"})
		req := httptest.NewRequest(http.MethodPut, "/api/v2/user-preferences", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("happy Put: status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func assertUserPrefsSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorName != "InvalidRequestBody" {
		t.Errorf("errorName: got %q, want InvalidRequestBody", env.ErrorName)
	}
	if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
		t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
	}
}
