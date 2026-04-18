package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_RequiresDSN(t *testing.T) {
	// Isolate from any ambient PG_DSN so the binary doesn't accidentally
	// connect to a live DB during unit tests.
	t.Setenv("PG_DSN", "")

	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (operator error)", code)
	}
	if !strings.Contains(stderr.String(), "-dsn is required") {
		t.Errorf("stderr should explain -dsn requirement: %q", stderr.String())
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-nonsense"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
