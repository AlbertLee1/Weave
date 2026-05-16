//go:build integration

package main

import (
	"context"
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

// TestBDD_US472_Given_UserEditedNotes_When_IngestModifies_Then_NotesPreserved
// exercises the exact production wiring path main.go installs for US-472:
//
//	res := newEditOnlyResolver(deps.OmsRepo)
//	_  = res.Refresh(ctx)
//	deps.FunnelConsumer.SetEditOnlyField(res.IsEditOnly)
//
// against a real testcontainers PG so any regression in the resolver's
// (objectType, field) -> IsEditOnly cache contract or the funnel
// preserveEditOnlyFields filter trips here. Distinct from the pre-existing
// test/integration/edit_only_test.go which wires an inline closure — that
// test exercises the funnel filter; this test exercises the prod-shaped
// resolver bridging OMS schema to the filter.
//
// Given an Order ObjectType with notes flagged IsEditOnly=true in OMS PG
//
//	And the production editOnlyResolver wired into the funnel consumer
//
// When the user CREATEs ord-1 with notes="VIP"
//
//	And ingest later MODIFYs ord-1 with notes="SPAM", status="shipped"
//	(ingest timestamp STRICTLY NEWER than the user edit so US-021's
//	 timestamp guard cannot mask the test — only the US-027 editOnly
//	 guard, wired by US-472, can preserve notes)
//
// Then the final bleve doc carries notes="VIP" and status="shipped",
//
//	proving the resolver correctly reported IsEditOnly(order, notes)=true
//	and that the consumer's preserveEditOnlyFields filter applied it.
func TestBDD_US472_Given_UserEditedNotes_When_IngestModifies_Then_NotesPreserved(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "us472",
		DisplayName: "US-472 Edit-Only Production Wiring",
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

	props := []*oms.Property{
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "orderID",
			BaseType:      "string",
			IsSearchable:  true,
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "status",
			BaseType:      "string",
			IsSearchable:  true,
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: order.RID,
			APIName:       "notes",
			BaseType:      "string",
			IsSearchable:  true,
			IsEditOnly:    true,
		},
	}
	for _, p := range props {
		if err := repo.CreateProperty(ctx, p); err != nil {
			t.Fatalf("create property %s: %v", p.APIName, err)
		}
	}

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() { _ = mgr.Close() })
	scopedKey := index.ScopedKey(ont.APIName, order.APIName)
	if _, err := mgr.EnsureIndex(scopedKey, []index.Property{
		{APIName: "orderID", BaseType: "string", IsSearchable: true},
		{APIName: "status", BaseType: "string", IsSearchable: true},
		{APIName: "notes", BaseType: "string", IsSearchable: true},
	}); err != nil {
		t.Fatalf("ensure index: %v", err)
	}

	// === PRODUCTION-SHAPED WIRING (mirrors main.go) ===
	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{order.APIName: order.RID})
	editOnlyRes := newEditOnlyResolver(repo)
	if err := editOnlyRes.Refresh(ctx); err != nil {
		t.Fatalf("editOnly refresh: %v", err)
	}
	if !editOnlyRes.IsEditOnly(order.APIName, "notes") {
		t.Fatal("resolver did not pick up IsEditOnly=true from PG — refresh broken")
	}
	if editOnlyRes.IsEditOnly(order.APIName, "status") {
		t.Fatal("resolver flagged status as editOnly — over-eager cache")
	}
	consumer.SetEditOnlyField(editOnlyRes.IsEditOnly)
	// === END WIRING ===

	userTS := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	// Strictly newer ingest timestamp ensures US-021's timestamp guard does
	// NOT silently cover for a missing US-472 wiring — the test fails
	// closed if the resolver path is bypassed.
	ingestTS := userTS.Add(1 * time.Hour)

	if err := consumer.ApplyBatch(ctx, funnel.EditBatch{
		ID:              "user-1",
		OntologyAPIName: ont.APIName,
		UserID:          "alice",
		Timestamp:       userTS,
		Edits: []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: order.APIName,
			PrimaryKey: "ord-1",
			Source:     funnel.EditSourceUser,
			Properties: map[string]interface{}{
				"orderID": "ord-1",
				"status":  "pending",
				"notes":   "VIP",
			},
		}},
	}); err != nil {
		t.Fatalf("user create: %v", err)
	}

	if err := consumer.ApplyBatch(ctx, funnel.EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: ont.APIName,
		UserID:          "ingest-svc",
		Timestamp:       ingestTS,
		Edits: []funnel.Edit{{
			Type:       funnel.EditTypeModify,
			ObjectType: order.APIName,
			PrimaryKey: "ord-1",
			Source:     funnel.EditSourceIngest,
			Properties: map[string]interface{}{
				"orderID": "ord-1",
				"status":  "shipped",
				"notes":   "SPAM",
			},
		}},
	}); err != nil {
		t.Fatalf("ingest modify: %v", err)
	}

	doc := us472FetchDoc(t, mgr, scopedKey, "ord-1")
	if got, _ := doc["notes"].(string); got != "VIP" {
		t.Fatalf("US-472 wiring broken: notes=%q, want VIP (ingest overwrote user edit)", got)
	}
	if got, _ := doc["status"].(string); got != "shipped" {
		t.Fatalf("non-editOnly status not updated: got %q, want shipped", got)
	}
	if got, _ := doc["orderID"].(string); got != "ord-1" {
		t.Fatalf("orderID lost: got %q, want ord-1", got)
	}
}

// TestBDD_US472_Given_PropertyFlagFlipped_When_RefreshFires_Then_GuardUpdates
// verifies the resolver picks up admin schema changes within one refresh
// cycle — the production wiring runs a 5-minute tick precisely so an admin
// who flips IsEditOnly via PUT /api/admin/.../properties/{rid} does not need
// a server restart. Compresses the cycle to a single explicit Refresh call
// for the test.
//
// Given an Order with notes IsEditOnly=true and the resolver refreshed
// When an admin clears IsEditOnly (PUT property) and the resolver refreshes
// Then ingest is once again allowed to overwrite notes.
func TestBDD_US472_Given_PropertyFlagFlipped_When_RefreshFires_Then_GuardUpdates(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "us472_flip",
		DisplayName: "US-472 Flag Flip",
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
	notesProp := &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "notes",
		BaseType:      "string",
		IsSearchable:  true,
		IsEditOnly:    true,
		// Status is DEFAULT 'ACTIVE' at INSERT time but UpdateProperty
		// re-writes every column, so the Go-side zero value must match the
		// CHECK constraint or the second-phase UPDATE trips SQLSTATE 23514.
		Status: "ACTIVE",
	}
	if err := repo.CreateProperty(ctx, notesProp); err != nil {
		t.Fatalf("create notes property: %v", err)
	}

	res := newEditOnlyResolver(repo)
	if err := res.Refresh(ctx); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if !res.IsEditOnly("order", "notes") {
		t.Fatal("first refresh: expected notes editOnly=true")
	}

	// Admin clears IsEditOnly.
	notesProp.IsEditOnly = false
	if err := repo.UpdateProperty(ctx, notesProp); err != nil {
		t.Fatalf("update property: %v", err)
	}

	if err := res.Refresh(ctx); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if res.IsEditOnly("order", "notes") {
		t.Fatal("second refresh: expected notes editOnly=false after admin cleared the flag")
	}
}

// us472FetchDoc loads the current bleve doc for (scopedKey, pk) as a flat
// map. Local helper rather than reusing fetchOrderDoc from test/integration
// (different package) so the BDD stays self-contained.
func us472FetchDoc(t *testing.T, mgr *index.Manager, scopedKey, pk string) map[string]interface{} {
	t.Helper()
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(scopedKey, req)
	if err != nil {
		t.Fatalf("search %q/%q: %v", scopedKey, pk, err)
	}
	if res == nil || res.Total == 0 {
		t.Fatalf("doc %q missing from %q", pk, scopedKey)
	}
	hit := res.Hits[0]
	out := make(map[string]interface{}, len(hit.Fields))
	for k, v := range hit.Fields {
		out[k] = v
	}
	return out
}
