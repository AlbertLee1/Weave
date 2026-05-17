package funnel

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestBDD_US470_ConsumerFails_DLQEnqueued_AdminReplays_RepublishesPayload is
// the end-to-end BDD scenario for US-470: a downstream apply failure
// dead-letters an EditBatch via the consumer's existing publishToDLQ path,
// the admin DLQReader surface exposes the row, and ReplayDLQEntry pushes
// the original payload back onto the live `edits.<objectType>` subject —
// proving the manufacture-failure → DLQ → replay loop is wired top to
// bottom.
//
// The test wires the same DLQPublishFunc both Consumer.publishToDLQ and
// fakeDLQStream consume, so the DLQReader sees the exact bytes the
// consumer would have shipped to NATS. The replay path then exercises
// ReplayDLQEntry against a separate live publisher to assert the
// republish lands on the original subject.
func TestBDD_US470_ConsumerFails_DLQEnqueued_AdminReplays_RepublishesPayload(t *testing.T) {
	// Given: a Consumer wired with a fake DLQ store as its DLQPublishFunc
	// destination — mirrors the production NATS DLQ stream shape without
	// needing a running server.
	stream := newFakeDLQStream(t)
	consumer, _ := setupTestConsumer(t)
	consumer.dlqPublish = stream.AsDLQPublish()

	originalSubject := "edits.employee"
	originalPayload := EditBatch{
		ID:        "batch-bdd-470",
		UserID:    "user-bdd",
		Timestamp: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Edits: []Edit{{
			Type:       EditTypeCreate,
			ObjectType: "employee",
			PrimaryKey: "emp-bdd",
			Properties: map[string]interface{}{"name": "Dead"},
		}},
	}
	rawPayload, err := json.Marshal(originalPayload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// When: the consumer reaches max deliveries and dead-letters the message.
	if err := consumer.publishToDLQ(originalSubject, rawPayload, 6, 42, 38); err != nil {
		t.Fatalf("publishToDLQ: %v", err)
	}

	// Then: the DLQReader surface sees the entry with the original payload.
	entries, err := stream.ListPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Subject != "edits.dlq.employee" {
		t.Errorf("subject = %q, want %q", entry.Subject, "edits.dlq.employee")
	}
	if entry.Message.OriginalSubject != originalSubject {
		t.Errorf("originalSubject = %q, want %q", entry.Message.OriginalSubject, originalSubject)
	}
	var roundTrip EditBatch
	if err := json.Unmarshal(entry.Message.OriginalData, &roundTrip); err != nil {
		t.Fatalf("decode original data: %v", err)
	}
	if roundTrip.ID != originalPayload.ID {
		t.Errorf("roundtrip ID = %q, want %q", roundTrip.ID, originalPayload.ID)
	}

	// And: ReplayDLQEntry republishes onto the live subject and drains the DLQ.
	live := &fakeDLQPublisher{}
	subject, err := ReplayDLQEntry(context.Background(), stream, entry.ID, live.Publish)
	if err != nil {
		t.Fatalf("ReplayDLQEntry: %v", err)
	}
	if subject != originalSubject {
		t.Errorf("replay destination = %q, want %q", subject, originalSubject)
	}
	if len(live.published) != 1 {
		t.Fatalf("expected 1 republish, got %d", len(live.published))
	}
	if live.published[0].subject != originalSubject {
		t.Errorf("republish subject = %q, want %q", live.published[0].subject, originalSubject)
	}
	if string(live.published[0].data) != string(rawPayload) {
		t.Errorf("republish payload mismatch")
	}

	// And: the DLQ is empty after a successful replay so dashboards reflect
	// the drain immediately rather than waiting on JetStream MaxAge.
	size, err := stream.Size(context.Background())
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 0 {
		t.Errorf("post-replay size = %d, want 0", size)
	}

	// And: a subsequent ReplayDLQEntry call against the same id is a 404 —
	// the gate that prevents duplicate replays after a successful drain.
	if _, err := ReplayDLQEntry(context.Background(), stream, entry.ID, live.Publish); err == nil {
		t.Fatal("expected ErrDLQEntryNotFound on duplicate replay")
	}
}

// fakeDLQStream is an in-memory DLQReader that doubles as a DLQPublishFunc
// sink. It mimics the NATS JetStream contract (monotonically increasing
// sequence numbers; subject preserved per entry) without needing a real
// server, so the BDD test can exercise consumer-fail → DLQ-list →
// admin-replay end-to-end inside a single goroutine.
type fakeDLQStream struct {
	t  *testing.T
	mu sync.Mutex
	// entries indexed by id (decimal stream sequence). order preserves
	// insertion ordering so ListPending returns rows in stream order.
	entries map[string]DLQEntry
	order   []string
	nextSeq uint64
}

func newFakeDLQStream(t *testing.T) *fakeDLQStream {
	return &fakeDLQStream{
		t:       t,
		entries: map[string]DLQEntry{},
		nextSeq: 1,
	}
}

// AsDLQPublish returns a DLQPublishFunc that records every publish call as
// a new DLQ entry in the stream. Subject is preserved verbatim so the
// DLQ-side derivation (BuildDLQSubject) is captured in the entry.
func (s *fakeDLQStream) AsDLQPublish() DLQPublishFunc {
	return func(subject string, data []byte) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		seq := s.nextSeq
		s.nextSeq++
		id := strconv.FormatUint(seq, 10)
		var envelope DLQMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			s.t.Fatalf("fakeDLQStream: bad envelope: %v", err)
		}
		s.entries[id] = DLQEntry{
			ID:      id,
			Subject: subject,
			Message: envelope,
		}
		s.order = append(s.order, id)
		return nil
	}
}

func (s *fakeDLQStream) ListPending(ctx context.Context, limit int) ([]DLQEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.order) {
		limit = len(s.order)
	}
	out := make([]DLQEntry, 0, limit)
	for _, id := range s.order[:limit] {
		out = append(out, s.entries[id])
	}
	return out, nil
}

func (s *fakeDLQStream) GetByID(ctx context.Context, id string) (DLQEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return DLQEntry{}, ErrDLQEntryNotFound
	}
	return e, nil
}

func (s *fakeDLQStream) DeleteByID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return ErrDLQEntryNotFound
	}
	delete(s.entries, id)
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

func (s *fakeDLQStream) Size(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.order)), nil
}
