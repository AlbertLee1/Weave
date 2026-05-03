package funnel

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// fakeTxRecorder is a minimal in-memory DatasetTransactionRecorder for the
// US-379 funnel-side tests. It tracks every Record call (so tests can assert
// the per-batch chain shape) and stitches LatestForOntology to the in-memory
// rows so consumer code that resolves parent_tx_id sees a deterministic
// chain without a real PG backend.
type fakeTxRecorder struct {
	mu        sync.Mutex
	rows      []oms.DatasetTransaction
	recordErr error
	latestErr error
}

func (f *fakeTxRecorder) RecordDatasetTransaction(_ context.Context, tx *oms.DatasetTransaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	f.rows = append(f.rows, *tx)
	return nil
}

func (f *fakeTxRecorder) LatestForOntology(_ context.Context, ontologyAPIName string) (*oms.DatasetTransaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.latestErr != nil {
		return nil, f.latestErr
	}
	// The store contract is committed_at-DESC + tx_id-DESC tiebreaker; the
	// in-memory list is append-ordered by Record time, so re-sort on read
	// rather than relying on insert order.
	matches := make([]oms.DatasetTransaction, 0, len(f.rows))
	for _, r := range f.rows {
		if r.OntologyAPIName == ontologyAPIName {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].CommittedAt.Equal(matches[j].CommittedAt) {
			return matches[i].TxID > matches[j].TxID
		}
		return matches[i].CommittedAt.After(matches[j].CommittedAt)
	})
	out := matches[0]
	return &out, nil
}

func (f *fakeTxRecorder) snapshot() []oms.DatasetTransaction {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]oms.DatasetTransaction, len(f.rows))
	copy(out, f.rows)
	return out
}

// TestUS379_RecordsTransactionPerBatch verifies the funnel consumer writes
// one dataset_transactions row per applied EditBatch, with txId derived
// from EditBatch.ID (prefixed when missing) and committedAt mirroring
// EditBatch.Timestamp.
func TestUS379_RecordsTransactionPerBatch(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	hist := &fakeHistoryRepo{}
	tx := &fakeTxRecorder{}
	consumer.SetHistoryRepo(hist)
	consumer.SetTxRecorder(tx)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	t1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	batches := []EditBatch{
		{
			ID: "tx-001", OntologyAPIName: testOntology, UserID: "alice", Timestamp: t1,
			Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1",
				Source: EditSourceUser, Properties: map[string]interface{}{"name": "Alice"}}},
		},
		{
			ID: "tx-002", OntologyAPIName: testOntology, UserID: "alice", Timestamp: t2,
			Edits: []Edit{{Type: EditTypeModify, ObjectType: "employee", PrimaryKey: "emp-1",
				Source: EditSourceUser, Properties: map[string]interface{}{"name": "Alice2"}}},
		},
	}
	for _, b := range batches {
		if err := consumer.applyBatchWithHistory(context.Background(), b); err != nil {
			t.Fatalf("apply batch %s: %v", b.ID, err)
		}
	}

	rows := tx.snapshot()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Row 0 is the genesis tx — no parent.
	if rows[0].TxID != "tx-001" {
		t.Errorf("rows[0].TxID = %q, want tx-001", rows[0].TxID)
	}
	if rows[0].ParentTxID != "" {
		t.Errorf("rows[0].ParentTxID = %q, want empty", rows[0].ParentTxID)
	}
	if !rows[0].CommittedAt.Equal(t1) {
		t.Errorf("rows[0].CommittedAt = %v, want %v", rows[0].CommittedAt, t1)
	}
	if rows[0].EditsCount != 1 {
		t.Errorf("rows[0].EditsCount = %d, want 1", rows[0].EditsCount)
	}
	// Row 1 chains to row 0.
	if rows[1].TxID != "tx-002" {
		t.Errorf("rows[1].TxID = %q, want tx-002", rows[1].TxID)
	}
	if rows[1].ParentTxID != "tx-001" {
		t.Errorf("rows[1].ParentTxID = %q, want tx-001", rows[1].ParentTxID)
	}
	if !rows[1].CommittedAt.Equal(t2) {
		t.Errorf("rows[1].CommittedAt = %v, want %v", rows[1].CommittedAt, t2)
	}
}

// TestUS379_AddsTxPrefixWhenMissing covers the helper's normalisation: an
// EditBatch.ID without the canonical "tx-" prefix is rewritten so the
// downstream OSS asOf parser keys cleanly off the prefix.
func TestUS379_AddsTxPrefixWhenMissing(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	hist := &fakeHistoryRepo{}
	tx := &fakeTxRecorder{}
	consumer.SetHistoryRepo(hist)
	consumer.SetTxRecorder(tx)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	batch := EditBatch{
		ID:              "batch-no-prefix", // funnel publisher emits raw uuids in some paths
		OntologyAPIName: testOntology,
		UserID:          "alice",
		Timestamp:       time.Now(),
		Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1",
			Source: EditSourceUser, Properties: map[string]interface{}{"name": "A"}}},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("apply batch: %v", err)
	}
	rows := tx.snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].TxID != "tx-batch-no-prefix" {
		t.Errorf("TxID = %q, want tx-batch-no-prefix", rows[0].TxID)
	}
}

// TestUS379_StampsTxIDOnObjectHistory verifies the per-batch tx_id is
// propagated onto every ObjectHistory row produced by the same batch so a
// future query can disambiguate concurrent batches that share a
// millisecond-level recorded_at.
func TestUS379_StampsTxIDOnObjectHistory(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	hist := &fakeHistoryRepo{}
	tx := &fakeTxRecorder{}
	consumer.SetHistoryRepo(hist)
	consumer.SetTxRecorder(tx)
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	batch := EditBatch{
		ID: "tx-stamping", OntologyAPIName: testOntology, UserID: "alice",
		Timestamp: time.Now(),
		Edits: []Edit{
			{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1",
				Source: EditSourceUser, Properties: map[string]interface{}{"name": "A"}},
			{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-2",
				Source: EditSourceUser, Properties: map[string]interface{}{"name": "B"}},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("apply batch: %v", err)
	}
	rows := hist.snapshot()
	if len(rows) != 2 {
		t.Fatalf("history rows = %d, want 2", len(rows))
	}
	for i, r := range rows {
		if r.TxID != "tx-stamping" {
			t.Errorf("rows[%d].TxID = %q, want tx-stamping", i, r.TxID)
		}
	}
}

// TestUS379_NilRecorderIsNoOp guarantees the absence of a recorder degrades
// cleanly — no panics, no errors, history rows still land with empty TxID
// so legacy deployments without dataset_transactions stay green.
func TestUS379_NilRecorderIsNoOp(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	hist := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(hist)
	// no SetTxRecorder
	consumer.SetObjectTypeRIDs(map[string]string{
		"employee": "ri.ontology.main.object-type.employee",
	})

	batch := EditBatch{
		ID: "tx-noop", OntologyAPIName: testOntology, UserID: "alice", Timestamp: time.Now(),
		Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1",
			Source: EditSourceUser, Properties: map[string]interface{}{"name": "A"}}},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("apply batch: %v", err)
	}
	rows := hist.snapshot()
	if len(rows) != 1 {
		t.Fatalf("history rows = %d, want 1", len(rows))
	}
	if rows[0].TxID != "" {
		t.Errorf("rows[0].TxID = %q, want empty when no recorder is wired", rows[0].TxID)
	}
}
