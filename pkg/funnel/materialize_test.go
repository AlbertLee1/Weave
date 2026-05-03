package funnel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
)

type fakeMaterializer struct {
	mu       sync.Mutex
	batches  []EditBatch
	returnErr error
}

func (f *fakeMaterializer) MaterializeBatch(_ context.Context, batch EditBatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.returnErr != nil {
		return f.returnErr
	}
	cp := EditBatch{
		ID:              batch.ID,
		OntologyAPIName: batch.OntologyAPIName,
		Edits:           append([]Edit(nil), batch.Edits...),
		UserID:          batch.UserID,
		Timestamp:       batch.Timestamp,
	}
	f.batches = append(f.batches, cp)
	return nil
}

func (f *fakeMaterializer) calls() []EditBatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]EditBatch(nil), f.batches...)
}

// setupMaterializeConsumer is the local fixture: a Consumer with a real
// index.Manager and a Customer index pre-registered so ApplyBatch can
// commit CREATE/MODIFY edits without hitting "index not found".
func setupMaterializeConsumer(t *testing.T) *Consumer {
	t.Helper()
	mgr := index.NewManager(t.TempDir())
	props := []index.Property{
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(index.ScopedKey("northwind", "Customer"), props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	t.Cleanup(func() { mgr.Close() })
	return NewConsumer(nil, mgr)
}

func TestConsumer_Materialize_DispatchesAppliedBatch(t *testing.T) {
	c := setupMaterializeConsumer(t)
	mat := &fakeMaterializer{}
	c.SetEditMaterializer(mat)

	batch := EditBatch{
		ID:              "tx-disp-1",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 5, 4, 16, 0, 0, 0, time.UTC),
		UserID:          "tester",
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "Customer",
				PrimaryKey: "C-1",
				Properties: map[string]interface{}{"name": "Alice"},
			},
		},
	}
	if err := c.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	calls := mat.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 materialize call, got %d", len(calls))
	}
	got := calls[0]
	if got.ID != "tx-disp-1" || got.OntologyAPIName != "northwind" {
		t.Fatalf("unexpected batch: %+v", got)
	}
	if len(got.Edits) != 1 || got.Edits[0].PrimaryKey != "C-1" {
		t.Fatalf("expected 1 edit for C-1, got %+v", got.Edits)
	}
}

func TestConsumer_Materialize_FailureDoesNotAbortBatch(t *testing.T) {
	c := setupMaterializeConsumer(t)
	mat := &fakeMaterializer{returnErr: errors.New("disk full")}
	c.SetEditMaterializer(mat)

	batch := EditBatch{
		ID:              "tx-fail",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 5, 4, 16, 30, 0, 0, time.UTC),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "Customer",
				PrimaryKey: "C-2",
				Properties: map[string]interface{}{"name": "Bob"},
			},
		},
	}
	// Even though the materializer fails, ApplyBatch must succeed because
	// materialization is best-effort.
	if err := c.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch returned error from materializer: %v", err)
	}
}

func TestConsumer_Materialize_NilHookIsNoOp(t *testing.T) {
	c := setupMaterializeConsumer(t)
	// No SetEditMaterializer call: materializer is nil.

	batch := EditBatch{
		ID:              "tx-nil",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 5, 4, 17, 0, 0, 0, time.UTC),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "Customer",
				PrimaryKey: "C-3",
				Properties: map[string]interface{}{"name": "Carol"},
			},
		},
	}
	if err := c.ApplyBatch(context.Background(), batch); err != nil {
		t.Fatalf("ApplyBatch with nil materializer: %v", err)
	}
}
