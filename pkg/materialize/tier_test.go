package materialize

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
)

func TestTierRouter_NilMaterializer_ReturnsEmpty(t *testing.T) {
	r := NewTierRouter(nil)
	pks, err := r.ColdPrimaryKeys(context.Background(), "northwind", "Customer", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pks) != 0 {
		t.Fatalf("nil materializer should yield no rows, got %v", pks)
	}
}

func TestTierRouter_RejectsBlankOntology(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	r := NewTierRouter(m)
	if _, err := r.ColdPrimaryKeys(context.Background(), "", "Customer", time.Time{}); err == nil {
		t.Fatal("expected error for blank ontology")
	}
}

func TestTierRouter_RejectsBlankObjectType(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	r := NewTierRouter(m)
	if _, err := r.ColdPrimaryKeys(context.Background(), "northwind", "", time.Time{}); err == nil {
		t.Fatal("expected error for blank objectType")
	}
}

func TestTierRouter_MissingDirectory_NoError(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	r := NewTierRouter(m)
	pks, err := r.ColdPrimaryKeys(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error on empty data dir: %v", err)
	}
	if len(pks) != 0 {
		t.Fatalf("expected no PKs, got %v", pks)
	}
}

// TestTierRouter_RoundTrip writes two CREATE batches, then reads them back
// via ColdPrimaryKeys with an open-ended cutoff (zero time = "all time").
// Both PKs must surface in sorted order (BuildSnapshot's contract).
func TestTierRouter_RoundTrip(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	ts := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-1",
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-2", Properties: map[string]interface{}{"name": "Bob"}},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch: %v", err)
	}

	r := NewTierRouter(m)
	pks, err := r.ColdPrimaryKeys(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("ColdPrimaryKeys: %v", err)
	}
	sort.Strings(pks)
	want := []string{"C-1", "C-2"}
	if len(pks) != len(want) {
		t.Fatalf("expected %v, got %v", want, pks)
	}
	for i := range want {
		if pks[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, pks)
		}
	}
}

// TestTierRouter_AsOfCutoff exercises the "before" cutoff plumbing — rows
// materialised AFTER the cutoff must NOT surface (they belong to the hot
// tier in the executor's merge).
func TestTierRouter_AsOfCutoff(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	old := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	for _, batch := range []struct {
		id string
		ts time.Time
		pk string
	}{
		{"tx-old", old, "C-old"},
		{"tx-new", recent, "C-new"},
	} {
		if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
			ID:              batch.id,
			OntologyAPIName: "northwind",
			Timestamp:       batch.ts,
			Edits: []funnel.Edit{
				{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: batch.pk},
			},
		}); err != nil {
			t.Fatalf("MaterializeBatch %s: %v", batch.id, err)
		}
	}

	cutoff := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	r := NewTierRouter(m)
	pks, err := r.ColdPrimaryKeys(context.Background(), "northwind", "Customer", cutoff)
	if err != nil {
		t.Fatalf("ColdPrimaryKeys: %v", err)
	}
	if len(pks) != 1 || pks[0] != "C-old" {
		t.Fatalf("expected only [C-old] before cutoff, got %v", pks)
	}
}

// TestTierRouter_DeleteDropped: a DELETE materialised at the highest
// __patch_offset removes the PK from the projection. This is the same
// invariant BuildSnapshot already enforces and the router inherits.
func TestTierRouter_DeleteDropped(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	ts := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-1",
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1"},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch create: %v", err)
	}
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-2",
		OntologyAPIName: "northwind",
		Timestamp:       ts.Add(time.Minute),
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeDelete, ObjectType: "Customer", PrimaryKey: "C-1"},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch delete: %v", err)
	}

	r := NewTierRouter(m)
	pks, err := r.ColdPrimaryKeys(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("ColdPrimaryKeys: %v", err)
	}
	if len(pks) != 0 {
		t.Fatalf("expected delete to drop PK, got %v", pks)
	}
}
