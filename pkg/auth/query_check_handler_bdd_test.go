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

// TestBDD_QueryCheckHandler covers round 113 — third axis of the
// per-resource check family (after round-103 actions and round-105
// objects). SPA gates per-query-type "Run Query" affordances by
// GETting this endpoint.
//
// Scenarios mirror round-103/105 structure exactly.

type fakeQueryResolver struct {
	byKey map[string]*auth.ResolvedQueryType
}

func (f *fakeQueryResolver) GetQueryType(_ context.Context, ontologyRID, apiName string) (*auth.ResolvedQueryType, error) {
	if qt, ok := f.byKey[ontologyRID+"|"+apiName]; ok {
		return qt, nil
	}
	return nil, auth.ErrQueryTypeNotFound
}

func TestBDD_QueryCheckHandler(t *testing.T) {
	const (
		ontRID = "ri.ontology.main.ontology.northwind"
		qtRID  = "ri.query-type.main.topCustomers"
	)
	ontResolver := &fakeResolver{byApiName: map[string]*auth.ResolvedOntology{
		"northwind": {RID: ontRID, APIName: "northwind", DisplayName: "Northwind"},
	}}
	queryResolver := &fakeQueryResolver{byKey: map[string]*auth.ResolvedQueryType{
		ontRID + "|topCustomers": {RID: qtRID, APIName: "topCustomers"},
	}}

	newServer := func() *chi.Mux {
		r := chi.NewRouter()
		r.Method(http.MethodGet,
			"/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryTypeApiName}/check",
			auth.QueryCheckHandler(ontResolver, queryResolver))
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, ontApi, qtApi string, u *auth.User) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontApi+"/queryTypes/"+qtApi+"/check", nil)
		if u != nil {
			req = req.WithContext(auth.WithUser(req.Context(), u))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Viewer role grants canExecute (queryType.read)", func(t *testing.T) {
		// QueryType execution gates on queryType.read; viewer holds
		// that perm. Validates that the wrapper passes the right
		// constant to HasPermission.
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", "topCustomers", u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.QueryCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanExecute {
			t.Errorf("CanExecute=false, want true for viewer")
		}
		if resp.OntologyAPIName != "northwind" {
			t.Errorf("OntologyAPIName=%q, want northwind", resp.OntologyAPIName)
		}
		if resp.QueryTypeAPIName != "topCustomers" {
			t.Errorf("QueryTypeAPIName=%q, want topCustomers", resp.QueryTypeAPIName)
		}
		if resp.QueryTypeRID != qtRID {
			t.Errorf("QueryTypeRID=%q, want %q", resp.QueryTypeRID, qtRID)
		}
	})

	t.Run("No-role user returns canExecute=false (200 not 403)", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-2"}
		rec := doGet(t, r, "northwind", "topCustomers", u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200 (probe is informational)", rec.Code)
		}
		var resp auth.QueryCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.CanExecute {
			t.Errorf("CanExecute=true, want false for no-role user")
		}
	})

	t.Run("Scoped ontology-owner grants canExecute", func(t *testing.T) {
		r := newServer()
		u := &auth.User{
			ID:            "u-3",
			OntologyRoles: map[string]string{ontRID: "ontology-owner"},
		}
		rec := doGet(t, r, "northwind", "topCustomers", u)
		var resp auth.QueryCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanExecute {
			t.Errorf("ontology-owner on northwind should grant canExecute; got false")
		}
	})

	t.Run("Other-ontology scoped role does NOT leak", func(t *testing.T) {
		// Cross-ontology leak guard — caller has ontology-owner on
		// a different ontology, no roles on northwind. The user has
		// NO global roles to fall back on, so canExecute must be
		// false despite their other-ontology owner status.
		r := newServer()
		u := &auth.User{
			ID: "u-4",
			OntologyRoles: map[string]string{
				"ri.ontology.main.ontology.other": "ontology-owner",
			},
		}
		rec := doGet(t, r, "northwind", "topCustomers", u)
		var resp auth.QueryCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.CanExecute {
			t.Errorf("other-ontology owner must NOT leak canExecute; got true")
		}
	})

	t.Run("Unauthenticated returns 401", func(t *testing.T) {
		r := newServer()
		rec := doGet(t, r, "northwind", "topCustomers", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Unknown ontology returns 404 OntologyNotFound", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "does-not-exist", "topCustomers", u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "OntologyNotFound" {
			t.Errorf("errorName=%v, want OntologyNotFound", body["errorName"])
		}
	})

	t.Run("Unknown query type returns 404 QueryTypeNotFound", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", "ghostQuery", u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "QueryTypeNotFound" {
			t.Errorf("errorName=%v, want QueryTypeNotFound", body["errorName"])
		}
	})

	t.Run("Response carries all 4 required fields", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", "topCustomers", u)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for _, field := range []string{
			"ontologyApiName", "queryTypeApiName", "queryTypeRid", "canExecute",
		} {
			if _, ok := raw[field]; !ok {
				t.Errorf("response missing required field %q; body=%s",
					field, rec.Body.String())
			}
		}
	})
}
