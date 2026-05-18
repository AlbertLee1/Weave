package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// chainStoreStub stubs out the narrow ChainReader interface so the
// publisher can be tested without a live MemoryStore.
type chainStoreStub struct {
	byDay map[string][]AuditEvent
	calls atomic.Int64
}

func (s *chainStoreStub) ListChainByDay(_ context.Context, day time.Time) ([]AuditEvent, error) {
	s.calls.Add(1)
	key := day.UTC().Format("2006-01-02")
	return s.byDay[key], nil
}

func TestRootHashPublisher_WritesLineForDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-roots.log")

	day := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	events := []AuditEvent{
		{ChainSeq: 1, EntryHash: "aaaa"},
		{ChainSeq: 2, EntryHash: "bbbb"},
	}
	expected := ComputeRootHash(events)

	pub := NewRootHashPublisher(&chainStoreStub{
		byDay: map[string][]AuditEvent{"2026-04-18": events},
	}, path)

	if err := pub.PublishDay(context.Background(), day); err != nil {
		t.Fatalf("PublishDay error = %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	line := strings.TrimRight(string(b), "\n")
	parts := strings.Split(line, "\t")
	if len(parts) != 2 {
		t.Fatalf("expected 'YYYY-MM-DD\\tHEX' format, got %q", line)
	}
	if parts[0] != "2026-04-18" {
		t.Errorf("date = %q, want 2026-04-18", parts[0])
	}
	if parts[1] != expected {
		t.Errorf("root hash = %q, want %q", parts[1], expected)
	}
}

func TestRootHashPublisher_AppendsNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-roots.log")

	store := &chainStoreStub{byDay: map[string][]AuditEvent{
		"2026-04-16": {{ChainSeq: 1, EntryHash: "h1"}},
		"2026-04-17": {{ChainSeq: 2, EntryHash: "h2"}},
	}}
	pub := NewRootHashPublisher(store, path)

	if err := pub.PublishDay(context.Background(), time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("publish day 1: %v", err)
	}
	if err := pub.PublishDay(context.Background(), time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("publish day 2: %v", err)
	}

	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(b))
	}
	if !strings.HasPrefix(lines[0], "2026-04-16\t") {
		t.Errorf("line 1 = %q, expected 2026-04-16 prefix", lines[0])
	}
	if !strings.HasPrefix(lines[1], "2026-04-17\t") {
		t.Errorf("line 2 = %q, expected 2026-04-17 prefix", lines[1])
	}
}

func TestRootHashPublisher_EmptyDaySkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-roots.log")

	store := &chainStoreStub{byDay: map[string][]AuditEvent{}}
	pub := NewRootHashPublisher(store, path)

	day := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if err := pub.PublishDay(context.Background(), day); err != nil {
		t.Fatalf("PublishDay on empty day should not error: %v", err)
	}

	// File should not be created for a no-op publication.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no root-hash file for empty day, got err = %v", err)
	}
}

func TestRootHashPublisher_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "audit-roots.log")

	store := &chainStoreStub{byDay: map[string][]AuditEvent{
		"2026-04-18": {{ChainSeq: 1, EntryHash: "aa"}},
	}}
	pub := NewRootHashPublisher(store, path)

	if err := pub.PublishDay(context.Background(), time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("PublishDay error = %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat published file: %v", err)
	}
}

func TestRootHashPublisher_FilePermissionsRestricted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-roots.log")
	store := &chainStoreStub{byDay: map[string][]AuditEvent{
		"2026-04-18": {{ChainSeq: 1, EntryHash: "aa"}},
	}}
	pub := NewRootHashPublisher(store, path)
	_ = pub.PublishDay(context.Background(), time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// File must be owner-only (0600) since it's a tamper-evidence
	// anchor — world-readable is acceptable in theory but unusual for
	// compliance artefacts.
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestRootHashPublisher_Scheduler_RunsImmediateCycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-roots.log")

	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	key := yesterday.Format("2006-01-02")
	store := &chainStoreStub{byDay: map[string][]AuditEvent{
		key: {{ChainSeq: 1, EntryHash: "h"}},
	}}

	pub := NewRootHashPublisher(store, path)
	// Disable the recurring loop; just exercise the immediate cycle.
	pub.SetInterval(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pub.Start(ctx)
	defer pub.Stop()

	// Wait for the first cycle to publish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if store.calls.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if store.calls.Load() == 0 {
		t.Fatal("expected scheduler to publish on first tick")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.HasPrefix(string(b), key) {
		t.Errorf("expected line prefixed with %q, got %q", key, string(b))
	}
}
