package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_ActionCheckBatchHandler covers round 109 — bulk action
// applicability probe. Sibling of round-107 ObjectCheckBatchHandler;
// together they let the SPA resolve a freshly-loaded page's OT
// matrix + action list in TWO POSTs instead of K+M parallel GETs.
//
// Invariants mirror round-107: input order preserved, found:bool
// discriminator distinguishes "removed from config" from "exists
// but no perm", found=false entries always have canApply=false
// regardless of caller perms.

func newActionCheckBatchServer(
	ontResolver auth.OntologyResolver,
	actionResolver auth.ActionTypeResolver,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v2/me/checks/actionTypes",
		auth.ActionCheckBatchHandler(ontResolver, actionResolver))
	return mux
}

func doActionCheckBatch(
	t *testing.T,
	h http.Handler,
	u *auth.User,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/me/checks/actionTypes", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBDD_ActionCheckBatchHandler(t *testing.T) {
	const (
		ontRID  = "ri.ontology.main.ontology.northwind"
		actCust = "ri.action-type.main.createCustomer"
		actOrd  = "ri.action-type.main.createOrder"
	)
	ontResolver := &fakeResolver{byApiName: map[string]*auth.ResolvedOntology{
		"northwind": {RID: ontRID, APIName: "northwind", DisplayName: "Northwind"},
	}}
	actResolver := &fakeActionResolver{byKey: map[string]*auth.ResolvedActionType{
		ontRID + "|createCustomer": {RID: actCust, APIName: "createCustomer"},
		ontRID + "|createOrder":    {RID: actOrd, APIName: "createOrder"},
	}}

	t.Run("Admin reads N actions in one round-trip with canApply=true", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"admin"}}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"actionTypeApiNames": []string{"createCustomer", "createOrder"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.ActionCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.OntologyAPIName != "northwind" {
			t.Errorf("OntologyAPIName=%q, want northwind", resp.OntologyAPIName)
		}
		if len(resp.Results) != 2 {
			t.Fatalf("results len=%d, want 2", len(resp.Results))
		}
		// Input order preserved.
		if resp.Results[0].ActionTypeAPIName != "createCustomer" {
			t.Errorf("Results[0]=%q, want createCustomer", resp.Results[0].ActionTypeAPIName)
		}
		if resp.Results[1].ActionTypeAPIName != "createOrder" {
			t.Errorf("Results[1]=%q, want createOrder", resp.Results[1].ActionTypeAPIName)
		}
		for i, e := range resp.Results {
			if !e.Found {
				t.Errorf("Results[%d].Found=false, want true", i)
			}
			if !e.CanApply {
				t.Errorf("Results[%d].CanApply=false, want true for admin", i)
			}
		}
	})

	t.Run("Missing action returns found=false with canApply=false", func(t *testing.T) {
		// Critical contract — missing entries report canApply=false
		// REGARDLESS of caller perms so the SPA never accidentally
		// shows an Apply button for a deleted action.
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-2", Roles: []string{"admin"}}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"ontologyApiName": "northwind",
			"actionTypeApiNames": []string{
				"createCustomer", "ghostAction", "createOrder",
			},
		})
		var resp auth.ActionCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Results) != 3 {
			t.Fatalf("len=%d, want 3 (input preserved)", len(resp.Results))
		}
		ghost := resp.Results[1]
		if ghost.ActionTypeAPIName != "ghostAction" {
			t.Errorf("Results[1]=%q, want ghostAction", ghost.ActionTypeAPIName)
		}
		if ghost.Found {
			t.Errorf("ghostAction.Found=true, want false")
		}
		if ghost.CanApply {
			t.Errorf("missing action must report CanApply=false regardless of caller role")
		}
		if ghost.ActionTypeRID != "" {
			t.Errorf("missing action should have empty RID; got %q", ghost.ActionTypeRID)
		}
	})

	t.Run("Viewer role returns canApply=false across all entries", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-3", Roles: []string{"viewer"}}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"actionTypeApiNames": []string{"createCustomer", "createOrder"},
		})
		var resp auth.ActionCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		for _, e := range resp.Results {
			if !e.Found {
				t.Errorf("viewer should still see existing actions as found=true; got false for %s",
					e.ActionTypeAPIName)
			}
			if e.CanApply {
				t.Errorf("viewer should NOT have canApply for %s", e.ActionTypeAPIName)
			}
		}
	})

	t.Run("Scoped ontology-owner grants canApply for that ontology only", func(t *testing.T) {
		// Cross-ontology leak guard — caller has ontology-owner on a
		// DIFFERENT ontology and only the global viewer role on
		// northwind. canApply must NOT leak through.
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{
			ID:    "u-4",
			Roles: []string{"viewer"},
			OntologyRoles: map[string]string{
				"ri.ontology.main.ontology.other": "ontology-owner",
			},
		}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"actionTypeApiNames": []string{"createCustomer"},
		})
		var resp auth.ActionCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Results[0].CanApply {
			t.Errorf("other-ontology owner must NOT leak canApply on northwind")
		}
	})

	t.Run("Unauthenticated returns 401", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		rec := doActionCheckBatch(t, h, nil, map[string]any{
			"ontologyApiName":    "northwind",
			"actionTypeApiNames": []string{"createCustomer"},
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Unknown ontology returns 404 OntologyNotFound", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "does-not-exist",
			"actionTypeApiNames": []string{"createCustomer"},
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "OntologyNotFound" {
			t.Errorf("errorName=%v, want OntologyNotFound", body["errorName"])
		}
	})

	t.Run("Empty actionTypeApiNames returns 400", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"actionTypeApiNames": []string{},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})

	t.Run("Missing ontologyApiName returns 400", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doActionCheckBatch(t, h, u, map[string]any{
			"actionTypeApiNames": []string{"createCustomer"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})

	t.Run("Missing body returns 400", func(t *testing.T) {
		h := newActionCheckBatchServer(ontResolver, actResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/me/checks/actionTypes", nil)
		req = req.WithContext(auth.WithUser(req.Context(), u))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})
}
