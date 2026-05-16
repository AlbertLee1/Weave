// File parquet_writer.go owns the on-disk Parquet schema and the
// row-level writer the Materializer drives. The PRD (US-485) calls out
// this file path explicitly so the cold-tier writer has a single
// canonical home — readers (snapshot rebuild, tier router) consume the
// same EditRecord schema declared here.
//
// We use the pure-Go parquet-go/parquet-go implementation rather than
// apache-arrow's CGO-backed reader. The PRD parenthetical mentions
// apache-arrow Go; parquet-go is a real, on-disk Parquet 1.0 producer
// (PAR1 magic header + Thrift footer + columnar encodings) so the
// load-bearing PRD criterion "真实写 Parquet" is satisfied without the
// CGO toolchain churn. Tests in parquet_writer_us485_test.go pin the
// physical file shape so a downgrade to a stub encoding would fail
// loudly.
package materialize

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
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

// writeParquetFile is the row-level Parquet writer. It writes to a
// temp file first and atomically renames into place so a crash between
// open and close can never expose a half-written file to readers. The
// returned size is the on-disk byte count, used for cost accounting.
//
// records MUST be non-empty; the caller (Materializer.writeFile) does
// that check before paying for the file open.
func writeParquetFile(path string, records []EditRecord) (int64, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return 0, fmt.Errorf("materialize: mkdir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return 0, fmt.Errorf("materialize: open %s: %w", tmp, err)
	}
	w := parquet.NewGenericWriter[EditRecord](f)
	if _, err := w.Write(records); err != nil {
		_ = w.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("materialize: write rows: %w", err)
	}
	if err := w.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("materialize: close writer: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("materialize: close file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("materialize: rename %s: %w", tmp, err)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return 0, nil
	}
	return info.Size(), nil
}
