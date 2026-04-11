//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestProperty_IsEditOnlyRoundTrip is the US-025 acceptance scenario: once
// migration 000019 is applied the properties table carries an is_edit_only
// bool column (default false) and PGRepository CRUD round-trips the new
// Property.IsEditOnly field through Create / List / Get / Update with no
// data loss. The column default guarantees legacy rows read back as false.
func TestProperty_IsEditOnlyRoundTrip(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "editonly_property",
		DisplayName: "EditOnly Property Demo",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	order := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "Order",
		DisplayName: "Order",
		PrimaryKey:  "orderID",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, order); err != nil {
		t.Fatalf("create Order: %v", err)
	}

	// legacy property: is_edit_only defaulted on the column
	legacy := &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "freight",
		BaseType:      "double",
		IsSearchable:  true,
	}
	if err := repo.CreateProperty(ctx, legacy); err != nil {
		t.Fatalf("create legacy property: %v", err)
	}

	// edit-only property: set explicitly at create time
	notes := &oms.Property{
		RID:           rid.NewPropertyRID(),
		ObjectTypeRID: order.RID,
		APIName:       "notes",
		BaseType:      "string",
		IsSearchable:  true,
		IsEditOnly:    true,
	}
	if err := repo.CreateProperty(ctx, notes); err != nil {
		t.Fatalf("create edit-only property: %v", err)
	}

	// GetProperty reads both back with the right value
	gotLegacy, err := repo.GetProperty(ctx, legacy.RID)
	if err != nil {
		t.Fatalf("get legacy property: %v", err)
	}
	if gotLegacy.IsEditOnly {
		t.Errorf("legacy property IsEditOnly = true, want false")
	}

	gotNotes, err := repo.GetProperty(ctx, notes.RID)
	if err != nil {
		t.Fatalf("get notes property: %v", err)
	}
	if !gotNotes.IsEditOnly {
		t.Errorf("notes property IsEditOnly = false, want true")
	}

	// ListProperties preserves the flag
	props, err := repo.ListProperties(ctx, order.RID)
	if err != nil {
		t.Fatalf("list properties: %v", err)
	}
	byName := map[string]oms.Property{}
	for _, p := range props {
		byName[p.APIName] = p
	}
	if byName["freight"].IsEditOnly {
		t.Errorf("ListProperties freight.IsEditOnly = true, want false")
	}
	if !byName["notes"].IsEditOnly {
		t.Errorf("ListProperties notes.IsEditOnly = false, want true")
	}

	// UpdateProperty toggles the flag back off
	gotNotes.IsEditOnly = false
	if err := repo.UpdateProperty(ctx, gotNotes); err != nil {
		t.Fatalf("update notes property: %v", err)
	}
	reloaded, err := repo.GetProperty(ctx, notes.RID)
	if err != nil {
		t.Fatalf("reload notes property: %v", err)
	}
	if reloaded.IsEditOnly {
		t.Errorf("reloaded notes.IsEditOnly = true after update to false")
	}

	// UpdateProperty toggles it back on
	reloaded.IsEditOnly = true
	if err := repo.UpdateProperty(ctx, reloaded); err != nil {
		t.Fatalf("update notes property back to edit-only: %v", err)
	}
	reloaded2, err := repo.GetProperty(ctx, notes.RID)
	if err != nil {
		t.Fatalf("reload notes property: %v", err)
	}
	if !reloaded2.IsEditOnly {
		t.Errorf("reloaded notes.IsEditOnly = false after update back to true")
	}
}
