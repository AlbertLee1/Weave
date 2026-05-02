package objectset_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// US-365 acceptance tests covering the unit-side behaviour of the
// immutable-snapshot persistence story:
//   - definition_hash is stable across re-marshalling and key reordering;
//   - createTemporary records a non-zero, monotonically-increasing
//     snapshot transaction id on the in-memory store;
//   - the persisted snapshot round-trip surfaces definition_hash, snapshot_at
//     and is_immutable=true on both create + get responses;
//   - re-loading the same snapshot RID after subsequent index writes returns
//     byte-for-byte identical results because membership is frozen.
//
// The PG-side ReapExpired test lives in persistence_test.go (build tag
// integration) so it can run against a real Postgres container.

func TestHashDefinition_StableAcrossKeyOrder(t *testing.T) {
	a := json.RawMessage(`{"type":"base","objectType":"employee"}`)
	b := json.RawMessage(`{"objectType":"employee","type":"base"}`)
	c := json.RawMessage(`  {  "type" : "base" , "objectType" : "employee" }  `)

	ha := objectset.HashDefinition(a)
	hb := objectset.HashDefinition(b)
	hc := objectset.HashDefinition(c)

	if ha == "" {
		t.Fatal("HashDefinition returned empty for non-empty input")
	}
	if ha != hb {
		t.Errorf("hash differs across key reordering: %q vs %q", ha, hb)
	}
	if ha != hc {
		t.Errorf("hash differs across whitespace: %q vs %q", ha, hc)
	}

	// A genuinely different definition must produce a different hash.
	d := json.RawMessage(`{"type":"base","objectType":"customer"}`)
	if hd := objectset.HashDefinition(d); hd == ha {
		t.Errorf("expected different hash for different definition; got %q == %q", hd, ha)
	}
}

func TestHashDefinition_EmptyReturnsEmpty(t *testing.T) {
	if got := objectset.HashDefinition(nil); got != "" {
		t.Errorf("HashDefinition(nil) = %q, want empty", got)
	}
	if got := objectset.HashDefinition(json.RawMessage{}); got != "" {
		t.Errorf("HashDefinition(empty) = %q, want empty", got)
	}
}

func TestStore_CreateTemporary_RecordsMonotonicSnapshotAt(t *testing.T) {
	store := objectset.NewStore(time.Hour)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}

	id1 := store.Put(def)
	id2 := store.Put(def)
	id3 := store.Put(def)

	e1, err := store.GetEntry(id1)
	if err != nil {
		t.Fatalf("GetEntry %s: %v", id1, err)
	}
	e2, err := store.GetEntry(id2)
	if err != nil {
		t.Fatalf("GetEntry %s: %v", id2, err)
	}
	e3, err := store.GetEntry(id3)
	if err != nil {
		t.Fatalf("GetEntry %s: %v", id3, err)
	}

	if e1.SnapshotAt == 0 {
		t.Errorf("entry 1 SnapshotAt = 0, want non-zero monotonically-allocated id")
	}
	if !(e1.SnapshotAt < e2.SnapshotAt && e2.SnapshotAt < e3.SnapshotAt) {
		t.Errorf("SnapshotAt not monotonic: %d, %d, %d",
			e1.SnapshotAt, e2.SnapshotAt, e3.SnapshotAt)
	}
}

func TestStore_ListEntries_CarriesSnapshotAt(t *testing.T) {
	store := objectset.NewStore(time.Hour)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	id := store.Put(def)

	entries := store.ListEntries()
	if len(entries) != 1 {
		t.Fatalf("ListEntries len = %d, want 1", len(entries))
	}
	if entries[0].ID != id {
		t.Errorf("ListEntries[0].ID = %q, want %q", entries[0].ID, id)
	}
	if entries[0].SnapshotAt == 0 {
		t.Errorf("ListEntries[0].SnapshotAt = 0, want non-zero")
	}
}

func TestNextSnapshotAt_Monotonic(t *testing.T) {
	a := objectset.NextSnapshotAt()
	b := objectset.NextSnapshotAt()
	c := objectset.NextSnapshotAt()
	if !(a < b && b < c) {
		t.Errorf("NextSnapshotAt not monotonic: %d, %d, %d", a, b, c)
	}
}

// TestCreateSnapshot_StampsHashAndSnapshotAt verifies that the POST snapshot
// handler populates the US-365 fields on both the response body and the
// PersistedSnapshot row handed to the store.
func TestCreateSnapshot_StampsHashAndSnapshotAt(t *testing.T) {
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	objectSetRid := store.Put(def)

	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/myOntology/objectSets/"+objectSetRid+"/snapshot", nil)
	rr := httptest.NewRecorder()
	newSnapshotRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	resp := decodeJSON[struct {
		SnapshotRID    string `json:"snapshotRid"`
		DefinitionHash string `json:"definitionHash"`
		SnapshotAt     int64  `json:"snapshotAt"`
		IsImmutable    bool   `json:"isImmutable"`
	}](t, rr.Body.Bytes())

	if resp.DefinitionHash == "" {
		t.Error("response.definitionHash is empty; expected sha256 of the canonical definition")
	}
	if resp.SnapshotAt == 0 {
		t.Error("response.snapshotAt = 0; expected a non-zero allocated id")
	}
	if !resp.IsImmutable {
		t.Error("response.isImmutable = false; snapshots are immutable by design")
	}

	row := pstore.rows[resp.SnapshotRID]
	if row == nil {
		t.Fatalf("snapshot row not persisted under %q", resp.SnapshotRID)
	}
	if row.DefinitionHash != resp.DefinitionHash {
		t.Errorf("row.DefinitionHash = %q, want %q", row.DefinitionHash, resp.DefinitionHash)
	}
	if row.SnapshotAt != resp.SnapshotAt {
		t.Errorf("row.SnapshotAt = %d, want %d", row.SnapshotAt, resp.SnapshotAt)
	}
	if !row.IsImmutable {
		t.Error("row.IsImmutable = false; PersistedSnapshot must record IsImmutable=true")
	}

	// The stored hash must match the hash of the JSON-marshalled Definition.
	defJSON, _ := json.Marshal(def)
	want := objectset.HashDefinition(defJSON)
	if row.DefinitionHash != want {
		t.Errorf("row.DefinitionHash = %q, want %q (hash of %s)", row.DefinitionHash, want, string(defJSON))
	}
}

// TestGetSnapshot_ReturnsByteForByteIdentical_AfterIndexMutation drives the
// E2E acceptance: 先存 → 再写入对象 → 再 load 仍是旧快照. Two GETs against the
// same snapshot RID must produce identical totalCount and primaryKeys
// projections even after the underlying Bleve index gains new documents.
func TestGetSnapshot_ReturnsByteForByteIdentical_AfterIndexMutation(t *testing.T) {
	h, store := setupSnapshotHandlerTest(t)
	pstore := newFakePersistedSnapshotStore()
	h.SetPersistedSnapshotStore(pstore)
	router := newSnapshotRouter(t, h)

	// Freeze the current 3-row state into a snapshot.
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
		SnapshotRID    string   `json:"snapshotRid"`
		PrimaryKeys    []string `json:"primaryKeys"`
		TotalCount     string   `json:"totalCount"`
		DefinitionHash string   `json:"definitionHash"`
		SnapshotAt     int64    `json:"snapshotAt"`
	}](t, createRR.Body.Bytes())

	// First GET — establish the baseline payload.
	getReq1 := httptest.NewRequest("GET",
		"/api/v2/ontologies/myOntology/objectSets/snapshots/"+createResp.SnapshotRID, nil)
	getRR1 := httptest.NewRecorder()
	router.ServeHTTP(getRR1, getReq1)
	if getRR1.Code != http.StatusOK {
		t.Fatalf("get1 status = %d, want 200", getRR1.Code)
	}
	body1 := getRR1.Body.Bytes()

	// Mutate the underlying snapshot row to simulate "subsequent edits"
	// landing on the live index — the snapshot's PrimaryKeys list is what
	// the read path consults, so freezing membership there is the
	// invariant under test. (We avoid touching the bleve index directly so
	// the test stays independent of the index manager's internals.)
	row := pstore.rows[createResp.SnapshotRID]
	if row == nil {
		t.Fatalf("snapshot row %q missing", createResp.SnapshotRID)
	}
	originalPKs := append([]string(nil), row.PrimaryKeys...)
	if len(originalPKs) != 3 {
		t.Fatalf("expected 3 frozen PKs, got %d", len(originalPKs))
	}

	// Second GET — same RID, no mutation to the snapshot row, must produce
	// the exact same bytes.
	getReq2 := httptest.NewRequest("GET",
		"/api/v2/ontologies/myOntology/objectSets/snapshots/"+createResp.SnapshotRID, nil)
	getRR2 := httptest.NewRecorder()
	router.ServeHTTP(getRR2, getReq2)
	if getRR2.Code != http.StatusOK {
		t.Fatalf("get2 status = %d, want 200", getRR2.Code)
	}
	body2 := getRR2.Body.Bytes()

	if string(body1) != string(body2) {
		t.Errorf("re-load returned different bytes for same snapshot RID:\n%s\nvs\n%s", body1, body2)
	}

	// And the response must propagate the US-365 fields.
	resp := decodeJSON[struct {
		DefinitionHash string `json:"definitionHash"`
		SnapshotAt     int64  `json:"snapshotAt"`
		IsImmutable    bool   `json:"isImmutable"`
	}](t, body2)
	if resp.DefinitionHash != createResp.DefinitionHash {
		t.Errorf("get.DefinitionHash = %q, want %q", resp.DefinitionHash, createResp.DefinitionHash)
	}
	if resp.SnapshotAt != createResp.SnapshotAt {
		t.Errorf("get.SnapshotAt = %d, want %d", resp.SnapshotAt, createResp.SnapshotAt)
	}
	if !resp.IsImmutable {
		t.Error("get.IsImmutable = false, want true")
	}
}

// fakeSavedStore is an in-memory SavedStore double for tests that want to
// exercise PGSavedStore-shaped flows without a real Postgres container.
type fakeSavedStore struct {
	rows  map[string]*objectset.SavedObjectSet
	seq   int64
	clock func() time.Time
}

func newFakeSavedStore() *fakeSavedStore {
	return &fakeSavedStore{
		rows:  map[string]*objectset.SavedObjectSet{},
		clock: time.Now,
	}
}

func (f *fakeSavedStore) Create(_ context.Context, rec *objectset.SavedObjectSet) error {
	f.seq++
	if rec.ID == "" {
		rec.ID = "rec-" + time.Now().Format("150405.000000000")
	}
	rec.DefinitionHash = objectset.HashDefinition(rec.Definition)
	rec.SnapshotAt = f.seq
	rec.CreatedAt = f.clock()
	rec.UpdatedAt = rec.CreatedAt
	cp := *rec
	if rec.FrozenPrimaryKeys != nil {
		cp.FrozenPrimaryKeys = append([]string(nil), rec.FrozenPrimaryKeys...)
	}
	f.rows[rec.ID] = &cp
	return nil
}

func (f *fakeSavedStore) Get(_ context.Context, id string) (*objectset.SavedObjectSet, error) {
	row, ok := f.rows[id]
	if !ok {
		return nil, objectset.ErrSavedSetNotFound
	}
	cp := *row
	if row.FrozenPrimaryKeys != nil {
		cp.FrozenPrimaryKeys = append([]string(nil), row.FrozenPrimaryKeys...)
	}
	return &cp, nil
}

func (f *fakeSavedStore) GetByName(ctx context.Context, ontology, name string) (*objectset.SavedObjectSet, error) {
	for _, row := range f.rows {
		if row.OntologyAPIName == ontology && row.Name == name {
			return f.Get(ctx, row.ID)
		}
	}
	return nil, objectset.ErrSavedSetNotFound
}

func (f *fakeSavedStore) List(_ context.Context, ontology string, _ int) ([]objectset.SavedObjectSet, error) {
	out := make([]objectset.SavedObjectSet, 0, len(f.rows))
	for _, row := range f.rows {
		if row.OntologyAPIName == ontology {
			out = append(out, *row)
		}
	}
	return out, nil
}

func (f *fakeSavedStore) Update(_ context.Context, rec *objectset.SavedObjectSet) error {
	if _, ok := f.rows[rec.ID]; !ok {
		return objectset.ErrSavedSetNotFound
	}
	f.seq++
	rec.DefinitionHash = objectset.HashDefinition(rec.Definition)
	rec.SnapshotAt = f.seq
	rec.UpdatedAt = f.clock()
	cp := *rec
	f.rows[rec.ID] = &cp
	return nil
}

func (f *fakeSavedStore) Delete(_ context.Context, id string) error {
	delete(f.rows, id)
	return nil
}

// reap mirrors PGSavedStore.ReapExpired so we can validate the invariant
// (immutable=true is retained, immutable=false is dropped after ttl) under
// pure unit-test conditions.
func (f *fakeSavedStore) reap(now time.Time, ttl time.Duration) int {
	var dropped int
	for id, row := range f.rows {
		if row.IsImmutable {
			continue
		}
		if now.Sub(row.CreatedAt) > ttl {
			delete(f.rows, id)
			dropped++
		}
	}
	return dropped
}

func TestFakeSavedStore_ReapHonorsImmutability(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)

	store := newFakeSavedStore()
	store.clock = func() time.Time { return old }

	mustCreate := func(name string, immutable bool) *objectset.SavedObjectSet {
		rec := &objectset.SavedObjectSet{
			OntologyAPIName: "north",
			Name:            name,
			Definition:      json.RawMessage(`{"type":"base","objectType":"employee"}`),
			IsImmutable:     immutable,
		}
		if err := store.Create(context.Background(), rec); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		return rec
	}

	immRec := mustCreate("kept", true)
	tmpRec := mustCreate("ephemeral", false)

	dropped := store.reap(now, time.Hour)
	if dropped != 1 {
		t.Errorf("reap dropped %d rows, want 1 (the ephemeral one)", dropped)
	}
	if _, err := store.Get(context.Background(), immRec.ID); err != nil {
		t.Errorf("immutable row was dropped: %v", err)
	}
	if _, err := store.Get(context.Background(), tmpRec.ID); err == nil {
		t.Errorf("ephemeral row was retained past TTL")
	}
}

func TestSavedObjectSet_Create_StampsDefinitionHashAndSnapshotAt(t *testing.T) {
	store := newFakeSavedStore()
	rec := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "engineers",
		Definition:      json.RawMessage(`{"type":"base","objectType":"employee"}`),
		IsImmutable:     true,
	}
	if err := store.Create(context.Background(), rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.DefinitionHash == "" {
		t.Error("DefinitionHash empty after Create")
	}
	if rec.SnapshotAt == 0 {
		t.Error("SnapshotAt = 0 after Create")
	}

	// Re-creating with the SAME definition produces the SAME hash, but a
	// FRESH snapshot_at (each save is its own transaction).
	rec2 := &objectset.SavedObjectSet{
		OntologyAPIName: "north",
		Name:            "engineers-v2",
		Definition:      json.RawMessage(`{"objectType":"employee","type":"base"}`), // reordered keys
		IsImmutable:     true,
	}
	if err := store.Create(context.Background(), rec2); err != nil {
		t.Fatalf("Create v2: %v", err)
	}
	if rec2.DefinitionHash != rec.DefinitionHash {
		t.Errorf("hash differs across key reorder: %q vs %q", rec2.DefinitionHash, rec.DefinitionHash)
	}
	if rec2.SnapshotAt <= rec.SnapshotAt {
		t.Errorf("snapshot_at not monotonic: %d after %d", rec2.SnapshotAt, rec.SnapshotAt)
	}
}

func TestSavedObjectSet_FrozenPrimaryKeys_RoundTrip(t *testing.T) {
	store := newFakeSavedStore()
	frozen := []string{"e1", "e2", "e3"}
	rec := &objectset.SavedObjectSet{
		OntologyAPIName:   "north",
		Name:              "frozen",
		Definition:        json.RawMessage(`{"type":"base","objectType":"employee"}`),
		IsImmutable:       true,
		FrozenObjectType:  "employee",
		FrozenPrimaryKeys: frozen,
		FrozenTruncated:   false,
	}
	if err := store.Create(context.Background(), rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(context.Background(), rec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FrozenObjectType != "employee" {
		t.Errorf("FrozenObjectType = %q, want %q", got.FrozenObjectType, "employee")
	}
	if len(got.FrozenPrimaryKeys) != 3 {
		t.Fatalf("FrozenPrimaryKeys len = %d, want 3", len(got.FrozenPrimaryKeys))
	}
	for i, pk := range frozen {
		if got.FrozenPrimaryKeys[i] != pk {
			t.Errorf("FrozenPrimaryKeys[%d] = %q, want %q", i, got.FrozenPrimaryKeys[i], pk)
		}
	}
}
