package audit

// Daily root-hash publication for US-266 tamper-proof audit logs.
//
// Every day, the server computes a single sha256 root over the entry_hash
// values of every audit_events row whose timestamp falls in that UTC day,
// and appends one `YYYY-MM-DD\t<hex>\n` line to a configured file. The
// file lives on append-only storage (or is itself write-once via the FS /
// filesystem-snapshot guarantees of the host) so a later attacker can't
// rewrite published anchors to cover up a tampered chain.
//
// Verification compares the file's per-day root against a recomputed root
// over the live DB chain — mismatches are tamper evidence.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultRootHashInterval is the publication cadence when none is set.
// 24h matches "daily" in the US-266 acceptance criteria; operators can
// dial it down for more frequent anchoring.
const DefaultRootHashInterval = 24 * time.Hour

// ChainDayReader returns every audit event whose timestamp falls on the
// given UTC day ORDERED BY chain_seq ASC. A single-store method keeps the
// publisher's data dependency narrow (no need for a List+filter surface).
type ChainDayReader interface {
	ListChainByDay(ctx context.Context, day time.Time) ([]AuditEvent, error)
}

// RootHashPublisher periodically computes the previous UTC day's chain
// root and appends it to an append-only file.
type RootHashPublisher struct {
	store    ChainDayReader
	path     string
	interval time.Duration
	nowFunc  func() time.Time

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewRootHashPublisher wires a publisher around store, writing anchors to
// path. path's parent directory is created on first publish.
func NewRootHashPublisher(store ChainDayReader, path string) *RootHashPublisher {
	return &RootHashPublisher{
		store:    store,
		path:     path,
		interval: DefaultRootHashInterval,
		nowFunc:  time.Now,
	}
}

// SetInterval overrides the publish cadence. Values <= 0 leave the
// interval unchanged, protecting against misconfigured wiring.
func (p *RootHashPublisher) SetInterval(d time.Duration) {
	if d > 0 {
		p.interval = d
	}
}

// SetNowFunc injects a deterministic clock for tests.
func (p *RootHashPublisher) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		p.nowFunc = fn
	}
}

// PublishDay computes the root hash for the UTC day containing `day` and
// appends a single line to the target file. Empty days are a no-op —
// nothing is appended so the file only accumulates evidence for days
// that actually produced events.
func (p *RootHashPublisher) PublishDay(ctx context.Context, day time.Time) error {
	events, err := p.store.ListChainByDay(ctx, day)
	if err != nil {
		return fmt.Errorf("audit root publisher: list chain for %s: %w",
			day.UTC().Format("2006-01-02"), err)
	}
	if len(events) == 0 {
		return nil
	}
	root := ComputeRootHash(events)
	line := fmt.Sprintf("%s\t%s\n", day.UTC().Format("2006-01-02"), root)

	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return fmt.Errorf("audit root publisher: mkdir %s: %w",
			filepath.Dir(p.path), err)
	}
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit root publisher: open %s: %w", p.path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("audit root publisher: write %s: %w", p.path, err)
	}
	return nil
}

// Start launches the publication loop. Runs the YESTERDAY cycle
// immediately on boot, then once per interval. Returns immediately; the
// loop exits when ctx is cancelled OR Stop is called. Idempotent —
// calling Start twice is a no-op.
func (p *RootHashPublisher) Start(ctx context.Context) {
	p.mu.Lock()
	if p.stopCh != nil {
		p.mu.Unlock()
		return
	}
	p.stopCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	p.mu.Unlock()

	go func() {
		defer close(p.doneCh)
		p.runOnce(ctx)
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopCh:
				return
			case <-t.C:
				p.runOnce(ctx)
			}
		}
	}()
}

// Stop cancels the loop and waits for the in-flight cycle to drain.
func (p *RootHashPublisher) Stop() {
	p.mu.Lock()
	stopCh, doneCh := p.stopCh, p.doneCh
	p.stopCh = nil
	p.mu.Unlock()
	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (p *RootHashPublisher) runOnce(ctx context.Context) {
	// Publish YESTERDAY's anchor — today may still be receiving events,
	// and the cut-off at midnight UTC is the only clock boundary that
	// doesn't drift under server-clock skew.
	yesterday := p.nowFunc().UTC().AddDate(0, 0, -1)
	if err := p.PublishDay(ctx, yesterday); err != nil {
		log.Printf("audit root publisher: %v", err)
	}
}
