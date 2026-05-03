//go:build integration

package oms_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// --- DatasetTransaction PG repo tests (US-379) ---

// TestDatasetTransaction_RecordAndGet exercises the round-trip on the
// dataset_transactions table: a Record-then-Get must produce a row whose
// canonical fields all survive the wire encoding without loss. The test
// pinpoints the field-by-field SQL → Go mapping so an accidental column
// rename in pg_repository_dataset_transaction.go fails loud.
func TestDatasetTransaction_RecordAndGet(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	want := &oms.DatasetTransaction{
		TxID:            "tx-test-001",
		OntologyAPIName: "test_ontology_us379",
		CommittedAt:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		EditsCount:      3,
		UserID:          "alice",
	}
	if err := repo.RecordDatasetTransaction(ctx, want); err != nil {
		t.Fatalf("RecordDatasetTransaction: %v", err)
	}

	got, err := repo.GetDatasetTransaction(ctx, "tx-test-001")
	if err != nil {
		t.Fatalf("GetDatasetTransaction: %v", err)
	}
	if got.TxID != want.TxID {
		t.Errorf("TxID = %q, want %q", got.TxID, want.TxID)
	}
	if got.OntologyAPIName != want.OntologyAPIName {
		t.Errorf("OntologyAPIName = %q, want %q", got.OntologyAPIName, want.OntologyAPIName)
	}
	if !got.CommittedAt.Equal(want.CommittedAt) {
		t.Errorf("CommittedAt = %v, want %v", got.CommittedAt, want.CommittedAt)
	}
	if got.EditsCount != want.EditsCount {
		t.Errorf("EditsCount = %d, want %d", got.EditsCount, want.EditsCount)
	}
	if got.UserID != want.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, want.UserID)
	}
	if got.ParentTxID != "" {
		t.Errorf("ParentTxID = %q, want empty (genesis)", got.ParentTxID)
	}
}

// TestDatasetTransaction_Get_NotFound covers the sentinel contract: a
// missing tx_id surfaces as oms.ErrNotFound so the cmd/server adapter
// can map it to objectset.ErrTransactionNotFound.
func TestDatasetTransaction_Get_NotFound(t *testing.T) {
	repo := setupRepo(t)
	_, err := repo.GetDatasetTransaction(context.Background(), "tx-does-not-exist")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestDatasetTransaction_LatestForOntology returns the most-recent
// committed transaction by committed_at-DESC. The chain is per-ontology
// so a tx from ontology A must not leak into ontology B's lookup.
func TestDatasetTransaction_LatestForOntology(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	t1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	rows := []*oms.DatasetTransaction{
		{TxID: "tx-latest-A", OntologyAPIName: "ont_A_us379", CommittedAt: t1, EditsCount: 1},
		{TxID: "tx-latest-A2", OntologyAPIName: "ont_A_us379", CommittedAt: t2, EditsCount: 2},
		{TxID: "tx-latest-B", OntologyAPIName: "ont_B_us379", CommittedAt: t1, EditsCount: 1},
	}
	for _, r := range rows {
		if err := repo.RecordDatasetTransaction(ctx, r); err != nil {
			t.Fatalf("Record %s: %v", r.TxID, err)
		}
	}

	latestA, err := repo.LatestForOntology(ctx, "ont_A_us379")
	if err != nil {
		t.Fatalf("LatestForOntology A: %v", err)
	}
	if latestA == nil || latestA.TxID != "tx-latest-A2" {
		t.Errorf("ont_A latest = %v, want tx-latest-A2", latestA)
	}

	latestB, err := repo.LatestForOntology(ctx, "ont_B_us379")
	if err != nil {
		t.Fatalf("LatestForOntology B: %v", err)
	}
	if latestB == nil || latestB.TxID != "tx-latest-B" {
		t.Errorf("ont_B latest = %v, want tx-latest-B", latestB)
	}

	// Unknown ontology returns (nil, nil) — soft miss, not an error.
	missing, err := repo.LatestForOntology(ctx, "does_not_exist_us379")
	if err != nil {
		t.Fatalf("LatestForOntology missing: %v", err)
	}
	if missing != nil {
		t.Errorf("missing ontology returned %v, want nil", missing)
	}
}

// TestDatasetTransaction_ListByOntology rejects rows from sibling ontologies
// and returns the chain newest-first.
func TestDatasetTransaction_ListByOntology(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	want := []*oms.DatasetTransaction{
		{TxID: "tx-list-1", OntologyAPIName: "ont_list_us379", CommittedAt: base, EditsCount: 1},
		{TxID: "tx-list-2", ParentTxID: "tx-list-1", OntologyAPIName: "ont_list_us379", CommittedAt: base.Add(time.Minute), EditsCount: 2},
		{TxID: "tx-list-3", ParentTxID: "tx-list-2", OntologyAPIName: "ont_list_us379", CommittedAt: base.Add(2 * time.Minute), EditsCount: 3},
		// Sibling row that must NOT appear in the result.
		{TxID: "tx-other", OntologyAPIName: "other_us379", CommittedAt: base, EditsCount: 1},
	}
	for _, r := range want {
		if err := repo.RecordDatasetTransaction(ctx, r); err != nil {
			t.Fatalf("Record %s: %v", r.TxID, err)
		}
	}

	got, err := repo.ListByOntology(ctx, "ont_list_us379", 0)
	if err != nil {
		t.Fatalf("ListByOntology: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantOrder := []string{"tx-list-3", "tx-list-2", "tx-list-1"}
	for i, tx := range got {
		if tx.TxID != wantOrder[i] {
			t.Errorf("got[%d].TxID = %q, want %q", i, tx.TxID, wantOrder[i])
		}
	}
}

// TestDatasetTransaction_Validate rejects rows the persistence layer cannot
// interpret — empty tx_id, missing prefix, blank ontology, zero
// committed_at — at the boundary so a malformed insert never lands a
// row the asOf parser cannot find.
func TestDatasetTransaction_Validate(t *testing.T) {
	repo := setupRepo(t)
	ctx := context.Background()

	cases := []struct {
		name string
		tx   *oms.DatasetTransaction
	}{
		{"empty txID", &oms.DatasetTransaction{OntologyAPIName: "x", CommittedAt: time.Now()}},
		{"missing prefix", &oms.DatasetTransaction{TxID: "raw-uuid", OntologyAPIName: "x", CommittedAt: time.Now()}},
		{"empty ontology", &oms.DatasetTransaction{TxID: "tx-x", CommittedAt: time.Now()}},
		{"zero committedAt", &oms.DatasetTransaction{TxID: "tx-x", OntologyAPIName: "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := repo.RecordDatasetTransaction(ctx, c.tx); err == nil {
				t.Errorf("RecordDatasetTransaction(%v) succeeded; want validation error", c.tx)
			}
		})
	}
}
