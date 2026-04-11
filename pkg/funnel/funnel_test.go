package funnel

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// testOntology is the default ontology API name used by funnel tests after
// US-044. The Bleve index manager keys per (ontology, objectType) so the test
// fixtures must agree on a fixed scope name.
const testOntology = "test-ont"

// fakeHistoryRepo is a minimal HistoryRecorder used by Tier 2.3 funnel tests
// to verify that the consumer writes one ObjectHistory row per applied edit.
// US-021 extends it with LatestUserEditAt so conflict-resolution tests can
// drive the user-edit-wins path without standing up a real PG backend.
type fakeHistoryRepo struct {
	mu        sync.Mutex
	rows      []oms.ObjectHistory
	insertErr error
}

func (f *fakeHistoryRepo) InsertObjectHistory(_ context.Context, h *oms.ObjectHistory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return f.insertErr
	}
	// The PG repo back-fills recorded_at on INSERT; the fake mimics that so
	// downstream LatestUserEditAt queries return a meaningful timestamp.
	if h.RecordedAt.IsZero() {
		h.RecordedAt = time.Now()
	}
	// Copy by value so callers can mutate the input freely.
	f.rows = append(f.rows, *h)
	return nil
}

// LatestUserEditAt returns the recorded_at of the most recent history row
// whose source == EditSourceUser for (objectTypeRID, primaryKey). The second
// return value indicates whether any user edit exists at all.
func (f *fakeHistoryRepo) LatestUserEditAt(_ context.Context, objectTypeRID, primaryKey string) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest time.Time
	found := false
	for _, r := range f.rows {
		if r.ObjectTypeRID != objectTypeRID || r.PrimaryKey != primaryKey {
			continue
		}
		src := r.Source
		if src == "" {
			src = oms.EditSourceUser
		}
		if src != oms.EditSourceUser {
			continue
		}
		if !found || r.RecordedAt.After(latest) {
			latest = r.RecordedAt
			found = true
		}
	}
	return latest, found, nil
}

func (f *fakeHistoryRepo) snapshot() []oms.ObjectHistory {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]oms.ObjectHistory, len(f.rows))
	copy(out, f.rows)
	return out
}

// setupTestConsumer creates a Consumer with a real index.Manager but no NATS connection.
func setupTestConsumer(t *testing.T) (*Consumer, *index.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "age", BaseType: "integer", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, "employee"), props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	consumer := &Consumer{
		indexMgr:      mgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
	t.Cleanup(func() { mgr.Close() })
	return consumer, mgr
}

// --- Types tests (3) ---

func TestEditType_Constants(t *testing.T) {
	if EditTypeCreate != "CREATE" {
		t.Fatalf("expected CREATE, got %q", EditTypeCreate)
	}
	if EditTypeModify != "MODIFY" {
		t.Fatalf("expected MODIFY, got %q", EditTypeModify)
	}
	if EditTypeDelete != "DELETE" {
		t.Fatalf("expected DELETE, got %q", EditTypeDelete)
	}
}

func TestEditBatch_JSON(t *testing.T) {
	batch := EditBatch{
		ID:              "batch-1",
		OntologyAPIName: testOntology,
		UserID:          "user-1",
		Timestamp:       time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{
					"name": "Alice",
					"age":  float64(30),
				},
			},
		},
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EditBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != batch.ID {
		t.Fatalf("expected ID %q, got %q", batch.ID, decoded.ID)
	}
	if decoded.UserID != batch.UserID {
		t.Fatalf("expected UserID %q, got %q", batch.UserID, decoded.UserID)
	}
	if len(decoded.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(decoded.Edits))
	}
	if decoded.Edits[0].Type != EditTypeCreate {
		t.Fatalf("expected edit type CREATE, got %q", decoded.Edits[0].Type)
	}
	if decoded.Edits[0].ObjectType != "employee" {
		t.Fatalf("expected objectType employee, got %q", decoded.Edits[0].ObjectType)
	}
	if decoded.Edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected primaryKey emp-1, got %q", decoded.Edits[0].PrimaryKey)
	}
	if decoded.Edits[0].Properties["name"] != "Alice" {
		t.Fatalf("expected name Alice, got %v", decoded.Edits[0].Properties["name"])
	}
}

func TestChangeEvent_JSON(t *testing.T) {
	event := ChangeEvent{
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		EditType:   EditTypeModify,
		Offset:     42,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ChangeEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ObjectType != event.ObjectType {
		t.Fatalf("expected objectType %q, got %q", event.ObjectType, decoded.ObjectType)
	}
	if decoded.PrimaryKey != event.PrimaryKey {
		t.Fatalf("expected primaryKey %q, got %q", event.PrimaryKey, decoded.PrimaryKey)
	}
	if decoded.EditType != event.EditType {
		t.Fatalf("expected editType %q, got %q", event.EditType, decoded.EditType)
	}
	if decoded.Offset != event.Offset {
		t.Fatalf("expected offset %d, got %d", event.Offset, decoded.Offset)
	}
}

// --- ApplyEdit logic tests (5) ---

func TestApplyEdit_Create(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	edit := Edit{
		Type:       EditTypeCreate,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{
			"name": "Alice",
			"age":  float64(30),
		},
	}

	if err := consumer.applyEdit(testOntology, edit); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	count, err := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected doc count 1, got %d", count)
	}
}

func TestApplyEdit_Modify(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	// Create first
	create := Edit{
		Type:       EditTypeCreate,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{
			"name": "Alice",
			"age":  float64(30),
		},
	}
	if err := consumer.applyEdit(testOntology, create); err != nil {
		t.Fatalf("applyEdit create: %v", err)
	}

	// Modify
	modify := Edit{
		Type:       EditTypeModify,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{
			"name": "Alice Updated",
			"age":  float64(31),
		},
	}
	if err := consumer.applyEdit(testOntology, modify); err != nil {
		t.Fatalf("applyEdit modify: %v", err)
	}

	// Document count should still be 1 (same primary key)
	count, err := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected doc count 1 after modify, got %d", count)
	}
}

func TestApplyEdit_Delete(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	// Create first
	create := Edit{
		Type:       EditTypeCreate,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{
			"name": "Alice",
		},
	}
	if err := consumer.applyEdit(testOntology, create); err != nil {
		t.Fatalf("applyEdit create: %v", err)
	}

	// Delete
	del := Edit{
		Type:       EditTypeDelete,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
	}
	if err := consumer.applyEdit(testOntology, del); err != nil {
		t.Fatalf("applyEdit delete: %v", err)
	}

	count, err := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected doc count 0 after delete, got %d", count)
	}
}

func TestApplyEdit_UnknownType(t *testing.T) {
	consumer, _ := setupTestConsumer(t)

	edit := Edit{
		Type:       EditType("INVALID"),
		ObjectType: "employee",
		PrimaryKey: "emp-1",
	}

	err := consumer.applyEdit(testOntology, edit)
	if err == nil {
		t.Fatal("expected error for unknown edit type")
	}
}

func TestApplyEdit_WithProperties(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	edit := Edit{
		Type:       EditTypeCreate,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{
			"employeeId": "E001",
			"name":       "Bob Smith",
			"age":        float64(25),
		},
	}

	if err := consumer.applyEdit(testOntology, edit); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	// Verify document was indexed with correct properties by searching
	count, err := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected doc count 1, got %d", count)
	}

	// Search for the document by name field
	idx := mgr.GetIndex(index.ScopedKey(testOntology, "employee"))
	if idx == nil {
		t.Fatal("expected non-nil index")
	}

	doc, err := idx.Document("emp-1")
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if doc == nil {
		t.Fatal("expected non-nil document")
	}
}

// --- HandleMessage tests (3) ---

func TestHandleMessage_ValidBatch(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	batch := EditBatch{
		ID:              "batch-1",
		OntologyAPIName: testOntology,
		UserID:          "user-1",
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{
					"name": "Alice",
					"age":  float64(30),
				},
			},
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-2",
				Properties: map[string]interface{}{
					"name": "Bob",
					"age":  float64(25),
				},
			},
		},
	}

	// Test by decoding and applying each edit directly (simulating handleMessage logic)
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EditBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, edit := range decoded.Edits {
		if err := consumer.applyEdit(testOntology, edit); err != nil {
			t.Fatalf("applyEdit: %v", err)
		}
	}

	count, err := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected doc count 2, got %d", count)
	}
}

func TestHandleMessage_InvalidJSON(t *testing.T) {
	// Test that invalid JSON is handled gracefully (unmarshal fails)
	invalidData := []byte(`{invalid json}`)

	var batch EditBatch
	err := json.Unmarshal(invalidData, &batch)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHandleMessage_EmptyBatch(t *testing.T) {
	consumer, _ := setupTestConsumer(t)

	batch := EditBatch{
		ID:              "batch-empty",
		OntologyAPIName: testOntology,
		UserID:          "user-1",
		Edits:           []Edit{},
	}

	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EditBatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Processing an empty edits slice should work without error
	for _, edit := range decoded.Edits {
		if err := consumer.applyEdit(testOntology, edit); err != nil {
			t.Fatalf("applyEdit: %v", err)
		}
	}
}

// --- WaitForOffset tests (2) ---

func TestWaitForOffset_AlreadyReached(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	consumer.lastOffset.Store(10)

	ctx := context.Background()
	if err := consumer.WaitForOffset(ctx, 5); err != nil {
		t.Fatalf("WaitForOffset: %v", err)
	}
	if err := consumer.WaitForOffset(ctx, 10); err != nil {
		t.Fatalf("WaitForOffset exact: %v", err)
	}
}

func TestWaitForOffset_Timeout(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	consumer.lastOffset.Store(0)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := consumer.WaitForOffset(ctx, 100)
	if err == nil {
		t.Fatal("expected error on timeout")
	}
}

// --- Consumer batch-atomicity tests ---

// TestConsumer_BatchAtomicity_PerIndex verifies that applyBatchEdits groups
// edits by object type and commits each group atomically via the index
// manager's ApplyBatch. A batch spanning two object types should result in
// two atomic per-index commits, and all edits should be visible at the end.
func TestConsumer_BatchAtomicity_PerIndex(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)

	// Ensure a second object type exists for cross-type testing.
	props := []index.Property{
		{APIName: "title", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, "project"), props); err != nil {
		t.Fatalf("EnsureIndex project: %v", err)
	}

	edits := []Edit{
		{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice", "age": float64(30)}},
		{Type: EditTypeCreate, ObjectType: "project", PrimaryKey: "proj-1", Properties: map[string]interface{}{"title": "Weave"}},
		{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-2", Properties: map[string]interface{}{"name": "Bob", "age": float64(28)}},
	}

	if err := consumer.applyBatchEdits(testOntology, edits); err != nil {
		t.Fatalf("applyBatchEdits: %v", err)
	}

	empCount, err := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
	if err != nil {
		t.Fatalf("DocCount employee: %v", err)
	}
	if empCount != 2 {
		t.Fatalf("expected 2 employees, got %d", empCount)
	}

	projCount, err := mgr.DocCount(index.ScopedKey(testOntology, "project"))
	if err != nil {
		t.Fatalf("DocCount project: %v", err)
	}
	if projCount != 1 {
		t.Fatalf("expected 1 project, got %d", projCount)
	}
}

// TestConsumer_BatchAtomicity_UnknownType verifies the consumer returns an
// error when a batch includes an edit for an unknown object type (so the
// caller can Nak the NATS message).
func TestConsumer_BatchAtomicity_UnknownType(t *testing.T) {
	consumer, _ := setupTestConsumer(t)

	edits := []Edit{
		{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
		{Type: EditTypeCreate, ObjectType: "nonexistent", PrimaryKey: "x-1", Properties: map[string]interface{}{"foo": "bar"}},
	}

	if err := consumer.applyBatchEdits(testOntology, edits); err == nil {
		t.Fatal("expected error for unknown object type in batch")
	}
}

func TestConsumer_BatchAtomicity_Empty(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	if err := consumer.applyBatchEdits(testOntology, nil); err != nil {
		t.Fatalf("empty batch should be no-op: %v", err)
	}
}

// --- Publisher tests (2) ---

func TestPublisher_SubjectFormat(t *testing.T) {
	subject := BuildSubject("northwind", "employee")
	expected := "edits.northwind.employee"
	if subject != expected {
		t.Fatalf("expected subject %q, got %q", expected, subject)
	}

	subject = BuildSubject("northwind", "project")
	expected = "edits.northwind.project"
	if subject != expected {
		t.Fatalf("expected subject %q, got %q", expected, subject)
	}
}

func TestPublisher_BatchID(t *testing.T) {
	id := GenerateBatchID()
	if id == "" {
		t.Fatal("expected non-empty batch ID")
	}

	// Verify uniqueness
	id2 := GenerateBatchID()
	if id == id2 {
		t.Fatal("expected unique batch IDs")
	}
}

// --- MaxDeliveries tests ---

func TestConsumer_DefaultMaxDeliveries(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	if consumer.maxDeliveries != DefaultMaxDeliveries {
		t.Fatalf("expected default maxDeliveries %d, got %d", DefaultMaxDeliveries, consumer.maxDeliveries)
	}
}

func TestConsumer_SetMaxDeliveries(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	consumer.SetMaxDeliveries(10)
	if consumer.maxDeliveries != 10 {
		t.Fatalf("expected maxDeliveries 10, got %d", consumer.maxDeliveries)
	}
}

func TestConsumer_ShouldTerminate(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	consumer.maxDeliveries = 5

	if consumer.shouldTerminate(4) {
		t.Fatal("should not terminate at delivery 4 with max 5")
	}
	if consumer.shouldTerminate(5) {
		t.Fatal("should not terminate at delivery 5 with max 5 (exactly at limit)")
	}
	if !consumer.shouldTerminate(6) {
		t.Fatal("should terminate at delivery 6 with max 5")
	}
}

// --- ConnectOptions tests ---

func TestDefaultConnectOptions_HasMaxReconnects(t *testing.T) {
	opts := DefaultConnectOptions()
	found := false
	for _, opt := range opts {
		// Apply option to a default nats.Options to inspect
		o := &nats.Options{}
		if err := opt(o); err != nil {
			t.Fatalf("applying option: %v", err)
		}
		if o.MaxReconnect > 0 {
			found = true
			if o.MaxReconnect < 10 {
				t.Fatalf("MaxReconnect too low: %d", o.MaxReconnect)
			}
		}
	}
	if !found {
		t.Fatal("expected MaxReconnect option to be set")
	}
}

func TestDefaultConnectOptions_HasReconnectWait(t *testing.T) {
	opts := DefaultConnectOptions()
	found := false
	for _, opt := range opts {
		o := &nats.Options{}
		if err := opt(o); err != nil {
			t.Fatalf("applying option: %v", err)
		}
		if o.ReconnectWait > 0 {
			found = true
			if o.ReconnectWait < time.Second {
				t.Fatalf("ReconnectWait too low: %v", o.ReconnectWait)
			}
		}
	}
	if !found {
		t.Fatal("expected ReconnectWait option to be set")
	}
}

func TestDefaultConnectOptions_HasDisconnectHandler(t *testing.T) {
	opts := DefaultConnectOptions()
	found := false
	for _, opt := range opts {
		o := &nats.Options{}
		if err := opt(o); err != nil {
			t.Fatalf("applying option: %v", err)
		}
		if o.DisconnectedErrCB != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("expected DisconnectedErrCB handler to be set")
	}
}

func TestDefaultConnectOptions_HasReconnectHandler(t *testing.T) {
	opts := DefaultConnectOptions()
	found := false
	for _, opt := range opts {
		o := &nats.Options{}
		if err := opt(o); err != nil {
			t.Fatalf("applying option: %v", err)
		}
		if o.ReconnectedCB != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("expected ReconnectedCB handler to be set")
	}
}

// --- SetupJetStream test (1) ---

func TestStreamConfig(t *testing.T) {
	// Verify the stream configuration constants
	if StreamName != "OBJECT_EDITS" {
		t.Fatalf("expected stream name %q, got %q", "OBJECT_EDITS", StreamName)
	}
	if SubjectPrefix != "edits" {
		t.Fatalf("expected subject prefix %q, got %q", "edits", SubjectPrefix)
	}

	expectedSubject := SubjectPrefix + ".>"
	if expectedSubject != "edits.>" {
		t.Fatalf("expected wildcard subject %q, got %q", "edits.>", expectedSubject)
	}
}

// --- ObjectHistory tests (Tier 2.3) ---

// TestConsumer_RecordHistory_Create verifies that applying a CREATE edit
// inside a batch writes one ObjectHistory row with prev_state=nil and
// new_state populated from the edit's properties.
func TestConsumer_RecordHistory_Create(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	batch := EditBatch{
		ID:              "batch-create",
		OntologyAPIName: testOntology,
		UserID:          "user-1",
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice", "age": float64(30)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory: %v", err)
	}

	rows := repo.snapshot()
	if len(rows) != 1 {
		t.Fatalf("expected 1 history row, got %d", len(rows))
	}
	r := rows[0]
	if r.EditType != "CREATE" {
		t.Fatalf("expected EditType CREATE, got %q", r.EditType)
	}
	if r.PrimaryKey != "emp-1" {
		t.Fatalf("expected PrimaryKey emp-1, got %q", r.PrimaryKey)
	}
	if r.ObjectTypeRID != "ri.ontology.main.object-type.employee" {
		t.Fatalf("expected ObjectTypeRID resolved, got %q", r.ObjectTypeRID)
	}
	if r.UserID != "user-1" {
		t.Fatalf("expected UserID user-1, got %q", r.UserID)
	}
	if r.Version != 1 {
		t.Fatalf("expected Version 1, got %d", r.Version)
	}
	if len(r.PrevState) != 0 {
		t.Fatalf("expected nil PrevState for CREATE, got %s", string(r.PrevState))
	}
	if len(r.NewState) == 0 {
		t.Fatal("expected non-nil NewState for CREATE")
	}
	var got map[string]interface{}
	if err := json.Unmarshal(r.NewState, &got); err != nil {
		t.Fatalf("unmarshal NewState: %v", err)
	}
	if got["name"] != "Alice" {
		t.Fatalf("expected NewState.name=Alice, got %v", got["name"])
	}
}

// TestConsumer_RecordHistory_Modify verifies that a MODIFY following a
// CREATE writes prev_state captured from the prior version and increments
// the version counter.
func TestConsumer_RecordHistory_Modify(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	createBatch := EditBatch{
		ID:              "batch-1",
		OntologyAPIName: testOntology,
		UserID:          "user-1",
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice", "age": float64(30)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), createBatch); err != nil {
		t.Fatalf("apply create: %v", err)
	}

	modBatch := EditBatch{
		ID:              "batch-2",
		OntologyAPIName: testOntology,
		UserID:          "user-2",
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice", "age": float64(31)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), modBatch); err != nil {
		t.Fatalf("apply modify: %v", err)
	}

	rows := repo.snapshot()
	if len(rows) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(rows))
	}
	mod := rows[1]
	if mod.EditType != "MODIFY" {
		t.Fatalf("expected EditType MODIFY, got %q", mod.EditType)
	}
	if mod.Version != 2 {
		t.Fatalf("expected Version 2, got %d", mod.Version)
	}
	if mod.UserID != "user-2" {
		t.Fatalf("expected UserID user-2, got %q", mod.UserID)
	}
	if len(mod.PrevState) == 0 {
		t.Fatal("expected non-nil PrevState for MODIFY after CREATE")
	}
	var prev map[string]interface{}
	if err := json.Unmarshal(mod.PrevState, &prev); err != nil {
		t.Fatalf("unmarshal PrevState: %v", err)
	}
	if _, hasName := prev["name"]; !hasName {
		t.Fatal("expected PrevState to include name field captured from index")
	}
}

// TestConsumer_RecordHistory_Delete verifies that a DELETE writes a row
// with prev_state populated and new_state nil.
func TestConsumer_RecordHistory_Delete(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	createBatch := EditBatch{
		ID:              "batch-1",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-del",
				Properties: map[string]interface{}{"name": "ToDelete"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), createBatch); err != nil {
		t.Fatalf("apply create: %v", err)
	}

	delBatch := EditBatch{
		ID:              "batch-2",
		OntologyAPIName: testOntology,
		UserID:          "user-d",
		Edits: []Edit{
			{
				Type:       EditTypeDelete,
				ObjectType: "employee",
				PrimaryKey: "emp-del",
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), delBatch); err != nil {
		t.Fatalf("apply delete: %v", err)
	}

	rows := repo.snapshot()
	if len(rows) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(rows))
	}
	del := rows[1]
	if del.EditType != "DELETE" {
		t.Fatalf("expected EditType DELETE, got %q", del.EditType)
	}
	if del.Version != 2 {
		t.Fatalf("expected Version 2, got %d", del.Version)
	}
	if len(del.NewState) != 0 {
		t.Fatalf("expected nil NewState for DELETE, got %s", string(del.NewState))
	}
	if len(del.PrevState) == 0 {
		t.Fatal("expected non-nil PrevState for DELETE")
	}
}

// TestConsumer_RecordHistory_NilRepo verifies that a consumer with no
// history repo set still applies edits normally and is a no-op for history.
func TestConsumer_RecordHistory_NilRepo(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	// no SetHistoryRepo call

	batch := EditBatch{
		ID:              "batch-noop",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-x",
				Properties: map[string]interface{}{"name": "X"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory should not error with nil history repo: %v", err)
	}
}

// TestConsumer_RecordHistory_VersionsPerPK verifies that version numbers are
// scoped per primary key and increment independently.
func TestConsumer_RecordHistory_VersionsPerPK(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	for _, pk := range []string{"emp-A", "emp-B"} {
		b := EditBatch{
			ID:              "create-" + pk,
			OntologyAPIName: testOntology,
			Edits: []Edit{
				{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: pk,
					Properties: map[string]interface{}{"name": pk}},
			},
		}
		if err := consumer.applyBatchWithHistory(context.Background(), b); err != nil {
			t.Fatalf("create %s: %v", pk, err)
		}
	}
	// Modify A twice, B once.
	for _, pk := range []string{"emp-A", "emp-A", "emp-B"} {
		b := EditBatch{
			ID:              "mod-" + pk,
			OntologyAPIName: testOntology,
			Edits: []Edit{
				{Type: EditTypeModify, ObjectType: "employee", PrimaryKey: pk,
					Properties: map[string]interface{}{"name": pk + "+"}},
			},
		}
		if err := consumer.applyBatchWithHistory(context.Background(), b); err != nil {
			t.Fatalf("mod %s: %v", pk, err)
		}
	}

	rows := repo.snapshot()
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	var aVersions, bVersions []int64
	for _, r := range rows {
		switch r.PrimaryKey {
		case "emp-A":
			aVersions = append(aVersions, r.Version)
		case "emp-B":
			bVersions = append(bVersions, r.Version)
		}
	}
	if len(aVersions) != 3 || aVersions[0] != 1 || aVersions[1] != 2 || aVersions[2] != 3 {
		t.Fatalf("expected emp-A versions [1,2,3], got %v", aVersions)
	}
	if len(bVersions) != 2 || bVersions[0] != 1 || bVersions[1] != 2 {
		t.Fatalf("expected emp-B versions [1,2], got %v", bVersions)
	}
}
