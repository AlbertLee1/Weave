package export

import (
	"context"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

func sampleEvent(id string) audit.AuditEvent {
	return audit.AuditEvent{
		ID:           id,
		ActorID:      "user:alice",
		Action:       "object.update",
		ResourceType: "Customer",
		ResourceRID:  "ri.oms.main.object.c-1",
		IP:           "10.0.0.1",
		UserAgent:    "weave-cli/1.0",
		Timestamp:    time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
	}
}

// flakyExporter fails the first N Export calls, then succeeds.
type flakyExporter struct {
	name        string
	failures    int
	attempts    int
	lastBatch   []audit.AuditEvent
	batchCounts []int
}

func (f *flakyExporter) Name() string { return f.name }

func (f *flakyExporter) Export(_ context.Context, batch []audit.AuditEvent) error {
	f.attempts++
	f.batchCounts = append(f.batchCounts, len(batch))
	if f.attempts <= f.failures {
		return errTransient
	}
	f.lastBatch = append([]audit.AuditEvent(nil), batch...)
	return nil
}

var errTransient = &transientErr{}

type transientErr struct{}

func (*transientErr) Error() string { return "transient failure" }
