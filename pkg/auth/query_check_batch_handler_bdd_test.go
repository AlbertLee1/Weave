package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_QueryCheckBatchHandler covers round 115 — third axis of
// the bulk probe trio. Identical structure to round-107/109; same
// found discriminator + cross-ontology guard + 401/400/404 contract.

func newQueryCheckBatchServer(
	ontResolver auth.OntologyResolver,
	queryResolver auth.QueryTypeResolver,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v2/me/checks/queryTypes",
		auth.QueryCheckBatchHandler(ontResolver, queryResolver))
	return mux
}

func doQueryCheckBatch(
	t *testing.T,
	h http.Handler,
	u *auth.User,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/me/checks/queryTypes", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBDD_QueryCheckBatchHandler(t *testing.T) {
	const (
		ontRID = "ri.ontology.main.ontology.northwind"
		qtTop  = "ri.qt.topCustomers"
		qtLate = "ri.qt.lateShipments"
	)
	ontResolver := &fakeResolver{byApiName: map[string]*auth.ResolvedOntology{
		"northwind": {RID: ontRID, APIName: "northwind", DisplayName: "Northwind"},
	}}
	queryResolver := &fakeQueryResolver{byKey: map[string]*auth.ResolvedQueryType{
		ontRID + "|topCustomers":  {RID: qtTop, APIName: "topCustomers"},
		ontRID + "|lateShipments": {RID: qtLate, APIName: "lateShipments"},
	}}

	t.Run("Viewer reads N queries in one round-trip with canExecute=true", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":   "northwind",
			"queryTypeApiNames": []string{"topCustomers", "lateShipments"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.QueryCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.OntologyAPIName != "northwind" {
			t.Errorf("OntologyAPIName=%q, want northwind", resp.OntologyAPIName)
		}
		if len(resp.Results) != 2 {
			t.Fatalf("results len=%d, want 2", len(resp.Results))
		}
		// Input order preserved.
		if resp.Results[0].QueryTypeAPIName != "topCustomers" {
			t.Errorf("Results[0]=%q, want topCustomers", resp.Results[0].QueryTypeAPIName)
		}
		if resp.Results[1].QueryTypeAPIName != "lateShipments" {
			t.Errorf("Results[1]=%q, want lateShipments", resp.Results[1].QueryTypeAPIName)
		}
		for i, e := range resp.Results {
			if !e.Found {
				t.Errorf("Results[%d].Found=false, want true", i)
			}
			if !e.CanExecute {
				t.Errorf("Results[%d].CanExecute=false, want true for viewer", i)
			}
		}
	})

	t.Run("Missing query returns found=false + canExecute=false", func(t *testing.T) {
		// Critical contract — missing entries report canExecute=false
		// REGARDLESS of caller perms so the SPA never shows a Run
		// button for a deleted/renamed query.
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-2", Roles: []string{"admin"}}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"ontologyApiName": "northwind",
			"queryTypeApiNames": []string{
				"topCustomers", "ghostQuery", "lateShipments",
			},
		})
		var resp auth.QueryCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Results) != 3 {
			t.Fatalf("len=%d, want 3 (input preserved)", len(resp.Results))
		}
		ghost := resp.Results[1]
		if ghost.QueryTypeAPIName != "ghostQuery" {
			t.Errorf("Results[1]=%q, want ghostQuery", ghost.QueryTypeAPIName)
		}
		if ghost.Found {
			t.Errorf("ghostQuery.Found=true, want false")
		}
		if ghost.CanExecute {
			t.Errorf("missing query must report CanExecute=false regardless of admin role")
		}
		if ghost.QueryTypeRID != "" {
			t.Errorf("missing query should have empty RID; got %q", ghost.QueryTypeRID)
		}
	})

	t.Run("No-role user returns canExecute=false across all entries", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-3"}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":   "northwind",
			"queryTypeApiNames": []string{"topCustomers"},
		})
		var resp auth.QueryCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Results[0].CanExecute {
			t.Errorf("no-role user should NOT have canExecute")
		}
		if !resp.Results[0].Found {
			t.Errorf("found should be true (query exists)")
		}
	})

	t.Run("Other-ontology scoped role does NOT leak", func(t *testing.T) {
		// Cross-ontology guard — caller has ontology-owner on a
		// different ontology, no global roles. canExecute must be
		// false on northwind.
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{
			ID: "u-4",
			OntologyRoles: map[string]string{
				"ri.ontology.main.ontology.other": "ontology-owner",
			},
		}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":   "northwind",
			"queryTypeApiNames": []string{"topCustomers"},
		})
		var resp auth.QueryCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Results[0].CanExecute {
			t.Errorf("other-ontology owner must NOT leak canExecute")
		}
	})

	t.Run("Unauthenticated returns 401", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		rec := doQueryCheckBatch(t, h, nil, map[string]any{
			"ontologyApiName":   "northwind",
			"queryTypeApiNames": []string{"topCustomers"},
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Unknown ontology returns 404 OntologyNotFound", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":   "does-not-exist",
			"queryTypeApiNames": []string{"topCustomers"},
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

	t.Run("Empty queryTypeApiNames returns 400", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":   "northwind",
			"queryTypeApiNames": []string{},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})

	t.Run("Missing ontologyApiName returns 400", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doQueryCheckBatch(t, h, u, map[string]any{
			"queryTypeApiNames": []string{"topCustomers"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})

	t.Run("Missing body returns 400", func(t *testing.T) {
		h := newQueryCheckBatchServer(ontResolver, queryResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/me/checks/queryTypes", nil)
		req = req.WithContext(auth.WithUser(req.Context(), u))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})
}
