//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// US-223 integration tests for the bitemporal validity range columns and
// the SnapshotObjectsAt reader. These exercise the live PG path because
// the InsertObjectHistory write side runs an additional UPDATE statement
// to close out the prior version's valid_to — only the real PG backend
// honours that side effect.

func insertHistoryAt(t *testing.T, ctx context.Context, repo oms.Repository, otRID, pk string, version int64, editType string, newState string, recordedAt time.Time) {
	t.Helper()
	h := &oms.ObjectHistory{
		ObjectTypeRID: otRID,
		PrimaryKey:    pk,
		Version:       version,
		EditType:      editType,
		RecordedAt:    recordedAt,
	}
	if newState != "" {
		h.NewState = json.RawMessage(newState)
	}
	if err := repo.InsertObjectHistory(ctx, h); err != nil {
		t.Fatalf("InsertObjectHistory v%d: %v", version, err)
	}
}

func TestObjectHistory_InsertClosesPriorValidTo(t *testing.T) {
	// US-223: when a v2 row lands the v1 row's valid_to must be stamped
	// with v2.recorded_at so the [valid_from, valid_to) intervals are
	// contiguous and non-overlapping.
	repo := setupRepo(t)
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-closeprev"

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	insertHistoryAt(t, ctx, repo, otRID, pk, 1, "CREATE", `{"v":1}`, t1)
	insertHistoryAt(t, ctx, repo, otRID, pk, 2, "MODIFY", `{"v":2}`, t2)

	// Snapshot at t1+1ns ⇒ v1 (valid_to == t2 > t1+1ns).
	snap, err := repo.SnapshotObjectsAt(ctx, otRID, t1.Add(time.Nanosecond))
	if err != nil {
		t.Fatalf("SnapshotObjectsAt: %v", err)
	}
	if len(snap) != 1 || snap[0].PrimaryKey != pk {
		t.Fatalf("expected single snapshot for %q, got %+v", pk, snap)
	}
	var state map[string]interface{}
	if err := json.Unmarshal(snap[0].NewState, &state); err != nil {
		t.Fatalf("unmarshal new_state: %v", err)
	}
	if state["v"] != float64(1) {
		t.Errorf("snapshot at t1 returned v=%v, want 1", state["v"])
	}

	// Snapshot at t2 ⇒ v2 (the half-open interval is inclusive on
	// valid_from, exclusive on valid_to).
	snap2, err := repo.SnapshotObjectsAt(ctx, otRID, t2)
	if err != nil {
		t.Fatalf("SnapshotObjectsAt v2: %v", err)
	}
	if len(snap2) != 1 {
		t.Fatalf("expected single snapshot at t2, got %d rows", len(snap2))
	}
	if err := json.Unmarshal(snap2[0].NewState, &state); err != nil {
		t.Fatalf("unmarshal new_state v2: %v", err)
	}
	if state["v"] != float64(2) {
		t.Errorf("snapshot at t2 returned v=%v, want 2", state["v"])
	}
}

func TestObjectHistory_SnapshotSkipsDeleteTombstones(t *testing.T) {
	// A DELETE row at v2 means the object did NOT exist at any time after
	// v2.recorded_at. SnapshotObjectsAt must omit the PK from the result
	// for any asOf >= v2.
	repo := setupRepo(t)
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-deleted"

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	insertHistoryAt(t, ctx, repo, otRID, pk, 1, "CREATE", `{"v":1}`, t1)
	insertHistoryAt(t, ctx, repo, otRID, pk, 2, "DELETE", "", t2)

	store := repo

	beforeDelete, err := store.SnapshotObjectsAt(ctx, otRID, t1.Add(time.Hour))
	if err != nil {
		t.Fatalf("SnapshotObjectsAt before-delete: %v", err)
	}
	if len(beforeDelete) != 1 {
		t.Fatalf("expected 1 row before delete, got %d", len(beforeDelete))
	}

	afterDelete, err := store.SnapshotObjectsAt(ctx, otRID, t2.Add(time.Hour))
	if err != nil {
		t.Fatalf("SnapshotObjectsAt after-delete: %v", err)
	}
	for _, row := range afterDelete {
		if row.PrimaryKey == pk {
			t.Fatalf("DELETE tombstone leaked into snapshot: %+v", row)
		}
	}
}

func TestObjectHistory_SnapshotBeforeFirstVersion(t *testing.T) {
	// Asking for a snapshot before any history row exists must return
	// zero rows for the PK, not an error.
	repo := setupRepo(t)
	ctx := context.Background()
	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-future"

	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	insertHistoryAt(t, ctx, repo, otRID, pk, 1, "CREATE", `{"v":1}`, t1)

	store := repo

	preCreate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows, err := store.SnapshotObjectsAt(ctx, otRID, preCreate)
	if err != nil {
		t.Fatalf("SnapshotObjectsAt pre-create: %v", err)
	}
	for _, row := range rows {
		if row.PrimaryKey == pk {
			t.Fatalf("got snapshot for PK that did not yet exist: %+v", row)
		}
	}
}
