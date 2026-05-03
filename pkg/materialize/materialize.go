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
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/liyang/weave/pkg/funnel"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// EditRecord is the parquet row schema for materialized edits.
//
// __is_deleted and __patch_offset are required by the US-406 snapshot
// rebuild: a reader dedupes by (object_type, primary_key) keeping the
// row with the maximum __patch_offset, and discards it when
// __is_deleted is true. PropertiesJSON / MarkingsJSON store the
// per-edit user payload because the schema cannot anticipate every
// ObjectType's property shape.
type EditRecord struct {
	ObjectType     string `parquet:"object_type"`
	PrimaryKey     string `parquet:"primary_key"`
	EditType       string `parquet:"edit_type"`
	PropertiesJSON string `parquet:"properties_json"`
	MarkingsJSON   string `parquet:"markings_json"`
	Source         string `parquet:"source"`
	BatchID        string `parquet:"batch_id"`
	UserID         string `parquet:"user_id"`
	TimestampMs    int64  `parquet:"timestamp_ms"`
	IsDeleted      bool   `parquet:"__is_deleted"`
	PatchOffset    int64  `parquet:"__patch_offset"`
}

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
		if err := m.writeFile(batch.OntologyAPIName, ot, ts, records); err != nil {
			return err
		}
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

func (m *Materializer) writeFile(ontology, objectType string, ts time.Time, records []EditRecord) error {
	if len(records) == 0 {
		return nil
	}
	dir := filepath.Join(m.rootDir, ontology, objectType)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("materialize: mkdir %s: %w", dir, err)
	}
	name := fmt.Sprintf("%s_%020d.parquet", ts.Format("20060102T150405"), records[0].PatchOffset)
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return fmt.Errorf("materialize: open %s: %w", tmp, err)
	}
	w := parquet.NewGenericWriter[EditRecord](f)
	if _, err := w.Write(records); err != nil {
		_ = w.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("materialize: write rows: %w", err)
	}
	if err := w.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("materialize: close writer: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("materialize: close file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("materialize: rename %s: %w", tmp, err)
	}
	return nil
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
