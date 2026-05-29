package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_MeOntologiesHandler covers round 99 — Foundry-parity
// caller-scoped ontology inventory. SPA ontology-picker fetches
// "your ontologies" without listing every ontology + filtering
// against the OntologyRoles map client-side.
//
// Filtering rule: an ontology appears in the response iff
// u.OntologyRoles[ontology.RID] is non-empty.
//
// Scenarios:
//   - User with scoped role on one ontology: returns just that one
//   - User with scoped roles on multiple ontologies: returns all matching
//   - User with NO scoped roles: returns empty array (not 404)
//   - Scoped role on RID that no longer maps to a known ontology:
//     silently skipped (matches the established missing-RID-silent-skip
//     convention from rounds 79/81/83/85/87/89)
//   - Unauthenticated: 401
//   - Empty response is array (not null) so SPA can iterate without nil-checks
//   - Each entry carries rid + apiName + displayName + role
//   - Lister error returns 500 MeOntologiesFailed

type fakeLister struct {
	ontologies []auth.ResolvedOntology
	err        error
}

func (f *fakeLister) ListOntologies(_ context.Context) ([]auth.ResolvedOntology, error) {
	return f.ontologies, f.err
}

func newMeOntologiesServer(lister auth.OntologyLister) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v2/me/ontologies", auth.MeOntologiesHandler(lister))
	return mux
}

func doMeOntologies(t *testing.T, h http.Handler, u *auth.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me/ontologies", nil)
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBDD_MeOntologiesHandler(t *testing.T) {
	const (
		nwRID    = "ri.ontology.main.ontology.northwind"
		chRID    = "ri.ontology.main.ontology.chinook"
		otherRID = "ri.ontology.main.ontology.other"
	)
	allOntologies := []auth.ResolvedOntology{
		{RID: nwRID, APIName: "northwind", DisplayName: "Northwind"},
		{RID: chRID, APIName: "chinook", DisplayName: "Chinook"},
		{RID: otherRID, APIName: "other", DisplayName: "Other"},
	}
	lister := &fakeLister{ontologies: allOntologies}

	t.Run("Scoped role on one ontology returns that one", func(t *testing.T) {
		h := newMeOntologiesServer(lister)
		u := &auth.User{
			ID:            "u-1",
			OntologyRoles: map[string]string{nwRID: "ontology-editor"},
		}
		rec := doMeOntologies(t, h, u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.MeOntologiesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Ontologies) != 1 {
			t.Fatalf("len=%d, want 1; body=%s", len(resp.Ontologies), rec.Body.String())
		}
		got := resp.Ontologies[0]
		if got.RID != nwRID {
			t.Errorf("RID=%q, want %q", got.RID, nwRID)
		}
		if got.APIName != "northwind" {
			t.Errorf("APIName=%q, want northwind", got.APIName)
		}
		if got.DisplayName != "Northwind" {
			t.Errorf("DisplayName=%q, want Northwind", got.DisplayName)
		}
		if got.Role != "ontology-editor" {
			t.Errorf("Role=%q, want ontology-editor", got.Role)
		}
	})

	t.Run("Scoped roles on multiple ontologies returns all matching", func(t *testing.T) {
		h := newMeOntologiesServer(lister)
		u := &auth.User{
			ID: "u-2",
			OntologyRoles: map[string]string{
				nwRID: "ontology-editor",
				chRID: "ontology-admin",
			},
		}
		rec := doMeOntologies(t, h, u)
		var resp auth.MeOntologiesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Ontologies) != 2 {
			t.Fatalf("len=%d, want 2", len(resp.Ontologies))
		}
		gotRIDs := map[string]string{}
		for _, e := range resp.Ontologies {
			gotRIDs[e.RID] = e.Role
		}
		if gotRIDs[nwRID] != "ontology-editor" {
			t.Errorf("northwind role=%q, want ontology-editor", gotRIDs[nwRID])
		}
		if gotRIDs[chRID] != "ontology-admin" {
			t.Errorf("chinook role=%q, want ontology-admin", gotRIDs[chRID])
		}
		if _, ok := gotRIDs[otherRID]; ok {
			t.Errorf("other ontology leaked into response (no scoped role)")
		}
	})

	t.Run("No scoped roles returns empty array (not 404)", func(t *testing.T) {
		h := newMeOntologiesServer(lister)
		u := &auth.User{
			ID:    "u-3",
			Roles: []string{"viewer"},
			// OntologyRoles nil — no scoped roles at all
		}
		rec := doMeOntologies(t, h, u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.MeOntologiesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Ontologies == nil {
			t.Errorf("ontologies should be empty [], not null")
		}
		if len(resp.Ontologies) != 0 {
			t.Errorf("len=%d, want 0", len(resp.Ontologies))
		}
	})

	t.Run("Scoped role on unknown RID is silently skipped", func(t *testing.T) {
		// Foundry-parity: if a stale OntologyRoles entry points at a
		// RID that no longer exists, the listing silently drops it
		// rather than 404'ing. Same convention as the 8-of-8 batch
		// getByRid endpoints from rounds 79-89.
		h := newMeOntologiesServer(lister)
		u := &auth.User{
			ID: "u-4",
			OntologyRoles: map[string]string{
				nwRID:                                 "ontology-editor",
				"ri.ontology.main.ontology.ghost-rid": "ontology-admin",
			},
		}
		rec := doMeOntologies(t, h, u)
		var resp auth.MeOntologiesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Ontologies) != 1 {
			t.Fatalf("len=%d, want 1 (ghost-rid should be silently skipped)",
				len(resp.Ontologies))
		}
		if resp.Ontologies[0].RID != nwRID {
			t.Errorf("RID=%q, want %q (only northwind should resolve)",
				resp.Ontologies[0].RID, nwRID)
		}
	})

	t.Run("Unauthenticated returns 401 MissingAuthenticatedUser", func(t *testing.T) {
		h := newMeOntologiesServer(lister)
		rec := doMeOntologies(t, h, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "MissingAuthenticatedUser" {
			t.Errorf("errorName=%v, want MissingAuthenticatedUser", body["errorName"])
		}
	})

	t.Run("Empty response is array not null", func(t *testing.T) {
		// Same defensive contract as the round 79-89 batch endpoints:
		// the SPA iterates without nil-checks.
		h := newMeOntologiesServer(lister)
		u := &auth.User{ID: "u-5"} // no roles, no scoped roles
		rec := doMeOntologies(t, h, u)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		ontologies, ok := raw["ontologies"].([]any)
		if !ok {
			t.Errorf("ontologies field absent or not an array; body=%s", rec.Body.String())
		}
		if ontologies == nil {
			t.Errorf("ontologies should be empty [], not null")
		}
	})

	t.Run("Lister error returns 500 MeOntologiesFailed", func(t *testing.T) {
		erroringLister := &fakeLister{err: context.Canceled}
		h := newMeOntologiesServer(erroringLister)
		u := &auth.User{
			ID:            "u-6",
			OntologyRoles: map[string]string{nwRID: "ontology-editor"},
		}
		rec := doMeOntologies(t, h, u)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d, want 500; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "MeOntologiesFailed" {
			t.Errorf("errorName=%v, want MeOntologiesFailed", body["errorName"])
		}
	})
}
