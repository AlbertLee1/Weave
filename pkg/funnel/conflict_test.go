package funnel

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
)

// TestUserEditWinsProtection verifies the US-021 conflict resolution path:
// an ingest batch whose timestamp is older than the latest user edit for a
// given (objectType, primaryKey) must NOT overwrite the existing user state.
// The baseline (US-020) wrote ingest and user edits indiscriminately, so this
// test drives the consumer to consult the history repo before applying an
// ingest edit.
func TestUserEditWinsProtection(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	ctx := context.Background()
	userTS := time.Now()
	ingestTS := userTS.Add(-1 * time.Hour) // ingest batch claims an older point in time

	// 1) User creates the object (source=user). The history row drives the
	//    latest-user-edit lookup the consumer will use in step 2.
	userCreate := EditBatch{
		ID:              "user-1",
		OntologyAPIName: testOntology,
		UserID:          "alice",
		Timestamp:       userTS,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{
					"name": "Alice",
					"age":  float64(30),
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	// 2) Ingest tries to overwrite with stale data. The consumer must skip
	//    all non-always-apply fields.
	ingestMod := EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: testOntology,
		UserID:          "ingest-svc",
		Timestamp:       ingestTS,
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Source:     EditSourceIngest,
				Properties: map[string]interface{}{
					"name": "WRONG",
					"age":  float64(99),
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestMod); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}

	doc := consumer.fetchDocument(testOntology, "employee", "emp-1")
	if doc == nil {
		t.Fatal("expected doc to still exist after ingest attempt")
	}
	if doc["name"] != "Alice" {
		t.Fatalf("user edit overwritten: name=%v, want Alice", doc["name"])
	}
	if f, _ := doc["age"].(float64); f != 30 {
		t.Fatalf("user edit overwritten: age=%v, want 30", doc["age"])
	}
}

// TestUserEditOlder_NormalOverwrite verifies that when the latest user edit
// is older than the ingest batch timestamp, the ingest overwrite still
// applies normally — i.e., the user-edit-wins guard does NOT block fresh
// ingest data.
func TestUserEditOlder_NormalOverwrite(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	ctx := context.Background()
	userTS := time.Now().Add(-1 * time.Hour)
	ingestTS := time.Now()

	userCreate := EditBatch{
		ID:              "user-1",
		OntologyAPIName: testOntology,
		Timestamp:       userTS,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-2",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{"name": "Bob", "age": float64(20)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	ingestMod := EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: testOntology,
		Timestamp:       ingestTS,
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "employee",
				PrimaryKey: "emp-2",
				Source:     EditSourceIngest,
				Properties: map[string]interface{}{"name": "Bobby", "age": float64(21)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestMod); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}

	doc := consumer.fetchDocument(testOntology, "employee", "emp-2")
	if doc == nil {
		t.Fatal("doc missing")
	}
	if doc["name"] != "Bobby" {
		t.Fatalf("expected fresh ingest value 'Bobby', got %v", doc["name"])
	}
	if f, _ := doc["age"].(float64); f != 21 {
		t.Fatalf("expected fresh ingest age 21, got %v", doc["age"])
	}
}

// TestConflict_AlwaysApplyStub verifies that fields marked as always-apply
// via the alwaysApplyField hook still overwrite user state even when the
// user edit is newer. US-026 will wire this hook to the is_edit_only schema
// column; US-021 only validates that the hook is respected.
func TestConflict_AlwaysApplyStub(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})
	consumer.SetAlwaysApplyField(func(objectType, field string) bool {
		return objectType == "employee" && field == "age"
	})

	ctx := context.Background()
	userTS := time.Now()
	ingestTS := userTS.Add(-1 * time.Hour)

	userCreate := EditBatch{
		ID:              "user-1",
		OntologyAPIName: testOntology,
		Timestamp:       userTS,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-3",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{"name": "Carol", "age": float64(40)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	ingestMod := EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: testOntology,
		Timestamp:       ingestTS,
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "employee",
				PrimaryKey: "emp-3",
				Source:     EditSourceIngest,
				Properties: map[string]interface{}{"name": "WRONG", "age": float64(99)},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestMod); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}

	doc := consumer.fetchDocument(testOntology, "employee", "emp-3")
	if doc == nil {
		t.Fatal("doc missing")
	}
	if doc["name"] != "Carol" {
		t.Fatalf("expected protected user value 'Carol', got %v", doc["name"])
	}
	if f, _ := doc["age"].(float64); f != 99 {
		t.Fatalf("expected always-apply ingest age 99, got %v", doc["age"])
	}
}

// TestEditOnlyAlwaysWins is the US-027 acceptance scenario: fields marked
// IsEditOnly=true must preserve the user value regardless of ingest, even
// when the ingest batch is strictly newer than the user edit (which would
// normally let ingest win because the US-021 user-edit-wins guard only
// kicks in when LatestUserEditAt > batch.Timestamp). The editOnly guard
// runs unconditionally for every ingest CREATE/MODIFY, so the Order.notes
// field seeded by a user write survives every ingest attempt to overwrite
// it while non-editOnly fields still get updated from ingest.
func TestEditOnlyAlwaysWins(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { _ = mgr.Close() })

	props := []index.Property{
		{APIName: "orderID", BaseType: "string", IsSearchable: true},
		{APIName: "status", BaseType: "string", IsSearchable: true},
		{APIName: "notes", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, "order"), props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	consumer := &Consumer{
		indexMgr:      mgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
	consumer.SetEditOnlyField(func(objectType, field string) bool {
		return objectType == "order" && field == "notes"
	})
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"order": "ri.ontology.main.object-type.order",
	})

	ctx := context.Background()
	// User is OLDER than ingest — US-021's timestamp guard does NOT
	// protect the user value here; only the US-027 editOnly guard can.
	userTS := time.Now().Add(-1 * time.Hour)
	ingestTS := time.Now()

	userCreate := EditBatch{
		ID:              "user-1",
		OntologyAPIName: testOntology,
		UserID:          "alice",
		Timestamp:       userTS,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "order",
				PrimaryKey: "ord-1",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{
					"orderID": "ord-1",
					"status":  "pending",
					"notes":   "VIP",
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	ingestMod := EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: testOntology,
		UserID:          "ingest-svc",
		Timestamp:       ingestTS,
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "order",
				PrimaryKey: "ord-1",
				Source:     EditSourceIngest,
				Properties: map[string]interface{}{
					"orderID": "ord-1",
					"status":  "shipped",
					"notes":   "SPAM", // must be stripped
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestMod); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}

	doc := consumer.fetchDocument(testOntology, "order", "ord-1")
	if doc == nil {
		t.Fatal("expected order doc to exist after ingest")
	}
	if got, _ := doc["notes"].(string); got != "VIP" {
		t.Fatalf("editOnly notes overwritten: got %q, want VIP", got)
	}
	if got, _ := doc["status"].(string); got != "shipped" {
		t.Fatalf("non-editOnly status not updated: got %q, want shipped", got)
	}
}

// TestEditOnly_IngestOmitsField verifies that when an ingest edit does NOT
// explicitly send the editOnly field, the field survives on the Bleve doc
// via the fetch+merge path. Otherwise a bleve upsert would silently drop
// editOnly state from the full-doc replacement.
func TestEditOnly_IngestOmitsField(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { _ = mgr.Close() })

	props := []index.Property{
		{APIName: "orderID", BaseType: "string", IsSearchable: true},
		{APIName: "status", BaseType: "string", IsSearchable: true},
		{APIName: "notes", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, "order"), props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	consumer := &Consumer{
		indexMgr:      mgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
	consumer.SetEditOnlyField(func(objectType, field string) bool {
		return objectType == "order" && field == "notes"
	})
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"order": "ri.ontology.main.object-type.order",
	})
	ctx := context.Background()

	userCreate := EditBatch{
		ID:              "user-1",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now().Add(-1 * time.Hour),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "order",
				PrimaryKey: "ord-2",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{
					"orderID": "ord-2",
					"status":  "pending",
					"notes":   "HANDLE WITH CARE",
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	// Ingest update that does NOT send "notes" at all.
	ingestMod := EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: testOntology,
		Timestamp:       time.Now(),
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "order",
				PrimaryKey: "ord-2",
				Source:     EditSourceIngest,
				Properties: map[string]interface{}{
					"orderID": "ord-2",
					"status":  "delivered",
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestMod); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}

	doc := consumer.fetchDocument(testOntology, "order", "ord-2")
	if doc == nil {
		t.Fatal("doc missing")
	}
	if got, _ := doc["notes"].(string); got != "HANDLE WITH CARE" {
		t.Fatalf("editOnly notes silently dropped by ingest upsert: got %q", got)
	}
	if got, _ := doc["status"].(string); got != "delivered" {
		t.Fatalf("non-editOnly status not updated: got %q", got)
	}
}

// TestConflict_NoHistoryRepo verifies that the conflict resolver is a no-op
// when history recording is disabled (legacy / boot-time fallback). Ingest
// edits should apply with no protection.
func TestConflict_NoHistoryRepo(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	// no SetHistoryRepo

	ctx := context.Background()
	createBatch := EditBatch{
		ID:              "u1",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-4",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{"name": "Dave"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, createBatch); err != nil {
		t.Fatalf("user create: %v", err)
	}
	ingestBatch := EditBatch{
		ID:              "i1",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeModify,
				ObjectType: "employee",
				PrimaryKey: "emp-4",
				Source:     EditSourceIngest,
				Properties: map[string]interface{}{"name": "Dave2"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestBatch); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}
	doc := consumer.fetchDocument(testOntology, "employee", "emp-4")
	if doc["name"] != "Dave2" {
		t.Fatalf("expected unprotected overwrite to Dave2, got %v", doc["name"])
	}
}

// TestConflict_DeleteSkippedWhenUserNewer verifies that a stale ingest DELETE
// cannot erase an object protected by a newer user edit.
func TestConflict_DeleteSkippedWhenUserNewer(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	ctx := context.Background()
	userTS := time.Now()
	ingestTS := userTS.Add(-1 * time.Hour)

	userCreate := EditBatch{
		ID:              "u1",
		OntologyAPIName: testOntology,
		Timestamp:       userTS,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-5",
				Source:     EditSourceUser,
				Properties: map[string]interface{}{"name": "Eve"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	ingestDelete := EditBatch{
		ID:              "i1",
		OntologyAPIName: testOntology,
		Timestamp:       ingestTS,
		Edits: []Edit{
			{
				Type:       EditTypeDelete,
				ObjectType: "employee",
				PrimaryKey: "emp-5",
				Source:     EditSourceIngest,
			},
		},
	}
	if err := consumer.applyBatchWithHistory(ctx, ingestDelete); err != nil {
		t.Fatalf("ingest delete: %v", err)
	}

	doc := consumer.fetchDocument(testOntology, "employee", "emp-5")
	if doc == nil {
		t.Fatal("user object was deleted by stale ingest")
	}
	if doc["name"] != "Eve" {
		t.Fatalf("user state corrupted: %v", doc)
	}
}
