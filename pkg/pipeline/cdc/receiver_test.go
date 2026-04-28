package cdc_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/pipeline/cdc"
)

// scriptedSource is the test fixture for cdc.Source. It hands out a
// pre-recorded list of pgoutput byte payloads then returns io.EOF so
// the receiver loop terminates cleanly. Watermark commits and Close
// calls are recorded for assertion.
type scriptedSource struct {
	mu          sync.Mutex
	scripts     [][]byte
	idx         int
	commits     []pglogrepl.LSN
	closed      bool
	failOnNext  error
	pauseAtIdx  int
	pauseSignal chan struct{}
}

func newScriptedSource(payloads [][]byte) *scriptedSource {
	return &scriptedSource{
		scripts:    payloads,
		pauseAtIdx: -1,
	}
}

func (s *scriptedSource) Next(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if s.failOnNext != nil {
		err := s.failOnNext
		s.failOnNext = nil
		s.mu.Unlock()
		return nil, err
	}
	if s.idx == s.pauseAtIdx && s.pauseSignal != nil {
		ch := s.pauseSignal
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		s.mu.Lock()
	}
	if s.idx >= len(s.scripts) {
		s.mu.Unlock()
		return nil, io.EOF
	}
	buf := s.scripts[s.idx]
	s.idx++
	s.mu.Unlock()
	return buf, nil
}

func (s *scriptedSource) CommitWatermark(_ context.Context, lsn pglogrepl.LSN) error {
	s.mu.Lock()
	s.commits = append(s.commits, lsn)
	s.mu.Unlock()
	return nil
}

func (s *scriptedSource) Close(_ context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// recordingPublisher accumulates published batches.
type recordingPublisher struct {
	mu      sync.Mutex
	batches []*funnel.EditBatch
	failOn  int
	calls   int
}

func (r *recordingPublisher) PublishBatch(_ context.Context, batch *funnel.EditBatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.failOn > 0 && r.calls == r.failOn {
		return errors.New("simulated publish failure")
	}
	clone := *batch
	clone.Edits = append([]funnel.Edit(nil), batch.Edits...)
	r.batches = append(r.batches, &clone)
	return nil
}

func (r *recordingPublisher) snapshot() []*funnel.EditBatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*funnel.EditBatch, len(r.batches))
	copy(out, r.batches)
	return out
}

// newReceiverWith wires a Receiver over the supplied scripted source,
// publisher, and config, using a fixed clock for deterministic batch
// timestamps.
func newReceiverWith(t *testing.T, src cdc.Source, pub cdc.Publisher, cfg *cdc.Config) *cdc.Receiver {
	t.Helper()
	r, err := cdc.NewReceiver(src, pub, cfg, cdc.Options{
		Now:    func() time.Time { return time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC) },
		Logger: func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("NewReceiver error: %v", err)
	}
	return r
}

func TestReceiver_HandleWAL_TransactionFlushesOnCommit(t *testing.T) {
	cfg := &cdc.Config{
		Tables: []cdc.TableMapping{newOrdersMapping()},
	}
	src := newScriptedSource(nil)
	pub := &recordingPublisher{}
	r := newReceiverWith(t, src, pub, cfg)
	ctx := context.Background()

	commitTs := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	mustHandle := func(msg pglogrepl.Message) {
		t.Helper()
		// We bypass the WAL byte path by using the decoder directly via
		// a dedicated method on Receiver — but Receiver only exposes
		// HandleWAL today. So we encode each typed message into a
		// byte payload using a small helper that round-trips via the
		// pglogrepl encoder. Since pglogrepl doesn't ship encoders for
		// these types we instead use the reflection-free shortcut:
		// build messages directly through the decoder, but the
		// receiver embeds the decoder privately. Easiest path: build
		// raw bytes by hand for the messages we need.
		_ = msg
	}
	_ = mustHandle

	// Build raw pgoutput bytes for a (Begin, Relation, Insert, Commit)
	// transaction using the helper functions defined below.
	begin := encodeBegin(0xabcd, commitTs)
	rel := encodeRelation(7001, "public", "orders", []relCol{
		{name: "id", key: true},
		{name: "customer_id", key: false},
		{name: "total", key: false},
	})
	insert := encodeInsert(7001, []textOrNull{textVal("10248"), textVal("ALFKI"), textVal("440.00")})
	commit := encodeCommit(0xabcd, commitTs)

	for _, buf := range [][]byte{begin, rel, insert, commit} {
		if err := r.HandleWAL(ctx, buf); err != nil {
			t.Fatalf("HandleWAL error: %v", err)
		}
	}

	batches := pub.snapshot()
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if batches[0].OntologyAPIName != "northwind" {
		t.Fatalf("ontology=%q", batches[0].OntologyAPIName)
	}
	if len(batches[0].Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(batches[0].Edits))
	}
	edit := batches[0].Edits[0]
	if edit.Type != funnel.EditTypeCreate || edit.PrimaryKey != "10248" || edit.Source != funnel.EditSourceIngest {
		t.Fatalf("edit shape wrong: %+v", edit)
	}
	if got := edit.Properties["customerId"]; got != "ALFKI" {
		t.Fatalf("customerId=%v want ALFKI", got)
	}
	if r.LastFlushedLSN() != 0xabcd {
		t.Fatalf("LastFlushedLSN=%s want 0xabcd", r.LastFlushedLSN().String())
	}
	if len(src.commits) != 1 || src.commits[0] != 0xabcd {
		t.Fatalf("watermark commits=%v want [0xabcd]", src.commits)
	}
}

func TestReceiver_UnmappedTableDropped(t *testing.T) {
	cfg := &cdc.Config{
		Tables: []cdc.TableMapping{newOrdersMapping()},
	}
	src := newScriptedSource(nil)
	pub := &recordingPublisher{}
	r := newReceiverWith(t, src, pub, cfg)
	ctx := context.Background()

	begin := encodeBegin(0x1, time.Now())
	// Relation NOT in mapping
	rel := encodeRelation(8001, "public", "audit_log", []relCol{{name: "id", key: true}})
	insert := encodeInsert(8001, []textOrNull{textVal("99")})
	commit := encodeCommit(0x1, time.Now())
	for _, buf := range [][]byte{begin, rel, insert, commit} {
		if err := r.HandleWAL(ctx, buf); err != nil {
			t.Fatalf("HandleWAL error: %v", err)
		}
	}
	if len(pub.snapshot()) != 0 {
		t.Fatalf("unmapped table should not produce batches")
	}
}

func TestReceiver_MultiOntologyBatchesSplit(t *testing.T) {
	cfg := &cdc.Config{
		Tables: []cdc.TableMapping{
			{
				Schema:            "public",
				Table:             "orders",
				OntologyAPIName:   "northwind",
				ObjectType:        "Order",
				PrimaryKeyColumns: []string{"id"},
			},
			{
				Schema:            "public",
				Table:             "tracks",
				OntologyAPIName:   "chinook",
				ObjectType:        "Track",
				PrimaryKeyColumns: []string{"id"},
			},
		},
	}
	src := newScriptedSource(nil)
	pub := &recordingPublisher{}
	r := newReceiverWith(t, src, pub, cfg)
	ctx := context.Background()

	begin := encodeBegin(0x42, time.Now())
	relOrders := encodeRelation(9001, "public", "orders", []relCol{{name: "id", key: true}})
	relTracks := encodeRelation(9002, "public", "tracks", []relCol{{name: "id", key: true}})
	insOrder := encodeInsert(9001, []textOrNull{textVal("100")})
	insTrack := encodeInsert(9002, []textOrNull{textVal("200")})
	commit := encodeCommit(0x42, time.Now())
	for _, buf := range [][]byte{begin, relOrders, relTracks, insOrder, insTrack, commit} {
		if err := r.HandleWAL(ctx, buf); err != nil {
			t.Fatalf("HandleWAL error: %v", err)
		}
	}
	batches := pub.snapshot()
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches (one per ontology), got %d", len(batches))
	}
	seen := map[string]int{}
	for _, b := range batches {
		seen[b.OntologyAPIName] += len(b.Edits)
	}
	if seen["northwind"] != 1 || seen["chinook"] != 1 {
		t.Fatalf("per-ontology edit counts wrong: %#v", seen)
	}
}

func TestReceiver_RunDrainsScriptedSource(t *testing.T) {
	cfg := &cdc.Config{Tables: []cdc.TableMapping{newOrdersMapping()}}
	begin := encodeBegin(0x7, time.Now())
	rel := encodeRelation(7001, "public", "orders", []relCol{
		{name: "id", key: true},
		{name: "customer_id"},
		{name: "total"},
		{name: "shipped_at"},
	})
	ins := encodeInsert(7001, []textOrNull{textVal("10248"), textVal("ALFKI"), textVal("440.00"), nullV()})
	commit := encodeCommit(0x7, time.Now())
	src := newScriptedSource([][]byte{begin, rel, ins, commit})
	pub := &recordingPublisher{}
	r := newReceiverWith(t, src, pub, cfg)

	err := r.Run(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Run error=%v want io.EOF", err)
	}
	if !src.closed {
		t.Fatalf("Source.Close should be invoked on Run shutdown")
	}
	if len(pub.snapshot()) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(pub.snapshot()))
	}
}

func TestReceiver_RunCancelsOnContext(t *testing.T) {
	cfg := &cdc.Config{Tables: []cdc.TableMapping{newOrdersMapping()}}
	src := newScriptedSource(nil)
	src.pauseAtIdx = 0
	src.pauseSignal = make(chan struct{})
	pub := &recordingPublisher{}
	r := newReceiverWith(t, src, pub, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return after context cancellation")
	}
}

func TestReceiver_PublisherFailureAborts(t *testing.T) {
	cfg := &cdc.Config{Tables: []cdc.TableMapping{newOrdersMapping()}}
	pub := &recordingPublisher{failOn: 1}
	src := newScriptedSource(nil)
	r := newReceiverWith(t, src, pub, cfg)
	ctx := context.Background()

	begin := encodeBegin(0x8, time.Now())
	rel := encodeRelation(7001, "public", "orders", []relCol{
		{name: "id", key: true},
		{name: "customer_id"},
		{name: "total"},
		{name: "shipped_at"},
	})
	ins := encodeInsert(7001, []textOrNull{textVal("10248"), textVal("ALFKI"), textVal("440.00"), nullV()})
	commit := encodeCommit(0x8, time.Now())

	for _, buf := range [][]byte{begin, rel, ins} {
		if err := r.HandleWAL(ctx, buf); err != nil {
			t.Fatalf("HandleWAL error: %v", err)
		}
	}
	err := r.HandleWAL(ctx, commit)
	if err == nil {
		t.Fatalf("expected publish error to bubble up")
	}
	if pub.calls != 1 {
		t.Fatalf("publisher should have been called once, got %d", pub.calls)
	}
}

func TestNewReceiver_Validation(t *testing.T) {
	cfg := &cdc.Config{Tables: []cdc.TableMapping{newOrdersMapping()}}
	if _, err := cdc.NewReceiver(nil, &recordingPublisher{}, cfg, cdc.Options{}); err == nil {
		t.Fatalf("expected nil-source error")
	}
	if _, err := cdc.NewReceiver(newScriptedSource(nil), nil, cfg, cdc.Options{}); err == nil {
		t.Fatalf("expected nil-publisher error")
	}
	bad := &cdc.Config{Tables: []cdc.TableMapping{{Schema: "public"}}}
	if _, err := cdc.NewReceiver(newScriptedSource(nil), &recordingPublisher{}, bad, cdc.Options{}); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestFunnelPublisher_NilSafe(t *testing.T) {
	var p *cdc.FunnelPublisher
	if err := p.PublishBatch(context.Background(), &funnel.EditBatch{}); err == nil {
		t.Fatalf("expected nil-receiver error")
	}
	p = &cdc.FunnelPublisher{}
	if err := p.PublishBatch(context.Background(), &funnel.EditBatch{}); err == nil {
		t.Fatalf("expected nil-inner error")
	}
}
