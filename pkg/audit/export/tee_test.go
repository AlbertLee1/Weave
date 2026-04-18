package export

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// recordExporter captures every batch handed to Export without failing.
type recordExporter struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (r *recordExporter) Name() string { return "record" }

func (r *recordExporter) Export(_ context.Context, batch []audit.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, batch...)
	return nil
}

func (r *recordExporter) Events() []audit.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.AuditEvent(nil), r.events...)
}

func TestTeeStore_PersistAndExport(t *testing.T) {
	mem := audit.NewMemoryStore()
	sink := &recordExporter{}
	batched := NewBatchedExporter(sink, BatchedOptions{BatchSize: 1})
	tee := NewTeeStore(mem, batched)

	evt := sampleEvent("x")
	if err := audit.Record(context.Background(), tee, evt); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Give the async Enqueue a moment to run.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(sink.Events()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if len(mem.Events()) != 1 {
		t.Fatalf("underlying store should have 1 event, got %d", len(mem.Events()))
	}
	got := sink.Events()
	if len(got) != 1 {
		t.Fatalf("exporter should have received 1 event, got %d", len(got))
	}
	if got[0].ID == "" {
		t.Fatalf("expected populated ID, got empty")
	}
}

func TestTeeStore_UnderlyingErrorNotExported(t *testing.T) {
	// If the PG insert fails, we MUST NOT leak the event to the external
	// sink — a half-committed audit trail is worse than either extreme.
	errStore := &stubStore{err: errTransient}
	sink := &recordExporter{}
	batched := NewBatchedExporter(sink, BatchedOptions{BatchSize: 1})
	tee := NewTeeStore(errStore, batched)

	_ = tee.Insert(context.Background(), sampleEvent("x"))
	time.Sleep(20 * time.Millisecond)
	if n := len(sink.Events()); n != 0 {
		t.Fatalf("exporter should not have received any events on underlying failure, got %d", n)
	}
}

func TestTeeStore_ListDelegates(t *testing.T) {
	mem := audit.NewMemoryStore()
	tee := NewTeeStore(mem, nil) // nil exporter means "no tee" — still valid

	_ = audit.Record(context.Background(), tee, sampleEvent("a"))

	events, err := tee.List(context.Background(), audit.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event via delegated list, got %d", len(events))
	}
}

func TestTeeStore_NilExporterIsPassthrough(t *testing.T) {
	mem := audit.NewMemoryStore()
	tee := NewTeeStore(mem, nil)
	if err := tee.Insert(context.Background(), sampleEvent("x")); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(mem.Events()) != 1 {
		t.Fatalf("expected 1 event in underlying store")
	}
}

type stubStore struct {
	err error
}

func (s *stubStore) Insert(_ context.Context, _ audit.AuditEvent) error { return s.err }
func (s *stubStore) List(_ context.Context, _ audit.ListFilter) ([]audit.AuditEvent, error) {
	return nil, s.err
}
