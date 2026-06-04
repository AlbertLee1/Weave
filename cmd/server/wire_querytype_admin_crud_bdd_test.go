package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// qtStubOmsRepo embeds oms.Repository so only the few methods the QueryType
// admin handlers touch need real behaviour. GetOntology resolves any
// apiName/RID so resolveOntologyRID succeeds; the QueryType store is an
// in-memory slice exercised through the production router.
type qtStubOmsRepo struct {
	oms.Repository
	queryTypes []oms.QueryType
}

func (r *qtStubOmsRepo) GetOntology(_ context.Context, ridOrAPIName string) (*oms.Ontology, error) {
	if ridOrAPIName == "does-not-exist" {
		return nil, oms.ErrNotFound
	}
	return &oms.Ontology{RID: "ri.ontology.main.ontology.nw", APIName: ridOrAPIName, DisplayName: ridOrAPIName}, nil
}

func (r *qtStubOmsRepo) CreateQueryType(_ context.Context, qt *oms.QueryType) error {
	r.queryTypes = append(r.queryTypes, *qt)
	return nil
}

func (r *qtStubOmsRepo) GetQueryType(_ context.Context, rid string) (*oms.QueryType, error) {
	for i := range r.queryTypes {
		if r.queryTypes[i].RID == rid {
			return &r.queryTypes[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (r *qtStubOmsRepo) UpdateQueryType(_ context.Context, qt *oms.QueryType) error {
	for i := range r.queryTypes {
		if r.queryTypes[i].RID == qt.RID {
			r.queryTypes[i] = *qt
			return nil
		}
	}
	return oms.ErrNotFound
}

func (r *qtStubOmsRepo) DeleteQueryType(_ context.Context, rid string) error {
	for i := range r.queryTypes {
		if r.queryTypes[i].RID == rid {
			r.queryTypes = append(r.queryTypes[:i], r.queryTypes[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

// TestBDD_QueryTypeAdminCRUDRoutesWired drives the FULL production router
// (NewFullRouter) end-to-end. It is RED until cmd/server/routes.go mounts the
// QueryType admin-CRUD routes in the ActionType byRid style. The READ routes
// (GET queryTypes / GET queryTypes/{apiName}) were already mounted; only
// Create / Update / Delete were missing.
//
//	Given the production router built by NewFullRouter
//	When  a client POSTs a new QueryType to the V2 ontology surface
//	Then  it is created (201) and retrievable + updatable + deletable via
//	      the byRid routes — proving all four routes are registered.
func TestBDD_QueryTypeAdminCRUDRoutesWired(t *testing.T) {
	repo := &qtStubOmsRepo{}
	deps := &ServerDeps{
		OmsRepo: repo,
		OssSvc:  us006StubOSSService{},
	}
	router := NewFullRouter(deps)

	do := func(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	base := "/api/v2/ontologies/northwind/queryTypes"

	// CREATE -> 201
	rec := do(t, http.MethodPost, base,
		`{"apiName":"topCustomers","displayName":"Top Customers","description":"d","parameters":[],"output":{},"query":{}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("CREATE: status=%d, want 201 (route must be mounted); body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.queryTypes) != 1 {
		t.Fatalf("CREATE: repo has %d query types, want 1", len(repo.queryTypes))
	}
	rid := repo.queryTypes[0].RID

	// UPDATE via byRid -> 200
	rec = do(t, http.MethodPut, base+"/byRid/"+rid,
		`{"displayName":"Top Customers v2","status":"ACTIVE"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("UPDATE: status=%d, want 200 (route must be mounted); body=%s", rec.Code, rec.Body.String())
	}
	if repo.queryTypes[0].DisplayName != "Top Customers v2" {
		t.Errorf("UPDATE: displayName=%q, want 'Top Customers v2'", repo.queryTypes[0].DisplayName)
	}

	// DELETE via byRid -> 204
	rec = do(t, http.MethodDelete, base+"/byRid/"+rid, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status=%d, want 204 (route must be mounted); body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.queryTypes) != 0 {
		t.Errorf("DELETE: repo has %d query types, want 0", len(repo.queryTypes))
	}
}
