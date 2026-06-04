package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestBDD_MarkingCreatedAt_ExposedInListResponse covers the admin marking
// catalog surface:
//
//   - GET /api/admin/markings  (MarkingHandler.ListMarkings)
//
// Markings carry a DB-populated CreatedAt instant (the migration seed time or
// the operator's add-marking timestamp), but the wire response historically
// dropped it: MarkingResponse omitted the field and toMarkingResponse never
// copied m.CreatedAt. That left the admin UI unable to show "when was this
// marking defined?". This scenario locks the contract that every marking row
// in the list response exposes its createdAt as an RFC3339 string.
//
// Given a marking with a known CreatedAt instant seeded into the repository,
// When an admin lists markings,
// Then the matching response row carries that instant formatted as RFC3339.
func TestBDD_MarkingCreatedAt_ExposedInListResponse(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)

	created := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	repo.markings = append(repo.markings, Marking{
		Name:        "AUDIT",
		DisplayName: "Audit",
		Description: "Audit-only marking",
		Color:       "#a855f7",
		CreatedAt:   created,
	})

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/markings", nil))
	rec := httptest.NewRecorder()
	h.ListMarkings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp MarkingListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var found *MarkingResponse
	for i := range resp.Markings {
		if resp.Markings[i].Name == "AUDIT" {
			found = &resp.Markings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("AUDIT marking missing from response: %+v", resp.Markings)
	}

	want := created.Format(time.RFC3339)
	if found.CreatedAt != want {
		t.Fatalf("createdAt mismatch: got %q want %q", found.CreatedAt, want)
	}
}

// TestBDD_MarkingCreatedAt_RawJSONCarriesField guards the raw wire shape: a
// proxy / log scraper / non-Go SDK re-parsing the response bytes must see a
// "createdAt" key, not just the typed Go struct field. Asserting against the
// decoded generic map proves the json tag is present and populated.
func TestBDD_MarkingCreatedAt_RawJSONCarriesField(t *testing.T) {
	h, repo, _, _ := newMarkingHandlerHarness(t)

	created := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	repo.markings = []Marking{{
		Name:        "PUBLIC",
		DisplayName: "Public",
		Color:       "#10b981",
		CreatedAt:   created,
	}}

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/markings", nil))
	rec := httptest.NewRecorder()
	h.ListMarkings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var raw struct {
		Markings []map[string]any `json:"markings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if len(raw.Markings) != 1 {
		t.Fatalf("expected 1 marking, got %d", len(raw.Markings))
	}
	got, ok := raw.Markings[0]["createdAt"]
	if !ok {
		t.Fatalf("raw response missing createdAt key: %v", raw.Markings[0])
	}
	if got != created.Format(time.RFC3339) {
		t.Fatalf("raw createdAt mismatch: got %v want %q", got, created.Format(time.RFC3339))
	}
}
