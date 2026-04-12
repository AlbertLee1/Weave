//go:build integration

package phase7_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestPhase7_EditConflictPolicy is a Phase 7 cross-US gate test (US-076).
// It combines Phase 6 edit-only preservation (US-027) with a write-level
// property filter mimicking Phase 7 column-level security policy enforcement.
//
// Scenario:
//   - Order has fields: orderID (pk), status, amount, notes (IsEditOnly=true)
//   - A write-level property filter grants the "ingest" role write access only
//     to Order.notes — status and amount are NOT writable by ingest.
//   - A user seeds 20 orders with notes="VIP", status="pending", amount=100.
//   - 5 rounds of ingest edits try to write ALL four fields with new values.
//
// Expected outcome (both guards compose):
//   - status / amount writes are stripped by the writable-property filter
//     (ingest has no write access to those fields per policy).
//   - notes writes are stripped by the edit-only guard (user value "VIP"
//     survives regardless of ingest attempting to overwrite it).
//   - orderID (PK) is preserved because it's the identity column and ingest
//     rewrites don't change it.
//   - All user-set values remain intact after the ingest flood.
func TestPhase7_EditConflictPolicy(t *testing.T) {
	ctx := context.Background()

	// --- Infrastructure: PG + migrations ---
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	// --- OMS: ontology + ObjectType + Properties ---
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "phase7_edit_conflict_policy",
		DisplayName: "Phase 7 Edit Conflict x Policy",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	order := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "order",
		DisplayName: "Order",
		PrimaryKey:  "orderID",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, order); err != nil {
		t.Fatalf("create order: %v", err)
	}

	for _, p := range []oms.Property{
		{RID: rid.NewPropertyRID(), ObjectTypeRID: order.RID, APIName: "orderID", BaseType: "string", IsSearchable: true},
		{RID: rid.NewPropertyRID(), ObjectTypeRID: order.RID, APIName: "status", BaseType: "string", IsSearchable: true},
		{RID: rid.NewPropertyRID(), ObjectTypeRID: order.RID, APIName: "amount", BaseType: "double", IsSearchable: true},
		{RID: rid.NewPropertyRID(), ObjectTypeRID: order.RID, APIName: "notes", BaseType: "string", IsSearchable: true, IsEditOnly: true},
	} {
		prop := p
		if err := repo.CreateProperty(ctx, &prop); err != nil {
			t.Fatalf("create property %s: %v", prop.APIName, err)
		}
	}

	// --- Bleve index ---
	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})
	scopedKey := index.ScopedKey(ont.APIName, order.APIName)
	indexProps := []index.Property{
		{APIName: "orderID", BaseType: "string", IsSearchable: true},
		{APIName: "status", BaseType: "string", IsSearchable: true},
		{APIName: "amount", BaseType: "double", IsSearchable: true},
		{APIName: "notes", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(scopedKey, indexProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// --- Funnel consumer with all guards wired ---
	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		order.APIName: order.RID,
	})

	// Edit-only guard: notes is user-managed.
	editOnlyFields := map[string]bool{"notes": true}
	consumer.SetEditOnlyField(func(objectType, field string) bool {
		if objectType != order.APIName {
			return false
		}
		return editOnlyFields[field]
	})

	// Write-level property filter (mimics column-level security policy):
	// ingest role can ONLY write to "notes". All other fields (status,
	// amount) are stripped from ingest edits.
	writableForIngest := map[string]bool{"notes": true, "orderID": true}
	consumer.SetWritablePropertyFilter(func(objectType, field string) bool {
		if objectType != order.APIName {
			return true // no filter for other types
		}
		return writableForIngest[field]
	})

	// ------------------------------------------------------------------
	// Step 1: User seeds 20 orders with known values.
	// ------------------------------------------------------------------
	const orderCount = 20
	userTS := time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)
	primaryKeys := make([]string, 0, orderCount)
	userEdits := make([]funnel.Edit, 0, orderCount)
	for i := 0; i < orderCount; i++ {
		pk := fmt.Sprintf("ord-%03d", i)
		primaryKeys = append(primaryKeys, pk)
		userEdits = append(userEdits, funnel.Edit{
			Type:       funnel.EditTypeCreate,
			ObjectType: order.APIName,
			PrimaryKey: pk,
			Source:     funnel.EditSourceUser,
			Properties: map[string]interface{}{
				"orderID": pk,
				"status":  "pending",
				"amount":  float64(100),
				"notes":   "VIP",
			},
		})
	}
	if err := consumer.ApplyBatch(ctx, funnel.EditBatch{
		ID:              "user-seed",
		OntologyAPIName: ont.APIName,
		UserID:          "alice",
		Timestamp:       userTS,
		Edits:           userEdits,
	}); err != nil {
		t.Fatalf("user seed batch: %v", err)
	}

	// ------------------------------------------------------------------
	// Step 2: Ingest flood — 5 rounds, each writing ALL fields with new
	// values. The writable-property filter should strip status + amount;
	// the edit-only guard should preserve notes="VIP".
	// ------------------------------------------------------------------
	const ingestRounds = 5
	for round := 0; round < ingestRounds; round++ {
		ingestTS := userTS.Add(time.Duration(round+1) * time.Hour)
		ingestEdits := make([]funnel.Edit, 0, orderCount)
		for _, pk := range primaryKeys {
			ingestEdits = append(ingestEdits, funnel.Edit{
				Type:       funnel.EditTypeModify,
				ObjectType: order.APIName,
				PrimaryKey: pk,
				Source:     funnel.EditSourceIngest,
				Properties: map[string]interface{}{
					"orderID": pk,
					"status":  fmt.Sprintf("shipped-r%d", round),
					"amount":  float64(999 + round),
					"notes":   fmt.Sprintf("OVERWRITTEN-r%d", round),
				},
			})
		}
		if err := consumer.ApplyBatch(ctx, funnel.EditBatch{
			ID:              fmt.Sprintf("ingest-%d", round),
			OntologyAPIName: ont.APIName,
			UserID:          "ingest-svc",
			Timestamp:       ingestTS,
			Edits:           ingestEdits,
		}); err != nil {
			t.Fatalf("ingest round %d: %v", round, err)
		}
	}

	// ------------------------------------------------------------------
	// Assertions: every order must retain its user-set values.
	// ------------------------------------------------------------------
	for _, pk := range primaryKeys {
		doc := fetchDocP7Conflict(t, mgr, scopedKey, pk)

		// notes="VIP": editOnly guard protects user value from ingest overwrite.
		if got, _ := doc["notes"].(string); got != "VIP" {
			t.Errorf("notes[%s] = %q, want %q (editOnly guard failed)", pk, got, "VIP")
		}

		// status="pending": writable-property filter stripped ingest writes.
		if got, _ := doc["status"].(string); got != "pending" {
			t.Errorf("status[%s] = %q, want %q (policy filter leaked)", pk, got, "pending")
		}

		// amount=100: writable-property filter stripped ingest writes.
		if got, ok := doc["amount"]; !ok {
			t.Errorf("amount[%s] missing from bleve doc", pk)
		} else if amt, ok := got.(float64); !ok || amt != 100 {
			t.Errorf("amount[%s] = %v, want 100 (policy filter leaked)", pk, got)
		}

		// orderID preserved as identity column.
		if got, _ := doc["orderID"].(string); got != pk {
			t.Errorf("orderID[%s] = %q, want %q", pk, got, pk)
		}
	}
}

// fetchDocP7Conflict reads the current bleve doc for (scopedKey, pk) as a
// flat map. Returns an empty map when the doc is missing so assertions fail
// with clear mismatches instead of panics.
func fetchDocP7Conflict(t *testing.T, mgr *index.Manager, scopedKey, pk string) map[string]interface{} {
	t.Helper()
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(scopedKey, req)
	if err != nil {
		t.Fatalf("fetchDoc %s: %v", pk, err)
	}
	if res == nil || res.Total == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(res.Hits[0].Fields))
	for k, v := range res.Hits[0].Fields {
		out[k] = v
	}
	return out
}
