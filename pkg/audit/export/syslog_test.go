package export

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/audit"
)

func TestSyslogExporter_RFC3164Format(t *testing.T) {
	var buf bytes.Buffer
	exp := NewSyslogExporterWriter(&buf, SyslogOptions{
		Facility: SyslogFacilityUser,
		Severity: SyslogSeverityInfo,
		Hostname: "weave-host",
		AppName:  "weave",
	})

	if err := exp.Export(context.Background(), []audit.AuditEvent{sampleEvent("x")}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	out := buf.String()
	// <14> = facility=1 (user) * 8 + severity=6 (info) = 14
	if !strings.HasPrefix(out, "<14>") {
		t.Fatalf("expected priority <14>, got %q", out[:min(20, len(out))])
	}
	if !strings.Contains(out, "weave-host") {
		t.Fatalf("expected hostname in output, got %q", out)
	}
	if !strings.Contains(out, "weave:") {
		t.Fatalf("expected app name in output, got %q", out)
	}
	if !strings.Contains(out, `"id":"x"`) {
		t.Fatalf("expected event JSON in output, got %q", out)
	}
	// RFC3164 timestamp shape "Mon DD HH:MM:SS" (Apr 19 12:00:00).
	tsRe := regexp.MustCompile(`[A-Z][a-z]{2} (?: \d|\d\d) \d\d:\d\d:\d\d`)
	if !tsRe.MatchString(out) {
		t.Fatalf("expected RFC3164 timestamp in output, got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected newline suffix, got %q", out)
	}
}

func TestSyslogExporter_MultipleEventsOneLineEach(t *testing.T) {
	var buf bytes.Buffer
	exp := NewSyslogExporterWriter(&buf, SyslogOptions{})

	err := exp.Export(context.Background(), []audit.AuditEvent{
		sampleEvent("a"), sampleEvent("b"), sampleEvent("c"),
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), buf.String())
	}
}

func TestSyslogExporter_DefaultsFillIn(t *testing.T) {
	var buf bytes.Buffer
	// Zero-value options should still produce valid output.
	exp := NewSyslogExporterWriter(&buf, SyslogOptions{})
	if err := exp.Export(context.Background(), []audit.AuditEvent{sampleEvent("x")}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "<") {
		t.Fatalf("expected priority prefix, got %q", buf.String())
	}
}

func TestSyslogExporter_Name(t *testing.T) {
	exp := NewSyslogExporterWriter(nil, SyslogOptions{})
	if got := exp.Name(); got != "syslog" {
		t.Fatalf("Name()=%q want syslog", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
