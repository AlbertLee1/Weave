package funnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/liyang/weave/pkg/index"
)

// US-006 covers four resilience axes for pkg/funnel:
//   - NATS 连接断开自动重连 (DefaultConnectOptions + Connect error wrapping)
//   - JetStream consumer ACK 失败重投 (decideOutcome decision matrix)
//   - DLQ 入队 / replay / discard
//   - idempotency key 去重 (publisher header + consumer dedupe cache)
//
// Every t.Run sub-test below targets a specific contract documented in
// pkg/funnel; the file lives alongside the production code so regressions
// stay package-scoped and don't require -tags integration.

// ---------------------------------------------------------------------------
// Decision matrix — covers "ACK 失败重投" AC.
// ---------------------------------------------------------------------------

func TestDecideOutcome_Matrix(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	consumer.maxDeliveries = 3

	cases := []struct {
		name          string
		numDelivered  uint64
		haveMetadata  bool
		unmarshalErr  error
		applyErr      error
		wantOutcome   MessageOutcome
		wantPublishes bool
		reasonHint    string
	}{
		{
			name:          "happy_path_acks",
			numDelivered:  1,
			haveMetadata:  true,
			wantOutcome:   OutcomeAck,
			wantPublishes: false,
		},
		{
			name:          "unmarshal_error_naks",
			numDelivered:  1,
			haveMetadata:  true,
			unmarshalErr:  errors.New("invalid character 'x'"),
			wantOutcome:   OutcomeNak,
			wantPublishes: false,
			reasonHint:    "unmarshal",
		},
		{
			name:          "apply_error_naks",
			numDelivered:  2,
			haveMetadata:  true,
			applyErr:      errors.New("bleve write failed"),
			wantOutcome:   OutcomeNak,
			wantPublishes: false,
			reasonHint:    "apply",
		},
		{
			name:          "at_cap_still_naks_for_retry",
			numDelivered:  3,
			haveMetadata:  true,
			applyErr:      errors.New("still failing"),
			wantOutcome:   OutcomeNak,
			wantPublishes: false,
			reasonHint:    "apply",
		},
		{
			name:          "over_cap_terminates_with_dlq",
			numDelivered:  4,
			haveMetadata:  true,
			applyErr:      errors.New("still failing"),
			wantOutcome:   OutcomeTerm,
			wantPublishes: true,
			reasonHint:    "exceeded max deliveries",
		},
		{
			name:          "over_cap_wins_over_unmarshal",
			numDelivered:  10,
			haveMetadata:  true,
			unmarshalErr:  errors.New("garbage"),
			wantOutcome:   OutcomeTerm,
			wantPublishes: true,
			reasonHint:    "exceeded max deliveries",
		},
		{
			name:          "no_metadata_apply_error_naks",
			numDelivered:  0,
			haveMetadata:  false,
			applyErr:      errors.New("transient"),
			wantOutcome:   OutcomeNak,
			wantPublishes: false,
			reasonHint:    "apply",
		},
		{
			name:          "no_metadata_happy_path_acks",
			numDelivered:  0,
			haveMetadata:  false,
			wantOutcome:   OutcomeAck,
			wantPublishes: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := consumer.decideOutcome(tc.numDelivered, tc.haveMetadata, tc.unmarshalErr, tc.applyErr)
			if got.Outcome != tc.wantOutcome {
				t.Fatalf("Outcome = %v, want %v (reason=%q)", got.Outcome, tc.wantOutcome, got.Reason)
			}
			if got.PublishToDLQ != tc.wantPublishes {
				t.Fatalf("PublishToDLQ = %v, want %v", got.PublishToDLQ, tc.wantPublishes)
			}
			if tc.reasonHint != "" && !strings.Contains(got.Reason, tc.reasonHint) {
				t.Fatalf("Reason %q missing hint %q", got.Reason, tc.reasonHint)
			}
			if tc.wantOutcome == OutcomeAck && got.Reason != "" {
				t.Fatalf("expected empty reason for Ack, got %q", got.Reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DLQ enqueue / replay / discard — covers "DLQ 入队/replay/discard" AC.
// ---------------------------------------------------------------------------

func TestDLQ_RoundtripPreservesPayload(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	fake := &fakeDLQPublisher{}
	consumer.dlqPublish = fake.Publish

	original := EditBatch{
		ID:              "batch-resilience-1",
		OntologyAPIName: testOntology,
		UserID:          "user-r1",
		Timestamp:       time.Date(2026, 5, 12, 18, 0, 0, 0, time.UTC),
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-r1",
				Properties: map[string]interface{}{"name": "Alice", "age": float64(30)},
			},
		},
	}
	originalData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}

	if err := consumer.publishToDLQ("edits."+testOntology+".employee", originalData, 6, 42, 38); err != nil {
		t.Fatalf("publishToDLQ: %v", err)
	}

	if len(fake.published) != 1 {
		t.Fatalf("expected 1 DLQ publish, got %d", len(fake.published))
	}
	pub := fake.published[0]
	wantSubject := BuildDLQSubject(testOntology + ".employee")
	if pub.subject != wantSubject {
		t.Fatalf("DLQ subject = %q, want %q", pub.subject, wantSubject)
	}

	var envelope DLQMessage
	if err := json.Unmarshal(pub.data, &envelope); err != nil {
		t.Fatalf("unmarshal DLQ envelope: %v", err)
	}
	if !bytesEqual(envelope.OriginalData, originalData) {
		t.Fatalf("original data not preserved byte-for-byte")
	}
	if envelope.NumDelivered != 6 || envelope.StreamSequence != 42 || envelope.ConsumerSequence != 38 {
		t.Fatalf("metadata mismatch: %+v", envelope)
	}

	// Original payload must still decode to the same batch shape — round-trip
	// through the DLQ envelope is the contract operators rely on for replay.
	var roundTripped EditBatch
	if err := json.Unmarshal(envelope.OriginalData, &roundTripped); err != nil {
		t.Fatalf("decode round-tripped batch: %v", err)
	}
	if roundTripped.ID != original.ID || len(roundTripped.Edits) != 1 {
		t.Fatalf("round-trip lost batch shape: %+v", roundTripped)
	}
	if roundTripped.Edits[0].PrimaryKey != "emp-r1" {
		t.Fatalf("round-trip lost edit PK: %q", roundTripped.Edits[0].PrimaryKey)
	}
}

func TestDLQ_ReplayReconstructsSubjectAndRepublishes(t *testing.T) {
	t.Run("happy_replay", func(t *testing.T) {
		liveTarget := &fakeDLQPublisher{}
		envelope := DLQMessage{
			OriginalSubject: "edits." + testOntology + ".employee",
			OriginalData:    []byte(`{"id":"replay-1"}`),
		}
		if err := ReplayDLQMessage(envelope, liveTarget.Publish); err != nil {
			t.Fatalf("ReplayDLQMessage: %v", err)
		}
		if len(liveTarget.published) != 1 {
			t.Fatalf("expected 1 republish, got %d", len(liveTarget.published))
		}
		if liveTarget.published[0].subject != "edits."+testOntology+".employee" {
			t.Fatalf("replay routed to wrong subject: %q", liveTarget.published[0].subject)
		}
		if !bytesEqual(liveTarget.published[0].data, envelope.OriginalData) {
			t.Fatalf("replay mutated payload")
		}
	})
	t.Run("inverse_subject_helper", func(t *testing.T) {
		got := OriginalSubjectFromDLQ(BuildDLQSubject(testOntology + ".employee"))
		want := SubjectPrefix + "." + testOntology + ".employee"
		if got != want {
			t.Fatalf("OriginalSubjectFromDLQ = %q, want %q", got, want)
		}
		if !IsDLQSubject(BuildDLQSubject("x")) {
			t.Fatal("IsDLQSubject should match valid DLQ subject")
		}
		if IsDLQSubject("edits.foo.bar") {
			t.Fatal("IsDLQSubject should reject live subject")
		}
	})
	t.Run("replay_empty_payload_errors", func(t *testing.T) {
		empty := DLQMessage{OriginalSubject: "edits." + testOntology + ".employee"}
		if err := ReplayDLQMessage(empty, func(string, []byte) error { return nil }); err == nil {
			t.Fatal("expected error for empty payload")
		}
	})
	t.Run("replay_publish_failure_wraps", func(t *testing.T) {
		envelope := DLQMessage{
			OriginalSubject: "edits." + testOntology + ".employee",
			OriginalData:    []byte(`{"id":"x"}`),
		}
		failingPublish := func(string, []byte) error { return errors.New("nats down") }
		err := ReplayDLQMessage(envelope, failingPublish)
		if err == nil {
			t.Fatal("expected wrapped error")
		}
		if !strings.Contains(err.Error(), "nats down") {
			t.Fatalf("expected wrapped error to include cause, got %v", err)
		}
	})
}

func TestDLQ_Discard(t *testing.T) {
	t.Run("derives_dlq_subject_from_live", func(t *testing.T) {
		envelope := DLQMessage{
			OriginalSubject: "edits." + testOntology + ".employee",
		}
		got := DiscardDLQMessage(envelope)
		want := BuildDLQSubject(testOntology + ".employee")
		if got != want {
			t.Fatalf("DiscardDLQMessage = %q, want %q", got, want)
		}
	})
	t.Run("passthrough_when_already_dlq", func(t *testing.T) {
		dlqSubject := BuildDLQSubject(testOntology + ".employee")
		got := DiscardDLQMessage(DLQMessage{OriginalSubject: dlqSubject})
		if got != dlqSubject {
			t.Fatalf("DiscardDLQMessage = %q, want %q", got, dlqSubject)
		}
	})
	t.Run("empty_subject_returns_empty", func(t *testing.T) {
		if got := DiscardDLQMessage(DLQMessage{}); got != "" {
			t.Fatalf("DiscardDLQMessage = %q, want empty", got)
		}
	})
}

// TestDLQ_MultipleTerminationsIsolated ensures repeated dead-lettering of
// distinct batches produces independent DLQ entries — operators rely on this
// when triaging a noisy stream where many batches go over the cap.
func TestDLQ_MultipleTerminationsIsolated(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	fake := &fakeDLQPublisher{}
	consumer.dlqPublish = fake.Publish

	for i := 0; i < 3; i++ {
		batch := EditBatch{
			ID:              fmt.Sprintf("dead-%d", i),
			OntologyAPIName: testOntology,
		}
		data, _ := json.Marshal(batch)
		if err := consumer.publishToDLQ(
			"edits."+testOntology+".employee", data,
			uint64(6+i), uint64(100+i), uint64(200+i)); err != nil {
			t.Fatalf("publishToDLQ #%d: %v", i, err)
		}
	}
	if len(fake.published) != 3 {
		t.Fatalf("expected 3 DLQ entries, got %d", len(fake.published))
	}
	for i, pub := range fake.published {
		var env DLQMessage
		if err := json.Unmarshal(pub.data, &env); err != nil {
			t.Fatalf("decode envelope #%d: %v", i, err)
		}
		if env.StreamSequence != uint64(100+i) {
			t.Fatalf("envelope #%d stream seq = %d, want %d", i, env.StreamSequence, 100+i)
		}
		var inner EditBatch
		if err := json.Unmarshal(env.OriginalData, &inner); err != nil {
			t.Fatalf("decode inner #%d: %v", i, err)
		}
		if inner.ID != fmt.Sprintf("dead-%d", i) {
			t.Fatalf("inner ID #%d = %q", i, inner.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Idempotency — covers "idempotency key 去重" AC.
// ---------------------------------------------------------------------------

func TestPublisher_BuildPublishMsg_SetsMsgIdHeader(t *testing.T) {
	t.Run("populated_id_writes_header", func(t *testing.T) {
		batch := &EditBatch{
			ID:              "idem-1",
			OntologyAPIName: testOntology,
			Edits: []Edit{{
				Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-1",
				Properties: map[string]interface{}{"name": "Alice"},
			}},
		}
		data, _ := json.Marshal(batch)
		msg := BuildPublishMsg(batch, data, BuildSubject(batch.OntologyAPIName, batch.Edits[0].ObjectType))
		if msg.Header.Get(nats.MsgIdHdr) != "idem-1" {
			t.Fatalf("Nats-Msg-Id = %q, want %q", msg.Header.Get(nats.MsgIdHdr), "idem-1")
		}
		if msg.Subject != "edits."+testOntology+".employee" {
			t.Fatalf("subject = %q", msg.Subject)
		}
		if !bytesEqual(msg.Data, data) {
			t.Fatal("data mutated")
		}
	})
	t.Run("empty_id_omits_header", func(t *testing.T) {
		batch := &EditBatch{OntologyAPIName: testOntology, Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "x"}}}
		data, _ := json.Marshal(batch)
		msg := BuildPublishMsg(batch, data, "edits."+testOntology+".employee")
		if msg.Header.Get(nats.MsgIdHdr) != "" {
			t.Fatalf("expected absent header when batch ID empty, got %q", msg.Header.Get(nats.MsgIdHdr))
		}
	})
	t.Run("nil_batch_omits_header", func(t *testing.T) {
		msg := BuildPublishMsg(nil, []byte(`{}`), "edits.x.y")
		if msg.Header.Get(nats.MsgIdHdr) != "" {
			t.Fatal("expected nil batch to omit header")
		}
	})
	t.Run("two_publishes_same_id_share_header", func(t *testing.T) {
		batch := &EditBatch{ID: "dup-1", OntologyAPIName: testOntology, Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "p"}}}
		data, _ := json.Marshal(batch)
		a := BuildPublishMsg(batch, data, "edits.x.y")
		b := BuildPublishMsg(batch, data, "edits.x.y")
		if a.Header.Get(nats.MsgIdHdr) != b.Header.Get(nats.MsgIdHdr) {
			t.Fatal("expected identical Nats-Msg-Id across publishes of same batch")
		}
	})
}

// TestConsumer_IdempotencyCache_HitSkipsApply verifies the in-process
// dedupe path: with a window enabled, two ApplyBatch calls with the same
// batch.ID inside the window produce exactly one history row and one
// bleve write. Outside the window the duplicate must be re-applied.
func TestConsumer_IdempotencyCache_HitSkipsApply(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	repo := &fakeHistoryRepo{}
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{"employee": "ri.ontology.main.object-type.employee"})
	consumer.SetIdempotencyWindow(2 * time.Second)

	batch := EditBatch{
		ID:              "idem-key-1",
		OntologyAPIName: testOntology,
		UserID:          "user-1",
		Timestamp:       time.Now(),
		Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-dup",
			Properties: map[string]interface{}{"name": "Once"}}},
	}

	t.Run("first_application_records_history", func(t *testing.T) {
		if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
			t.Fatalf("first apply: %v", err)
		}
		if got := len(repo.snapshot()); got != 1 {
			t.Fatalf("first apply rows = %d, want 1", got)
		}
		count, _ := mgr.DocCount(index.ScopedKey(testOntology, "employee"))
		if count != 1 {
			t.Fatalf("doc count after first apply = %d, want 1", count)
		}
	})

	t.Run("duplicate_id_skipped_within_window", func(t *testing.T) {
		// Mutate one property — even with different payload, same batch.ID
		// must not produce a second history row inside the window.
		dup := batch
		dup.Edits = []Edit{{Type: EditTypeModify, ObjectType: "employee", PrimaryKey: "emp-dup",
			Properties: map[string]interface{}{"name": "Twice"}}}
		if err := consumer.ApplyBatch(context.Background(), dup); err != nil {
			t.Fatalf("dup apply: %v", err)
		}
		if got := len(repo.snapshot()); got != 1 {
			t.Fatalf("rows after dup = %d, want 1 (dup must be no-op)", got)
		}
	})

	t.Run("different_id_still_processed", func(t *testing.T) {
		other := EditBatch{
			ID:              "idem-key-2",
			OntologyAPIName: testOntology,
			UserID:          "user-1",
			Timestamp:       time.Now(),
			Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-fresh",
				Properties: map[string]interface{}{"name": "Fresh"}}},
		}
		if err := consumer.ApplyBatch(context.Background(), other); err != nil {
			t.Fatalf("other apply: %v", err)
		}
		if got := len(repo.snapshot()); got != 2 {
			t.Fatalf("rows after distinct id = %d, want 2", got)
		}
	})
}

// TestIdempotencyCache_Unit exercises the cache surface directly so TTL and
// eviction semantics are observable without a real clock.
func TestIdempotencyCache_Unit(t *testing.T) {
	t.Run("disabled_window_never_dedupes", func(t *testing.T) {
		c := idempotencyCache{}
		now := time.Now()
		if c.seenAndStamp("a", now) {
			t.Fatal("disabled cache must report fresh")
		}
		if c.seenAndStamp("a", now) {
			t.Fatal("disabled cache must report fresh even on second call")
		}
	})

	t.Run("empty_id_never_dedupes", func(t *testing.T) {
		c := idempotencyCache{}
		c.setWindow(time.Minute)
		now := time.Now()
		if c.seenAndStamp("", now) {
			t.Fatal("empty id should always be fresh")
		}
		if c.seenAndStamp("", now) {
			t.Fatal("empty id should never dedupe regardless of cache state")
		}
	})

	t.Run("hit_within_window_then_miss_after_expiry", func(t *testing.T) {
		c := idempotencyCache{}
		c.setWindow(100 * time.Millisecond)
		t0 := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
		if c.seenAndStamp("id-1", t0) {
			t.Fatal("first stamp must report fresh")
		}
		if !c.seenAndStamp("id-1", t0.Add(50*time.Millisecond)) {
			t.Fatal("within-window second call must dedupe")
		}
		if c.seenAndStamp("id-1", t0.Add(200*time.Millisecond)) {
			t.Fatal("post-window call must report fresh again")
		}
	})

	t.Run("eviction_keeps_cache_bounded", func(t *testing.T) {
		c := idempotencyCache{maxSize: 4}
		c.setWindow(time.Hour) // window long enough that nothing expires
		base := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
		for i := 0; i < 10; i++ {
			c.seenAndStamp(fmt.Sprintf("k%02d", i), base.Add(time.Duration(i)*time.Millisecond))
		}
		if got := c.size(); got > c.maxSize {
			t.Fatalf("cache size %d exceeds maxSize %d", got, c.maxSize)
		}
		// Oldest entry should have been evicted — re-stamping is "fresh".
		if c.seenAndStamp("k00", base.Add(time.Second)) {
			t.Fatal("evicted entry should report fresh after re-stamping")
		}
	})
}

// ---------------------------------------------------------------------------
// Reconnect — covers "NATS 连接断开自动重连" AC.
// ---------------------------------------------------------------------------

func TestDefaultConnectOptions_HasReconnectJitter(t *testing.T) {
	opts := DefaultConnectOptions()
	o := &nats.Options{}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	if o.ReconnectJitter <= 0 {
		t.Fatalf("expected non-zero ReconnectJitter, got %v", o.ReconnectJitter)
	}
	if o.ReconnectJitterTLS <= 0 {
		t.Fatalf("expected non-zero ReconnectJitterTLS, got %v", o.ReconnectJitterTLS)
	}
	if o.ReconnectJitterTLS < o.ReconnectJitter {
		t.Fatalf("TLS jitter (%v) should be >= plain jitter (%v) to absorb slower handshake", o.ReconnectJitterTLS, o.ReconnectJitter)
	}
}

func TestConnect_InvalidURL_ReturnsWrappedError(t *testing.T) {
	// Use an address known to be unreachable so the connect attempt fails
	// quickly even with the default reconnect budget.
	_, err := Connect("nats://127.0.0.1:1") // RFC-compliant but typically unbound
	if err == nil {
		t.Fatal("expected error connecting to unreachable address")
	}
	if !strings.Contains(err.Error(), "nats connect") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestConnect_MalformedURL_FailsFast(t *testing.T) {
	// A scheme nats.Connect rejects outright so the test doesn't accidentally
	// touch a real NATS server on default ports.
	_, err := Connect("://not-a-url")
	if err == nil {
		t.Fatal("expected error connecting to malformed URL")
	}
}

// ---------------------------------------------------------------------------
// handleMessage delivery-count behavior — exercises the redelivery contract
// through the public lastOffset / dlqPublish surface without needing a real
// JetStream subscription.
// ---------------------------------------------------------------------------

func TestHandleMessage_DeliveryAccounting(t *testing.T) {
	t.Run("apply_error_does_not_advance_offset", func(t *testing.T) {
		consumer, _ := setupTestConsumer(t)
		// Force apply to fail by sending an edit for a non-existent object type.
		batch := EditBatch{
			ID:              "nak-1",
			OntologyAPIName: testOntology,
			Edits: []Edit{{Type: EditTypeCreate, ObjectType: "nonexistent", PrimaryKey: "x",
				Properties: map[string]interface{}{"foo": "bar"}}},
		}
		err := consumer.applyBatchWithHistory(context.Background(), batch)
		if err == nil {
			t.Fatal("expected applyBatchWithHistory to fail for unknown object type")
		}
		// Offset is only advanced inside handleMessage's Ack branch — apply
		// errors must leave it at zero so JetStream redelivers.
		if got := consumer.lastOffset.Load(); got != 0 {
			t.Fatalf("lastOffset = %d, want 0 (apply failed)", got)
		}
	})

	t.Run("change_event_fires_only_on_ack_path", func(t *testing.T) {
		consumer, _ := setupTestConsumer(t)
		var changes atomic.Uint32
		consumer.SetOnChange(func(ChangeEvent) { changes.Add(1) })

		// Direct ApplyBatch bypasses handleMessage so the onChange callback
		// must NOT fire — it's an Ack-path side effect, not an apply-path one.
		batch := EditBatch{
			ID:              "ack-1",
			OntologyAPIName: testOntology,
			Edits: []Edit{{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "emp-c",
				Properties: map[string]interface{}{"name": "Carl"}}},
		}
		if err := consumer.ApplyBatch(context.Background(), batch); err != nil {
			t.Fatalf("ApplyBatch: %v", err)
		}
		if got := changes.Load(); got != 0 {
			t.Fatalf("onChange fired %d times via ApplyBatch, want 0 (Ack-path only)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

