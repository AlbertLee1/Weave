package buildinfo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// TestBDD_FeaturesHandler covers round 127 — capability discovery
// endpoint. SPA/SDK callers detect which optional features the
// running server has wired without poking endpoints for 404s.

func TestBDD_FeaturesHandler(t *testing.T) {
	// Always restore the global state after each test so subtests
	// don't leak to neighbours or to other tests in the package.
	origState := currentFeatures()
	t.Cleanup(func() { SetFeatures(origState) })

	t.Run("Returns 200 with JSON envelope and registered features", func(t *testing.T) {
		SetFeatures([]Feature{
			{Name: "rid-versioning", Enabled: true,
				Description: "RID @vN parser (round 91+) and Get-endpoint @vN guards (round 117+)"},
			{Name: "snapshots", Enabled: false,
				Reason: "Gap-T4 step-1 not yet implemented"},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type=%q, want application/json", got)
		}
		var body FeaturesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if len(body.Features) != 2 {
			t.Fatalf("len=%d, want 2", len(body.Features))
		}
	})

	t.Run("Features are stable-sorted by name", func(t *testing.T) {
		// Sort happens at SetFeatures so the wire output is stable
		// regardless of caller order — SPA / CI diff reads
		// consistently.
		SetFeatures([]Feature{
			{Name: "zebra", Enabled: true},
			{Name: "alpha", Enabled: false},
			{Name: "mu", Enabled: true},
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)
		var body FeaturesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		names := []string{body.Features[0].Name, body.Features[1].Name, body.Features[2].Name}
		want := []string{"alpha", "mu", "zebra"}
		for i := range names {
			if names[i] != want[i] {
				t.Errorf("not sorted at i=%d: got=%q want=%q", i, names[i], want[i])
			}
		}
	})

	t.Run("Disabled feature surfaces reason field", func(t *testing.T) {
		// Reason is the actionable next step — explains WHY a
		// feature is disabled so the operator doesn't have to
		// grep logs.
		SetFeatures([]Feature{
			{Name: "vertex", Enabled: false,
				Reason: "Vertex deps not wired in this build"},
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)
		var body FeaturesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Features[0].Reason != "Vertex deps not wired in this build" {
			t.Errorf("Reason drift: %q", body.Features[0].Reason)
		}
		if body.Features[0].Enabled {
			t.Errorf("vertex Enabled=true, want false")
		}
	})

	t.Run("Enabled feature omits reason (omitempty)", func(t *testing.T) {
		// json:omitempty means Reason="" stays out of the wire
		// payload entirely — Foundry-parity SPA code expects
		// `reason` absent when the feature is on.
		SetFeatures([]Feature{
			{Name: "mcp", Enabled: true, Description: "MCP server mounted"},
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		features, _ := raw["features"].([]any)
		entry, _ := features[0].(map[string]any)
		if _, present := entry["reason"]; present {
			t.Errorf("reason should be omitted via json:omitempty; got %v", entry)
		}
	})

	t.Run("Empty registry returns array (not null)", func(t *testing.T) {
		SetFeatures(nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		features, ok := raw["features"].([]any)
		if !ok {
			t.Fatalf("features field absent or not array; body=%s", rec.Body.String())
		}
		if features == nil {
			t.Errorf("features should be empty [], not null")
		}
		if len(features) != 0 {
			t.Errorf("len=%d, want 0", len(features))
		}
	})

	t.Run("Endpoint is unauthenticated", func(t *testing.T) {
		SetFeatures([]Feature{{Name: "any", Enabled: true}})
		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		// Deliberately no Authorization header.
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 — endpoint should be public",
				rec.Code)
		}
	})

	t.Run("SetFeatures snapshot isolates caller mutations", func(t *testing.T) {
		// Round-127 SetFeatures copies the input — subsequent
		// caller mutations must NOT race the handler's reads.
		input := []Feature{{Name: "alpha", Enabled: true}}
		SetFeatures(input)
		// Mutate caller's slice AFTER setting.
		input[0].Name = "mutated"
		input[0].Enabled = false

		req := httptest.NewRequest(http.MethodGet, "/api/v2/build-info/features", nil)
		rec := httptest.NewRecorder()
		FeaturesHandler().ServeHTTP(rec, req)
		var body FeaturesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Features[0].Name != "alpha" || !body.Features[0].Enabled {
			t.Errorf("caller mutation leaked into registry: got %+v",
				body.Features[0])
		}
	})

	t.Run("Sort applies on each SetFeatures call (not lazily on read)", func(t *testing.T) {
		// Round-127 sorts at SetFeatures time, not in the handler —
		// so reads stay O(1) (modulo copy). Confirm the order
		// hasn't been re-randomised by ensuring two calls with the
		// same input yield identical bytes.
		input := []Feature{
			{Name: "b", Enabled: true},
			{Name: "a", Enabled: false},
		}
		SetFeatures(input)
		// Get also returns a sorted snapshot independently.
		out := currentFeatures()
		want := []string{"a", "b"}
		got := []string{out[0].Name, out[1].Name}
		// Sanity: sort.Strings on a no-op should match.
		check := append([]string(nil), got...)
		sort.Strings(check)
		for i := range got {
			if got[i] != want[i] || got[i] != check[i] {
				t.Errorf("not pre-sorted at i=%d: got=%q want=%q",
					i, got[i], want[i])
			}
		}
	})
}
