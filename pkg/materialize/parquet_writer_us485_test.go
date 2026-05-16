package materialize

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/liyang/weave/pkg/funnel"
)

// TestUS485_ParquetWriter_RealFile asserts the parquet writer emits a
// physically-real Parquet file (PAR1 magic header + readable footer)
// rather than a JSON/CSV stub. The PRD literal calls for "真实写
// Parquet"; a byte-level header check is the cheapest reproducible proof
// that swapping the writer for a fake encoding would fail loudly.
func TestUS485_ParquetWriter_RealFile(t *testing.T) {
	root := t.TempDir()
	m := NewMaterializer(root)
	ts := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	m.SetNowFunc(func() time.Time { return ts })

	batch := funnel.EditBatch{
		ID:              "tx-us485-writer",
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "c-1", Properties: map[string]interface{}{"id": "c-1", "name": "alpha"}},
		},
	}
	if err := m.MaterializeBatch(context.Background(), batch); err != nil {
		t.Fatalf("MaterializeBatch: %v", err)
	}

	dir := filepath.Join(root, "northwind", "Customer")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 parquet file, got %d", len(entries))
	}
	path := filepath.Join(dir, entries[0].Name())
	if !strings.HasSuffix(path, ".parquet") {
		t.Fatalf("file must have .parquet suffix, got %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Parquet files begin and end with the PAR1 magic string. Both checks
	// fail loud if the writer ever degrades to JSON / CSV / gob.
	if len(data) < 8 || !bytes.HasPrefix(data, []byte("PAR1")) {
		t.Fatalf("file is not a real Parquet (missing PAR1 prefix): first8=%q", data[:min(8, len(data))])
	}
	if !bytes.HasSuffix(data, []byte("PAR1")) {
		t.Fatalf("file is not a real Parquet (missing PAR1 suffix): last8=%q", data[max(0, len(data)-8):])
	}

	// The footer must decode through the parquet-go schema so any silent
	// schema drift surfaces as a hard test failure.
	rows, err := parquet.Read[EditRecord](bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parquet.Read: %v", err)
	}
	if len(rows) != 1 || rows[0].PrimaryKey != "c-1" {
		t.Fatalf("decoded rows: want 1×c-1, got %+v", rows)
	}
}

// TestUS485_ParquetWriter_SchemaPublishesEditRecord pins the schema
// columns that downstream tier-router readers depend on. A regression
// renaming or dropping one of the metadata columns would silently break
// snapshot rebuild and cold-tier reads.
func TestUS485_ParquetWriter_SchemaPublishesEditRecord(t *testing.T) {
	schema := parquet.SchemaOf(EditRecord{})
	want := []string{
		"object_type",
		"primary_key",
		"edit_type",
		"properties_json",
		"markings_json",
		"source",
		"batch_id",
		"user_id",
		"timestamp_ms",
		"__is_deleted",
		"__patch_offset",
	}
	cols := schema.Columns()
	got := make([]string, 0, len(cols))
	for _, c := range cols {
		// Each column path is a single segment for the flat EditRecord schema.
		got = append(got, c[len(c)-1])
	}
	if len(got) != len(want) {
		t.Fatalf("column count: want %d, got %d (%v)", len(want), len(got), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("column[%d]: want %q, got %q (full=%v)", i, name, got[i], got)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
