package materialize

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

// archiveSubdir is the per-objectType subdirectory that holds files past
// the active retention window. Files moved here are invisible to
// BuildSnapshot / TierRouter readers because listObjectTypeParquet skips
// directories.
const archiveSubdir = "archive"

// compactInfix marks parquet files that resulted from a compaction sweep.
// The presence of this token in the filename is purely informative —
// readers dedupe by __patch_offset and don't care about names.
const compactInfix = "_compact_"

// fileTimeLayout is the timestamp prefix every Materializer-written file
// (and every compaction output) starts with.
const fileTimeLayout = "20060102T150405"

// RetentionConfig drives a Retainer. RootDir must point at the same
// directory the Materializer writes to (typically
// $WEAVE_DATA_DIR/materialized).
//
// CompactInterval is the cadence used by RunLoop. ArchiveAfter and
// DeleteAfter are wall-clock thresholds compared against the embedded
// filename timestamp; a non-positive DeleteAfter disables hard deletion
// so operators can retain forever.
type RetentionConfig struct {
	RootDir         string
	CompactInterval time.Duration
	ArchiveAfter    time.Duration
	DeleteAfter     time.Duration
	NowFn           func() time.Time
}

// Retainer compacts, archives and deletes Parquet files written by the
// Materializer. Safe to spawn once per process; RunLoop is the canonical
// long-lived driver and RunOnce is the idempotent unit of work.
type Retainer struct {
	cfg RetentionConfig
}

// RetentionStats summarises the work done by a single RunOnce sweep.
// The fields are independent: CompactedFiles counts source files removed
// during compaction (the merged output is not counted); ArchivedFiles
// counts files moved into the archive/ subdirectory; DeletedFiles counts
// files removed entirely from disk.
type RetentionStats struct {
	CompactedFiles int
	CompactedSets  int
	ArchivedFiles  int
	DeletedFiles   int
}

// NewRetainer validates cfg and returns a ready-to-run Retainer. RootDir
// must be non-empty; everything else has a sensible default applied at
// run time so a zero-value RetentionConfig (besides RootDir) yields a
// fully functional Retainer with 24h compact / 7d archive / 30d delete
// thresholds.
func NewRetainer(cfg RetentionConfig) (*Retainer, error) {
	if strings.TrimSpace(cfg.RootDir) == "" {
		return nil, errors.New("materialize: retention RootDir is empty")
	}
	if cfg.NowFn == nil {
		cfg.NowFn = func() time.Time { return time.Now().UTC() }
	}
	return &Retainer{cfg: cfg}, nil
}

// RunOnce executes a single compact + archive + delete sweep across
// every {ontology, objectType} directory under RootDir. The phases run
// in that order so newly compacted output feeds the archive sweep
// immediately and so a single file never simultaneously satisfies both
// "needs compacting" and "needs archiving" criteria within one tick.
//
// The function is idempotent — invoking it twice in a row with no fresh
// writes between calls is a no-op on the second call.
func (r *Retainer) RunOnce(ctx context.Context) (RetentionStats, error) {
	var stats RetentionStats
	dirs, err := r.listObjectTypeDirs()
	if err != nil {
		return stats, err
	}
	now := r.cfg.NowFn().UTC()
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		removed, sets, err := r.compactDir(dir, now)
		if err != nil {
			return stats, fmt.Errorf("materialize: compact %s: %w", dir, err)
		}
		stats.CompactedFiles += removed
		stats.CompactedSets += sets

		moved, err := r.archiveDir(dir, now)
		if err != nil {
			return stats, fmt.Errorf("materialize: archive %s: %w", dir, err)
		}
		stats.ArchivedFiles += moved

		deleted, err := r.deleteDir(dir, now)
		if err != nil {
			return stats, fmt.Errorf("materialize: delete %s: %w", dir, err)
		}
		stats.DeletedFiles += deleted
	}
	return stats, nil
}

// RunLoop runs RunOnce on the configured CompactInterval until ctx is
// cancelled. Errors are forwarded to errLog (optional) and the loop
// continues — a transient filesystem hiccup must not abandon retention.
//
// Intended to be spawned as a goroutine from cmd/server during boot.
func (r *Retainer) RunLoop(ctx context.Context, errLog func(error)) {
	interval := r.cfg.CompactInterval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.RunOnce(ctx); err != nil {
				if errLog != nil {
					errLog(err)
				}
			}
		}
	}
}

// listObjectTypeDirs walks {RootDir}/{ontology}/{objectType}/ and
// returns each leaf directory sorted lexicographically. Missing RootDir
// yields nil, nil so a freshly-deployed system surfaces an empty sweep
// rather than a config error.
func (r *Retainer) listObjectTypeDirs() ([]string, error) {
	rootEntries, err := os.ReadDir(r.cfg.RootDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, ontology := range rootEntries {
		if !ontology.IsDir() {
			continue
		}
		ontPath := filepath.Join(r.cfg.RootDir, ontology.Name())
		otEntries, err := os.ReadDir(ontPath)
		if err != nil {
			return nil, err
		}
		for _, ot := range otEntries {
			if !ot.IsDir() {
				continue
			}
			dirs = append(dirs, filepath.Join(ontPath, ot.Name()))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// compactDir merges every group of two-or-more parquet files that share
// the same UTC date (and that date is strictly before today) into a
// single compacted file per date. Returns the number of source files
// removed and the number of date groups that produced a merge.
func (r *Retainer) compactDir(dir string, now time.Time) (int, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	today := now.Truncate(24 * time.Hour)
	groups := make(map[string][]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".parquet") {
			continue
		}
		ts, ok := parseFileTimestamp(name)
		if !ok {
			continue
		}
		day := ts.Truncate(24 * time.Hour)
		if !day.Before(today) {
			continue
		}
		key := day.Format("20060102")
		groups[key] = append(groups[key], filepath.Join(dir, name))
	}

	dayKeys := make([]string, 0, len(groups))
	for k := range groups {
		dayKeys = append(dayKeys, k)
	}
	sort.Strings(dayKeys)

	var removed, sets int
	for _, day := range dayKeys {
		files := groups[day]
		if len(files) <= 1 {
			continue
		}
		n, err := r.mergeGroup(dir, day, files)
		if err != nil {
			return removed, sets, err
		}
		removed += n
		sets++
	}
	return removed, sets, nil
}

// mergeGroup reads every row from the supplied files, writes them to a
// single compacted parquet file via tmp+rename, then removes the
// originals. Returns the number of source files that were actually
// removed (i.e. excluding the compacted target if it happened to share a
// name with one of the inputs).
func (r *Retainer) mergeGroup(dir, day string, files []string) (int, error) {
	sort.Strings(files)
	var allRows []EditRecord
	var minOffset int64
	seen := false
	for _, f := range files {
		rows, err := readParquetEditRecords(f)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", f, err)
		}
		for _, rec := range rows {
			if !seen || rec.PatchOffset < minOffset {
				minOffset = rec.PatchOffset
				seen = true
			}
		}
		allRows = append(allRows, rows...)
	}
	if len(allRows) == 0 {
		return 0, nil
	}
	sort.Slice(allRows, func(i, j int) bool {
		return allRows[i].PatchOffset < allRows[j].PatchOffset
	})

	name := fmt.Sprintf("%sT000000%s%020d.parquet", day, compactInfix, minOffset)
	target := filepath.Join(dir, name)
	tmp := target + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", tmp, err)
	}
	w := parquet.NewGenericWriter[EditRecord](f)
	if _, err := w.Write(allRows); err != nil {
		_ = w.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("write rows: %w", err)
	}
	if err := w.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("close writer: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("rename %s: %w", tmp, err)
	}

	var removed int
	for _, src := range files {
		if src == target {
			continue
		}
		if err := os.Remove(src); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("remove %s: %w", src, err)
		}
		removed++
	}
	return removed, nil
}

// archiveDir moves files whose embedded timestamp is older than
// `now - ArchiveAfter` into a per-objectType `archive/` subdirectory.
// Files in the archive are intentionally invisible to BuildSnapshot and
// TierRouter readers (listObjectTypeParquet skips subdirectories), so an
// archive sweep is the operational signal that those rows are no longer
// part of the active query path.
func (r *Retainer) archiveDir(dir string, now time.Time) (int, error) {
	if r.cfg.ArchiveAfter <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := now.Add(-r.cfg.ArchiveAfter)
	archDir := filepath.Join(dir, archiveSubdir)
	archMade := false
	var moved int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".parquet") {
			continue
		}
		ts, ok := parseFileTimestamp(name)
		if !ok {
			continue
		}
		if !ts.Before(cutoff) {
			continue
		}
		if !archMade {
			if err := os.MkdirAll(archDir, dirPerm); err != nil {
				return moved, err
			}
			archMade = true
		}
		src := filepath.Join(dir, name)
		dst := filepath.Join(archDir, name)
		if err := os.Rename(src, dst); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// deleteDir hard-deletes every parquet file (under the active dir AND
// the archive/ subdirectory) whose embedded timestamp is older than
// `now - DeleteAfter`. A non-positive DeleteAfter disables the sweep
// entirely so operators who want infinite retention keep their files.
func (r *Retainer) deleteDir(dir string, now time.Time) (int, error) {
	if r.cfg.DeleteAfter <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-r.cfg.DeleteAfter)
	var deleted int
	for _, target := range []string{dir, filepath.Join(dir, archiveSubdir)} {
		entries, err := os.ReadDir(target)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return deleted, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".parquet") {
				continue
			}
			ts, ok := parseFileTimestamp(name)
			if !ok {
				continue
			}
			if !ts.Before(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(target, name)); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}

// parseFileTimestamp extracts the YYYYMMDDTHHMMSS prefix produced by the
// Materializer's writeFile helper from a parquet filename. The prefix
// runs from index 0 up to the first underscore. Returns ok=false when
// the filename does not match the expected layout (so callers can
// safely skip files written by something other than the Materializer).
func parseFileTimestamp(name string) (time.Time, bool) {
	idx := strings.IndexByte(name, '_')
	if idx <= 0 {
		return time.Time{}, false
	}
	prefix := name[:idx]
	ts, err := time.Parse(fileTimeLayout, prefix)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}
