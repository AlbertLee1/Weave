package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestBDD_GrantMarking_ReturnsFullMetadata covers a wire-shape bug
// on POST /api/admin/users/{userId}/markings. The handler builds
// its response by iterating over `[]string` returned by
// GetUserMarkings (which is just a bare list of marking names),
// then constructs `MarkingGrantResponse{UserID: userID, MarkingName: n}`
// literals — silently dropping GrantedAt / GrantedBy / ExpiresAt on
// every entry, including the brand-new grant that was just minted
// with valid values.
//
// The parallel GET endpoint (/api/admin/users/{userId}/markings)
// already uses admin.ListGrantsByUser + toMarkingGrantResponse and
// correctly returns the full metadata. The POST path drifted from
// the GET path's pattern and an SDK consuming "give me the grant
// I just created" only sees the user+marking pair, not the
// audit envelope it needs to show "granted by alice@example.com
// at 2026-05-24, expires 2026-06-23" in a confirmation modal.
//
// The fix mirrors the GET path: when h.admin is wired, list the
// full grant rows via admin.ListGrantsByUser and reuse the existing
// toMarkingGrantResponse helper. Degraded-mode (no admin store)
// keeps the bare-name fallback so the test harness without admin
// wiring still sees a sensible 200 response.
func TestBDD_GrantMarking_ReturnsFullMetadata(t *testing.T) {
	t.Run("POST grant returns the new grant with grantedBy + grantedAt populated", func(t *testing.T) {
		h, _, _, _ := newMarkingHandlerHarness(t)
		body, _ := json.Marshal(map[string]any{"marking": "PII"})
		req := withAdmin(httptest.NewRequest(http.MethodPost,
			"/api/admin/users/user:alice@example.com/markings",
			bytes.NewReader(body)))
		rec := httptest.NewRecorder()
		h.grantMarkingFor(rec, req, "user:alice@example.com")

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp MarkingGrantsResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Grants) == 0 {
			t.Fatal("response.grants is empty; expected at least one")
		}
		// Locate the freshly minted PII grant in the response.
		var found *MarkingGrantResponse
		for i := range resp.Grants {
			if resp.Grants[i].MarkingName == "PII" {
				found = &resp.Grants[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("freshly minted PII grant missing from response: %+v", resp.Grants)
		}
		if found.GrantedBy == "" {
			t.Errorf("grantedBy is empty; the just-minted grant carries grantedBy=%q (the admin user) and the wire shape must surface it for audit modals", "user:admin@example.com")
		}
		if found.GrantedAt == "" {
			t.Errorf("grantedAt is empty; the just-minted grant has a real timestamp and the wire shape must surface it so the SPA can render 'Granted just now'")
		}
		if found.UserID != "user:alice@example.com" {
			t.Errorf("userId: got %q, want user:alice@example.com", found.UserID)
		}
	})

	t.Run("POST grant with expiresInDays surfaces expiresAt in the response", func(t *testing.T) {
		h, _, _, _ := newMarkingHandlerHarness(t)
		body, _ := json.Marshal(map[string]any{"marking": "PII", "expiresInDays": 30})
		req := withAdmin(httptest.NewRequest(http.MethodPost,
			"/api/admin/users/user:alice@example.com/markings",
			bytes.NewReader(body)))
		rec := httptest.NewRecorder()
		h.grantMarkingFor(rec, req, "user:alice@example.com")

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp MarkingGrantsResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		var found *MarkingGrantResponse
		for i := range resp.Grants {
			if resp.Grants[i].MarkingName == "PII" {
				found = &resp.Grants[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("freshly minted PII grant missing from response: %+v", resp.Grants)
		}
		if found.ExpiresAt == "" {
			t.Fatalf("expiresAt is empty even though expiresInDays=30 was supplied; the SPA confirmation modal needs the wire shape to surface the expiry timestamp it just persisted")
		}
		// Sanity-check: the parsed expiresAt should be in the future
		// (we asked for ~30 days from now).
		ts, err := time.Parse(time.RFC3339, found.ExpiresAt)
		if err != nil {
			t.Fatalf("expiresAt %q is not RFC3339-parseable: %v", found.ExpiresAt, err)
		}
		if !ts.After(time.Now()) {
			t.Errorf("expiresAt %s is not in the future; expiresInDays=30 should land ~30 days out", ts)
		}
	})

	t.Run("GET grants /api/admin/users/{id}/markings still returns full metadata (no regression)", func(t *testing.T) {
		// Existing happy path: GET already uses admin.ListGrantsByUser
		// and toMarkingGrantResponse correctly. Round-14's fix on the
		// POST path must not regress GET — both should now surface the
		// same metadata-rich shape.
		h, _, _, _ := newMarkingHandlerHarness(t)
		// Seed via POST so the test exercises the round-14 fix end-to-end.
		body, _ := json.Marshal(map[string]any{"marking": "PII"})
		postReq := withAdmin(httptest.NewRequest(http.MethodPost,
			"/api/admin/users/user:alice@example.com/markings",
			bytes.NewReader(body)))
		postRec := httptest.NewRecorder()
		h.grantMarkingFor(postRec, postReq, "user:alice@example.com")
		if postRec.Code != http.StatusOK {
			t.Fatalf("POST seed: status=%d body=%s", postRec.Code, postRec.Body.String())
		}

		getReq := withAdmin(httptest.NewRequest(http.MethodGet,
			"/api/admin/users/user:alice@example.com/markings", nil))
		getRec := httptest.NewRecorder()
		h.listGrantsByUserFor(getRec, getReq, "user:alice@example.com")
		if getRec.Code != http.StatusOK {
			t.Fatalf("GET: status=%d body=%s", getRec.Code, getRec.Body.String())
		}
		var resp MarkingGrantsResponse
		_ = json.NewDecoder(getRec.Body).Decode(&resp)
		if len(resp.Grants) == 0 {
			t.Fatal("GET returned no grants")
		}
		if resp.Grants[0].GrantedAt == "" {
			t.Errorf("GET regression: grantedAt is empty, want non-empty")
		}
		if resp.Grants[0].GrantedBy == "" {
			t.Errorf("GET regression: grantedBy is empty, want non-empty")
		}
	})
}
