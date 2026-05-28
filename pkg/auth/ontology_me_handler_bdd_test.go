package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_OntologyMeHandler covers round 95 — per-ontology
// caller-scope query (Foundry-parity). The existing /api/v2/me
// returns the full user object including every per-ontology role
// they hold; this new /api/v2/ontologies/{ontologyApiName}/me
// returns the user's resolved role + effective permissions for
// ONE specific ontology, which is the shape the SPA needs to gate
// UI affordances ("can I see the Create button on THIS ontology?")
// without parsing the global map client-side.
//
// Response shape:
//
//   {
//     "ontologyRid":     "ri.ontology.main.ontology.northwind",
//     "ontologyApiName": "northwind",
//     "role":            "ontology-editor",
//     "permissions":     ["...", "..."],
//     "markings":        ["ACME"]
//   }
//
// Scenarios:
//   - Auth'd user with scoped role: role populated + perms include scoped grant
//   - Auth'd user with NO scoped role: role="" + perms still include global ones
//   - Unauthenticated request: 401 MissingAuthenticatedUser
//   - Unknown ontology: 404 OntologyNotFound
//   - Markings propagate from context (same source as /api/v2/me)
//   - Empty markings produce empty array (not null)

type fakeResolver struct {
	byAPIName map[string]*auth.ResolvedOntology
}

func (f *fakeResolver) GetOntology(_ context.Context, apiNameOrRID string) (*auth.ResolvedOntology, error) {
	if r, ok := f.byAPIName[apiNameOrRID]; ok {
		return r, nil
	}
	return nil, auth.ErrOntologyNotFound
}

func TestBDD_OntologyMeHandler(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.northwind"

	newServer := func() *chi.Mux {
		resolver := &fakeResolver{byAPIName: map[string]*auth.ResolvedOntology{
			"northwind": {RID: ontRID, APIName: "northwind"},
		}}
		r := chi.NewRouter()
		r.Method(http.MethodGet, "/api/v2/ontologies/{ontologyApiName}/me",
			auth.OntologyMeHandler(resolver))
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, ontAPIName string, u *auth.User, markings []string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontAPIName+"/me", nil)
		if u != nil {
			if markings != nil {
				if u.Attributes == nil {
					u.Attributes = map[string]any{}
				}
				u.Attributes[auth.MarkingsAttributeKey] = markings
			}
			req = req.WithContext(auth.WithUser(req.Context(), u))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Auth'd user with scoped role returns role + merged perms", func(t *testing.T) {
		r := newServer()
		u := &auth.User{
			ID: "u-1", Email: "alice@x", Name: "Alice",
			Roles:         []string{"viewer"},
			OntologyRoles: map[string]string{ontRID: "ontology-editor"},
		}
		rec := doGet(t, r, "northwind", u, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body auth.OntologyMeResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.OntologyRID != ontRID {
			t.Errorf("OntologyRID=%q, want %q", body.OntologyRID, ontRID)
		}
		if body.OntologyAPIName != "northwind" {
			t.Errorf("OntologyAPIName=%q, want northwind", body.OntologyAPIName)
		}
		if body.Role != "ontology-editor" {
			t.Errorf("Role=%q, want ontology-editor", body.Role)
		}
		if len(body.Permissions) == 0 {
			t.Errorf("Permissions should be non-empty; got %v", body.Permissions)
		}
	})

	t.Run("Auth'd user without scoped role returns empty role + global perms", func(t *testing.T) {
		r := newServer()
		u := &auth.User{
			ID: "u-2", Email: "bob@x", Name: "Bob",
			Roles: []string{"viewer"},
		}
		rec := doGet(t, r, "northwind", u, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var body auth.OntologyMeResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Role != "" {
			t.Errorf("Role=%q, want empty string (no scoped role)", body.Role)
		}
		if body.Permissions == nil {
			t.Errorf("Permissions should be a (possibly empty) array, not null")
		}
	})

	t.Run("Unauthenticated request returns 401 MissingAuthenticatedUser", func(t *testing.T) {
		r := newServer()
		rec := doGet(t, r, "northwind", nil, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "MissingAuthenticatedUser" {
			t.Errorf("errorName=%v, want MissingAuthenticatedUser", body["errorName"])
		}
	})

	t.Run("Unknown ontology returns 404 OntologyNotFound", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "does-not-exist", u, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "OntologyNotFound" {
			t.Errorf("errorName=%v, want OntologyNotFound", body["errorName"])
		}
	})

	t.Run("Markings propagate from request context", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", u, []string{"ACME", "PII"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var body auth.OntologyMeResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if len(body.Markings) != 2 {
			t.Errorf("Markings=%v, want 2 entries", body.Markings)
		}
		got := map[string]bool{}
		for _, m := range body.Markings {
			got[m] = true
		}
		if !got["ACME"] || !got["PII"] {
			t.Errorf("Markings %v missing ACME or PII", body.Markings)
		}
	})

	t.Run("Empty markings produce empty array (not null)", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", u, nil)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		marks, ok := body["markings"].([]any)
		if !ok {
			t.Errorf("markings field absent or not an array; body=%s", rec.Body.String())
		}
		if marks == nil {
			t.Errorf("markings should be empty [] not null")
		}
	})
}
