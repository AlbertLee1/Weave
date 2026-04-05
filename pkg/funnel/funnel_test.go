package funnel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/liyang/weave/pkg/index"
)

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
	if _, err := mgr.EnsureIndex("employee", props); err != nil {
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
		ID:     "batch-1",
		UserID: "user-1",
		Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
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

	if err := consumer.applyEdit(edit); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	count, err := mgr.DocCount("employee")
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
	if err := consumer.applyEdit(create); err != nil {
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
	if err := consumer.applyEdit(modify); err != nil {
		t.Fatalf("applyEdit modify: %v", err)
	}

	// Document count should still be 1 (same primary key)
	count, err := mgr.DocCount("employee")
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
	if err := consumer.applyEdit(create); err != nil {
		t.Fatalf("applyEdit create: %v", err)
	}

	// Delete
	del := Edit{
		Type:       EditTypeDelete,
		ObjectType: "employee",
		PrimaryKey: "emp-1",
	}
	if err := consumer.applyEdit(del); err != nil {
		t.Fatalf("applyEdit delete: %v", err)
	}

	count, err := mgr.DocCount("employee")
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

	err := consumer.applyEdit(edit)
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

	if err := consumer.applyEdit(edit); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	// Verify document was indexed with correct properties by searching
	count, err := mgr.DocCount("employee")
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected doc count 1, got %d", count)
	}

	// Search for the document by name field
	idx := mgr.GetIndex("employee")
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
		ID:     "batch-1",
		UserID: "user-1",
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
		if err := consumer.applyEdit(edit); err != nil {
			t.Fatalf("applyEdit: %v", err)
		}
	}

	count, err := mgr.DocCount("employee")
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
		ID:     "batch-empty",
		UserID: "user-1",
		Edits:  []Edit{},
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
		if err := consumer.applyEdit(edit); err != nil {
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

// --- Publisher tests (2) ---

func TestPublisher_SubjectFormat(t *testing.T) {
	subject := BuildSubject("employee")
	expected := "edits.employee"
	if subject != expected {
		t.Fatalf("expected subject %q, got %q", expected, subject)
	}

	subject = BuildSubject("project")
	expected = "edits.project"
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
