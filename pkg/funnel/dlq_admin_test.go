package funnel

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"
)

// US-470 unit coverage for the read-side DLQReader contract + the helper that
// pairs a DLQReader with a DLQPublishFunc to perform replays. The production
// JetStream implementation is exercised via the BDD test that drives a real
// nats-server; this file pins the abstraction's behaviour so any future
// in-memory / mock reader is forced into the same shape.

// --- Fake DLQReader implementing the US-470 contract ---

type fakeDLQReader struct {
	entries map[string]DLQEntry
	order   []string
	sizeErr error
	getErr  error
	listErr error
	delErr  error
}

func newFakeDLQReader() *fakeDLQReader {
	return &fakeDLQReader{entries: map[string]DLQEntry{}}
}

func (f *fakeDLQReader) seed(entries ...DLQEntry) {
	for _, e := range entries {
		f.entries[e.ID] = e
		f.order = append(f.order, e.ID)
	}
}

func (f *fakeDLQReader) ListPending(ctx context.Context, limit int) ([]DLQEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if limit <= 0 || limit > len(f.order) {
		limit = len(f.order)
	}
	out := make([]DLQEntry, 0, limit)
	for _, id := range f.order[:limit] {
		out = append(out, f.entries[id])
	}
	return out, nil
}

func (f *fakeDLQReader) GetByID(ctx context.Context, id string) (DLQEntry, error) {
	if f.getErr != nil {
		return DLQEntry{}, f.getErr
	}
	e, ok := f.entries[id]
	if !ok {
		return DLQEntry{}, ErrDLQEntryNotFound
	}
	return e, nil
}

func (f *fakeDLQReader) DeleteByID(ctx context.Context, id string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.entries[id]; !ok {
		return ErrDLQEntryNotFound
	}
	delete(f.entries, id)
	for i, existing := range f.order {
		if existing == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeDLQReader) Size(ctx context.Context) (int64, error) {
	if f.sizeErr != nil {
		return 0, f.sizeErr
	}
	return int64(len(f.order)), nil
}

// --- Helpers to manufacture a DLQ envelope ---

func dlqEnvelopeForTest(t *testing.T, id, originalSubject, batchID string) DLQEntry {
	t.Helper()
	batch := EditBatch{
		ID:        batchID,
		Timestamp: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Edits: []Edit{{
			Type:       EditTypeCreate,
			ObjectType: "employee",
			PrimaryKey: "emp-" + batchID,
			Properties: map[string]interface{}{"name": "DLQ-" + batchID},
		}},
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	return DLQEntry{
		ID:      id,
		Subject: BuildDLQSubject("employee"),
		Message: DLQMessage{
			OriginalSubject:  originalSubject,
			OriginalData:     raw,
			Reason:           "exceeded max deliveries (6/5)",
			NumDelivered:     6,
			MaxDeliveries:    5,
			TerminatedAt:     time.Date(2026, 5, 16, 12, 5, 0, 0, time.UTC),
			StreamSequence:   parseUintOrZero(t, id),
			ConsumerSequence: parseUintOrZero(t, id),
		},
	}
}

func parseUintOrZero(t *testing.T, s string) uint64 {
	t.Helper()
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// TestDLQReader_ListPending_ReturnsAllInOrder pins the contract that a freshly
// seeded reader returns every entry in stream insertion order.
func TestDLQReader_ListPending_ReturnsAllInOrder(t *testing.T) {
	reader := newFakeDLQReader()
	reader.seed(
		dlqEnvelopeForTest(t, "1", "edits.employee", "batch-a"),
		dlqEnvelopeForTest(t, "2", "edits.employee", "batch-b"),
		dlqEnvelopeForTest(t, "3", "edits.project", "batch-c"),
	)
	got, err := reader.ListPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	wantOrder := []string{"1", "2", "3"}
	for i, w := range wantOrder {
		if got[i].ID != w {
			t.Errorf("entry %d: id = %q, want %q", i, got[i].ID, w)
		}
	}
}

// TestDLQReader_ListPending_HonoursLimit pins the per-call cap.
func TestDLQReader_ListPending_HonoursLimit(t *testing.T) {
	reader := newFakeDLQReader()
	reader.seed(
		dlqEnvelopeForTest(t, "1", "edits.employee", "batch-a"),
		dlqEnvelopeForTest(t, "2", "edits.employee", "batch-b"),
		dlqEnvelopeForTest(t, "3", "edits.project", "batch-c"),
	)
	got, err := reader.ListPending(context.Background(), 2)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
}

// TestDLQReader_GetByID_NotFound asserts the sentinel error.
func TestDLQReader_GetByID_NotFound(t *testing.T) {
	reader := newFakeDLQReader()
	if _, err := reader.GetByID(context.Background(), "999"); !errors.Is(err, ErrDLQEntryNotFound) {
		t.Fatalf("expected ErrDLQEntryNotFound, got %v", err)
	}
}

// TestReplayDLQEntry_RepublishesOriginalSubject covers the happy path of the
// admin replay helper: read the envelope, publish OriginalData back to
// OriginalSubject, return the derived destination.
func TestReplayDLQEntry_RepublishesOriginalSubject(t *testing.T) {
	reader := newFakeDLQReader()
	reader.seed(dlqEnvelopeForTest(t, "42", "edits.employee", "batch-replay"))
	fake := &fakeDLQPublisher{}
	dest, err := ReplayDLQEntry(context.Background(), reader, "42", fake.Publish)
	if err != nil {
		t.Fatalf("ReplayDLQEntry: %v", err)
	}
	if dest != "edits.employee" {
		t.Fatalf("expected dest %q, got %q", "edits.employee", dest)
	}
	if len(fake.published) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(fake.published))
	}
	if fake.published[0].subject != "edits.employee" {
		t.Errorf("publish subject %q, want %q", fake.published[0].subject, "edits.employee")
	}
}

// TestReplayDLQEntry_PublisherFailureDoesNotDelete asserts that a failed
// republish leaves the DLQ row untouched so retry can be operator-driven.
func TestReplayDLQEntry_PublisherFailureDoesNotDelete(t *testing.T) {
	reader := newFakeDLQReader()
	reader.seed(dlqEnvelopeForTest(t, "7", "edits.employee", "batch-bad"))
	failingPublish := func(subj string, data []byte) error {
		return errors.New("nats publish failed")
	}
	if _, err := ReplayDLQEntry(context.Background(), reader, "7", failingPublish); err == nil {
		t.Fatal("expected publish error")
	}
	if _, err := reader.GetByID(context.Background(), "7"); err != nil {
		t.Fatalf("entry should remain in DLQ after failed replay, got %v", err)
	}
}

// TestReplayDLQEntry_NotFound surfaces the sentinel up the call chain.
func TestReplayDLQEntry_NotFound(t *testing.T) {
	reader := newFakeDLQReader()
	if _, err := ReplayDLQEntry(context.Background(), reader, "missing", func(string, []byte) error { return nil }); !errors.Is(err, ErrDLQEntryNotFound) {
		t.Fatalf("expected ErrDLQEntryNotFound, got %v", err)
	}
}

// TestReplayDLQEntry_NilPublisher rejects misconfiguration eagerly so the
// caller can render a 503 / clean error rather than swallowing the replay.
func TestReplayDLQEntry_NilPublisher(t *testing.T) {
	reader := newFakeDLQReader()
	reader.seed(dlqEnvelopeForTest(t, "1", "edits.employee", "batch-a"))
	if _, err := ReplayDLQEntry(context.Background(), reader, "1", nil); err == nil {
		t.Fatal("expected nil-publisher error")
	}
}

// TestDLQReader_InterfaceGuard pins the JetStream impl against the interface so
// the production wiring breaks the build (not just the runtime) when the
// contract drifts.
func TestDLQReader_InterfaceGuard(t *testing.T) {
	var _ DLQReader = (*JetStreamDLQReader)(nil)
	var _ DLQReader = (*fakeDLQReader)(nil)
}
