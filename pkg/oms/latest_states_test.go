//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// TestLoadLatestObjectStates_ExcludesDeletesAndCollapsesVersions verifies
// that LoadLatestObjectStates returns exactly one row per primary key (the
// newest non-tombstone version) and filters out rows whose latest edit is a
// DELETE. This is the authoritative data source for the weave index rebuild
// command introduced in US-011.
func TestLoadLatestObjectStates_ExcludesDeletesAndCollapsesVersions(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.employee-lates"

	// emp-A: three versions, latest is MODIFY -> should be included.
	for i := 1; i <= 3; i++ {
		h := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    "emp-A",
			Version:       int64(i),
			NewState:      json.RawMessage(`{"v":` + jsonNum(i) + `,"name":"Alice"}`),
			EditType:      "MODIFY",
		}
		if err := repo.InsertObjectHistory(ctx, h); err != nil {
			t.Fatalf("seed emp-A v%d: %v", i, err)
		}
	}

	// emp-B: create then delete -> should be excluded.
	if err := repo.InsertObjectHistory(ctx, &oms.ObjectHistory{
		ObjectTypeRID: otRID,
		PrimaryKey:    "emp-B",
		Version:       1,
		NewState:      json.RawMessage(`{"name":"Bob"}`),
		EditType:      "CREATE",
	}); err != nil {
		t.Fatalf("seed emp-B create: %v", err)
	}
	if err := repo.InsertObjectHistory(ctx, &oms.ObjectHistory{
		ObjectTypeRID: otRID,
		PrimaryKey:    "emp-B",
		Version:       2,
		PrevState:     json.RawMessage(`{"name":"Bob"}`),
		NewState:      nil,
		EditType:      "DELETE",
	}); err != nil {
		t.Fatalf("seed emp-B delete: %v", err)
	}

	// emp-C: single CREATE -> should be included.
	if err := repo.InsertObjectHistory(ctx, &oms.ObjectHistory{
		ObjectTypeRID: otRID,
		PrimaryKey:    "emp-C",
		Version:       1,
		NewState:      json.RawMessage(`{"name":"Carol"}`),
		EditType:      "CREATE",
	}); err != nil {
		t.Fatalf("seed emp-C: %v", err)
	}

	// Unrelated ObjectType — must be ignored.
	if err := repo.InsertObjectHistory(ctx, &oms.ObjectHistory{
		ObjectTypeRID: "ri.ontology.main.object-type.other",
		PrimaryKey:    "other-1",
		Version:       1,
		NewState:      json.RawMessage(`{"name":"X"}`),
		EditType:      "CREATE",
	}); err != nil {
		t.Fatalf("seed unrelated: %v", err)
	}

	rows, err := repo.LoadLatestObjectStates(ctx, otRID)
	if err != nil {
		t.Fatalf("LoadLatestObjectStates: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (emp-A, emp-C); rows=%+v", len(rows), rows)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].PrimaryKey < rows[j].PrimaryKey })

	if rows[0].PrimaryKey != "emp-A" {
		t.Errorf("rows[0].PrimaryKey = %q, want emp-A", rows[0].PrimaryKey)
	}
	var stateA map[string]any
	if err := json.Unmarshal(rows[0].NewState, &stateA); err != nil {
		t.Fatalf("unmarshal emp-A: %v", err)
	}
	if stateA["v"].(float64) != 3 {
		t.Errorf("emp-A latest v = %v, want 3", stateA["v"])
	}

	if rows[1].PrimaryKey != "emp-C" {
		t.Errorf("rows[1].PrimaryKey = %q, want emp-C", rows[1].PrimaryKey)
	}
}

func TestLoadLatestObjectStates_EmptyHistory(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	rows, err := repo.LoadLatestObjectStates(ctx, "ri.ontology.main.object-type.nonexistent")
	if err != nil {
		t.Fatalf("LoadLatestObjectStates: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(rows))
	}
}
