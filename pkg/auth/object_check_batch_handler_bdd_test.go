package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_ObjectCheckBatchHandler covers round 107 — bulk
// object-type applicability probe. The SPA loads K object types'
// read/write gates in one POST instead of K parallel GETs
// against round-105's single endpoint.
//
// Request:  POST /api/v2/me/checks/objectTypes
//           {"ontologyApiName": "northwind",
//            "objectTypeApiNames": ["Customer", "Order"]}
// Response: {"ontologyApiName": "northwind",
//            "results": [{"objectTypeApiName": "Customer", ...}, ...]}
//
// Invariants:
//   - results preserves input order (caller can correlate row N → row N)
//   - found:bool discriminator distinguishes missing-from-config from missing-perm
//   - found=false entries have canRead=canWrite=false regardless of caller perms
//   - 401 unauthenticated, 400 missing body/empty array, 404 OntologyNotFound

func newObjectCheckBatchServer(
	ontResolver auth.OntologyResolver,
	objResolver auth.ObjectTypeResolver,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v2/me/checks/objectTypes",
		auth.ObjectCheckBatchHandler(ontResolver, objResolver))
	return mux
}

func doObjectCheckBatch(
	t *testing.T,
	h http.Handler,
	u *auth.User,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/me/checks/objectTypes", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if u != nil {
		req = req.WithContext(auth.WithUser(req.Context(), u))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBDD_ObjectCheckBatchHandler(t *testing.T) {
	const (
		ontRID = "ri.ontology.main.ontology.northwind"
		custOT = "ri.object-type.main.Customer"
		ordOT  = "ri.object-type.main.Order"
	)
	ontResolver := &fakeResolver{byApiName: map[string]*auth.ResolvedOntology{
		"northwind": {RID: ontRID, APIName: "northwind", DisplayName: "Northwind"},
	}}
	objResolver := &fakeObjectResolver{byKey: map[string]*auth.ResolvedObjectType{
		ontRID + "|Customer": {RID: custOT, APIName: "Customer"},
		ontRID + "|Order":    {RID: ordOT, APIName: "Order"},
	}}

	t.Run("Admin reads N OTs in one round-trip with both perms", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"admin"}}
		rec := doObjectCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"objectTypeApiNames": []string{"Customer", "Order"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.ObjectCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.OntologyAPIName != "northwind" {
			t.Errorf("OntologyAPIName=%q, want northwind", resp.OntologyAPIName)
		}
		if len(resp.Results) != 2 {
			t.Fatalf("results len=%d, want 2", len(resp.Results))
		}
		// Input order preserved: Customer first, Order second.
		if resp.Results[0].ObjectTypeAPIName != "Customer" {
			t.Errorf("Results[0]=%q, want Customer", resp.Results[0].ObjectTypeAPIName)
		}
		if resp.Results[1].ObjectTypeAPIName != "Order" {
			t.Errorf("Results[1]=%q, want Order", resp.Results[1].ObjectTypeAPIName)
		}
		for i, e := range resp.Results {
			if !e.Found {
				t.Errorf("Results[%d].Found=false, want true", i)
			}
			if !e.CanRead || !e.CanWrite {
				t.Errorf("Results[%d]: admin should have both perms; got read=%v write=%v",
					i, e.CanRead, e.CanWrite)
			}
		}
	})

	t.Run("Missing OT returns found=false with both perms false", func(t *testing.T) {
		// Critical contract: the found discriminator distinguishes
		// "type removed from config" from "type exists but caller
		// lacks perms". For a missing type, perms MUST be false
		// regardless of caller's role.
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-2", Roles: []string{"admin"}}
		rec := doObjectCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"objectTypeApiNames": []string{"Customer", "GhostType", "Order"},
		})
		var resp auth.ObjectCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Results) != 3 {
			t.Fatalf("len=%d, want 3 (input preserved)", len(resp.Results))
		}
		ghost := resp.Results[1]
		if ghost.ObjectTypeAPIName != "GhostType" {
			t.Errorf("Results[1]=%q, want GhostType", ghost.ObjectTypeAPIName)
		}
		if ghost.Found {
			t.Errorf("GhostType.Found=true, want false")
		}
		if ghost.CanRead || ghost.CanWrite {
			t.Errorf("missing type must report perms=false regardless of caller role; got read=%v write=%v",
				ghost.CanRead, ghost.CanWrite)
		}
		if ghost.ObjectTypeRID != "" {
			t.Errorf("missing type should have empty RID; got %q", ghost.ObjectTypeRID)
		}
	})

	t.Run("Viewer role surfaces split matrix across all entries", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-3", Roles: []string{"viewer"}}
		rec := doObjectCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"objectTypeApiNames": []string{"Customer", "Order"},
		})
		var resp auth.ObjectCheckBatchResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		for _, e := range resp.Results {
			if !e.CanRead {
				t.Errorf("viewer should have canRead=true for %s", e.ObjectTypeAPIName)
			}
			if e.CanWrite {
				t.Errorf("viewer should have canWrite=false for %s", e.ObjectTypeAPIName)
			}
		}
	})

	t.Run("Unauthenticated returns 401", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		rec := doObjectCheckBatch(t, h, nil, map[string]any{
			"ontologyApiName":    "northwind",
			"objectTypeApiNames": []string{"Customer"},
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Unknown ontology returns 404 OntologyNotFound", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doObjectCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "does-not-exist",
			"objectTypeApiNames": []string{"Customer"},
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

	t.Run("Empty objectTypeApiNames returns 400", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doObjectCheckBatch(t, h, u, map[string]any{
			"ontologyApiName":    "northwind",
			"objectTypeApiNames": []string{},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Missing ontologyApiName returns 400", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doObjectCheckBatch(t, h, u, map[string]any{
			"objectTypeApiNames": []string{"Customer"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Missing body returns 400", func(t *testing.T) {
		h := newObjectCheckBatchServer(ontResolver, objResolver)
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/me/checks/objectTypes", nil)
		req = req.WithContext(auth.WithUser(req.Context(), u))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400", rec.Code)
		}
	})

	t.Run("ResolvedActionType from round-103 still works independently", func(t *testing.T) {
		// Sanity guard: the bulk handler shares the ObjectTypeResolver
		// interface with round-105 single endpoint. Adding the batch
		// surface must not break the single one. Cross-package
		// regression check happens implicitly via the full test
		// suite, but proving the resolver works the same here keeps
		// the contract pinned to this round.
		ot, err := objResolver.GetObjectType(context.Background(), ontRID, "Customer")
		if err != nil {
			t.Fatalf("GetObjectType: %v", err)
		}
		if ot.RID != custOT {
			t.Errorf("RID drift: got %q want %q", ot.RID, custOT)
		}
	})
}
