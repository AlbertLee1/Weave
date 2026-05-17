package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// fakeRebuildRepo is a minimal RebuildRepo that supports the admin handler
// path: ontology + objectType lookups and a property list.
type fakeRebuildRepo struct {
	ontology       *oms.Ontology
	objectType     *oms.ObjectType
	props          []oms.Property
	getOntologyErr error
	getOTErr       error
}

func (f *fakeRebuildRepo) GetOntology(_ context.Context, _ string) (*oms.Ontology, error) {
	if f.getOntologyErr != nil {
		return nil, f.getOntologyErr
	}
	if f.ontology == nil {
		return nil, oms.ErrNotFound
	}
	return f.ontology, nil
}

func (f *fakeRebuildRepo) GetObjectTypeByAPIName(_ context.Context, _ string, _ string) (*oms.ObjectType, error) {
	if f.getOTErr != nil {
		return nil, f.getOTErr
	}
	if f.objectType == nil {
		return nil, oms.ErrNotFound
	}
	return f.objectType, nil
}

func (f *fakeRebuildRepo) ListProperties(_ context.Context, _ string) ([]oms.Property, error) {
	return f.props, nil
}

type fakeDocSource struct {
	docs []index.LatestDocument
	err  error
}

func (f *fakeDocSource) LoadLatestDocuments(_ context.Context, _ string) ([]index.LatestDocument, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.docs, nil
}

func newAdminIndexHandler(t *testing.T, repo index.RebuildRepo, src index.LatestDocumentSource) http.Handler {
	t.Helper()
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	return NewAdminIndexRebuildHandler(AdminIndexRebuildDeps{
		IndexMgr:  mgr,
		Repo:      repo,
		DocSource: src,
	})
}

func TestAdminIndexRebuild_Success(t *testing.T) {
	repo := &fakeRebuildRepo{
		ontology:   &oms.Ontology{RID: "ri.ontology.main.ontology.nw", APIName: "northwind"},
		objectType: &oms.ObjectType{RID: "ri.ontology.main.objectType.customer", OntologyRID: "ri.ontology.main.ontology.nw", APIName: "Customer", PrimaryKey: "customerId"},
		props: []oms.Property{
			{APIName: "customerId", BaseType: "string", IsSearchable: true},
		},
	}
	src := &fakeDocSource{
		docs: []index.LatestDocument{
			{PrimaryKey: "ALFKI", Body: map[string]interface{}{"customerId": "ALFKI"}},
			{PrimaryKey: "ANATR", Body: map[string]interface{}{"customerId": "ANATR"}},
		},
	}

	h := newAdminIndexHandler(t, repo, src)

	body := bytes.NewBufferString(`{"ontology":"northwind","objectType":"Customer"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["indexedCount"].(float64) != 2 {
		t.Errorf("indexedCount = %v, want 2", resp["indexedCount"])
	}
	if resp["scopedKey"] != "northwind__Customer" {
		t.Errorf("scopedKey = %v, want northwind__Customer", resp["scopedKey"])
	}
}

func TestAdminIndexRebuild_MissingParams(t *testing.T) {
	repo := &fakeRebuildRepo{}
	h := newAdminIndexHandler(t, repo, nil)

	cases := []string{
		`{}`,
		`{"ontology":"northwind"}`,
		`{"objectType":"Customer"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rw.Code)
		}
	}
}

func TestAdminIndexRebuild_InvalidJSON(t *testing.T) {
	repo := &fakeRebuildRepo{}
	h := newAdminIndexHandler(t, repo, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", bytes.NewBufferString(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestAdminIndexRebuild_OntologyNotFound(t *testing.T) {
	repo := &fakeRebuildRepo{getOntologyErr: oms.ErrNotFound}
	h := newAdminIndexHandler(t, repo, nil)

	body := `{"ontology":"ghost","objectType":"Customer"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", bytes.NewBufferString(body))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

func TestAdminIndexRebuild_ObjectTypeNotFound(t *testing.T) {
	repo := &fakeRebuildRepo{
		ontology: &oms.Ontology{RID: "ri.ontology.main.ontology.nw", APIName: "northwind"},
		getOTErr: oms.ErrNotFound,
	}
	h := newAdminIndexHandler(t, repo, nil)

	body := `{"ontology":"northwind","objectType":"Ghost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", bytes.NewBufferString(body))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

func TestAdminIndexRebuild_DocSourceError(t *testing.T) {
	repo := &fakeRebuildRepo{
		ontology:   &oms.Ontology{RID: "ri.ontology.main.ontology.nw", APIName: "northwind"},
		objectType: &oms.ObjectType{RID: "ri.ontology.main.objectType.customer", OntologyRID: "ri.ontology.main.ontology.nw", APIName: "Customer", PrimaryKey: "customerId"},
		props:      []oms.Property{{APIName: "customerId", BaseType: "string", IsSearchable: true}},
	}
	src := &fakeDocSource{err: errors.New("pg down")}
	h := newAdminIndexHandler(t, repo, src)

	body := `{"ontology":"northwind","objectType":"Customer"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", bytes.NewBufferString(body))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rw.Code)
	}
}

func TestAdminIndexRebuild_IndexMgrNotConfigured(t *testing.T) {
	h := NewAdminIndexRebuildHandler(AdminIndexRebuildDeps{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/indexes/rebuild", bytes.NewBufferString(`{"ontology":"northwind","objectType":"Customer"}`))
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rw.Code)
	}
}

// newAdminIndexReindexRouter mounts the US-461 path-style reindex handler
// behind a tiny chi router so the test can issue requests with the
// {objectType} placeholder populated.
func newAdminIndexReindexRouter(t *testing.T, repo index.RebuildRepo, src index.LatestDocumentSource) http.Handler {
	t.Helper()
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	r := chi.NewRouter()
	r.Method(http.MethodPost, "/api/admin/index/reindex/{objectType}", NewAdminIndexReindexHandler(AdminIndexRebuildDeps{
		IndexMgr:  mgr,
		Repo:      repo,
		DocSource: src,
	}))
	return r
}

// TestAdminIndexReindexPath_Success is the US-461 acceptance test for the
// path-style admin endpoint. POSTing to /api/admin/index/reindex/Customer
// with ?ontology=northwind must rebuild the Customer index and return the
// same {scopedKey, indexedCount} shape as the legacy /indexes/rebuild path.
func TestAdminIndexReindexPath_Success(t *testing.T) {
	repo := &fakeRebuildRepo{
		ontology:   &oms.Ontology{RID: "ri.ontology.main.ontology.nw", APIName: "northwind"},
		objectType: &oms.ObjectType{RID: "ri.ontology.main.objectType.customer", OntologyRID: "ri.ontology.main.ontology.nw", APIName: "Customer", PrimaryKey: "customerId"},
		props: []oms.Property{
			{APIName: "customerId", BaseType: "string", IsSearchable: true},
		},
	}
	src := &fakeDocSource{
		docs: []index.LatestDocument{
			{PrimaryKey: "ALFKI", Body: map[string]interface{}{"customerId": "ALFKI"}},
			{PrimaryKey: "ANATR", Body: map[string]interface{}{"customerId": "ANATR"}},
			{PrimaryKey: "ANTON", Body: map[string]interface{}{"customerId": "ANTON"}},
		},
	}

	r := newAdminIndexReindexRouter(t, repo, src)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/index/reindex/Customer?ontology=northwind", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["indexedCount"].(float64) != 3 {
		t.Errorf("indexedCount = %v, want 3", resp["indexedCount"])
	}
	if resp["scopedKey"] != "northwind__Customer" {
		t.Errorf("scopedKey = %v, want northwind__Customer", resp["scopedKey"])
	}
}

// TestAdminIndexReindexPath_MissingOntology covers the negative path: the
// objectType in the URL is required (chi enforces) but the ontology query
// parameter is mandatory too, since a bare ObjectType API name is ambiguous
// across multiple ontologies on the same server.
func TestAdminIndexReindexPath_MissingOntology(t *testing.T) {
	repo := &fakeRebuildRepo{}
	r := newAdminIndexReindexRouter(t, repo, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/index/reindex/Customer", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

// TestAdminIndexReindexPath_OntologyNotFound surfaces oms.ErrNotFound as a
// 404 just like the legacy JSON-body endpoint, so operators see consistent
// errors regardless of which spelling they POST to.
func TestAdminIndexReindexPath_OntologyNotFound(t *testing.T) {
	repo := &fakeRebuildRepo{getOntologyErr: oms.ErrNotFound}
	r := newAdminIndexReindexRouter(t, repo, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/index/reindex/Customer?ontology=ghost", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

// TestAdminIndexReindexPath_IndexMgrNotConfigured returns 503 when the
// server is running without a Bleve backend, mirroring the legacy handler's
// degradation contract.
func TestAdminIndexReindexPath_IndexMgrNotConfigured(t *testing.T) {
	r := chi.NewRouter()
	r.Method(http.MethodPost, "/api/admin/index/reindex/{objectType}", NewAdminIndexReindexHandler(AdminIndexRebuildDeps{}))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/index/reindex/Customer?ontology=northwind", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rw.Code)
	}
}
