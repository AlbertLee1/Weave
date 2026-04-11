//go:build integration

package phase6_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestPhase6_EditOnlyIngest is the Phase 6 cross-US gate test for US-037.
// It verifies that Order.notes (marked IsEditOnly=true per US-027) survives
// a sustained ingest flood: 50 user-written "VIP" notes, then 10 rounds of
// ingest updates each writing empty notes across all 50 orders. Every
// order must still report notes="VIP" at the end. Non-editOnly fields
// (status) are allowed to change and serve as a liveness check: the ingest
// path reached bleve, but the guard surgically rewrote the notes column
// to its pre-ingest user value.
//
// Unlike the existing US-027 single-row integration test, this scenario
// pressurises the edit-only hook at scale (500 ingest edits) and in the
// presence of a mixed-field ingest payload, so any regression that leaks
// empty notes for even a single order trips the final assertion loop.
func TestPhase6_EditOnlyIngest(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "phase6_editonly_ingest",
		DisplayName: "Phase 6 EditOnly Ingest",
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
		t.Fatalf("create Order: %v", err)
	}

	// orderID + status are normal columns; notes is IsEditOnly=true.
	if err := repo.CreateProperty(ctx, &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "orderID",
		BaseType:      "string",
		IsSearchable:  true,
	}); err != nil {
		t.Fatalf("create orderID property: %v", err)
	}
	if err := repo.CreateProperty(ctx, &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "status",
		BaseType:      "string",
		IsSearchable:  true,
	}); err != nil {
		t.Fatalf("create status property: %v", err)
	}
	if err := repo.CreateProperty(ctx, &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "notes",
		BaseType:      "string",
		IsSearchable:  true,
		IsEditOnly:    true,
	}); err != nil {
		t.Fatalf("create notes property: %v", err)
	}

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
		{APIName: "notes", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(scopedKey, indexProps); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		order.APIName: order.RID,
	})

	// Cache the notes.IsEditOnly lookup once per test — ListProperties
	// inside the hook would otherwise fire 500+ times during the flood.
	editOnlyFields := map[string]bool{}
	props, err := repo.ListProperties(ctx, order.RID)
	if err != nil {
		t.Fatalf("list properties: %v", err)
	}
	for _, p := range props {
		if p.IsEditOnly {
			editOnlyFields[p.APIName] = true
		}
	}
	consumer.SetEditOnlyField(func(objectType, field string) bool {
		if objectType != order.APIName {
			return false
		}
		return editOnlyFields[field]
	})

	// ------------------------------------------------------------------
	// Step 1: user seeds 50 orders, each with notes="VIP".
	// ------------------------------------------------------------------
	const orderCount = 50
	userTS := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
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
	// Step 2: 10 ingest flood rounds, each writing empty notes across
	// every order with a strictly newer timestamp than the user seed.
	// status is also written so we can confirm the ingest pipeline is
	// live (non-editOnly fields must flow through).
	// ------------------------------------------------------------------
	const ingestRounds = 10
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
					"notes":   "",
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
	// Assertions: every order must still report notes="VIP". The last
	// ingest round's status value must have landed so the flood wasn't
	// silently discarded by some unrelated guard.
	// ------------------------------------------------------------------
	wantStatus := fmt.Sprintf("shipped-r%d", ingestRounds-1)
	for _, pk := range primaryKeys {
		doc := fetchDocP6(t, mgr, scopedKey, pk)
		if got, _ := doc["notes"].(string); got != "VIP" {
			t.Errorf("notes[%s] = %q, want %q (editOnly leak)", pk, got, "VIP")
		}
		if got, _ := doc["status"].(string); got != wantStatus {
			t.Errorf("status[%s] = %q, want %q (ingest did not propagate)", pk, got, wantStatus)
		}
		if got, _ := doc["orderID"].(string); got != pk {
			t.Errorf("orderID[%s] = %q, want %q", pk, got, pk)
		}
	}
}
