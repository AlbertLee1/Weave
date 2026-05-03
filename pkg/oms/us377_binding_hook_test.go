package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// us377BindingRepo wraps mockRepo with a map-backed DatasourceBinding
// store so the Update / Delete handlers can resolve a previously-created
// binding by RID. The base mockRepo's binding methods are stubs that
// don't persist anything, which would otherwise yield a 404 on the
// Update path.
type us377BindingRepo struct {
	*mockRepo
	bindings map[string]oms.DatasourceBinding
}

func (r *us377BindingRepo) CreateDatasourceBinding(_ context.Context, b *oms.DatasourceBinding) error {
	if r.bindings == nil {
		r.bindings = map[string]oms.DatasourceBinding{}
	}
	r.bindings[b.RID] = *b
	return nil
}

func (r *us377BindingRepo) GetDatasourceBinding(_ context.Context, ridStr string) (*oms.DatasourceBinding, error) {
	if b, ok := r.bindings[ridStr]; ok {
		return &b, nil
	}
	return nil, oms.ErrNotFound
}

func (r *us377BindingRepo) UpdateDatasourceBinding(_ context.Context, b *oms.DatasourceBinding) error {
	if _, ok := r.bindings[b.RID]; !ok {
		return oms.ErrNotFound
	}
	r.bindings[b.RID] = *b
	return nil
}

func (r *us377BindingRepo) DeleteDatasourceBinding(_ context.Context, ridStr string) error {
	if _, ok := r.bindings[ridStr]; !ok {
		return oms.ErrNotFound
	}
	delete(r.bindings, ridStr)
	return nil
}

// US-377: integration test for the datasource-binding write-path hook.
// Asserts that Create / Update / Delete on a binding propagates to the
// ColumnLineageStore so the property-level read endpoints serve fresh
// edges synchronously.

func setupUS377Handler(t *testing.T, store *oms.MemoryColumnLineageStore) (*oms.OMSHandler, *us377BindingRepo, string, []oms.Property) {
	t.Helper()
	base := &mockRepo{}
	objectTypeRID := "ri.ontology.main.object-type.cust"
	properties := []oms.Property{
		{RID: "ri.ontology.main.property.p-name", APIName: "name", ObjectTypeRID: objectTypeRID, BaseType: "string"},
		{RID: "ri.ontology.main.property.p-email", APIName: "email", ObjectTypeRID: objectTypeRID, BaseType: "string"},
	}
	base.properties = append(base.properties, properties...)
	repo := &us377BindingRepo{mockRepo: base, bindings: map[string]oms.DatasourceBinding{}}
	h := oms.NewOMSHandler(repo)
	if store != nil {
		h.SetColumnLineageStore(store)
	}
	return h, repo, objectTypeRID, properties
}

func mountBindingRouter(h *oms.OMSHandler) chi.Router {
	r := chi.NewRouter()
	// Mirror the legacy admin routes purely so this test exercises the
	// HTTP→handler path. The production server no longer registers
	// these routes (they were stripped in US-006), but the handler
	// methods are still the canonical write surface for the binding.
	r.Post("/api/admin/objectTypes/{objectTypeRid}/datasourceBindings", h.CreateDatasourceBinding)
	r.Put("/api/admin/datasourceBindings/{datasourceBindingRid}", h.UpdateDatasourceBinding)
	r.Delete("/api/admin/datasourceBindings/{datasourceBindingRid}", h.DeleteDatasourceBinding)
	return r
}

func TestUS377_BindingHook_CreateProducesColumnEdges(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	h, _, objectTypeRID, props := setupUS377Handler(t, store)
	r := mountBindingRouter(h)

	body, _ := json.Marshal(map[string]any{
		"datasetRid": "ri.datasets.main.dataset.northwind",
		"branch":     "main",
		"columnMapping": map[string]string{
			"name":  "customer_name",
			"email": "customer_email",
		},
		"isPrimary": true,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/admin/objectTypes/"+objectTypeRID+"/datasourceBindings",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Both properties should now have an upstream edge.
	for _, p := range props {
		got, err := store.ListUpstreamColumnLineageForProperty(context.Background(), p.RID, 0)
		if err != nil {
			t.Fatalf("list upstream for %s: %v", p.APIName, err)
		}
		if len(got) != 1 {
			t.Fatalf("property %s: expected 1 upstream edge, got %d", p.APIName, len(got))
		}
	}
}

func TestUS377_BindingHook_UpdateReplacesEdges(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	h, _, objectTypeRID, _ := setupUS377Handler(t, store)
	r := mountBindingRouter(h)

	// Create the initial binding
	body, _ := json.Marshal(map[string]any{
		"datasetRid":    "ri.datasets.main.dataset.v1",
		"columnMapping": map[string]string{"name": "old_name", "email": "old_email"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/admin/objectTypes/"+objectTypeRID+"/datasourceBindings",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created oms.DatasourceBinding
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Sanity: the 'name' property is sourced from old_name in v1.
	got, _ := store.ListUpstreamColumnLineageForProperty(context.Background(), "ri.ontology.main.property.p-name", 0)
	if len(got) != 1 || got[0].SrcColumn != "old_name" {
		t.Fatalf("v1 edge wrong: %+v", got)
	}

	// Update the binding — different mapping shape.
	updated, _ := json.Marshal(map[string]any{
		"datasetRid":    "ri.datasets.main.dataset.v2",
		"columnMapping": map[string]string{"name": "new_name"}, // email dropped
		"isPrimary":     true,
	})
	updReq := httptest.NewRequest(http.MethodPut,
		"/api/admin/datasourceBindings/"+created.RID,
		bytes.NewReader(updated))
	updReq.Header.Set("Content-Type", "application/json")
	updRec := httptest.NewRecorder()
	r.ServeHTTP(updRec, updReq)
	if updRec.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", updRec.Code, updRec.Body.String())
	}

	// 'name' now points at new_name in dataset v2.
	got, _ = store.ListUpstreamColumnLineageForProperty(context.Background(), "ri.ontology.main.property.p-name", 0)
	if len(got) != 1 || got[0].SrcColumn != "new_name" || got[0].SrcDatasetRID != "ri.datasets.main.dataset.v2" {
		t.Fatalf("v2 edge wrong: %+v", got)
	}
	// 'email' was dropped from the mapping → no surviving edges.
	got, _ = store.ListUpstreamColumnLineageForProperty(context.Background(), "ri.ontology.main.property.p-email", 0)
	if len(got) != 0 {
		t.Fatalf("expected zero edges for 'email' after replace, got %d: %+v", len(got), got)
	}
}

func TestUS377_BindingHook_DeleteCascadesEdges(t *testing.T) {
	store := oms.NewMemoryColumnLineageStore()
	h, _, objectTypeRID, _ := setupUS377Handler(t, store)
	r := mountBindingRouter(h)

	body, _ := json.Marshal(map[string]any{
		"datasetRid":    "ri.datasets.main.dataset.x",
		"columnMapping": map[string]string{"name": "n", "email": "e"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/admin/objectTypes/"+objectTypeRID+"/datasourceBindings",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", rec.Code, rec.Body.String())
	}
	var created oms.DatasourceBinding
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete,
		"/api/admin/datasourceBindings/"+created.RID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d body=%s", delRec.Code, delRec.Body.String())
	}

	// Both properties should now have zero edges.
	got, _ := store.ListUpstreamColumnLineageForProperty(context.Background(), "ri.ontology.main.property.p-name", 0)
	if len(got) != 0 {
		t.Fatalf("expected zero edges for 'name' after delete, got %d", len(got))
	}
	got, _ = store.ListUpstreamColumnLineageForProperty(context.Background(), "ri.ontology.main.property.p-email", 0)
	if len(got) != 0 {
		t.Fatalf("expected zero edges for 'email' after delete, got %d", len(got))
	}
}

func TestUS377_BindingHook_StoreUnwiredIsHarmless(t *testing.T) {
	// No store — Create must still succeed (degraded mode).
	h, _, objectTypeRID, _ := setupUS377Handler(t, nil)
	r := mountBindingRouter(h)
	body, _ := json.Marshal(map[string]any{
		"datasetRid":    "ri.datasets.main.dataset.x",
		"columnMapping": map[string]string{"name": "n"},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/admin/objectTypes/"+objectTypeRID+"/datasourceBindings",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 in degraded mode, got %d body=%s", rec.Code, rec.Body.String())
	}
}
