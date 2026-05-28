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

// TestBDD_ObjectCheckHandler covers round 105 — Foundry-parity
// object-type applicability probe. Sibling of round-103
// ActionCheckHandler; returns two booleans (canRead + canWrite)
// so the SPA can gate per-object-type UI affordances in one call.
//
// Endpoint: GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check
// Response: {ontologyApiName, objectTypeApiName, objectTypeRid, canRead, canWrite}
//
// Distinct from round-103 ActionCheckHandler: returns the
// two-axis read/write matrix because object UI gates split that
// way (read = show-the-row, write = show-the-pencil).
//
// Scenarios:
//   - Admin role: canRead=true + canWrite=true
//   - Viewer role: canRead=true + canWrite=false
//   - No-role user: canRead=false + canWrite=false (still 200)
//   - Scoped ontology-owner grants both
//   - Other-ontology scoped role does NOT leak
//   - 401 unauthenticated
//   - 404 OntologyNotFound
//   - 404 ObjectTypeNotFound
//   - Response carries all 5 required fields

type fakeObjectResolver struct {
	byKey map[string]*auth.ResolvedObjectType
}

func (f *fakeObjectResolver) GetObjectType(_ context.Context, ontologyRID, apiName string) (*auth.ResolvedObjectType, error) {
	if ot, ok := f.byKey[ontologyRID+"|"+apiName]; ok {
		return ot, nil
	}
	return nil, auth.ErrObjectTypeNotFound
}

func TestBDD_ObjectCheckHandler(t *testing.T) {
	const (
		ontRID = "ri.ontology.main.ontology.northwind"
		otRID  = "ri.object-type.main.Customer"
	)
	ontResolver := &fakeResolver{byAPIName: map[string]*auth.ResolvedOntology{
		"northwind": {RID: ontRID, APIName: "northwind", DisplayName: "Northwind"},
	}}
	objResolver := &fakeObjectResolver{byKey: map[string]*auth.ResolvedObjectType{
		ontRID + "|Customer": {RID: otRID, APIName: "Customer"},
	}}

	newServer := func() *chi.Mux {
		r := chi.NewRouter()
		r.Method(http.MethodGet,
			"/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/check",
			auth.ObjectCheckHandler(ontResolver, objResolver))
		return r
	}

	doGet := func(t *testing.T, r *chi.Mux, ontApi, otApi string, u *auth.User) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontApi+"/objectTypes/"+otApi+"/check", nil)
		if u != nil {
			req = req.WithContext(auth.WithUser(req.Context(), u))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Admin role returns canRead+canWrite both true", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"admin"}}
		rec := doGet(t, r, "northwind", "Customer", u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp auth.ObjectCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanRead {
			t.Errorf("CanRead=false, want true for admin")
		}
		if !resp.CanWrite {
			t.Errorf("CanWrite=false, want true for admin")
		}
	})

	t.Run("Viewer role returns canRead=true canWrite=false", func(t *testing.T) {
		// The two-axis split is the whole point of this endpoint vs
		// round-97's flat permission probe — assert the split actually
		// distinguishes read from write.
		r := newServer()
		u := &auth.User{ID: "u-2", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", "Customer", u)
		var resp auth.ObjectCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanRead {
			t.Errorf("CanRead=false, want true for viewer")
		}
		if resp.CanWrite {
			t.Errorf("CanWrite=true, want false for viewer")
		}
	})

	t.Run("No-role user returns both false (still 200)", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-3"}
		rec := doGet(t, r, "northwind", "Customer", u)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200 (probe is informational)", rec.Code)
		}
		var resp auth.ObjectCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.CanRead || resp.CanWrite {
			t.Errorf("no-role user should not grant either; got read=%v write=%v",
				resp.CanRead, resp.CanWrite)
		}
	})

	t.Run("Scoped ontology-owner grants both", func(t *testing.T) {
		r := newServer()
		u := &auth.User{
			ID:            "u-4",
			OntologyRoles: map[string]string{ontRID: "ontology-owner"},
		}
		rec := doGet(t, r, "northwind", "Customer", u)
		var resp auth.ObjectCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if !resp.CanRead || !resp.CanWrite {
			t.Errorf("ontology-owner on northwind should grant both; got read=%v write=%v",
				resp.CanRead, resp.CanWrite)
		}
	})

	t.Run("Other-ontology scoped role does NOT leak", func(t *testing.T) {
		// Mirror of round-97/103 cross-ontology leak guard.
		r := newServer()
		u := &auth.User{
			ID: "u-5",
			OntologyRoles: map[string]string{
				"ri.ontology.main.ontology.other": "ontology-owner",
			},
		}
		rec := doGet(t, r, "northwind", "Customer", u)
		var resp auth.ObjectCheckResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.CanRead || resp.CanWrite {
			t.Errorf("other-ontology owner must NOT leak; got read=%v write=%v",
				resp.CanRead, resp.CanWrite)
		}
	})

	t.Run("Unauthenticated returns 401", func(t *testing.T) {
		r := newServer()
		rec := doGet(t, r, "northwind", "Customer", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Unknown ontology returns 404 OntologyNotFound", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "does-not-exist", "Customer", u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "OntologyNotFound" {
			t.Errorf("errorName=%v, want OntologyNotFound", body["errorName"])
		}
	})

	t.Run("Unknown object type returns 404 ObjectTypeNotFound", func(t *testing.T) {
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"viewer"}}
		rec := doGet(t, r, "northwind", "GhostType", u)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "ObjectTypeNotFound" {
			t.Errorf("errorName=%v, want ObjectTypeNotFound", body["errorName"])
		}
	})

	t.Run("Response carries all 5 required fields", func(t *testing.T) {
		// Wire-format regression guard — same shape the SDK will
		// parse in round 106. Dropping any field would break per-row
		// gating without a server-side error.
		r := newServer()
		u := &auth.User{ID: "u-1", Roles: []string{"admin"}}
		rec := doGet(t, r, "northwind", "Customer", u)
		var raw map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		for _, field := range []string{
			"ontologyApiName", "objectTypeApiName", "objectTypeRid",
			"canRead", "canWrite",
		} {
			if _, ok := raw[field]; !ok {
				t.Errorf("response missing required field %q; body=%s",
					field, rec.Body.String())
			}
		}
	})
}
