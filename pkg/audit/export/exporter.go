// Package export ships the SIEM-facing side of the audit pipeline. It
// defines the Exporter abstraction plus three built-in sinks (stdout,
// syslog, S3) and a BatchedExporter wrapper that adds configurable batch
// size and retry policy around any underlying Exporter.
//
// The stock PGStore in pkg/audit is the source of truth for audit events;
// the exporters are a tail that forwards a subset (or all) of events to an
// external observability/compliance system. A typical deployment wires a
// single BatchedExporter in cmd/server and hands it to the per-request
// audit recorder, which calls Enqueue after each PG Insert.
package export

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/audit"
)

// Exporter pushes a batch of audit events to an external sink.
// Implementations MUST be safe for concurrent Export calls and should be
// idempotent — BatchedExporter retries the same batch on transient errors.
type Exporter interface {
	Export(ctx context.Context, batch []audit.AuditEvent) error
	Name() string
}

// ExportError wraps the last error observed after a BatchedExporter has
// exhausted its retry budget, carrying the attempt count so operators /
// alert rules can distinguish "flaked and recovered" from "gave up".
type ExportError struct {
	Exporter string
	Attempts int
	Err      error
}

func (e *ExportError) Error() string {
	return fmt.Sprintf("audit export to %s failed after %d attempts: %v",
		e.Exporter, e.Attempts, e.Err)
}

func (e *ExportError) Unwrap() error { return e.Err }
