package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
)

// TestBDD_MarkingGrantRejectsAmbiguousJSONBody_P2A306 covers the marking
// grant admin write surface:
//
//   - POST /api/admin/users/{userId}/markings  (MarkingHandler.GrantMarking)
//
// The endpoint mints a marking grant onto an existing user, so a body
// composed of two concatenated JSON objects must be rejected with HTTP 400
// plus a "single JSON value" reason instead of silently decoding only the
// first object and dropping the trailing bytes. Without the hardening, a
// payload like `{"marking":"PUBLIC"}{"marking":"PII"}` decodes cleanly to
// {Marking:"PUBLIC"} under json.Decoder while a proxy / WAF / log scraper
// re-parsing the raw bytes can be tricked into believing PII was the grant
// that landed — exactly the smuggling class P2A-301..305 already closed for
// role definitions, group writes, service-account mutations, API-key admin
// writes, and user-role grants.
//
// The test snapshots the user's marking list before/after to prove the
// rejected request was non-mutating, and the trailing happy-path sub-test
// asserts that well-formed grants still land after the hardening.
func TestBDD_MarkingGrantRejectsAmbiguousJSONBody_P2A306(t *testing.T) {
	t.Run("GrantMarking rejects concatenated JSON without granting the marking", func(t *testing.T) {
		h, repo, _, auditStore := newMarkingHandlerHarness(t)
		const userID = "user:alice@example.com"
		seedMarkings := snapshotUserMarkings(repo, userID)
		seedAuditCount := len(auditStore.events)

		// {"marking":"PUBLIC"}{"marking":"PII"} — first decodes cleanly to
		// PUBLIC, the smuggled trailer would silently swap to PII under the
		// pre-hardened decoder. Both names exist in the fake repo's seeded
		// marking catalog, so the only thing distinguishing legitimate from
		// smuggled is the decode-rejection contract.
		body := `{"marking":"PUBLIC"}{"marking":"PII"}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/markings", bytes.NewReader([]byte(body))))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.grantMarkingFor(rec, req, userID)

		assertSingleJSONValueRejection(t, rec, "InvalidMarkingRequest")

		afterMarkings := snapshotUserMarkings(repo, userID)
		if !reflect.DeepEqual(afterMarkings, seedMarkings) {
			t.Fatalf("GrantMarking with concatenated body mutated user markings: before=%v after=%v", seedMarkings, afterMarkings)
		}
		if got := len(auditStore.events); got != seedAuditCount {
			t.Fatalf("GrantMarking with concatenated body wrote audit events: before=%d after=%d", seedAuditCount, got)
		}
	})

	t.Run("well-formed body still grants the requested marking", func(t *testing.T) {
		h, repo, _, _ := newMarkingHandlerHarness(t)
		const userID = "user:alice@example.com"

		body, err := json.Marshal(map[string]any{"marking": "PII"})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/markings", bytes.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.grantMarkingFor(rec, req, userID)

		if rec.Code != http.StatusOK {
			t.Fatalf("happy GrantMarking returned %d body=%s", rec.Code, rec.Body.String())
		}
		got, err := repo.GetUserMarkings(context.Background(), userID)
		if err != nil {
			t.Fatalf("GetUserMarkings after happy grant: %v", err)
		}
		if len(got) != 1 || got[0] != "PII" {
			t.Fatalf("happy GrantMarking did not persist PII: got %v", got)
		}
	})
}

// snapshotUserMarkings returns a sorted copy of the markings granted to
// userID in the fake repository. The sort makes the before/after comparison
// stable against map iteration order.
func snapshotUserMarkings(repo *fakeMarkingRepo, userID string) []string {
	names, _ := repo.GetUserMarkings(context.Background(), userID)
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}
