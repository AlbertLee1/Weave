//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// --- ObjectHistory PG repo tests (Tier 2.3) ---

func TestObjectHistory_Insert_Create(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	h := &oms.ObjectHistory{
		ObjectTypeRID: "ri.ontology.main.object-type.employee",
		PrimaryKey:    "emp-1",
		Version:       1,
		PrevState:     nil,
		NewState:      json.RawMessage(`{"name":"Alice","age":30}`),
		EditType:      "CREATE",
		UserID:        "user-1",
	}
	if err := repo.InsertObjectHistory(ctx, h); err != nil {
		t.Fatalf("InsertObjectHistory: %v", err)
	}
	if h.ID == "" {
		t.Fatal("expected generated ID after insert")
	}
	if h.RecordedAt.IsZero() {
		t.Fatal("expected RecordedAt to be set after insert")
	}
}

func TestObjectHistory_Insert_Modify(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	h := &oms.ObjectHistory{
		ObjectTypeRID: "ri.ontology.main.object-type.employee",
		PrimaryKey:    "emp-1",
		Version:       2,
		PrevState:     json.RawMessage(`{"name":"Alice","age":30}`),
		NewState:      json.RawMessage(`{"name":"Alice","age":31}`),
		EditType:      "MODIFY",
		UserID:        "user-1",
	}
	if err := repo.InsertObjectHistory(ctx, h); err != nil {
		t.Fatalf("InsertObjectHistory: %v", err)
	}
}

func TestObjectHistory_Insert_Delete(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	h := &oms.ObjectHistory{
		ObjectTypeRID: "ri.ontology.main.object-type.employee",
		PrimaryKey:    "emp-1",
		Version:       3,
		PrevState:     json.RawMessage(`{"name":"Alice","age":31}`),
		NewState:      nil,
		EditType:      "DELETE",
		UserID:        "user-1",
	}
	if err := repo.InsertObjectHistory(ctx, h); err != nil {
		t.Fatalf("InsertObjectHistory: %v", err)
	}
}

func TestObjectHistory_List_OrderedDesc(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-list"

	for i := 1; i <= 3; i++ {
		h := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    pk,
			Version:       int64(i),
			NewState:      json.RawMessage(`{"v":` + jsonNum(i) + `}`),
			EditType:      "MODIFY",
			UserID:        "user-1",
		}
		if err := repo.InsertObjectHistory(ctx, h); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	rows, err := repo.ListObjectHistory(ctx, otRID, pk, 10)
	if err != nil {
		t.Fatalf("ListObjectHistory: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Version != 3 || rows[1].Version != 2 || rows[2].Version != 1 {
		t.Fatalf("expected versions [3,2,1], got [%d,%d,%d]",
			rows[0].Version, rows[1].Version, rows[2].Version)
	}
}

func TestObjectHistory_List_LimitRespected(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-limit"

	for i := 1; i <= 5; i++ {
		h := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    pk,
			Version:       int64(i),
			NewState:      json.RawMessage(`{}`),
			EditType:      "MODIFY",
		}
		if err := repo.InsertObjectHistory(ctx, h); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rows, err := repo.ListObjectHistory(ctx, otRID, pk, 2)
	if err != nil {
		t.Fatalf("ListObjectHistory: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (limit), got %d", len(rows))
	}
}

func TestObjectHistory_List_FiltersByPK(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.employee"
	for _, pk := range []string{"emp-A", "emp-B"} {
		h := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    pk,
			Version:       1,
			NewState:      json.RawMessage(`{}`),
			EditType:      "CREATE",
		}
		if err := repo.InsertObjectHistory(ctx, h); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rows, err := repo.ListObjectHistory(ctx, otRID, "emp-A", 10)
	if err != nil {
		t.Fatalf("ListObjectHistory: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row for emp-A, got %d", len(rows))
	}
	if rows[0].PrimaryKey != "emp-A" {
		t.Fatalf("expected primaryKey emp-A, got %q", rows[0].PrimaryKey)
	}
}

func TestObjectHistory_GetVersionCount(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-count"

	count, err := repo.GetObjectVersionCount(ctx, otRID, pk)
	if err != nil {
		t.Fatalf("GetObjectVersionCount empty: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 versions, got %d", count)
	}

	for i := 1; i <= 4; i++ {
		h := &oms.ObjectHistory{
			ObjectTypeRID: otRID,
			PrimaryKey:    pk,
			Version:       int64(i),
			NewState:      json.RawMessage(`{}`),
			EditType:      "MODIFY",
		}
		if err := repo.InsertObjectHistory(ctx, h); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	count, err = repo.GetObjectVersionCount(ctx, otRID, pk)
	if err != nil {
		t.Fatalf("GetObjectVersionCount: %v", err)
	}
	if count != 4 {
		t.Fatalf("expected 4 versions, got %d", count)
	}
}

func TestObjectHistory_PrevAndNewState_RoundTrip(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	otRID := "ri.ontology.main.object-type.employee"
	pk := "emp-rt"

	prev := json.RawMessage(`{"name":"Alice"}`)
	next := json.RawMessage(`{"name":"Alice Updated"}`)
	h := &oms.ObjectHistory{
		ObjectTypeRID: otRID,
		PrimaryKey:    pk,
		Version:       1,
		PrevState:     prev,
		NewState:      next,
		EditType:      "MODIFY",
		UserID:        "user-rt",
	}
	if err := repo.InsertObjectHistory(ctx, h); err != nil {
		t.Fatalf("InsertObjectHistory: %v", err)
	}

	rows, err := repo.ListObjectHistory(ctx, otRID, pk, 10)
	if err != nil {
		t.Fatalf("ListObjectHistory: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]

	var gotPrev, gotNew map[string]interface{}
	if err := json.Unmarshal(got.PrevState, &gotPrev); err != nil {
		t.Fatalf("unmarshal prev: %v", err)
	}
	if gotPrev["name"] != "Alice" {
		t.Fatalf("prev name mismatch: %v", gotPrev["name"])
	}
	if err := json.Unmarshal(got.NewState, &gotNew); err != nil {
		t.Fatalf("unmarshal new: %v", err)
	}
	if gotNew["name"] != "Alice Updated" {
		t.Fatalf("new name mismatch: %v", gotNew["name"])
	}
	if got.UserID != "user-rt" {
		t.Fatalf("userId mismatch: %q", got.UserID)
	}
	if got.EditType != "MODIFY" {
		t.Fatalf("editType mismatch: %q", got.EditType)
	}
}

// jsonNum is a tiny helper that avoids importing strconv just for one int.
func jsonNum(n int) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
