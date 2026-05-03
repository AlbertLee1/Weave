package lineage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// testCtx returns a fresh background context for the US-377 handler
// tests; isolated here so the existing handler_test.go file is untouched.
func testCtx() context.Context { return context.Background() }

func newColumnLineageRouter(t *testing.T, store oms.ColumnLineageStore) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	h := NewHandler(nil)
	h.SetColumnLineageStore(store)
	h.RegisterRoutes(r)
	return r
}

func TestUS377_GetPropertyLineage_HappyPath(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	_ = store.ReplaceColumnLineageForBinding(testCtx(), "ri.b.1", []oms.ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.northwind", SrcColumn: "first_name",
			DstPropertyRID: "ri.ontology.main.property.p-first", DstObjectTypeRID: "ri.ot.cust", DstPropertyAPIName: "firstName"},
	})

	r := newColumnLineageRouter(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/lineage/property/ri.ontology.main.property.p-first", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp PropertyLineageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PropertyRID != "ri.ontology.main.property.p-first" {
		t.Errorf("PropertyRID echo wrong: %q", resp.PropertyRID)
	}
	if len(resp.Upstream) != 1 || resp.Upstream[0].SrcColumn != "first_name" {
		t.Fatalf("upstream wrong: %+v", resp.Upstream)
	}
	if resp.Truncated {
		t.Errorf("Truncated should be false on a 1-row response")
	}
}

func TestUS377_GetPropertyLineage_StoreUnwired_Returns404(t *testing.T) {
	r := newColumnLineageRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/lineage/property/ri.x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), "ColumnLineageNotConfigured") {
		t.Errorf("expected ColumnLineageNotConfigured envelope, got %s", rec.Body.String())
	}
}

func TestUS377_GetPropertyLineage_EmptyResults(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	r := newColumnLineageRouter(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/lineage/property/ri.ontology.main.property.unknown", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp PropertyLineageResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Upstream == nil {
		t.Fatalf("Upstream must be a non-nil empty slice for SDK consumers")
	}
	if len(resp.Upstream) != 0 {
		t.Fatalf("expected empty list, got %d", len(resp.Upstream))
	}
}

func TestUS377_GetDatasetColumnImpact_HappyPath(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	_ = store.ReplaceColumnLineageForBinding(testCtx(), "ri.b.A", []oms.ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.shared", SrcColumn: "email",
			DstPropertyRID: "ri.p.cust.email", DstObjectTypeRID: "ri.ot.cust", DstPropertyAPIName: "email"},
	})
	_ = store.ReplaceColumnLineageForBinding(testCtx(), "ri.b.B", []oms.ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.shared", SrcColumn: "email",
			DstPropertyRID: "ri.p.emp.email", DstObjectTypeRID: "ri.ot.emp", DstPropertyAPIName: "email"},
	})
	_ = store.ReplaceColumnLineageForBinding(testCtx(), "ri.b.C", []oms.ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.shared", SrcColumn: "phone",
			DstPropertyRID: "ri.p.cust.phone", DstObjectTypeRID: "ri.ot.cust", DstPropertyAPIName: "phone"},
	})

	r := newColumnLineageRouter(t, store)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/lineage/dataset-columns/impact?dataset=ri.ds.shared&column=email", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp DatasetColumnImpactResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Impacted) != 2 {
		t.Fatalf("expected 2 impacted properties, got %d", len(resp.Impacted))
	}
	want := map[string]bool{
		"ri.p.cust.email": true,
		"ri.p.emp.email":  true,
	}
	for _, ip := range resp.Impacted {
		if !want[ip.DstPropertyRID] {
			t.Errorf("unexpected impacted property: %+v", ip)
		}
	}
}

func TestUS377_GetDatasetColumnImpact_MissingParams(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	r := newColumnLineageRouter(t, store)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/lineage/dataset-columns/impact?dataset=&column=", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), "MissingDatasetOrColumn") {
		t.Errorf("expected MissingDatasetOrColumn envelope, got %s", rec.Body.String())
	}
}

func TestUS377_GetDatasetColumnImpact_StoreUnwired_Returns404(t *testing.T) {
	r := newColumnLineageRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/lineage/dataset-columns/impact?dataset=ri.ds&column=col", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
