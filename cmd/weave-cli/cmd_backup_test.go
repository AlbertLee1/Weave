package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liyang/weave/internal/backup"
)

// TestBackupCLIRequiresOutput exercises the missing -o validation arm.
func TestBackupCLIRequiresOutput(t *testing.T) {
	cfg := t.TempDir()
	_, stderr, exit := runCLIWith(t, cfg, "backup", "--pg-dsn", "postgres://x")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "-o") || !strings.Contains(stderr, "required") {
		t.Fatalf("stderr should mention required output: %q", stderr)
	}
}

// TestBackupCLIRequiresDSN exercises the missing dsn validation arm; both
// the flag and PG_DSN env var are unset.
func TestBackupCLIRequiresDSN(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("PG_DSN", "")
	_, stderr, exit := runCLIWith(t, cfg, "backup", "-o", filepath.Join(cfg, "out.tar.gz"))
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "pg-dsn") {
		t.Fatalf("stderr should mention pg-dsn: %q", stderr)
	}
}

func TestRestoreCLIRequiresInput(t *testing.T) {
	cfg := t.TempDir()
	_, stderr, exit := runCLIWith(t, cfg, "restore", "--pg-dsn", "postgres://x")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "-i") {
		t.Fatalf("stderr should mention required input: %q", stderr)
	}
}

// TestBackupRestoreCLIRoundTrip drives a full bundle build + extract using
// stub pg_dump / pg_restore functions. Asserts the integration witness
// from the PRD: 备份→清库→恢复→数据一致.
func TestBackupRestoreCLIRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	writeRepoFile(t, filepath.Join(dataDir, "indexes", "Customer", "store", "root.bolt"), "I-bytes")
	writeRepoFile(t, filepath.Join(dataDir, "materialized", "nw", "Customer", "f.parquet"), "P-bytes")
	writeRepoFile(t, filepath.Join(dataDir, "media", "blob.bin"), "M-bytes")

	out := filepath.Join(tmp, "backup.tar.gz")
	stub := newStubPG()
	withInjectedPG(t, stub, func() {
		cfg := t.TempDir()
		stdout, stderr, exit := runCLIWith(t, cfg,
			"backup", "-o", out,
			"--data-dir", dataDir,
			"--pg-dsn", "postgres://stub",
		)
		if exit != 0 {
			t.Fatalf("backup exit=%d stderr=%q", exit, stderr)
		}
		if !strings.Contains(stdout, "Backup written") {
			t.Fatalf("stdout missing 'Backup written': %q", stdout)
		}
	})

	// Wipe the data dir to simulate "clean target".
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatalf("remove dataDir: %v", err)
	}

	withInjectedPG(t, stub, func() {
		cfg := t.TempDir()
		stdout, stderr, exit := runCLIWith(t, cfg,
			"restore", "-i", out,
			"--data-dir", dataDir,
			"--pg-dsn", "postgres://stub",
		)
		if exit != 0 {
			t.Fatalf("restore exit=%d stderr=%q", exit, stderr)
		}
		if !strings.Contains(stdout, "Restore complete") {
			t.Fatalf("stdout missing 'Restore complete': %q", stdout)
		}
	})

	if got := stub.lastDump.String(); got == "" {
		t.Fatalf("stub did not capture pg_dump output")
	}
	if got := stub.lastRestore.String(); got != stub.lastDump.String() {
		t.Fatalf("restore stream %q != dump stream %q", got, stub.lastDump.String())
	}

	checkRepoFile(t, filepath.Join(dataDir, "indexes", "Customer", "store", "root.bolt"), "I-bytes")
	checkRepoFile(t, filepath.Join(dataDir, "materialized", "nw", "Customer", "f.parquet"), "P-bytes")
	checkRepoFile(t, filepath.Join(dataDir, "media", "blob.bin"), "M-bytes")
}

func TestBackupCLIJSONManifest(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	writeRepoFile(t, filepath.Join(dataDir, "indexes", "X", "marker"), "x")
	out := filepath.Join(tmp, "b.tar.gz")
	stub := newStubPG()
	withInjectedPG(t, stub, func() {
		cfg := t.TempDir()
		stdout, stderr, exit := runCLIWith(t, cfg,
			"backup", "-o", out,
			"--data-dir", dataDir,
			"--pg-dsn", "postgres://stub",
			"--json",
		)
		if exit != 0 {
			t.Fatalf("exit=%d stderr=%q", exit, stderr)
		}
		var m backup.Manifest
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &m); err != nil {
			t.Fatalf("parse json manifest %q: %v", stdout, err)
		}
		if m.Version != backup.BundleVersion {
			t.Fatalf("manifest.Version = %d", m.Version)
		}
		if _, ok := m.Components["db.dump"]; !ok {
			t.Fatalf("manifest missing db.dump component: %+v", m)
		}
	})
}

// --- helpers -------------------------------------------------------------

type stubPG struct {
	lastDump    bytes.Buffer
	lastRestore bytes.Buffer
}

func newStubPG() *stubPG { return &stubPG{} }

// withInjectedPG temporarily replaces the package-level dump/restore hooks
// so the CLI uses the test stub instead of real exec.Command shell-outs.
func withInjectedPG(t *testing.T, stub *stubPG, fn func()) {
	t.Helper()
	prevDump := defaultPGDumpFn
	prevRestore := defaultPGRestoreFn
	defaultPGDumpFn = func(_ context.Context, _ string, w io.Writer) error {
		stub.lastDump.Reset()
		body := "STUB-DUMP-" + randString()
		stub.lastDump.WriteString(body)
		_, err := io.Copy(w, strings.NewReader(body))
		return err
	}
	defaultPGRestoreFn = func(_ context.Context, _ string, r io.Reader) error {
		stub.lastRestore.Reset()
		_, err := io.Copy(&stub.lastRestore, r)
		return err
	}
	t.Cleanup(func() {
		defaultPGDumpFn = prevDump
		defaultPGRestoreFn = prevRestore
	})
	fn()
}

// randString seeds the stub dump with non-trivial bytes so the backup ↔
// restore comparison can't accidentally match on zero-length payloads.
func randString() string {
	var buf bytes.Buffer
	for i := 0; i < 64; i++ {
		buf.WriteByte(byte('a' + (i*17+13)%26))
	}
	return buf.String()
}

func writeRepoFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func checkRepoFile(t *testing.T, p, want string) {
	t.Helper()
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	if string(body) != want {
		t.Fatalf("file %s body = %q, want %q", p, string(body), want)
	}
}
