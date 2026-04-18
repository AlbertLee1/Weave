package objectset_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakePersistedSnapshotStore is an in-memory test double for the US-224
// PersistedSnapshotStore interface. It records create / get traffic so tests
// can assert routes hit it the right way and lets a single instance be shared
// across the create + get halves of the round-trip.
type fakePersistedSnapshotStore struct {
	mu       sync.Mutex
	rows     map[string]*objectset.PersistedSnapshot
	createN  int
	getN     int
	createEr error
	getEr    error
}

func newFakePersistedSnapshotStore() *fakePersistedSnapshotStore {
	return &fakePersistedSnapshotStore{rows: map[string]*objectset.PersistedSnapshot{}}
}

func (f *fakePersistedSnapshotStore) CreatePersistedSnapshot(_ context.Context, snap *objectset.PersistedSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createN++
	if f.createEr != nil {
		return f.createEr
	}
	cp := *snap
	if snap.PrimaryKeys != nil {
		cp.PrimaryKeys = append([]string(nil), snap.PrimaryKeys...)
	}
	f.rows[snap.RID] = &cp
	return nil
}

func (f *fakePersistedSnapshotStore) GetPersistedSnapshot(_ context.Context, rid string) (*objectset.PersistedSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getN++
	if f.getEr != nil {
		return nil, f.getEr
	}
	row, ok := f.rows[rid]
	if !ok {
		return nil, objectset.ErrSnapshotNotFound
	}
	cp := *row
	if row.PrimaryKeys != nil {
		cp.PrimaryKeys = append([]string(nil), row.PrimaryKeys...)
	}
	return &cp, nil
}

func newSnapshotRouter(t *testing.T, h *objectset.Handler) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/snapshot", h.CreateSnapshot)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/snapshots/{snapshotRid}", h.GetSnapshot)
	return r
}

// setupSnapshotHandlerTest wires a Handler over a real index + store and a
// pre-seeded "employee" ObjectType so the executor can produce a non-empty
// PrimaryKeys list during snapshot creation.
func setupSnapshotHandlerTest(t *testing.T) (*objectset.Handler, *objectset.Store) {
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
	docs := []map[string]interface{}{
		{"id": "e1", "name": "alice"},
		{"id": "e2", "name": "bob"},
		{"id": "e3", "name": "carol"},
	}
	for _, d := range docs {
		if err := mgr.IndexDocument("employee", d["id"].(string), d); err != nil {
			t.Fatalf("IndexDocument %v: %v", d["id"], err)
		}
	}

	store := objectset.NewStore(time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	h := objectset.NewHandler(executor, mgr, store)
	return h, store
}

// --- POST /api/v2/ontologies/{ont}/objectSets/{objectSetRid}/snapshot ---

func TestCreateSnapshot_PersistsExecutorResult(t *testing.T) {
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	objectSetRid := store.Put(def)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot",
		nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		SnapshotRID string   `json:"snapshotRid"`
		ObjectType  string   `json:"objectType"`
		PrimaryKeys []string `json:"primaryKeys"`
		TotalCount  string   `json:"totalCount"`
	}](t, rr.Body.Bytes())

	if !strings.HasPrefix(resp.SnapshotRID, "ri.objectsets.main.snapshot.") {
		t.Errorf("snapshotRid = %q, want prefix ri.objectsets.main.snapshot.", resp.SnapshotRID)
	}
	if resp.ObjectType != "employee" {
		t.Errorf("objectType = %q, want employee", resp.ObjectType)
	}
	if got := len(resp.PrimaryKeys); got != 3 {
		t.Errorf("primaryKeys len = %d, want 3", got)
	}
	if resp.TotalCount != "3" {
		t.Errorf("totalCount = %q, want 3", resp.TotalCount)
	}
	if pstore.createN != 1 {
		t.Errorf("CreatePersistedSnapshot calls = %d, want 1", pstore.createN)
	}

	row := pstore.rows[resp.SnapshotRID]
	if row == nil {
		t.Fatalf("snapshot row not persisted under %q", resp.SnapshotRID)
	}
	if row.OntologyAPIName != "myOntology" {
		t.Errorf("row.OntologyAPIName = %q, want myOntology", row.OntologyAPIName)
	}
	if row.ObjectType != "employee" {
		t.Errorf("row.ObjectType = %q, want employee", row.ObjectType)
	}
	if got := len(row.PrimaryKeys); got != 3 {
		t.Errorf("row.PrimaryKeys len = %d, want 3", got)
	}
	if row.Definition == nil || row.Definition.Type != "base" || row.Definition.ObjectType != "employee" {
		t.Errorf("row.Definition = %+v, want {base, employee}", row.Definition)
	}
	if row.CreatedAt.IsZero() {
		t.Error("row.CreatedAt was not stamped")
	}
}

func TestCreateSnapshot_UnknownObjectSetReturns404(t *testing.T) {
	h, _ := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/does-not-exist/snapshot", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "ObjectSetNotFound" {
		t.Errorf("errorName = %q, want ObjectSetNotFound", apiErr.ErrorName)
	}
	if pstore.createN != 0 {
		t.Errorf("snapshot must not be persisted on lookup miss; createN=%d", pstore.createN)
	}
}

func TestCreateSnapshot_StoreNotConfigured(t *testing.T) {
	h, store := setupSnapshotHandlerTest(t)
	// no SetPersistedSnapshotStore — degraded mode

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	objectSetRid := store.Put(def)
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "SnapshotsUnavailable" {
		t.Errorf("errorName = %q, want SnapshotsUnavailable", apiErr.ErrorName)
	}
}

func TestCreateSnapshot_PersistFailureSurfaces(t *testing.T) {
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	pstore.createEr = errors.New("disk full")
	h.SetPersistedSnapshotStore(pstore)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	objectSetRid := store.Put(def)
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "SnapshotPersistFailed" {
		t.Errorf("errorName = %q, want SnapshotPersistFailed", apiErr.ErrorName)
	}
	if !strings.Contains(apiErr.Parameters["error"], "disk full") {
		t.Errorf("parameters.error = %q, want it to mention the underlying message", apiErr.Parameters["error"])
	}
}

// --- GET /api/v2/ontologies/{ont}/objectSets/snapshots/{snapshotRid} ---

func TestGetSnapshot_ReturnsFrozenResults(t *testing.T) {
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)
	router := newSnapshotRouter(t, h)

	// Create the snapshot first.
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	objectSetRid := store.Put(def)
	createReq := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot", nil)
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body = %s", createRR.Code, createRR.Body.String())
	}
	createResp := decodeJSON[struct {
		SnapshotRID string `json:"snapshotRid"`
	}](t, createRR.Body.Bytes())

	// Read it back.
	getReq := httptest.NewRequest("GET",
		"/api/v2/ontologies/myOntology/objectSets/snapshots/"+createResp.SnapshotRID, nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body = %s", getRR.Code, getRR.Body.String())
	}
	resp := decodeJSON[struct {
		SnapshotRID string                   `json:"snapshotRid"`
		ObjectType  string                   `json:"objectType"`
		Data        []map[string]interface{} `json:"data"`
		TotalCount  string                   `json:"totalCount"`
		CreatedAt   string                   `json:"createdAt"`
	}](t, getRR.Body.Bytes())

	if resp.SnapshotRID != createResp.SnapshotRID {
		t.Errorf("snapshotRid = %q, want %q", resp.SnapshotRID, createResp.SnapshotRID)
	}
	if resp.ObjectType != "employee" {
		t.Errorf("objectType = %q, want employee", resp.ObjectType)
	}
	if got := len(resp.Data); got != 3 {
		t.Fatalf("data len = %d, want 3", got)
	}
	if resp.TotalCount != "3" {
		t.Errorf("totalCount = %q, want 3", resp.TotalCount)
	}
	// Spot-check that frozen rows carry the live properties from Bleve under
	// the same WireObject envelope LoadObjects returns.
	row := resp.Data[0]
	if _, ok := row["__primaryKey"]; !ok {
		t.Errorf("data[0] missing __primaryKey: %+v", row)
	}
	if got := row["__apiName"]; got != "employee" {
		t.Errorf("data[0].__apiName = %v, want employee", got)
	}
	if resp.CreatedAt == "" {
		t.Error("createdAt was not surfaced")
	}
	if pstore.getN != 1 {
		t.Errorf("GetPersistedSnapshot calls = %d, want 1", pstore.getN)
	}
}

func TestGetSnapshot_FrozenAfterUnderlyingSetMutates(t *testing.T) {
	// US-224 invariant: snapshot's PK list is FROZEN at create time. Even if
	// new objects are indexed afterwards (which would have been included by
	// re-running the live ObjectSet) the snapshot only returns the original
	// rows. The whole point is "save for later comparison".
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)
	router := newSnapshotRouter(t, h)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	objectSetRid := store.Put(def)
	createReq := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot", nil)
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body = %s", createRR.Code, createRR.Body.String())
	}
	createResp := decodeJSON[struct {
		SnapshotRID string `json:"snapshotRid"`
	}](t, createRR.Body.Bytes())

	// Mutate the persisted row to remove one PK so we can verify GetSnapshot
	// reads from the row, not the live index.
	pstore.mu.Lock()
	pstore.rows[createResp.SnapshotRID].PrimaryKeys = []string{"e1"}
	pstore.mu.Unlock()

	getReq := httptest.NewRequest("GET",
		"/api/v2/ontologies/myOntology/objectSets/snapshots/"+createResp.SnapshotRID, nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body = %s", getRR.Code, getRR.Body.String())
	}
	resp := decodeJSON[struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}](t, getRR.Body.Bytes())
	if got := len(resp.Data); got != 1 {
		t.Fatalf("data len = %d, want 1 (frozen to row.PrimaryKeys)", got)
	}
	if resp.TotalCount != "1" {
		t.Errorf("totalCount = %q, want 1", resp.TotalCount)
	}
}

func TestGetSnapshot_UnknownReturnsNotFound(t *testing.T) {
	h, _ := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)

	req := httptest.NewRequest("GET",
		"/api/v2/ontologies/myOntology/objectSets/snapshots/ri.objectsets.main.snapshot.bogus", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "SnapshotNotFound" {
		t.Errorf("errorName = %q, want SnapshotNotFound", apiErr.ErrorName)
	}
}

func TestGetSnapshot_StoreNotConfigured(t *testing.T) {
	h, _ := setupSnapshotHandlerTest(t)
	// no SetPersistedSnapshotStore

	req := httptest.NewRequest("GET",
		"/api/v2/ontologies/myOntology/objectSets/snapshots/anything", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "SnapshotsUnavailable" {
		t.Errorf("errorName = %q, want SnapshotsUnavailable", apiErr.ErrorName)
	}
}

// --- Definition round-trips through JSONB safely ---

func TestCreateSnapshot_PreservesComplexDefinition(t *testing.T) {
	// A filter ObjectSet should round-trip through PersistedSnapshot.Definition
	// so a future re-run / comparison can rebuild the exact source query.
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)

	def := &objectset.Definition{
		Type:      "filter",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Where:     json.RawMessage(`{"type":"eq","field":"name","value":"alice"}`),
	}
	objectSetRid := store.Put(def)
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		SnapshotRID string `json:"snapshotRid"`
	}](t, rr.Body.Bytes())

	row := pstore.rows[resp.SnapshotRID]
	if row.Definition == nil || row.Definition.Type != "filter" {
		t.Fatalf("row.Definition.Type = %v, want filter", row.Definition)
	}
	if row.Definition.ObjectSet == nil || row.Definition.ObjectSet.Type != "base" {
		t.Errorf("row.Definition.ObjectSet = %v, want nested base", row.Definition.ObjectSet)
	}
	if len(row.Definition.Where) == 0 {
		t.Errorf("row.Definition.Where lost in round-trip")
	}
}
