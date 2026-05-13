package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestUS052_DatasourceBinding_Admin_CRUD_V2 exercises the V2-mounted admin
// surface for DatasourceBinding management — the deleted
// /api/admin/objectTypes/{rid}/datasourceBindings + /api/admin/datasourceBindings/{rid}
// routes were re-mounted under /api/v2/ontologies/{ontologyApiName}/... in
// US-052 (PC-A06). The six sub-tests lock the per-route shape contract the
// frontend Bindings tab in ObjectTypeAdminPage relies on.
func TestUS052_DatasourceBinding_Admin_CRUD_V2(t *testing.T) {
	t.Run("CreateDatasourceBinding_V2_route_returns_201_with_columnMapping", func(t *testing.T) {
		repo := &mockRepo{}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/datasourceBindings", handler.CreateDatasourceBinding)

		body := []byte(`{"datasetRid":"ri.dataset.main.dataset.northwind-customers","branch":"main","columnMapping":{"customerId":"customer_id","companyName":"company_name"},"isPrimary":true}`)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/objectTypes/byRid/ri.ontology.main.object-type.customer/datasourceBindings",
			bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var got oms.DatasourceBinding
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if got.DatasetRID != "ri.dataset.main.dataset.northwind-customers" {
			t.Errorf("unexpected datasetRid: %q", got.DatasetRID)
		}
		if got.Branch != "main" {
			t.Errorf("unexpected branch: %q", got.Branch)
		}
		if !got.IsPrimary {
			t.Errorf("expected isPrimary=true, got false")
		}
		if string(got.ColumnMapping) == "" || string(got.ColumnMapping) == "{}" {
			t.Errorf("expected columnMapping to round-trip, got %q", string(got.ColumnMapping))
		}
		if len(repo.datasourceBindings) != 1 {
			t.Errorf("expected exactly one binding persisted, got %d", len(repo.datasourceBindings))
		}
	})

	t.Run("CreateDatasourceBinding_V2_route_400_when_datasetRid_missing", func(t *testing.T) {
		repo := &mockRepo{}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/datasourceBindings", handler.CreateDatasourceBinding)

		body := []byte(`{"branch":"main","columnMapping":{},"isPrimary":false}`)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/northwind/objectTypes/byRid/ri.ontology.main.object-type.customer/datasourceBindings",
			bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListDatasourceBindings_V2_route_returns_data_envelope_scoped_by_objectType", func(t *testing.T) {
		repo := &mockRepo{
			datasourceBindings: []oms.DatasourceBinding{
				{RID: "ri.ontology.main.datasource-binding.a", ObjectTypeRID: "ri.ot.customer", DatasetRID: "ri.dataset.main.dataset.customers", Branch: "main", ColumnMapping: json.RawMessage(`{"id":"customer_id"}`), IsPrimary: true},
				{RID: "ri.ontology.main.datasource-binding.b", ObjectTypeRID: "ri.ot.customer", DatasetRID: "ri.dataset.main.dataset.customers-mirror", Branch: "preview", ColumnMapping: json.RawMessage(`{}`), IsPrimary: false},
				{RID: "ri.ontology.main.datasource-binding.c", ObjectTypeRID: "ri.ot.order", DatasetRID: "ri.dataset.main.dataset.orders", Branch: "main", ColumnMapping: json.RawMessage(`{}`), IsPrimary: true},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/datasourceBindings", handler.ListDatasourceBindings)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/northwind/objectTypes/byRid/ri.ot.customer/datasourceBindings", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var envelope struct {
			Data []oms.DatasourceBinding `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(envelope.Data) != 2 {
			t.Errorf("expected 2 bindings scoped to customer, got %d", len(envelope.Data))
		}
		// Order-independent: both customer bindings must be present.
		// NOTE: ObjectTypeRID is `json:"-"` on the wire (the URL already
		// scopes the list), so we cannot read it back from the response —
		// the scoping check is enforced by repo state and the URL path.
		seen := map[string]bool{}
		for _, b := range envelope.Data {
			seen[b.RID] = true
		}
		if !seen["ri.ontology.main.datasource-binding.a"] || !seen["ri.ontology.main.datasource-binding.b"] {
			t.Errorf("expected both customer bindings, got: %+v", envelope.Data)
		}
		if seen["ri.ontology.main.datasource-binding.c"] {
			t.Errorf("expected order binding NOT in customer list, got: %+v", envelope.Data)
		}
	})

	t.Run("GetDatasourceBinding_V2_route_returns_404_when_unknown", func(t *testing.T) {
		repo := &mockRepo{}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Get("/api/v2/ontologies/{ontologyApiName}/datasourceBindings/byRid/{datasourceBindingRid}", handler.GetDatasourceBinding)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/northwind/datasourceBindings/byRid/ri.ontology.main.datasource-binding.does-not-exist", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("UpdateDatasourceBinding_V2_route_persists_new_columnMapping_and_isPrimary_toggle", func(t *testing.T) {
		repo := &mockRepo{
			datasourceBindings: []oms.DatasourceBinding{
				{RID: "ri.test.binding.1", ObjectTypeRID: "ri.ot.customer", DatasetRID: "ri.dataset.main.dataset.customers", Branch: "main", ColumnMapping: json.RawMessage(`{"id":"customer_id"}`), IsPrimary: true},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Put("/api/v2/ontologies/{ontologyApiName}/datasourceBindings/byRid/{datasourceBindingRid}", handler.UpdateDatasourceBinding)

		body := []byte(`{"datasetRid":"ri.dataset.main.dataset.customers-v2","branch":"preview","columnMapping":{"customerId":"id","name":"full_name","city":"city_name"},"isPrimary":false}`)
		req := httptest.NewRequest(http.MethodPut,
			"/api/v2/ontologies/northwind/datasourceBindings/byRid/ri.test.binding.1",
			bytes.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var got oms.DatasourceBinding
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if got.DatasetRID != "ri.dataset.main.dataset.customers-v2" {
			t.Errorf("expected datasetRid updated, got %q", got.DatasetRID)
		}
		if got.Branch != "preview" {
			t.Errorf("expected branch=preview, got %q", got.Branch)
		}
		if got.IsPrimary {
			t.Errorf("expected isPrimary toggled to false, still true")
		}
		// Verify persistence via repo state.
		if len(repo.datasourceBindings) != 1 {
			t.Fatalf("expected exactly one binding after update, got %d", len(repo.datasourceBindings))
		}
		stored := repo.datasourceBindings[0]
		if stored.DatasetRID != "ri.dataset.main.dataset.customers-v2" {
			t.Errorf("stored binding datasetRid not updated: %q", stored.DatasetRID)
		}
		var mapped map[string]string
		if err := json.Unmarshal(stored.ColumnMapping, &mapped); err != nil {
			t.Fatalf("stored columnMapping not valid JSON: %v", err)
		}
		if mapped["customerId"] != "id" || mapped["name"] != "full_name" || mapped["city"] != "city_name" {
			t.Errorf("stored columnMapping unexpected: %v", mapped)
		}
	})

	t.Run("DeleteDatasourceBinding_V2_route_returns_204_and_removes_row", func(t *testing.T) {
		repo := &mockRepo{
			datasourceBindings: []oms.DatasourceBinding{
				{RID: "ri.test.binding.delme", ObjectTypeRID: "ri.ot.customer", DatasetRID: "ri.dataset.main.dataset.scratch", Branch: "main", ColumnMapping: json.RawMessage(`{}`), IsPrimary: false},
			},
		}
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Delete("/api/v2/ontologies/{ontologyApiName}/datasourceBindings/byRid/{datasourceBindingRid}", handler.DeleteDatasourceBinding)

		req := httptest.NewRequest(http.MethodDelete,
			"/api/v2/ontologies/northwind/datasourceBindings/byRid/ri.test.binding.delme", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if len(repo.datasourceBindings) != 0 {
			t.Errorf("expected binding row removed, still present: %d", len(repo.datasourceBindings))
		}
	})
}
