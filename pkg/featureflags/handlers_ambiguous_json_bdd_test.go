package featureflags

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBDD_FeatureFlags_Update_RejectsAmbiguousJSONBody is part of
// the round-23 P2A-30x close-out — completes the ambiguous-JSON
// hardening series (rounds 1, 15-22) for the last 3 sites in the
// repo. featureflags.Update is one of them; a body like
// `{"enabled":false}{"enabled":true}` flips a feature flag to
// disabled while audit pipelines re-parsing the raw bytes see it
// flipped to enabled — meaningful because feature flags are the
// primary control plane for staged rollouts.
//
// Fix mirrors rounds 15-22: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason.
func TestBDD_FeatureFlags_Update_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("Update rejects concatenated JSON without persisting the change", func(t *testing.T) {
		store := NewMemoryStore()
		// Seed a flag with enabled=false so the rejection's
		// non-mutation snapshot has something concrete to assert.
		_ = store.CreateFlag(context.Background(), &Flag{
			Name:        "rollout-x",
			Description: "stage",
			Enabled:     false,
		})
		r := newTestRouter(store)

		body := `{"enabled":false}{"enabled":true}`
		req := httptest.NewRequest(http.MethodPut,
			"/api/admin/feature-flags/rollout-x",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertFeatureFlagsSingleJSONRejection(t, w)

		// Non-mutation snapshot: enabled stayed false.
		after, err := store.GetFlag(context.Background(), "rollout-x")
		if err != nil {
			t.Fatalf("GetFlag after rejected PUT: %v", err)
		}
		if after.Enabled {
			t.Errorf("ambiguous PUT smuggled enabled=true; got Enabled=%v", after.Enabled)
		}
	})

	t.Run("Update with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		store := NewMemoryStore()
		_ = store.CreateFlag(context.Background(), &Flag{
			Name: "rollout-y", Enabled: false,
		})
		r := newTestRouter(store)
		body, _ := json.Marshal(map[string]any{"enabled": true})
		req := httptest.NewRequest(http.MethodPut,
			"/api/admin/feature-flags/rollout-y",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("happy Update: status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func assertFeatureFlagsSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
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
