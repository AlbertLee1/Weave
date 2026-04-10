package objectset_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// setupHandlerTest creates a Handler with a seeded index and store for HTTP-level tests.
func setupHandlerTest(t *testing.T) (*objectset.Handler, *objectset.Store, *index.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"e1", map[string]interface{}{"id": "e1", "name": "alice"}},
		{"e2", map[string]interface{}{"id": "e2", "name": "bob"}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}

	linkResolver := &mockLinkResolverWithType{
		results:    map[string][]string{"employeeDept": {"d1", "d2"}},
		targetType: map[string]string{"employeeDept": "department"},
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, linkResolver, store)
	handler := objectset.NewHandler(executor, mgr, store)
	return handler, store, mgr
}

// --- GET /api/v2/ontologies/{o}/objectSets/{objectSetRid} ---

func TestGetObjectSet_Found(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)

	// Store a temporary ObjectSet definition
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	rid := store.Put(def)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}", handler.GetObjectSet)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/myOntology/objectSets/"+rid, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should return the ObjectSet definition
	if resp["type"] != "base" {
		t.Errorf("expected type=base, got %v", resp["type"])
	}
	if resp["objectType"] != "employee" {
		t.Errorf("expected objectType=employee, got %v", resp["objectType"])
	}
}

func TestGetObjectSet_NotFound(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}", handler.GetObjectSet)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/myOntology/objectSets/nonexistent-rid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetObjectSet_ComplexDefinition(t *testing.T) {
	handler, store, _ := setupHandlerTest(t)

	// Store a filter definition (more complex)
	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Where:     json.RawMessage(`{"type":"eq","field":"name","value":"alice"}`),
	}
	rid := store.Put(def)

	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}", handler.GetObjectSet)

	req := httptest.NewRequest("GET", "/api/v2/ontologies/myOntology/objectSets/"+rid, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["type"] != "filter" {
		t.Errorf("expected type=filter, got %v", resp["type"])
	}
	nested, ok := resp["objectSet"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested objectSet map, got %T", resp["objectSet"])
	}
	if nested["type"] != "base" {
		t.Errorf("expected nested type=base, got %v", nested["type"])
	}
}

// --- POST /api/v2/ontologies/{o}/objectSets/loadLinks ---

func TestLoadLinks_Success(t *testing.T) {
	handler, _, mgr := setupHandlerTest(t)

	// Seed a "department" index so link targets can be loaded
	deptProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{"id": "d1", "deptName": "Engineering"}); err != nil {
		t.Fatalf("IndexDocument d1: %v", err)
	}
	if err := mgr.IndexDocument("department", "d2", map[string]interface{}{"id": "d2", "deptName": "Sales"}); err != nil {
		t.Fatalf("IndexDocument d2: %v", err)
	}

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"linkTypeApiName": "employeeDept",
		"select":          []string{"id", "deptName"},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", handler.LoadLinks)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadLinks", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 linked objects, got %d", len(data))
	}
}

func TestLoadLinks_MissingObjectSet(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"linkTypeApiName": "employeeDept",
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", handler.LoadLinks)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadLinks", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadLinks_MissingLinkType(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", handler.LoadLinks)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadLinks", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadLinks_InvalidBody(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", handler.LoadLinks)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadLinks", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoadLinks_WithPagination(t *testing.T) {
	handler, _, mgr := setupHandlerTest(t)

	// Seed department index
	deptProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "deptName", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("department", deptProps); err != nil {
		t.Fatalf("EnsureIndex department: %v", err)
	}
	if err := mgr.IndexDocument("department", "d1", map[string]interface{}{"id": "d1", "deptName": "Engineering"}); err != nil {
		t.Fatalf("IndexDocument d1: %v", err)
	}
	if err := mgr.IndexDocument("department", "d2", map[string]interface{}{"id": "d2", "deptName": "Sales"}); err != nil {
		t.Fatalf("IndexDocument d2: %v", err)
	}

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"linkTypeApiName": "employeeDept",
		"pageSize":        1,
	}
	bodyBytes, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", handler.LoadLinks)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadLinks", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data array, got %T", resp["data"])
	}
	if len(data) != 1 {
		t.Errorf("expected 1 object (pageSize=1), got %d", len(data))
	}

	// Should have a next page token since there are 2 results total
	if _, ok := resp["nextPageToken"]; !ok {
		t.Error("expected nextPageToken for paginated result")
	}
}
