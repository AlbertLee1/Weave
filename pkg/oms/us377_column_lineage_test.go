package oms

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// helper to build a binding for the derive tests
func newTestBinding(t *testing.T, mapping any) *DatasourceBinding {
	t.Helper()
	raw, err := json.Marshal(mapping)
	if err != nil {
		t.Fatalf("marshal mapping: %v", err)
	}
	return &DatasourceBinding{
		RID:           "ri.ontology.main.datasource-binding.b-1",
		ObjectTypeRID: "ri.ontology.main.object-type.ot-1",
		DatasetRID:    "ri.datasets.main.dataset.ds-1",
		Branch:        "main",
		ColumnMapping: raw,
		IsPrimary:     true,
	}
}

func TestUS377_DeriveEdges_FlatObject(t *testing.T) {
	binding := newTestBinding(t, map[string]string{
		"firstName":   "first_name",
		"lastName":    "last_name",
		"orphanedKey": "ignored_column", // no matching property
	})
	props := []Property{
		{RID: "ri.ontology.main.property.p-first", APIName: "firstName", ObjectTypeRID: binding.ObjectTypeRID},
		{RID: "ri.ontology.main.property.p-last", APIName: "lastName", ObjectTypeRID: binding.ObjectTypeRID},
		{RID: "ri.ontology.main.property.p-extra", APIName: "extra", ObjectTypeRID: binding.ObjectTypeRID}, // unmapped
	}
	edges, err := DeriveColumnLineageEdges(binding, props)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("want 2 edges (orphanedKey has no matching property; extra is unmapped), got %d: %+v", len(edges), edges)
	}
	// Output must be sorted by (api_name, src_column).
	if edges[0].DstPropertyAPIName != "firstName" || edges[1].DstPropertyAPIName != "lastName" {
		t.Fatalf("edges not sorted: %+v", edges)
	}
	for _, e := range edges {
		if e.BindingRID != binding.RID {
			t.Errorf("BindingRID stamp wrong: %q", e.BindingRID)
		}
		if e.SrcDatasetRID != binding.DatasetRID {
			t.Errorf("SrcDatasetRID stamp wrong: %q", e.SrcDatasetRID)
		}
		if e.DstObjectTypeRID != binding.ObjectTypeRID {
			t.Errorf("DstObjectTypeRID stamp wrong: %q", e.DstObjectTypeRID)
		}
	}
}

func TestUS377_DeriveEdges_ArrayForm(t *testing.T) {
	binding := newTestBinding(t, []map[string]string{
		{"property": "name", "column": "full_name"},
		{"p": "code", "c": "short_code"},
		{"property": "noColumn"},  // missing column → skipped
		{"column": "noProperty"},  // missing property → skipped
	})
	props := []Property{
		{RID: "ri.ontology.main.property.p-name", APIName: "name"},
		{RID: "ri.ontology.main.property.p-code", APIName: "code"},
		{RID: "ri.ontology.main.property.p-noColumn", APIName: "noColumn"},
	}
	edges, err := DeriveColumnLineageEdges(binding, props)
	if err != nil {
		t.Fatalf("derive failed: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("want 2 edges; got %d: %+v", len(edges), edges)
	}
	if edges[0].DstPropertyAPIName != "code" || edges[1].DstPropertyAPIName != "name" {
		t.Fatalf("edges not sorted: %+v", edges)
	}
	if edges[0].SrcColumn != "short_code" {
		t.Errorf("short-key array shape failed to map column: %q", edges[0].SrcColumn)
	}
}

func TestUS377_DeriveEdges_EmptyMappingReturnsNil(t *testing.T) {
	binding := newTestBinding(t, map[string]string{})
	edges, err := DeriveColumnLineageEdges(binding, []Property{
		{RID: "ri.ontology.main.property.p", APIName: "x"},
	})
	if err != nil {
		t.Fatalf("derive failed on empty mapping: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("expected zero edges on empty mapping, got %d", len(edges))
	}
}

func TestUS377_DeriveEdges_NilBindingReturnsNil(t *testing.T) {
	edges, err := DeriveColumnLineageEdges(nil, nil)
	if err != nil || edges != nil {
		t.Fatalf("nil binding should yield (nil, nil), got %v / %v", edges, err)
	}
}

func TestUS377_DeriveEdges_RejectsEmptyDatasetRID(t *testing.T) {
	binding := newTestBinding(t, map[string]string{"a": "b"})
	binding.DatasetRID = ""
	if _, err := DeriveColumnLineageEdges(binding, nil); err == nil {
		t.Fatal("expected ErrEmptyDatasetRID, got nil")
	}
}

func TestUS377_DeriveEdges_RejectsMalformedMapping(t *testing.T) {
	binding := newTestBinding(t, map[string]string{"a": "b"})
	binding.ColumnMapping = json.RawMessage(`{"good": "col", "bad":`) // truncated
	if _, err := DeriveColumnLineageEdges(binding, []Property{{APIName: "good", RID: "ri.x"}}); err == nil {
		t.Fatal("expected error on malformed json, got nil")
	}
}

func TestUS377_DeriveEdges_DropsEmptyColumn(t *testing.T) {
	binding := newTestBinding(t, map[string]string{"a": "", "b": "col_b"})
	props := []Property{
		{APIName: "a", RID: "ri.ontology.main.property.p-a"},
		{APIName: "b", RID: "ri.ontology.main.property.p-b"},
	}
	edges, err := DeriveColumnLineageEdges(binding, props)
	if err != nil || len(edges) != 1 {
		t.Fatalf("expected 1 edge (empty column for 'a' dropped), got %d / %v", len(edges), err)
	}
	if edges[0].DstPropertyAPIName != "b" {
		t.Errorf("wrong surviving edge: %+v", edges[0])
	}
}

func TestUS377_MemoryStore_ReplaceClearsPriorEdges(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryColumnLineageStore()
	bindingRID := "ri.ontology.main.datasource-binding.b-1"

	v1 := []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.x", SrcColumn: "first", DstObjectTypeRID: "ri.ot.1", DstPropertyRID: "ri.p.first", DstPropertyAPIName: "first"},
		{SrcDatasetRID: "ri.ds.x", SrcColumn: "last", DstObjectTypeRID: "ri.ot.1", DstPropertyRID: "ri.p.last", DstPropertyAPIName: "last"},
	}
	if err := store.ReplaceColumnLineageForBinding(ctx, bindingRID, v1); err != nil {
		t.Fatalf("replace v1: %v", err)
	}

	got, err := store.ListUpstreamColumnLineageForProperty(ctx, "ri.p.first", 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("v1 lookup: %d / %v", len(got), err)
	}

	// Replace with a different shape.
	v2 := []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.x", SrcColumn: "renamed_first", DstObjectTypeRID: "ri.ot.1", DstPropertyRID: "ri.p.first", DstPropertyAPIName: "first"},
	}
	if err := store.ReplaceColumnLineageForBinding(ctx, bindingRID, v2); err != nil {
		t.Fatalf("replace v2: %v", err)
	}
	got, err = store.ListUpstreamColumnLineageForProperty(ctx, "ri.p.first", 0)
	if err != nil {
		t.Fatalf("v2 lookup: %v", err)
	}
	if len(got) != 1 || got[0].SrcColumn != "renamed_first" {
		t.Fatalf("expected single renamed edge, got %+v", got)
	}
	// 'last' is gone after the replace.
	got, _ = store.ListUpstreamColumnLineageForProperty(ctx, "ri.p.last", 0)
	if len(got) != 0 {
		t.Fatalf("expected zero edges for 'last' after replace, got %d", len(got))
	}
}

func TestUS377_MemoryStore_DeleteForBinding(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryColumnLineageStore()
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.1", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds", SrcColumn: "c1", DstPropertyRID: "ri.p.1", DstObjectTypeRID: "ri.ot.1", DstPropertyAPIName: "p1"},
		{SrcDatasetRID: "ri.ds", SrcColumn: "c2", DstPropertyRID: "ri.p.2", DstObjectTypeRID: "ri.ot.1", DstPropertyAPIName: "p2"},
	})
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.2", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds", SrcColumn: "c3", DstPropertyRID: "ri.p.3", DstObjectTypeRID: "ri.ot.2", DstPropertyAPIName: "p3"},
	})
	n, err := store.DeleteColumnLineageForBinding(ctx, "ri.b.1")
	if err != nil || n != 2 {
		t.Fatalf("delete b.1 should remove 2 rows, got %d / %v", n, err)
	}
	// b.2 untouched.
	got, _ := store.ListUpstreamColumnLineageForProperty(ctx, "ri.p.3", 0)
	if len(got) != 1 {
		t.Fatalf("b.2's edges should survive, got %d", len(got))
	}
	// idempotent
	n, _ = store.DeleteColumnLineageForBinding(ctx, "ri.b.1")
	if n != 0 {
		t.Fatalf("second delete should remove nothing, got %d", n)
	}
}

func TestUS377_MemoryStore_ReverseImpact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryColumnLineageStore()
	// Two bindings, both pulling from the same upstream column.
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.A", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.shared", SrcColumn: "email", DstPropertyRID: "ri.p.cust.email", DstObjectTypeRID: "ri.ot.cust", DstPropertyAPIName: "email"},
	})
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.B", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.shared", SrcColumn: "email", DstPropertyRID: "ri.p.emp.email", DstObjectTypeRID: "ri.ot.emp", DstPropertyAPIName: "email"},
	})
	// Unrelated column.
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.C", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds.shared", SrcColumn: "phone", DstPropertyRID: "ri.p.cust.phone", DstObjectTypeRID: "ri.ot.cust", DstPropertyAPIName: "phone"},
	})
	got, err := store.ListDownstreamColumnLineageForDatasetColumn(ctx, "ri.ds.shared", "email", 0)
	if err != nil {
		t.Fatalf("reverse impact: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 impacted properties for shared.email, got %d: %+v", len(got), got)
	}
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.DstPropertyRID] = true
	}
	if !seen["ri.p.cust.email"] || !seen["ri.p.emp.email"] {
		t.Fatalf("missing impacted properties: %v", seen)
	}
}

func TestUS377_MemoryStore_ListLimit(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryColumnLineageStore()
	edges := make([]ColumnLineageEdge, 5)
	for i := range edges {
		edges[i] = ColumnLineageEdge{
			SrcDatasetRID:      "ri.ds",
			SrcColumn:          "col",
			DstObjectTypeRID:   "ri.ot",
			DstPropertyRID:     "ri.p.shared",
			DstPropertyAPIName: "p",
		}
	}
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b", edges)
	got, _ := store.ListUpstreamColumnLineageForProperty(ctx, "ri.p.shared", 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 with limit=3, got %d", len(got))
	}
	got, _ = store.ListUpstreamColumnLineageForProperty(ctx, "ri.p.shared", 0)
	if len(got) != 5 {
		t.Fatalf("expected all 5 with default limit, got %d", len(got))
	}
}

// TestUS377_MemoryStore_Newest_First asserts the time-ordered listing
// contract (newest first). The MemoryStore stamps timestamps so we
// inject a deterministic clock.
func TestUS377_MemoryStore_Newest_First(t *testing.T) {
	ctx := context.Background()
	tick := int64(0)
	clock := func() time.Time {
		tick++
		return time.Unix(tick, 0)
	}
	store := NewMemoryColumnLineageStore().WithClock(clock)
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.1", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds", SrcColumn: "c1", DstPropertyRID: "ri.p", DstObjectTypeRID: "ri.ot", DstPropertyAPIName: "p"},
	})
	_ = store.ReplaceColumnLineageForBinding(ctx, "ri.b.2", []ColumnLineageEdge{
		{SrcDatasetRID: "ri.ds", SrcColumn: "c2", DstPropertyRID: "ri.p", DstObjectTypeRID: "ri.ot", DstPropertyAPIName: "p"},
	})
	got, _ := store.ListUpstreamColumnLineageForProperty(ctx, "ri.p", 0)
	if len(got) != 2 {
		t.Fatalf("want 2 edges, got %d", len(got))
	}
	if got[0].SrcColumn != "c2" {
		t.Fatalf("expected newest-first ordering (c2 first), got %s first", got[0].SrcColumn)
	}
}
