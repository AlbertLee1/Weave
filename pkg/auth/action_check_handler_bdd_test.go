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

// TestBDD_ActionCheckHandler covers round 103 — Foundry-parity
// action applicability probe. SPA disables per-row "Apply Action"
// buttons by GETting this endpoint instead of round-tripping a
// real apply call.
//
// Endpoint: GET /api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check
// Response: {ontologyApiName, actionApiName, actionRid, canApply}
//
// Distinct from round-97 PermissionsCheckHandler:
//   1. Validates the action EXISTS (404 ActionTypeNotFound when missing)
//   2. Single GET — fits row-render code where POST+body is awkward
//
// Scenarios:
//   - Auth'd user with action.apply: canApply=true
//   - Auth'd user without action.apply: canApply=false (200, not 403)
//   - Scoped role grants action.apply: canApply=true
//   - Unauthenticated: 401 MissingAuthenticatedUser
//   - Unknown ontology: 404 OntologyNotFound
//   - Unknown action type: 404 ActionTypeNotFound
//   - Response carries ontologyApiName + actionApiName + actionRid echo

type fakeActionResolver struct {
	byKey map[string]*auth.ResolvedActionType
}

func (f *fakeActionResolver) GetActionType(_ context.Context, ontologyRID, apiName string) (*auth.ResolvedActionType, error) {
	if at, ok := f.byKey[ontologyRID+"|"+apiName]; ok {
		return at, nil
	}
	return nil, auth.ErrActionTypeNotFound
}

func TestBDD_ActionCheckHandler(t *testing.T) {
	const (
		ontRID = "ri.ontology.main.ontology.northwind"
		actRID = "ri.action-type.main.createCustomer"
	)
	ontResolver := &fakeResolver{byAPIName: map[string]*auth.ResolvedOntology{
		"northwind": {RID: ontRID, APIName: "northwind", DisplayName: "Northwind"},
	}}
	actResolver := &fakeActionResolver{byKey: map[string]*auth.ResolvedActionType{
		ontRID + "|createCustomer": {RID: actRID, APIName: "createCustomer"},
	}}

	newServer := func() *chi.Mux {
		r := chi.NewRouter()
		r.Method(http.MethodGet,
			"/api/v2/ontologies/{ontologyApiName}/actions/{actionApiName}/check",
			auth.ActionCheckHandler(ontResolver, actResolver))
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, ontApi, actionApi string, u *auth.User) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontApi+"/actions/"+actionApi+"/check", nil)
		if u != nil {
			req = req.WithContext(auth.WithUser(req.Context(), u))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Auth'd user with action.apply via global role returns canApply=true", func(t *testing.T) {
		r := newServer()
		// admin role grants every permission including action.apply
		u := &auth.User{ID: "u-1", Roles: []string{"admin"}}
		rec := doGet(t, r, "northwind", "createCustomer", u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.ActionCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanApply {
			t.Errorf("CanApply=false, want true for admin role")
		}
		if resp.OntologyAPIName != "northwind" {
			t.Errorf("OntologyAPIName=%q, want northwind", resp.OntologyAPIName)
		}
		if resp.ActionAPIName != "createCustomer" {
			t.Errorf("ActionAPIName=%q, want createCustomer", resp.ActionAPIName)
		}
		if resp.ActionRID != actRID {
			t.Errorf("ActionRID=%q, want %q", resp.ActionRID, actRID)
		}
	})

	t.Run("Auth'd user without action.apply returns canApply=false (200 not 403)", func(t *testing.T) {
		// The probe is informational — it always returns 200 with a
		// boolean so the SPA can use it for UI gating. A 403 would
		// muddle "no permission" with "endpoint error".
		r := newServer()
		u := &auth.User{ID: "u-2"} // no roles
		rec := doGet(t, r, "northwind", "createCustomer", u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.ActionCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.CanApply {
			t.Errorf("CanApply=true, want false for no-role user")
		}
	})

	t.Run("Scoped ontology-owner role grants action.execute", func(t *testing.T) {
		// ontology-owner is the project's per-ontology role that
		// includes PermActionExecute (see pkg/auth/permissions.go);
		// scoping it to northwind on a user with no global role
		// proves the resolver's per-ontology branch fires.
		r := newServer()
		u := &auth.User{
			ID:            "u-3",
			OntologyRoles: map[string]string{ontRID: "ontology-owner"},
		}
		rec := doGet(t, r, "northwind", "createCustomer", u)
		var resp auth.ActionCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanApply {
			t.Errorf("ontology-owner on northwind should grant action.execute; got false")
		}
	})

	t.Run("Scoped role on a DIFFERENT ontology does not leak through", func(t *testing.T) {
		// Mirror of round-97's other-ontology-leak guard. The user
		// holds ontology-owner on a non-northwind ontology and only
		// the global viewer role on northwind — they must NOT see
		// canApply=true here.
		r := newServer()
		u := &auth.User{
			ID:    "u-4",
			Roles: []string{"viewer"},
			OntologyRoles: map[string]string{
				"ri.ontology.main.ontology.other": "ontology-owner",
			},
		}
		rec := doGet(t, r, "northwind", "createCustomer", u)
		var resp auth.ActionCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.CanApply {
			t.Errorf("other-ontology owner must NOT grant action.execute on northwind; got true")
		}
	})

	t.Run("Unauthenticated returns 401 MissingAuthenticatedUser", func(t *testing.T) {
		r := newServer()
		rec := doGet(t, r, "northwind", "createCustomer", nil)
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
		rec := doGet(t, r, "does-not-exist", "createCustomer", u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "OntologyNotFound" {
			t.Errorf("errorName=%v, want OntologyNotFound", body["errorName"])
		}
	})

	t.Run("Unknown action type returns 404 ActionTypeNotFound", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", "ghost-action", u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "ActionTypeNotFound" {
			t.Errorf("errorName=%v, want ActionTypeNotFound", body["errorName"])
		}
	})

	t.Run("Response echoes ontologyApiName + actionApiName + actionRid", func(t *testing.T) {
		// Wire-format regression guard — these three fields back the SPA's
		// row-render logic, and dropping any would break the per-row
		// disable-button code without a server-side error.
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"admin"}}
		rec := doGet(t, r, "northwind", "createCustomer", u)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for _, field := range []string{"ontologyApiName", "actionApiName", "actionRid", "canApply"} {
			if _, ok := raw[field]; !ok {
				t.Errorf("response missing required field %q; body=%s", field, rec.Body.String())
			}
		}
	})
}
