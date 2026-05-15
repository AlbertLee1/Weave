package main

// US-006: Delete Weave extension routes from Foundry API surface.
//
// These tests verify that all non-Foundry routes have been removed:
//   - All /api/admin/* routes (OMS admin CRUD, search, export/import, snapshots, AI suggest)
//   - SSE /subscribe endpoint (Weave extension in OSS handler)
//   - Object history endpoint (Weave extension in OSS handler)
//   - API key management endpoints (/api/v2/admin/api-keys)
//
// Routes that MUST remain:
//   - /health, /health/live, /health/ready, /metrics (ops)
//   - /swagger/, /api/openapi.yaml (docs)
//   - /mcp (AI agent JSON-RPC, not in Foundry namespace)
//   - /api/v2/ontologies/... (Foundry-aligned read-only + action + objectset routes)
//   - /api/auth/... (auth endpoints)
//   - /api/v2/me (current user)

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// ---------- stub types (formerly in wire_pr01_test.go) ----------

// us006FakeAPIKeyRepo is a no-op APIKeyRepository for route tests.
type us006FakeAPIKeyRepo struct{}

func (us006FakeAPIKeyRepo) Create(context.Context, *auth.APIKeyRecord) error { return nil }
func (us006FakeAPIKeyRepo) GetByPrefix(context.Context, string) (*auth.APIKeyRecord, error) {
	return nil, auth.ErrAPIKeyNotFound
}
func (us006FakeAPIKeyRepo) GetByID(context.Context, string) (*auth.APIKeyRecord, error) {
	return nil, auth.ErrAPIKeyNotFound
}
func (us006FakeAPIKeyRepo) ListByUser(context.Context, string) ([]*auth.APIKeyRecord, error) {
	return nil, nil
}
func (us006FakeAPIKeyRepo) Revoke(context.Context, string) error                   { return nil }
func (us006FakeAPIKeyRepo) TouchLastUsed(context.Context, string, time.Time) error { return nil }
func (us006FakeAPIKeyRepo) Rotate(context.Context, string, *auth.APIKeyRecord, time.Time) error {
	return nil
}
func (us006FakeAPIKeyRepo) ListPendingRotations(context.Context, time.Time, time.Duration) ([]*auth.APIKeyRecord, error) {
	return nil, nil
}

// us006StubOmsRepo embeds oms.Repository; only GetObjectTypeByAPIName is
// overridden so the handler returns a clean 404.
type us006StubOmsRepo struct{ oms.Repository }

func (us006StubOmsRepo) GetObjectTypeByAPIName(_ context.Context, _, _ string) (*oms.ObjectType, error) {
	return nil, oms.ErrNotFound
}

func (us006StubOmsRepo) ListOntologies(context.Context) ([]oms.Ontology, error) {
	return []oms.Ontology{}, nil
}

func (us006StubOmsRepo) GetOntology(_ context.Context, rid string) (*oms.Ontology, error) {
	return &oms.Ontology{RID: rid, APIName: rid, DisplayName: rid}, nil
}

func (us006StubOmsRepo) ListObjectTypes(context.Context, string) ([]oms.ObjectType, error) {
	return []oms.ObjectType{}, nil
}

func (us006StubOmsRepo) ListActionTypes(context.Context, string) ([]oms.ActionType, error) {
	return []oms.ActionType{}, nil
}

// us006StubOSSService embeds oss.Service as nil interface — sufficient for
// route registration tests.
type us006StubOSSService struct{ oss.Service }

func (us006StubOSSService) ListObjects(context.Context, oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	return &oss.ObjectPage{Data: []*oss.WireObject{}}, nil
}

// TestUS006_AdminRoutesRemoved verifies that all /api/admin/* routes are gone.
// A representative sample covering every resource group is tested.
func TestUS006_AdminRoutesRemoved(t *testing.T) {
	deps := &ServerDeps{
		OmsRepo: us006StubOmsRepo{},
		OssSvc:  us006StubOSSService{},
	}
	router := NewFullRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		// Ontology CRUD
		{http.MethodPost, "/api/admin/ontologies"},
		{http.MethodPut, "/api/admin/ontologies/ri.test"},
		// ObjectType CRUD
		{http.MethodPost, "/api/admin/ontologies/northwind/objectTypes"},
		{http.MethodPut, "/api/admin/objectTypes/ri.test"},
		{http.MethodDelete, "/api/admin/objectTypes/ri.test"},
		// Property CRUD
		{http.MethodPost, "/api/admin/objectTypes/ri.test/properties"},
		{http.MethodPut, "/api/admin/properties/ri.test"},
		{http.MethodDelete, "/api/admin/properties/ri.test"},
		// LinkType CRUD
		{http.MethodPost, "/api/admin/ontologies/northwind/linkTypes"},
		{http.MethodGet, "/api/admin/ontologies/northwind/linkTypes"},
		{http.MethodPut, "/api/admin/linkTypes/ri.test"},
		{http.MethodDelete, "/api/admin/linkTypes/ri.test"},
		// ActionType CRUD + logs
		{http.MethodPost, "/api/admin/ontologies/northwind/actionTypes"},
		{http.MethodPut, "/api/admin/actionTypes/ri.test"},
		{http.MethodDelete, "/api/admin/actionTypes/ri.test"},
		{http.MethodGet, "/api/admin/actionTypes/ri.test/logs"},
		// Interface CRUD
		{http.MethodPost, "/api/admin/ontologies/northwind/interfaces"},
		{http.MethodGet, "/api/admin/ontologies/northwind/interfaces"},
		{http.MethodGet, "/api/admin/interfaces/ri.test"},
		{http.MethodPut, "/api/admin/interfaces/ri.test"},
		{http.MethodDelete, "/api/admin/interfaces/ri.test"},
		{http.MethodPost, "/api/admin/objectTypes/ri.test/interfaces"},
		{http.MethodDelete, "/api/admin/objectTypes/ri.test/interfaces/ri.iface"},
		{http.MethodGet, "/api/admin/objectTypes/ri.test/interfaces"},
		// Shared Properties
		{http.MethodPost, "/api/admin/ontologies/northwind/shared-properties"},
		{http.MethodGet, "/api/admin/ontologies/northwind/shared-properties"},
		{http.MethodGet, "/api/admin/shared-properties/ri.test"},
		{http.MethodPut, "/api/admin/shared-properties/ri.test"},
		{http.MethodDelete, "/api/admin/shared-properties/ri.test"},
		// Type Groups
		{http.MethodPost, "/api/admin/ontologies/northwind/type-groups"},
		{http.MethodGet, "/api/admin/ontologies/northwind/type-groups"},
		{http.MethodGet, "/api/admin/type-groups/ri.test"},
		{http.MethodPut, "/api/admin/type-groups/ri.test"},
		{http.MethodDelete, "/api/admin/type-groups/ri.test"},
		{http.MethodPost, "/api/admin/objectTypes/ri.test/groups/ri.tg"},
		{http.MethodDelete, "/api/admin/objectTypes/ri.test/groups/ri.tg"},
		{http.MethodGet, "/api/admin/objectTypes/ri.test/groups"},
		// Value Types
		{http.MethodPost, "/api/admin/value-types"},
		{http.MethodGet, "/api/admin/value-types"},
		{http.MethodGet, "/api/admin/value-types/ri.test"},
		{http.MethodPut, "/api/admin/value-types/ri.test"},
		{http.MethodDelete, "/api/admin/value-types/ri.test"},
		// Datasource Bindings
		{http.MethodPost, "/api/admin/objectTypes/ri.test/datasourceBindings"},
		{http.MethodGet, "/api/admin/objectTypes/ri.test/datasourceBindings"},
		{http.MethodGet, "/api/admin/datasourceBindings/ri.test"},
		{http.MethodPut, "/api/admin/datasourceBindings/ri.test"},
		{http.MethodDelete, "/api/admin/datasourceBindings/ri.test"},
		// Security Policies
		{http.MethodPost, "/api/admin/objectTypes/ri.test/securityPolicies"},
		{http.MethodGet, "/api/admin/objectTypes/ri.test/securityPolicies"},
		{http.MethodGet, "/api/admin/securityPolicies/ri.test"},
		{http.MethodPut, "/api/admin/securityPolicies/ri.test"},
		{http.MethodDelete, "/api/admin/securityPolicies/ri.test"},
		// Query Types
		{http.MethodPost, "/api/admin/ontologies/northwind/queryTypes"},
		{http.MethodGet, "/api/admin/ontologies/northwind/queryTypes"},
		{http.MethodGet, "/api/admin/queryTypes/ri.test"},
		{http.MethodPut, "/api/admin/queryTypes/ri.test"},
		{http.MethodDelete, "/api/admin/queryTypes/ri.test"},
		// Search, Export, Import
		{http.MethodGet, "/api/admin/ontologies/northwind/search"},
		{http.MethodGet, "/api/admin/ontologies/northwind/export"},
		{http.MethodPost, "/api/admin/ontologies/import"},
		// Snapshots
		{http.MethodPost, "/api/admin/ontologies/northwind/snapshots"},
		{http.MethodGet, "/api/admin/ontologies/northwind/snapshots"},
		{http.MethodGet, "/api/admin/ontologies/northwind/snapshots/1"},
		// AI suggest
		{http.MethodPost, "/api/admin/ai/suggest-properties"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("route %s %s should return 404 (removed), got %d",
					tt.method, tt.path, rec.Code)
			}
		})
	}
}

// TestUS006_SSESubscribeRemoved verifies that the SSE subscribe endpoint
// is no longer registered.
func TestUS006_SSESubscribeRemoved(t *testing.T) {
	deps := &ServerDeps{
		OssSvc: us006StubOSSService{},
	}
	router := NewFullRouter(deps)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test/subscribe", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("SSE subscribe should return 404 (removed), got %d (body=%s)",
			rec.Code, rec.Body.String())
	}
}

// TestUS006_ObjectHistoryRemoved verifies that the object history endpoint
// is no longer registered, even when OmsRepo is available.
func TestUS006_ObjectHistoryRemoved(t *testing.T) {
	deps := &ServerDeps{
		OmsRepo: us006StubOmsRepo{},
		OssSvc:  us006StubOSSService{},
	}
	router := NewFullRouter(deps)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/test/objects/Employee/123/history", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("object history should return 404 (removed), got %d (body=%s)",
			rec.Code, rec.Body.String())
	}
}

// TestUS006_APIKeyRoutesRemoved verifies that the API key management
// endpoints are no longer registered, even when APIKeyRepo is available.
func TestUS006_APIKeyRoutesRemoved(t *testing.T) {
	deps := &ServerDeps{
		APIKeyRepo: us006FakeAPIKeyRepo{},
	}
	router := NewFullRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v2/admin/api-keys"},
		{http.MethodGet, "/api/v2/admin/api-keys"},
		{http.MethodDelete, "/api/v2/admin/api-keys/test-id"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"name":"test"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("route %s %s should return 404 (removed), got %d",
					tt.method, tt.path, rec.Code)
			}
		})
	}
}

// TestUS006_FoundryRoutesPreserved verifies that Foundry-aligned V2 routes
// and ops routes are still available after the removal.
func TestUS006_FoundryRoutesPreserved(t *testing.T) {
	deps := &ServerDeps{
		OmsRepo: us006StubOmsRepo{},
		OssSvc:  us006StubOSSService{},
	}
	router := NewFullRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		// V2 read routes
		{http.MethodGet, "/api/v2/ontologies"},
		{http.MethodGet, "/api/v2/ontologies/northwind"},
		{http.MethodGet, "/api/v2/ontologies/northwind/objectTypes"},
		{http.MethodGet, "/api/v2/ontologies/northwind/actionTypes"},
		// OSS routes
		{http.MethodGet, "/api/v2/ontologies/northwind/objects/Employee"},
		{http.MethodPost, "/api/v2/ontologies/northwind/objects/Employee/search"},
		// Ops routes
		{http.MethodGet, "/health"},
		{http.MethodGet, "/health/live"},
		{http.MethodGet, "/health/ready"},
		{http.MethodGet, "/metrics"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// A non-404 response means the route is registered.
			if rec.Code == http.StatusNotFound {
				t.Errorf("Foundry route %s %s should still exist, got 404",
					tt.method, tt.path)
			}
		})
	}
}

// TestUS006_OSSHandler_NoSubscribeOrHistoryRoutes verifies at the handler
// level that RegisterRoutes no longer registers subscribe or history.
func TestUS006_OSSHandler_NoSubscribeOrHistoryRoutes(t *testing.T) {
	handler := oss.NewHandler(us006StubOSSService{})

	// Use a dedicated chi router to inspect registered routes.
	r := newTestChiRouter()
	handler.RegisterRoutes(r)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"subscribe removed", http.MethodGet, "/api/v2/ontologies/test/subscribe"},
		{"history removed", http.MethodGet, "/api/v2/ontologies/test/objects/Employee/123/history"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: expected 404, got %d (body=%s)",
					tt.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// newTestChiRouter creates a bare chi.Mux for route-presence testing.
func newTestChiRouter() *chi.Mux {
	return chi.NewRouter()
}
