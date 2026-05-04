package materialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
)

// retainerTestNow returns a fixed UTC time used as "now" by tests.
func retainerTestNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}

// writeBatchAt persists a single edit through the Materializer with the
// supplied timestamp so the resulting filename embeds that timestamp.
func writeBatchAt(t *testing.T, m *Materializer, ts time.Time, ot, pk string) {
	t.Helper()
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-" + ts.Format("20060102T150405"),
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: ot,
			PrimaryKey: pk,
			Properties: map[string]interface{}{"ts": ts.UnixMilli()},
		}},
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
}

func listActiveFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".parquet") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func listArchiveFiles(t *testing.T, dir string) []string {
	t.Helper()
	return listActiveFiles(t, filepath.Join(dir, archiveSubdir))
}

func TestRetainer_NewRequiresRootDir(t *testing.T) {
	if _, err := NewRetainer(RetentionConfig{}); err == nil {
		t.Fatal("expected error for empty RootDir")
	}
}

func TestRetainer_RunOnce_NoFiles_NoOp(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        retainerTestNow,
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.CompactedFiles != 0 || stats.ArchivedFiles != 0 || stats.DeletedFiles != 0 {
		t.Fatalf("expected zero stats, got %+v", stats)
	}
}

func TestRetainer_Compact_MergesYesterdayFiles(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()
	yesterday := now.Add(-26 * time.Hour)

	m := NewMaterializer(dir)
	writeBatchAt(t, m, yesterday, "Customer", "C-1")
	writeBatchAt(t, m, yesterday.Add(time.Minute), "Customer", "C-2")
	writeBatchAt(t, m, yesterday.Add(2*time.Minute), "Customer", "C-3")

	otDir := filepath.Join(dir, "northwind", "Customer")
	if got := len(listActiveFiles(t, otDir)); got != 3 {
		t.Fatalf("expected 3 pre-compact files, got %d", got)
	}

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.CompactedFiles != 3 {
		t.Fatalf("expected CompactedFiles=3, got %d", stats.CompactedFiles)
	}
	if stats.CompactedSets != 1 {
		t.Fatalf("expected CompactedSets=1, got %d", stats.CompactedSets)
	}
	files := listActiveFiles(t, otDir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file post-compact, got %d (%v)", len(files), files)
	}
	if !strings.Contains(files[0], compactInfix) {
		t.Fatalf("expected compact infix in %s", files[0])
	}

	// Snapshot must still see all 3 PKs.
	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
}

func TestRetainer_Compact_LeavesTodayAlone(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()

	m := NewMaterializer(dir)
	writeBatchAt(t, m, now.Add(-3*time.Hour), "Customer", "C-1")
	writeBatchAt(t, m, now.Add(-2*time.Hour), "Customer", "C-2")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.CompactedFiles != 0 {
		t.Fatalf("expected no compaction today, got %+v", stats)
	}
	otDir := filepath.Join(dir, "northwind", "Customer")
	if got := len(listActiveFiles(t, otDir)); got != 2 {
		t.Fatalf("expected 2 files preserved, got %d", got)
	}
}

func TestRetainer_Compact_SingleFileLeftAlone(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()
	yesterday := now.Add(-26 * time.Hour)

	m := NewMaterializer(dir)
	writeBatchAt(t, m, yesterday, "Customer", "C-1")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.CompactedFiles != 0 || stats.CompactedSets != 0 {
		t.Fatalf("expected no compaction for single file, got %+v", stats)
	}
	otDir := filepath.Join(dir, "northwind", "Customer")
	files := listActiveFiles(t, otDir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file preserved, got %d", len(files))
	}
}

func TestRetainer_Archive_MovesAfter7Days(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()

	m := NewMaterializer(dir)
	writeBatchAt(t, m, now.Add(-8*24*time.Hour), "Customer", "C-old")
	writeBatchAt(t, m, now.Add(-6*24*time.Hour), "Customer", "C-recent")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.ArchivedFiles != 1 {
		t.Fatalf("expected ArchivedFiles=1, got %d", stats.ArchivedFiles)
	}
	otDir := filepath.Join(dir, "northwind", "Customer")
	active := listActiveFiles(t, otDir)
	if len(active) != 1 {
		t.Fatalf("expected 1 active file, got %d (%v)", len(active), active)
	}
	arch := listArchiveFiles(t, otDir)
	if len(arch) != 1 {
		t.Fatalf("expected 1 archived file, got %d (%v)", len(arch), arch)
	}
}

func TestRetainer_Archive_HiddenFromSnapshotReaders(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()

	m := NewMaterializer(dir)
	writeBatchAt(t, m, now.Add(-8*24*time.Hour), "Customer", "C-old")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	rows, err := m.BuildSnapshot(context.Background(), "northwind", "Customer", time.Time{})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("archived rows should be invisible to BuildSnapshot, got %d", len(rows))
	}
}

func TestRetainer_Delete_HardDeletesAfterRetention(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()

	m := NewMaterializer(dir)
	// Write something old enough to land in archive AND past the retention.
	writeBatchAt(t, m, now.Add(-31*24*time.Hour), "Customer", "C-veryold")
	// And something only old enough to be archived but not deleted.
	writeBatchAt(t, m, now.Add(-10*24*time.Hour), "Customer", "C-archived")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.ArchivedFiles != 2 {
		t.Fatalf("expected ArchivedFiles=2, got %d", stats.ArchivedFiles)
	}
	if stats.DeletedFiles != 1 {
		t.Fatalf("expected DeletedFiles=1, got %d", stats.DeletedFiles)
	}
	otDir := filepath.Join(dir, "northwind", "Customer")
	if files := listArchiveFiles(t, otDir); len(files) != 1 {
		t.Fatalf("expected 1 archived file remaining, got %d (%v)", len(files), files)
	}
}

func TestRetainer_Delete_DeleteAfterZeroDisablesDeletion(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()

	m := NewMaterializer(dir)
	writeBatchAt(t, m, now.Add(-365*24*time.Hour), "Customer", "C-ancient")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  0, // disables hard delete
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.DeletedFiles != 0 {
		t.Fatalf("expected zero deletions when DeleteAfter=0, got %d", stats.DeletedFiles)
	}
	otDir := filepath.Join(dir, "northwind", "Customer")
	if files := listArchiveFiles(t, otDir); len(files) != 1 {
		t.Fatalf("expected ancient file still archived, got %d (%v)", len(files), files)
	}
}

func TestRetainer_Compact_ReCompactionStable(t *testing.T) {
	dir := t.TempDir()
	now := retainerTestNow()
	yesterday := now.Add(-26 * time.Hour)

	m := NewMaterializer(dir)
	writeBatchAt(t, m, yesterday, "Customer", "C-1")
	writeBatchAt(t, m, yesterday.Add(time.Minute), "Customer", "C-2")

	r, err := NewRetainer(RetentionConfig{
		RootDir:      dir,
		ArchiveAfter: 7 * 24 * time.Hour,
		DeleteAfter:  30 * 24 * time.Hour,
		NowFn:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// Second sweep should be a no-op (single compacted file remains).
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce 2: %v", err)
	}
	if stats.CompactedFiles != 0 {
		t.Fatalf("expected idempotent compaction, got %+v", stats)
	}
}

func TestRetainer_RunOnce_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRetainer(RetentionConfig{
		RootDir: dir,
		NowFn:   retainerTestNow,
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		// Empty rootDir bypasses the per-dir loop where ctx is checked, so an
		// empty result is still acceptable. Materialise one file under a real
		// dir so the loop actually iterates.
		// Recreate the sub-tree:
		m := NewMaterializer(dir)
		writeBatchAt(t, m, retainerTestNow().Add(-26*time.Hour), "Customer", "C-1")
		writeBatchAt(t, m, retainerTestNow().Add(-26*time.Hour).Add(time.Minute), "Customer", "C-2")
		_, err = r.RunOnce(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	}
}

func TestParseFileTimestamp(t *testing.T) {
	cases := []struct {
		in    string
		want  time.Time
		valid bool
	}{
		{"20260504T100000_00000000000000000001.parquet", time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC), true},
		{"20260504T000000_compact_00000000000000000001.parquet", time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), true},
		{"not-a-parquet-file.parquet", time.Time{}, false},
		{"abcdefgh_12345.parquet", time.Time{}, false},
	}
	for _, tc := range cases {
		got, ok := parseFileTimestamp(tc.in)
		if ok != tc.valid {
			t.Errorf("parseFileTimestamp(%q) ok=%v, want %v", tc.in, ok, tc.valid)
			continue
		}
		if ok && !got.Equal(tc.want) {
			t.Errorf("parseFileTimestamp(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestRetainer_RunLoop_CancellableSleep(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRetainer(RetentionConfig{
		RootDir:         dir,
		CompactInterval: 24 * time.Hour,
		NowFn:           retainerTestNow,
	})
	if err != nil {
		t.Fatalf("NewRetainer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.RunLoop(ctx, nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not stop after context cancellation")
	}
}
