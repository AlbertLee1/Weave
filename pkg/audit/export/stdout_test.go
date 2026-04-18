package export

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/audit"
)

func TestStdoutExporter_EmitsNDJSON(t *testing.T) {
	var buf bytes.Buffer
	exp := NewStdoutExporter(&buf)

	events := []audit.AuditEvent{sampleEvent("a"), sampleEvent("b")}
	if err := exp.Export(context.Background(), events); err != nil {
		t.Fatalf("Export: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	for i, line := range lines {
		var out audit.AuditEvent
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			t.Fatalf("line %d not JSON: %v -- %q", i, err, line)
		}
		if out.ID != events[i].ID {
			t.Fatalf("line %d: got id=%q want %q", i, out.ID, events[i].ID)
		}
	}
}

func TestStdoutExporter_EmptyBatchNoop(t *testing.T) {
	var buf bytes.Buffer
	exp := NewStdoutExporter(&buf)
	if err := exp.Export(context.Background(), nil); err != nil {
		t.Fatalf("Export(nil): %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestStdoutExporter_Name(t *testing.T) {
	exp := NewStdoutExporter(nil)
	if got := exp.Name(); got != "stdout" {
		t.Fatalf("Name()=%q want stdout", got)
	}
}
