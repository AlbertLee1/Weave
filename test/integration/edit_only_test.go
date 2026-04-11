//go:build integration

package integration_test

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

// TestEditOnly_IngestCannotOverwriteUserNotes is the US-027 acceptance
// scenario: an Order.notes Property marked IsEditOnly=true must preserve
// the user-written value against any ingest attempt, regardless of batch
// timestamps. The test boots a real PG via testcontainers, persists the
// property flag via the OMS repository, then wires the funnel Consumer's
// editOnly hook to resolve IsEditOnly from live schema. A user CREATE
// writes notes="VIP"; a strictly newer ingest MODIFY tries to set
// notes="SPAM" alongside status="shipped"; the final Bleve doc must
// carry notes="VIP" and status="shipped".
func TestEditOnly_IngestCannotOverwriteUserNotes(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "edit_only",
		DisplayName: "Edit Only",
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

	// orderID + status are normal columns; notes is IsEditOnly=true, the
	// field US-027 protects.
	orderIDProp := &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "orderID",
		BaseType:      "string",
		IsSearchable:  true,
	}
	if err := repo.CreateProperty(ctx, orderIDProp); err != nil {
		t.Fatalf("create orderID property: %v", err)
	}
	statusProp := &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "status",
		BaseType:      "string",
		IsSearchable:  true,
	}
	if err := repo.CreateProperty(ctx, statusProp); err != nil {
		t.Fatalf("create status property: %v", err)
	}
	notesProp := &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "notes",
		BaseType:      "string",
		IsSearchable:  true,
		IsEditOnly:    true,
	}
	if err := repo.CreateProperty(ctx, notesProp); err != nil {
		t.Fatalf("create notes property: %v", err)
	}

	// Bleve index for the Order object type.
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

	// editOnly hook resolves against live OMS schema by listing the
	// ObjectType's properties per lookup. This mirrors how main.go will
	// eventually wire the production hook: one repo call per (ot, field)
	// probe, cached when perf matters.
	consumer.SetEditOnlyField(func(objectType, field string) bool {
		if objectType != order.APIName {
			return false
		}
		props, err := repo.ListProperties(ctx, order.RID)
		if err != nil {
			t.Logf("list properties: %v", err)
			return false
		}
		for _, p := range props {
			if p.APIName == field {
				return p.IsEditOnly
			}
		}
		return false
	})

	userTS := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	// Ingest is STRICTLY newer than the user edit — US-021's timestamp
	// guard would NOT protect notes here; only the US-027 edit-only guard
	// can.
	ingestTS := userTS.Add(1 * time.Hour)

	userCreate := funnel.EditBatch{
		ID:              "user-1",
		OntologyAPIName: ont.APIName,
		UserID:          "alice",
		Timestamp:       userTS,
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: order.APIName,
				PrimaryKey: "ord-1",
				Source:     funnel.EditSourceUser,
				Properties: map[string]interface{}{
					"orderID": "ord-1",
					"status":  "pending",
					"notes":   "VIP",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(ctx, userCreate); err != nil {
		t.Fatalf("user create: %v", err)
	}

	ingestMod := funnel.EditBatch{
		ID:              "ingest-1",
		OntologyAPIName: ont.APIName,
		UserID:          "ingest-svc",
		Timestamp:       ingestTS,
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeModify,
				ObjectType: order.APIName,
				PrimaryKey: "ord-1",
				Source:     funnel.EditSourceIngest,
				Properties: map[string]interface{}{
					"orderID": "ord-1",
					"status":  "shipped",
					"notes":   "SPAM",
				},
			},
		},
	}
	if err := consumer.ApplyBatch(ctx, ingestMod); err != nil {
		t.Fatalf("ingest mod: %v", err)
	}

	doc := fetchOrderDoc(t, mgr, ont.APIName, order.APIName, "ord-1")
	if got, _ := doc["notes"].(string); got != "VIP" {
		t.Fatalf("editOnly notes overwritten: got %q, want VIP", got)
	}
	if got, _ := doc["status"].(string); got != "shipped" {
		t.Fatalf("non-editOnly status not updated: got %q, want shipped", got)
	}
	if got, _ := doc["orderID"].(string); got != "ord-1" {
		t.Fatalf("orderID lost: got %q, want ord-1", got)
	}
}

// fetchOrderDoc loads the current bleve doc for (ontology, objectType, pk)
// as a flat map. Mirrors the (unexported) funnel.Consumer.fetchDocument
// helper via the exported index.Manager.Search path.
func fetchOrderDoc(t *testing.T, mgr *index.Manager, ontology, objectType, pk string) map[string]interface{} {
	t.Helper()
	scoped := index.ScopedKey(ontology, objectType)
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(scoped, req)
	if err != nil {
		t.Fatalf("search %q/%q: %v", scoped, pk, err)
	}
	if res == nil || res.Total == 0 {
		t.Fatalf("doc %q missing from %q", pk, scoped)
	}
	hit := res.Hits[0]
	if hit == nil || len(hit.Fields) == 0 {
		t.Fatalf("doc %q has no fields", pk)
	}
	out := make(map[string]interface{}, len(hit.Fields))
	for k, v := range hit.Fields {
		out[k] = v
	}
	return out
}
