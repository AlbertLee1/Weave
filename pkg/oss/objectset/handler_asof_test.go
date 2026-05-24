package objectset_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeSnapshotProvider is the in-memory test double for the US-223
// HistorySnapshotProvider hook. asOfData maps an RFC3339 timestamp to the
// snapshot the provider should return when asked for that exact instant;
// every other timestamp returns nil. Records calls so tests can assert the
// handler dispatched with the right (ontology, objectType, asOf) tuple.
type fakeSnapshotProvider struct {
	mu       sync.Mutex
	asOfData map[string][]objectset.ObjectSnapshot
	err      error
	calls    []snapshotCall
}

type snapshotCall struct {
	Ontology   string
	ObjectType string
	AsOf       time.Time
}

func newFakeSnapshotProvider() *fakeSnapshotProvider {
	return &fakeSnapshotProvider{asOfData: map[string][]objectset.ObjectSnapshot{}}
}

func (f *fakeSnapshotProvider) SnapshotObjectsAt(_ context.Context, ontologyAPIName, objectTypeAPIName string, asOf time.Time) ([]objectset.ObjectSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, snapshotCall{Ontology: ontologyAPIName, ObjectType: objectTypeAPIName, AsOf: asOf})
	if f.err != nil {
		return nil, f.err
	}
	return f.asOfData[asOf.Format(time.RFC3339)], nil
}

func newAsOfRouter(t *testing.T, h *objectset.Handler) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", h.LoadObjects)
	return r
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode json: %v; body=%s", err, body)
	}
	return v
}

func TestLoadObjects_AsOf_RoutesThroughHistorySnapshot(t *testing.T) {
	// US-223 happy path: asOf=<RFC3339> + a wired provider returns the
	// per-PK state the provider hands back, NOT whatever the live Bleve
	// index might have. The executor is intentionally nil — the asOf
	// branch must short-circuit before Execute is called.
	prov := newFakeSnapshotProvider()
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice", "age": 30.0}},
		{PrimaryKey: "emp-2", Properties: map[string]interface{}{"name": "Bob", "age": 25.0}},
	}
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name","age"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		Data []map[string]interface{} `json:"data"`
		Tc   string                   `json:"totalCount"`
		Acc  string                   `json:"totalCountAccuracy"`
	}](t, rr.Body.Bytes())
	if got := len(resp.Data); got != 2 {
		t.Fatalf("data len = %d, want 2; body = %s", got, rr.Body.String())
	}
	if resp.Tc != "2" {
		t.Errorf("totalCount = %q, want %q", resp.Tc, "2")
	}
	if resp.Acc != "EXACT" {
		t.Errorf("totalCountAccuracy = %q, want EXACT", resp.Acc)
	}

	if len(prov.calls) != 1 {
		t.Fatalf("expected exactly 1 provider call, got %d", len(prov.calls))
	}
	got := prov.calls[0]
	if got.Ontology != "test" || got.ObjectType != "Employee" {
		t.Errorf("provider call = %+v, want {Ontology: test, ObjectType: Employee}", got)
	}
	wantAsOf, _ := time.Parse(time.RFC3339, "2026-01-15T00:00:00Z")
	if !got.AsOf.Equal(wantAsOf) {
		t.Errorf("provider asOf = %v, want %v", got.AsOf, wantAsOf)
	}

	// First row is "Alice" because PKs are sorted ASC for stable pagination.
	if got := resp.Data[0]["__primaryKey"]; got != "emp-1" {
		t.Errorf("data[0].__primaryKey = %v, want emp-1", got)
	}
	if got := resp.Data[0]["name"]; got != "Alice" {
		t.Errorf("data[0].name = %v, want Alice", got)
	}
}

func TestLoadObjects_AsOf_FiltersToSelect(t *testing.T) {
	// Properties not in select must be dropped from the response.
	prov := newFakeSnapshotProvider()
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{
			"name": "Alice", "age": 30.0, "ssn": "123-45-6789",
		}},
	}
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		Data []map[string]interface{} `json:"data"`
	}](t, rr.Body.Bytes())
	if got := len(resp.Data); got != 1 {
		t.Fatalf("data len = %d, want 1", got)
	}
	row := resp.Data[0]
	if _, present := row["age"]; present {
		t.Errorf("age field leaked through select filter: %v", row)
	}
	if _, present := row["ssn"]; present {
		t.Errorf("ssn field leaked through select filter: %v", row)
	}
	if row["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", row["name"])
	}
}

func TestLoadObjects_AsOf_PaginatesByPK(t *testing.T) {
	// 3 PKs, pageSize=2 → first page = 2 rows + nextPageToken; second page
	// = 1 row + no nextPageToken. PKs come back sorted ASC.
	prov := newFakeSnapshotProvider()
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "z", Properties: map[string]interface{}{"name": "Z"}},
		{PrimaryKey: "a", Properties: map[string]interface{}{"name": "A"}},
		{PrimaryKey: "m", Properties: map[string]interface{}{"name": "M"}},
	}
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)
	router := newAsOfRouter(t, h)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"],"pageSize":2}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	page1 := decodeJSON[struct {
		Data          []map[string]interface{} `json:"data"`
		NextPageToken string                   `json:"nextPageToken"`
		TotalCount    string                   `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if len(page1.Data) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1.Data))
	}
	if page1.NextPageToken == "" {
		t.Fatal("expected nextPageToken on page1")
	}
	if page1.TotalCount != "3" {
		t.Errorf("totalCount = %q, want 3", page1.TotalCount)
	}
	wantPKs := []string{"a", "m"}
	for i, want := range wantPKs {
		if got := page1.Data[i]["__primaryKey"]; got != want {
			t.Errorf("page1[%d].__primaryKey = %v, want %v", i, got, want)
		}
	}

	// Page 2: pass nextPageToken back. Should get the trailing row.
	body2 := fmt.Sprintf(`{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"],"pageSize":2,"pageToken":%q}`, page1.NextPageToken)
	req2 := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d, want 200; body = %s", rr2.Code, rr2.Body.String())
	}
	page2 := decodeJSON[struct {
		Data          []map[string]interface{} `json:"data"`
		NextPageToken string                   `json:"nextPageToken"`
	}](t, rr2.Body.Bytes())
	if len(page2.Data) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(page2.Data))
	}
	if page2.NextPageToken != "" {
		t.Errorf("page2 unexpectedly has nextPageToken=%q", page2.NextPageToken)
	}
	if got := page2.Data[0]["__primaryKey"]; got != "z" {
		t.Errorf("page2[0].__primaryKey = %v, want z", got)
	}
}

func TestLoadObjects_AsOf_RejectsInvalidTimestamp(t *testing.T) {
	prov := newFakeSnapshotProvider()
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=not-a-timestamp",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "InvalidAsOf" {
		t.Errorf("errorName = %q, want InvalidAsOf", apiErr.ErrorName)
	}
	if len(prov.calls) != 0 {
		t.Errorf("provider must not be called for invalid timestamp; got %d call(s)", len(prov.calls))
	}
}

func TestLoadObjects_AsOf_NoProviderReturnsTimeTravelUnavailable(t *testing.T) {
	// US-223 graceful-degraded: when no provider is wired (degraded mode /
	// unit test routers without PG) the asOf branch returns a documented
	// 400 instead of falling through to the live Bleve path.
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store) // no SetHistorySnapshotProvider

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "TimeTravelUnavailable" {
		t.Errorf("errorName = %q, want TimeTravelUnavailable", apiErr.ErrorName)
	}
}

func TestLoadObjects_AsOf_RejectsCompositeObjectSet(t *testing.T) {
	// asOf only makes semantic sense on a base ObjectSet. Composite types
	// (filter / union / intersect / searchAround / ...) need a per-instant
	// Bleve index that we don't materialise — reject upfront.
	prov := newFakeSnapshotProvider()
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)

	body := `{"objectSet":{"type":"filter","objectSet":{"type":"base","objectType":"Employee"},"where":{}},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "TimeTravelUnsupportedObjectSet" {
		t.Errorf("errorName = %q, want TimeTravelUnsupportedObjectSet", apiErr.ErrorName)
	}
	if len(prov.calls) != 0 {
		t.Errorf("provider must not be called for composite objectSet; got %d", len(prov.calls))
	}
}

func TestLoadObjects_AsOf_PropagatesProviderError(t *testing.T) {
	// Wire-shape contract: a downstream HistorySnapshotProvider failure
	// is server-side, NOT bad user input. It surfaces as HTTP 500
	// INTERNAL with the TimeTravelFailed envelope so SDK callers route
	// to retry / oncall rather than "fix your request".
	prov := newFakeSnapshotProvider()
	prov.err = errors.New("backend unreachable")
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=2026-01-15T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "TimeTravelFailed" {
		t.Errorf("errorName = %q, want TimeTravelFailed", apiErr.ErrorName)
	}
	if !strings.Contains(apiErr.Parameters["error"], "backend unreachable") {
		t.Errorf("parameters.error = %q, want it to mention backend unreachable", apiErr.Parameters["error"])
	}
}

func TestLoadObjects_AsOf_EmptySnapshotReturnsEmptyData(t *testing.T) {
	// Provider returns an empty slice for the requested instant — handler
	// surfaces totalCount=0 and an empty data array, NOT an error.
	prov := newFakeSnapshotProvider() // asOfData empty
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=1990-01-01T00:00:00Z",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}](t, rr.Body.Bytes())
	if len(resp.Data) != 0 {
		t.Errorf("data len = %d, want 0", len(resp.Data))
	}
	if resp.TotalCount != "0" {
		t.Errorf("totalCount = %q, want 0", resp.TotalCount)
	}
}

func TestLoadObjects_AsOf_LivePathBypassesProvider(t *testing.T) {
	// US-223 belt-and-braces: when asOf is absent the snapshot provider
	// MUST NOT be consulted. The live path then dispatches to the
	// executor; with a nil IndexMgr the executor would panic, so we
	// recover and assert only the contract that matters here — the
	// provider received zero calls.
	prov := newFakeSnapshotProvider()
	store := objectset.NewStore(0)
	exec := objectset.NewExecutor(nil, nil, store)
	h := objectset.NewHandler(exec, nil, store)
	h.SetHistorySnapshotProvider(prov)

	defer func() {
		_ = recover()
		if len(prov.calls) != 0 {
			t.Errorf("provider must not be called when asOf is absent; got %d calls", len(prov.calls))
		}
	}()

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)
}
