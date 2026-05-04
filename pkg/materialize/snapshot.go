package materialize

import (
	"bytes"
	"context"
	"encoding/json"
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

// SnapshotRow is one alive row in a per-objectType snapshot. It carries
// the canonical wire-shape projected back from the EditRecord on disk —
// callers that need to project further (e.g. into a Bleve document) work
// from Properties + Markings directly.
type SnapshotRow struct {
	PrimaryKey  string
	Properties  map[string]interface{}
	Markings    []string
	PatchOffset int64
	TimestampMs int64
	BatchID     string
}

// BuildSnapshot replays every materialized edit under
// {rootDir}/{ontologyApiName}/{objectType}/ and returns the deduped live
// set as of asOf. Rules:
//
//   - Empty asOf (zero time) is "all time" — the live set as of the latest
//     materialized edit.
//   - For each PrimaryKey, the row with the highest PatchOffset wins.
//   - If that winning row is a DELETE the PrimaryKey is dropped from the
//     output entirely.
//
// Files for other ObjectTypes are never read because the directory layout
// already partitions by ObjectType. A missing directory yields an empty
// slice, not an error.
func (m *Materializer) BuildSnapshot(ctx context.Context, ontologyApiName, objectType string, asOf time.Time) ([]SnapshotRow, error) {
	if ontologyApiName == "" {
		return nil, errors.New("materialize: ontologyApiName is empty")
	}
	if objectType == "" {
		return nil, errors.New("materialize: objectType is empty")
	}

	dir := filepath.Join(m.rootDir, ontologyApiName, objectType)
	files, err := listObjectTypeParquet(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	var cutoffMs int64
	hasCutoff := !asOf.IsZero()
	if hasCutoff {
		cutoffMs = asOf.UnixMilli()
	}

	// Per-PK winner, keyed by PatchOffset (max wins).
	type winner struct {
		offset      int64
		isDeleted   bool
		propsJSON   string
		markingsRaw string
		batchID     string
		timestampMs int64
	}
	winners := make(map[string]winner)

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := readParquetEditRecords(path)
		if err != nil {
			return nil, fmt.Errorf("materialize: read %s: %w", path, err)
		}
		for _, r := range rows {
			if hasCutoff && r.TimestampMs > cutoffMs {
				continue
			}
			cur, ok := winners[r.PrimaryKey]
			if ok && cur.offset >= r.PatchOffset {
				continue
			}
			winners[r.PrimaryKey] = winner{
				offset:      r.PatchOffset,
				isDeleted:   r.IsDeleted,
				propsJSON:   r.PropertiesJSON,
				markingsRaw: r.MarkingsJSON,
				batchID:     r.BatchID,
				timestampMs: r.TimestampMs,
			}
		}
	}

	out := make([]SnapshotRow, 0, len(winners))
	for pk, w := range winners {
		if w.isDeleted {
			continue
		}
		props, err := decodePropertiesJSON(w.propsJSON)
		if err != nil {
			return nil, fmt.Errorf("materialize: decode properties for %s: %w", pk, err)
		}
		markings, err := decodeMarkingsJSON(w.markingsRaw)
		if err != nil {
			return nil, fmt.Errorf("materialize: decode markings for %s: %w", pk, err)
		}
		out = append(out, SnapshotRow{
			PrimaryKey:  pk,
			Properties:  props,
			Markings:    markings,
			PatchOffset: w.offset,
			TimestampMs: w.timestampMs,
			BatchID:     w.batchID,
		})
	}
	// Stable order so callers (CLI, snapshot writers) get reproducible output.
	sort.Slice(out, func(i, j int) bool { return out[i].PrimaryKey < out[j].PrimaryKey })
	return out, nil
}

// listObjectTypeParquet returns every .parquet file directly under dir,
// sorted lexicographically. Missing dir is not an error.
func listObjectTypeParquet(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("materialize: read dir %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".parquet") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files, nil
}

func readParquetEditRecords(path string) ([]EditRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows, err := parquet.Read[EditRecord](bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func decodePropertiesJSON(raw string) (map[string]interface{}, error) {
	if raw == "" {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeMarkingsJSON(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var m []string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}
