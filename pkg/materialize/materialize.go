// Package materialize writes funnel.EditBatch payloads to columnar Parquet
// files on disk so downstream snapshot rebuilds (US-406) and cold-tier
// queries (US-407) can replay the change log without going through Bleve.
//
// File layout: {rootDir}/{ontologyApiName}/{objectType}/{ts}_{offset}.parquet
// where ts is the batch's Timestamp formatted as YYYYMMDDTHHMMSS (UTC) and
// offset is the writer-local monotonic __patch_offset of the first row in
// the file. One Parquet file is produced per (batch, objectType) tuple; a
// batch that touches N object types writes N files.
//
// Each row carries the canonical metadata columns __is_deleted (BOOL) and
// __patch_offset (INT64). Properties and Markings round-trip via JSON-
// encoded string columns so the schema does not have to track per-object
// shape — readers use json.Unmarshal to project back into a Go map.
//
// Link edits (LINK_CREATE / LINK_DELETE) are deliberately not materialized:
// they have no per-object snapshot semantics and are persisted via the
// link_edges table.
package materialize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/metrics"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// EditRecord — see parquet_writer.go for the canonical schema (declared
// alongside the file writer so the on-disk shape lives in one place).

// Materializer writes EditBatches to per-(ontology, objectType) Parquet
// files under rootDir. Safe for concurrent use; the offset counter is
// atomically incremented so two goroutines running MaterializeBatch
// concurrently still produce strictly increasing __patch_offset values.
type Materializer struct {
	rootDir string
	counter atomic.Int64
	nowFn   func() time.Time
}

// NewMaterializer returns a Materializer that writes parquet files under
// rootDir. The directory tree is created lazily on first write so callers
// can pass a path that does not yet exist.
func NewMaterializer(rootDir string) *Materializer {
	return &Materializer{
		rootDir: rootDir,
		nowFn:   func() time.Time { return time.Now().UTC() },
	}
}

// SetNowFunc overrides the wall clock used as a fallback when an EditBatch
// arrives with a zero Timestamp. Tests use it to pin filenames.
func (m *Materializer) SetNowFunc(fn func() time.Time) {
	if fn == nil {
		return
	}
	m.nowFn = fn
}

// RootDir returns the configured root directory. Useful for tests and
// operational tooling.
func (m *Materializer) RootDir() string {
	return m.rootDir
}

// MaterializeBatch persists the supplied funnel.EditBatch to disk as one
// Parquet file per (batch, objectType) tuple. Empty batches and batches
// containing only link edits are no-ops. A non-empty batch with an empty
// OntologyAPIName is rejected so misrouted writes cannot land under an
// empty path component.
//
// MaterializeBatch satisfies the funnel.EditMaterializer interface so a
// configured Materializer can be wired directly into the funnel consumer
// via Consumer.SetEditMaterializer.
func (m *Materializer) MaterializeBatch(_ context.Context, batch funnel.EditBatch) error {
	if len(batch.Edits) == 0 {
		return nil
	}
	if batch.OntologyAPIName == "" {
		return errors.New("materialize: ontologyApiName is empty")
	}

	grouped := make(map[string][]funnel.Edit)
	for _, e := range batch.Edits {
		if e.Type == funnel.EditTypeLinkCreate || e.Type == funnel.EditTypeLinkDelete {
			continue
		}
		grouped[e.ObjectType] = append(grouped[e.ObjectType], e)
	}
	if len(grouped) == 0 {
		return nil
	}

	ts := batch.Timestamp
	if ts.IsZero() {
		ts = m.nowFn()
	}
	ts = ts.UTC()

	keys := make([]string, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, ot := range keys {
		records, err := m.buildRecords(batch, ts, grouped[ot])
		if err != nil {
			return err
		}
		size, err := m.writeFile(batch.OntologyAPIName, ot, ts, records)
		if err != nil {
			return err
		}
		metrics.MaterializeFileWritten(batch.OntologyAPIName, ot, m.nowFn().Sub(ts), size)
		metrics.RecordOntologyStorageBytes(batch.OntologyAPIName, metrics.CostStorageKindParquet, size)
	}
	return nil
}

func (m *Materializer) buildRecords(batch funnel.EditBatch, ts time.Time, edits []funnel.Edit) ([]EditRecord, error) {
	records := make([]EditRecord, 0, len(edits))
	for _, e := range edits {
		propsJSON, err := encodePropertiesJSON(e)
		if err != nil {
			return nil, fmt.Errorf("materialize: properties JSON for %s/%s: %w", e.ObjectType, e.PrimaryKey, err)
		}
		markingsJSON, err := encodeMarkingsJSON(e.Markings)
		if err != nil {
			return nil, fmt.Errorf("materialize: markings JSON for %s/%s: %w", e.ObjectType, e.PrimaryKey, err)
		}
		offset := m.counter.Add(1)
		records = append(records, EditRecord{
			ObjectType:     e.ObjectType,
			PrimaryKey:     e.PrimaryKey,
			EditType:       string(e.Type),
			PropertiesJSON: propsJSON,
			MarkingsJSON:   markingsJSON,
			Source:         e.Source,
			BatchID:        batch.ID,
			UserID:         batch.UserID,
			TimestampMs:    ts.UnixMilli(),
			IsDeleted:      e.Type == funnel.EditTypeDelete,
			PatchOffset:    offset,
		})
	}
	return records, nil
}

func (m *Materializer) writeFile(ontology, objectType string, ts time.Time, records []EditRecord) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}
	dir := filepath.Join(m.rootDir, ontology, objectType)
	name := fmt.Sprintf("%s_%020d.parquet", ts.Format("20060102T150405"), records[0].PatchOffset)
	path := filepath.Join(dir, name)
	return writeParquetFile(path, records)
}

// encodePropertiesJSON returns the JSON encoding of an edit's properties
// map. DELETE edits and edits with no properties produce an empty string
// so downstream readers can cheaply distinguish "no payload" from
// "explicit null map" without a separate column.
func encodePropertiesJSON(e funnel.Edit) (string, error) {
	if e.Type == funnel.EditTypeDelete {
		return "", nil
	}
	if len(e.Properties) == 0 {
		return "", nil
	}
	b, err := json.Marshal(e.Properties)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func encodeMarkingsJSON(markings []string) (string, error) {
	if len(markings) == 0 {
		return "", nil
	}
	b, err := json.Marshal(markings)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
