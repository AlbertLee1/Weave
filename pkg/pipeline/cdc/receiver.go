package cdc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"

	"github.com/liyang/weave/pkg/funnel"
)

// Source is the abstracted logical-replication transport. The
// production wiring uses a pglogrepl-backed implementation that
// streams XLogData from a real PG slot; tests inject an in-memory
// source that hands hand-built byte payloads back. The interface is
// pull-style so the Receiver controls back-pressure (no goroutine
// holds the buffered events between Source and consumer).
//
// Next blocks until the next message is available or ctx is canceled.
// Returning io.EOF or ctx.Err signals "stop without error". A normal
// (non-cancellation) error aborts the receiver loop.
//
// CommitWatermark must persist the LSN passed to it; the receiver
// invokes it after every successfully published transaction so that a
// crash + restart of either the receiver or PG resumes from the last
// committed LSN, never replaying already-applied edits.
type Source interface {
	Next(ctx context.Context) (walData []byte, err error)
	CommitWatermark(ctx context.Context, lsn pglogrepl.LSN) error
	Close(ctx context.Context) error
}

// Publisher is the destination for batches of edits the receiver
// produces. Production uses *funnel.Publisher; tests use an in-memory
// recorder that captures batches for assertion.
type Publisher interface {
	PublishBatch(ctx context.Context, batch *funnel.EditBatch) error
}

// FunnelPublisher adapts a funnel.Publisher to the cdc.Publisher
// interface. *funnel.Publisher.Publish returns (uint64, error); the
// uint64 is the JetStream sequence which is irrelevant to CDC.
type FunnelPublisher struct {
	Inner *funnel.Publisher
}

// PublishBatch implements Publisher.
func (p *FunnelPublisher) PublishBatch(_ context.Context, batch *funnel.EditBatch) error {
	if p == nil || p.Inner == nil {
		return errors.New("cdc: FunnelPublisher has no inner publisher")
	}
	_, err := p.Inner.Publish(batch)
	return err
}

// PublisherFunc adapts a plain function to Publisher.
type PublisherFunc func(ctx context.Context, batch *funnel.EditBatch) error

// PublishBatch implements Publisher.
func (f PublisherFunc) PublishBatch(ctx context.Context, batch *funnel.EditBatch) error {
	return f(ctx, batch)
}

// Receiver is the orchestrator that wires a Source through a Decoder
// to a Publisher, applying TableMappings on the way. Construct one
// per logical replication slot.
type Receiver struct {
	source    Source
	publisher Publisher
	config    *Config
	decoder   *Decoder
	now       func() time.Time
	logger    func(format string, v ...any)
	userID    string

	mu       sync.Mutex
	pending  map[string]*funnel.EditBatch
	lastLSN  pglogrepl.LSN
	lastTime time.Time
}

// Options tunes Receiver construction.
type Options struct {
	// UserID stamps EditBatch.UserID so the funnel consumer's audit
	// trail attributes CDC writes to a service identity. Defaults to
	// "cdc" when empty.
	UserID string
	// Now overrides time.Now for deterministic tests.
	Now func() time.Time
	// Logger receives non-fatal warnings (unmapped tables, parse
	// errors). Defaults to log.Printf.
	Logger func(format string, v ...any)
}

// NewReceiver wires a Receiver. source, publisher and config must be
// non-nil; opts may be the zero value.
func NewReceiver(source Source, publisher Publisher, config *Config, opts Options) (*Receiver, error) {
	if source == nil {
		return nil, errors.New("cdc: Source must not be nil")
	}
	if publisher == nil {
		return nil, errors.New("cdc: Publisher must not be nil")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Printf
	}
	userID := opts.UserID
	if userID == "" {
		userID = "cdc"
	}
	return &Receiver{
		source:    source,
		publisher: publisher,
		config:    config,
		decoder:   NewDecoder(),
		now:       now,
		logger:    logger,
		userID:    userID,
		pending:   make(map[string]*funnel.EditBatch),
	}, nil
}

// Run pumps the source until ctx is canceled or the source returns an
// error. Per-message decode/publish failures abort the loop with a
// wrapped error so operators can inspect the LSN that failed and
// either fix the mapping or skip past the offending transaction.
//
// Run is the canonical lifecycle entry point — Receiver doesn't
// expose a Start/Stop pair because the loop owns Source ordering and
// must run on a single goroutine to preserve PG's transactional
// ordering guarantees.
func (r *Receiver) Run(ctx context.Context) error {
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.source.Close(closeCtx); err != nil {
			r.logger("[cdc] source close failed: %v", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		buf, err := r.source.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("cdc: source next: %w", err)
		}
		if len(buf) == 0 {
			continue
		}
		if err := r.handle(ctx, buf); err != nil {
			return err
		}
	}
}

// HandleWAL is the synchronous variant of Run that processes one WAL
// payload. Exported for tests and for callers that already own a
// Source-equivalent (e.g. replay tools that read pre-recorded WAL
// from disk). Multi-call sequencing must follow PG's emission order:
// Begin → Relation → Insert/Update/Delete → Commit.
func (r *Receiver) HandleWAL(ctx context.Context, buf []byte) error {
	return r.handle(ctx, buf)
}

func (r *Receiver) handle(ctx context.Context, buf []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	events, commit, err := r.decoder.ProcessWAL(buf)
	if err != nil {
		return err
	}
	for _, ev := range events {
		mapping, ok := r.config.Lookup(ev.Schema, ev.Table)
		if !ok {
			r.logger("[cdc] unmapped change %s on %s.%s lsn=%s — dropped", ev.Op, ev.Schema, ev.Table, ev.LSN.String())
			continue
		}
		edit, err := EventToEdit(&ev, mapping)
		if err != nil {
			return fmt.Errorf("cdc: map %s on %s.%s: %w", ev.Op, ev.Schema, ev.Table, err)
		}
		batch, ok := r.pending[mapping.OntologyAPIName]
		if !ok {
			batch = &funnel.EditBatch{
				ID:              funnel.GenerateBatchID(),
				OntologyAPIName: mapping.OntologyAPIName,
				UserID:          r.userID,
				Timestamp:       r.eventTime(ev),
			}
			r.pending[mapping.OntologyAPIName] = batch
		}
		batch.Edits = append(batch.Edits, edit)
		if !ev.CommitTime.IsZero() {
			r.lastTime = ev.CommitTime
		}
		if ev.CommitLSN > r.lastLSN {
			r.lastLSN = ev.CommitLSN
		}
	}
	if !commit {
		return nil
	}
	return r.flushLocked(ctx)
}

func (r *Receiver) flushLocked(ctx context.Context) error {
	for ontology, batch := range r.pending {
		if len(batch.Edits) == 0 {
			delete(r.pending, ontology)
			continue
		}
		if err := r.publisher.PublishBatch(ctx, batch); err != nil {
			return fmt.Errorf("cdc: publish %s: %w", ontology, err)
		}
		delete(r.pending, ontology)
	}
	if r.lastLSN > 0 {
		if err := r.source.CommitWatermark(ctx, r.lastLSN); err != nil {
			return fmt.Errorf("cdc: commit watermark %s: %w", r.lastLSN.String(), err)
		}
	}
	return nil
}

func (r *Receiver) eventTime(ev ChangeEvent) time.Time {
	if !ev.CommitTime.IsZero() {
		return ev.CommitTime
	}
	return r.now()
}

// LastFlushedLSN returns the highest LSN the receiver has acknowledged
// upstream. Useful for metrics / health probes.
func (r *Receiver) LastFlushedLSN() pglogrepl.LSN {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastLSN
}
