package export

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/liyang/weave/pkg/audit"
)

// StdoutExporter writes each audit event as one JSON line to the configured
// writer (defaults to os.Stdout). Designed for container log collectors
// (Loki, Datadog Agent, CloudWatch Log Driver) that tail stdout.
type StdoutExporter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewStdoutExporter returns a StdoutExporter writing to w. A nil w falls
// back to os.Stdout so callers can keep configuration minimal.
func NewStdoutExporter(w io.Writer) *StdoutExporter {
	if w == nil {
		w = os.Stdout
	}
	return &StdoutExporter{w: w}
}

func (e *StdoutExporter) Name() string { return "stdout" }

func (e *StdoutExporter) Export(_ context.Context, batch []audit.AuditEvent) error {
	if len(batch) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	enc := json.NewEncoder(e.w)
	for i := range batch {
		if err := enc.Encode(&batch[i]); err != nil {
			return err
		}
	}
	return nil
}
