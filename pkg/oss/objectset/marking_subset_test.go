package objectset_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// staticMarkingFilter is a test double for objectset.MarkingFilterProvider. It
// uses the REAL auth.EvaluateMarkings subset semantics so the test exercises
// the same AND/subset rule the production adapter applies.
type staticMarkingFilter struct {
	enabled bool
	user    []string
}

func (m *staticMarkingFilter) MarkingsEnabled(_ context.Context, _ string) bool { return m.enabled }

func (m *staticMarkingFilter) FilterByMarkings(_ context.Context, _ string, objs []*oss.WireObject) []*oss.WireObject {
	if !m.enabled {
		return objs
	}
	out := make([]*oss.WireObject, 0, len(objs))
	for _, o := range objs {
		var om []string
		if o.Properties != nil {
			om = markingStrings(o.Properties["_markings"])
		}
		if auth.EvaluateMarkings(m.user, om) {
			out = append(out, o)
		}
	}
	return out
}

func markingStrings(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, it := range v {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// setupMarkingHandlerTest builds an objectset handler over an "employee" index
// that carries a multi-valued _markings field: e1 is marked {A}, e2 is marked
// {A,B}.
func setupMarkingHandlerTest(t *testing.T) (*objectset.Handler, *index.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "_markings", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"e1", map[string]interface{}{"id": "e1", "name": "alice", "_markings": []interface{}{"A"}}},
		{"e2", map[string]interface{}{"id": "e2", "name": "bob", "_markings": []interface{}{"A", "B"}}},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d.id, d.doc); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.id, err)
		}
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, &mockLinkResolverWithType{}, store)
	handler := objectset.NewHandler(executor, mgr, store)
	return handler, mgr
}

func loadEmployeeIDs(t *testing.T, handler *objectset.Handler, body string) []string {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/myOntology/objectSets/loadObjects",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
	}
	ids := make([]string, 0, len(resp.Data))
	for _, row := range resp.Data {
		if pk, ok := row["__primaryKey"].(string); ok {
			ids = append(ids, pk)
		}
	}
	return ids
}

// TestBDD_LoadObjects_MarkingSubsetFilter is the mandatory-marking subset
// contract for the objectSets/loadObjects path.
//
//	Given an employee index where e1 is marked {A} and e2 is marked {A,B}
//	  and a caller holding only marking {A}
//	When the client loadObjects (default select)
//	Then only e1 is returned — e2's {A,B} is NOT a subset of {A}, so the
//	  multi-valued marking is enforced (the executor's overlap query alone
//	  would have leaked e2 because it shares A).
func TestBDD_LoadObjects_MarkingSubsetFilter(t *testing.T) {
	handler, _ := setupMarkingHandlerTest(t)
	handler.SetMarkingFilterProvider(&staticMarkingFilter{enabled: true, user: []string{"A"}})

	ids := loadEmployeeIDs(t, handler, `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`)
	if len(ids) != 1 || ids[0] != "e1" {
		t.Fatalf("load = %v, want [e1] (e2 {A,B} not subset of {A})", ids)
	}
}

// TestBDD_LoadObjects_MarkingSubset_WithSelect exercises the select path: the
// handler must still fetch _markings (even though select omits it) to run the
// subset check, then strip _markings from the response.
func TestBDD_LoadObjects_MarkingSubset_WithSelect(t *testing.T) {
	handler, _ := setupMarkingHandlerTest(t)
	handler.SetMarkingFilterProvider(&staticMarkingFilter{enabled: true, user: []string{"A"}})

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/myOntology/objectSets/loadObjects",
		strings.NewReader(`{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0]["__primaryKey"] != "e1" {
		t.Fatalf("select load filtered = %v, want only e1", resp.Data)
	}
	// _markings was fetched only for the subset check; it must not leak.
	if _, leaked := resp.Data[0]["_markings"]; leaked {
		t.Errorf("_markings leaked into select=[id,name] response: %v", resp.Data[0])
	}
}

// TestBDD_LoadObjectsAsOf_MarkingSubsetFilter closes the time-travel
// marking-bypass: the ?asOf= read path must apply the same subset filter as
// the live path, else a caller could read marking-restricted rows via
// ?asOf=<now>.
func TestBDD_LoadObjectsAsOf_MarkingSubsetFilter(t *testing.T) {
	handler, _ := setupMarkingHandlerTest(t)
	handler.SetMarkingFilterProvider(&staticMarkingFilter{enabled: true, user: []string{"A"}})
	snapProvider := newFakeSnapshotProvider()
	snapProvider.asOfData["2020-01-01T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "e1", Properties: map[string]interface{}{"id": "e1", "name": "alice", "_markings": []interface{}{"A"}}},
		{PrimaryKey: "e2", Properties: map[string]interface{}{"id": "e2", "name": "bob", "_markings": []interface{}{"A", "B"}}},
	}
	handler.SetHistorySnapshotProvider(snapProvider)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/myOntology/objectSets/loadObjects?asOf=2020-01-01T00:00:00Z",
		strings.NewReader(`{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
	}
	if len(resp.Data) != 1 || resp.Data[0]["__primaryKey"] != "e1" {
		t.Fatalf("asOf load filtered = %v, want only e1 (e2 {A,B} not subset of {A})", resp.Data)
	}
	if _, leaked := resp.Data[0]["_markings"]; leaked {
		t.Errorf("_markings leaked into asOf response: %v", resp.Data[0])
	}
}

// TestLoadObjects_NoMarkingProvider_AllVisible guards back-compat: without a
// provider every row is returned regardless of markings.
func TestLoadObjects_NoMarkingProvider_AllVisible(t *testing.T) {
	handler, _ := setupMarkingHandlerTest(t)
	// No SetMarkingFilterProvider call.
	ids := loadEmployeeIDs(t, handler, `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","name"]}`)
	if len(ids) != 2 {
		t.Fatalf("no-provider load = %v, want both e1,e2", ids)
	}
}
