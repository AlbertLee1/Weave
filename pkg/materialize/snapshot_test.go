package materialize

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
)

// writeBatch is a tiny helper that materialises one batch via a fresh
// Materializer for the snapshot tests.
func writeBatch(t *testing.T, m *Materializer, ts time.Time, edits ...funnel.Edit) {
	t.Helper()
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-" + ts.Format("150405"),
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits:           edits,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
}

func sortByPK(rows []SnapshotRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].PrimaryKey < rows[j].PrimaryKey })
}

func TestBuildSnapshot_EmptyDirectory(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

func TestBuildSnapshot_RejectsBlankObjectType(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	if _, err := m.BuildSnapshot(context.Background(), "northwind", "", time.Time{}); err == nil {
		t.Fatal("expected error for blank objectType")
	}
}

func TestBuildSnapshot_RejectsBlankOntology(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	if _, err := m.BuildSnapshot(context.Background(), "", "Customer", time.Time{}); err == nil {
		t.Fatal("expected error for blank ontology")
	}
}

func TestBuildSnapshot_SingleCreate(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	ts := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	writeBatch(t, m, ts, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice"},
		Markings:   []string{"PUBLIC"},
	})

	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.PrimaryKey != "C-1" {
		t.Fatalf("unexpected pk %q", got.PrimaryKey)
	}
	if got.Properties["name"] != "Alice" {
		t.Fatalf("expected name=Alice, got %v", got.Properties)
	}
	if len(got.Markings) != 1 || got.Markings[0] != "PUBLIC" {
		t.Fatalf("expected markings=[PUBLIC], got %v", got.Markings)
	}
	if got.TimestampMs != ts.UnixMilli() {
		t.Fatalf("expected ts=%d, got %d", ts.UnixMilli(), got.TimestampMs)
	}
	if got.PatchOffset == 0 {
		t.Fatalf("expected non-zero patch offset")
	}
}

func TestBuildSnapshot_KeepsLatestOffsetPerPrimaryKey(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	t3 := t1.Add(2 * time.Minute)

	writeBatch(t, m, t1, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice", "city": "NY"},
	})
	writeBatch(t, m, t2, funnel.Edit{
		Type:       funnel.EditTypeModify,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice II"},
	})
	writeBatch(t, m, t3, funnel.Edit{
		Type:       funnel.EditTypeModify,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice III"},
	})

	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row (deduped), got %d", len(rows))
	}
	if rows[0].Properties["name"] != "Alice III" {
		t.Fatalf("expected latest name=Alice III, got %v", rows[0].Properties)
	}
}

func TestBuildSnapshot_DroppedAfterDelete(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	writeBatch(t, m, t1, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice"},
	})
	writeBatch(t, m, t2, funnel.Edit{
		Type:       funnel.EditTypeDelete,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
	})

	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows after delete, got %d: %+v", len(rows), rows)
	}
}

func TestBuildSnapshot_RecreateAfterDeleteSurfaces(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	t3 := t1.Add(2 * time.Minute)

	writeBatch(t, m, t1, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice"},
	})
	writeBatch(t, m, t2, funnel.Edit{
		Type:       funnel.EditTypeDelete,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
	})
	writeBatch(t, m, t3, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice rejoined"},
	})

	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Properties["name"] != "Alice rejoined" {
		t.Fatalf("expected name=Alice rejoined, got %v", rows[0].Properties)
	}
}

func TestBuildSnapshot_AsOfCutoffExcludesNewerRows(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	t3 := t1.Add(2 * time.Minute)

	writeBatch(t, m, t1, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice v1"},
	})
	writeBatch(t, m, t2, funnel.Edit{
		Type:       funnel.EditTypeModify,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "Alice v2"},
	})
	writeBatch(t, m, t3, funnel.Edit{
		Type:       funnel.EditTypeDelete,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
	})

	// Cutoff at t2: t3 delete is excluded so the row is still alive.
	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", t2)
	if err != nil {
		t.Fatalf("BuildSnapshot t2: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row at t2, got %d", len(rows))
	}
	if rows[0].Properties["name"] != "Alice v2" {
		t.Fatalf("expected v2 at t2 cutoff, got %v", rows[0].Properties)
	}

	// Cutoff at t1: only the original create is visible.
	rows, err = m.BuildSnapshot(context.Background(), "northwind", "Customer", t1)
	if err != nil {
		t.Fatalf("BuildSnapshot t1: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row at t1, got %d", len(rows))
	}
	if rows[0].Properties["name"] != "Alice v1" {
		t.Fatalf("expected v1 at t1 cutoff, got %v", rows[0].Properties)
	}

	// Cutoff at t3: the delete is now visible so the row is gone.
	rows, err = m.BuildSnapshot(context.Background(), "northwind", "Customer", t3)
	if err != nil {
		t.Fatalf("BuildSnapshot t3: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows at t3 cutoff, got %d", len(rows))
	}
}

func TestBuildSnapshot_MultiplePrimaryKeys(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	t3 := t1.Add(2 * time.Minute)

	writeBatch(t, m, t1,
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-2", Properties: map[string]interface{}{"name": "Bob"}},
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-3", Properties: map[string]interface{}{"name": "Carol"}},
	)
	writeBatch(t, m, t2,
		funnel.Edit{Type: funnel.EditTypeDelete, ObjectType: "Customer", PrimaryKey: "C-2"},
		funnel.Edit{Type: funnel.EditTypeModify, ObjectType: "Customer", PrimaryKey: "C-3", Properties: map[string]interface{}{"name": "Carol II"}},
	)
	writeBatch(t, m, t3,
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-4", Properties: map[string]interface{}{"name": "Dave"}},
	)

	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	sortByPK(rows)
	if len(rows) != 3 {
		t.Fatalf("expected 3 alive rows (C-1, C-3, C-4), got %d: %+v", len(rows), rows)
	}
	expectedNames := map[string]string{"C-1": "Alice", "C-3": "Carol II", "C-4": "Dave"}
	for _, r := range rows {
		if expectedNames[r.PrimaryKey] != r.Properties["name"] {
			t.Fatalf("pk %s: expected %q, got %v", r.PrimaryKey, expectedNames[r.PrimaryKey], r.Properties["name"])
		}
	}
}

func TestBuildSnapshot_IgnoresOtherObjectTypes(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	ts := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	writeBatch(t, m, ts,
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
		funnel.Edit{Type: funnel.EditTypeCreate, ObjectType: "Order", PrimaryKey: "O-1", Properties: map[string]interface{}{"total": 12.5}},
	)

	customers, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot Customer: %v", err)
	}
	if len(customers) != 1 || customers[0].PrimaryKey != "C-1" {
		t.Fatalf("expected only Customer row, got %+v", customers)
	}

	orders, err := m.BuildSnapshot(context.Background(), "northwind", "Order", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot Order: %v", err)
	}
	if len(orders) != 1 || orders[0].PrimaryKey != "O-1" {
		t.Fatalf("expected only Order row, got %+v", orders)
	}
}

// TestBuildSnapshot_OffsetWinsOverFilenameOrdering proves that even when
// two batches land in the same wall-clock second (so filenames may sort
// in either order), the higher __patch_offset wins. This is the core
// "later wins" tie-breaker the PRD demands.
func TestBuildSnapshot_OffsetWinsOverFilenameOrdering(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	ts := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)

	writeBatch(t, m, ts, funnel.Edit{
		Type:       funnel.EditTypeCreate,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "first"},
	})
	writeBatch(t, m, ts, funnel.Edit{
		Type:       funnel.EditTypeModify,
		ObjectType: "Customer",
		PrimaryKey: "C-1",
		Properties: map[string]interface{}{"name": "second"},
	})

	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Properties["name"] != "second" {
		t.Fatalf("expected latest-by-offset name=second, got %v", rows[0].Properties)
	}
}
