package export

import (
	"context"
	"log"

	"github.com/liyang/weave/pkg/audit"
)

// EventEnqueuer is the narrow subset of BatchedExporter used by the tee
// store — just enough to buffer events without exposing the Flush /
// retry surface. Any future sink (async queue, kinesis, kafka) can
// satisfy this interface.
type EventEnqueuer interface {
	Enqueue(ctx context.Context, evt audit.AuditEvent) error
}

// TeeStore wraps a primary audit.Store (e.g. audit.PGStore) and
// tees every persisted event to an EventEnqueuer. The tee is
// best-effort: export failures DO NOT propagate back to the Insert
// caller, because losing observability is strictly better than losing
// the audit trail itself. Export errors are logged via the stdlib log
// package so operators can notice without instrumenting every call
// site.
//
// When exporter is nil, TeeStore is a transparent passthrough — makes
// degraded-mode (disabled export) wiring trivial in cmd/server.
type TeeStore struct {
	inner    audit.Store
	exporter EventEnqueuer
}

// NewTeeStore wraps inner with a best-effort tee to exporter.
func NewTeeStore(inner audit.Store, exporter EventEnqueuer) *TeeStore {
	return &TeeStore{inner: inner, exporter: exporter}
}

func (t *TeeStore) Insert(ctx context.Context, evt audit.AuditEvent) error {
	if err := t.inner.Insert(ctx, evt); err != nil {
		return err
	}
	if t.exporter != nil {
		// Fire-and-forget. Use context.Background() so a cancelled
		// HTTP request context doesn't racily abort the export path
		// AFTER the insert has already committed — the event will
		// live in PG regardless, and we want the downstream sink to
		// get it.
		go func(evt audit.AuditEvent) {
			if err := t.exporter.Enqueue(context.Background(), evt); err != nil {
				log.Printf("audit export enqueue failed: %v", err)
			}
		}(evt)
	}
	return nil
}

func (t *TeeStore) List(ctx context.Context, f audit.ListFilter) ([]audit.AuditEvent, error) {
	return t.inner.List(ctx, f)
}
