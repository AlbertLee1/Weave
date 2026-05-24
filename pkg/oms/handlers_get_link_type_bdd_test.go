package oms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_GetLinkTypeByAPIName covers a 1:1 Foundry-OSv2 alignment gap:
// Weave exposes GET /api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}
// for ObjectTypes but only `byRid/{linkTypeRid}` mutation routes for
// LinkTypes — there is no per-API-name GET. Foundry SDKs hit
// /linkTypes/{linkType} (where {linkType} is the API name) to resolve a
// single link's metadata after a /search response surfaces a linkType
// api name they need to render. Without this endpoint, SDK clients have
// to fall back to ListLinkTypes + client-side filter on every call,
// blowing the cache budget and breaking response shape parity.
//
// The new endpoint:
//   - 200 + LinkType wire object on a happy hit, keyed by API name.
//   - 404 LinkTypeNotFound when the api-name is unknown (NOT 500 from
//     the underlying ErrNotFound — must be translated cleanly).
//   - shape MUST include rid / apiName / displayName / cardinality so a
//     downstream SDK can render an explorer panel from one call.
func TestBDD_GetLinkTypeByAPIName(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"
	const linkRID = "ri.ontology.main.link-type.emp-dept"

	newServer := func(t *testing.T) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		repo.linkTypes = append(repo.linkTypes, oms.LinkType{
			RID:              linkRID,
			OntologyRID:      ontRID,
			APIName:          "employeeDepartment",
			DisplayName:      "Employee Department",
			SourceObjectType: "employee",
			TargetObjectType: "department",
			Cardinality:      "MANY_TO_ONE",
		})
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/linkTypes/{linkType}", handler.GetLinkTypeByAPIName)
		return r, repo
	}

	t.Run("happy GET returns the LinkType wire object keyed by api name", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/linkTypes/employeeDepartment", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got["rid"] != linkRID {
			t.Errorf("rid: got %v, want %s", got["rid"], linkRID)
		}
		if got["apiName"] != "employeeDepartment" {
			t.Errorf("apiName: got %v, want employeeDepartment", got["apiName"])
		}
		if got["displayName"] != "Employee Department" {
			t.Errorf("displayName: got %v, want %q", got["displayName"], "Employee Department")
		}
		if got["cardinality"] != "MANY_TO_ONE" {
			t.Errorf("cardinality: got %v, want MANY_TO_ONE", got["cardinality"])
		}
	})

	t.Run("unknown api name returns 404 LinkTypeNotFound, never 500", func(t *testing.T) {
		r, _ := newServer(t)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/linkTypes/doesNotExist", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorName string `json:"errorName"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.ErrorName != "LinkTypeNotFound" {
			t.Errorf("errorName: got %q, want LinkTypeNotFound", env.ErrorName)
		}
	})

	t.Run("missing linkType path segment returns 400", func(t *testing.T) {
		// Empty segment can only happen via a misrouted URL like
		// `/.../linkTypes/`. Chi treats the trailing slash as a separate
		// route by default; bypass routing by calling the handler
		// directly with an empty url param so we cover the path-guard.
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID: ontRID, APIName: "test", DisplayName: "Test",
		})
		handler := oms.NewOMSHandler(repo)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontRID+"/linkTypes/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("ontologyApiName", ontRID)
		rctx.URLParams.Add("linkType", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		handler.GetLinkTypeByAPIName(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})
}
