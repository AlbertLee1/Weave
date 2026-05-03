package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/liyang/weave/pkg/funnel"
)

// listParquetFiles walks dir and returns every .parquet file path, sorted.
func listParquetFiles(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".parquet") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(found)
	return found
}

// readParquet reads every row of a parquet file written by the
// Materializer back into the canonical EditRecord struct.
func readParquet(t *testing.T, path string) []EditRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rows, err := parquet.Read[EditRecord](bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("parquet read %s: %v", path, err)
	}
	return rows
}

func TestMaterializer_Empty_NoFiles(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)

	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-empty",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if files := listParquetFiles(t, dir); len(files) != 0 {
		t.Fatalf("expected zero files, got %v", files)
	}
}

func TestMaterializer_RejectsBatchWithoutOntology(t *testing.T) {
	m := NewMaterializer(t.TempDir())
	err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1"},
		},
	})
	if err == nil {
		t.Fatal("expected error when ontologyApiName is empty")
	}
}

func TestMaterializer_WritesAtConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)
	ts := time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC)

	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-1",
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Customer",
				PrimaryKey: "C-1",
				Properties: map[string]interface{}{"name": "Alice"},
			},
		},
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	files := listParquetFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	got := files[0]
	expectedDir := filepath.Join(dir, "northwind", "Customer")
	if !strings.HasPrefix(got, expectedDir+string(os.PathSeparator)) {
		t.Fatalf("file %s not under %s", got, expectedDir)
	}
	if !strings.HasSuffix(got, ".parquet") {
		t.Fatalf("file %s missing .parquet suffix", got)
	}
	if !strings.Contains(filepath.Base(got), "20260504T103000") {
		t.Fatalf("file %s does not embed batch timestamp", got)
	}
}

func TestMaterializer_GroupsByObjectType(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)
	ts := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)

	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-mixed",
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-1", Properties: map[string]interface{}{"name": "Alice"}},
			{Type: funnel.EditTypeCreate, ObjectType: "Order", PrimaryKey: "O-1", Properties: map[string]interface{}{"total": 12.5}},
			{Type: funnel.EditTypeModify, ObjectType: "Customer", PrimaryKey: "C-2", Properties: map[string]interface{}{"name": "Bob"}},
		},
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	files := listParquetFiles(t, dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 files (one per object type), got %d: %v", len(files), files)
	}

	totalRecords := 0
	for _, p := range files {
		rows := readParquet(t, p)
		totalRecords += len(rows)
	}
	if totalRecords != 3 {
		t.Fatalf("expected 3 records across files, got %d", totalRecords)
	}
}

func TestMaterializer_HundredEdits_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)
	ts := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	const n = 100
	edits := make([]funnel.Edit, 0, n)
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			edits = append(edits, funnel.Edit{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Customer",
				PrimaryKey: fmt.Sprintf("C-%03d", i),
				Properties: map[string]interface{}{
					"name":  fmt.Sprintf("Cust %d", i),
					"index": float64(i),
				},
				Markings: []string{"PUBLIC"},
			})
		case 1:
			edits = append(edits, funnel.Edit{
				Type:       funnel.EditTypeModify,
				ObjectType: "Customer",
				PrimaryKey: fmt.Sprintf("C-%03d", i),
				Properties: map[string]interface{}{
					"name": fmt.Sprintf("Cust %d updated", i),
				},
			})
		default:
			edits = append(edits, funnel.Edit{
				Type:       funnel.EditTypeDelete,
				ObjectType: "Customer",
				PrimaryKey: fmt.Sprintf("C-%03d", i),
			})
		}
	}

	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-100",
		OntologyAPIName: "northwind",
		UserID:          "tester",
		Timestamp:       ts,
		Edits:           edits,
	}); err != nil {
		t.Fatalf("materialize 100: %v", err)
	}

	files := listParquetFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file (single object type), got %d: %v", len(files), files)
	}
	rows := readParquet(t, files[0])
	if len(rows) != n {
		t.Fatalf("expected %d rows, got %d", n, len(rows))
	}

	// Verify the metadata columns and JSON content survive the round trip.
	deletedSeen := 0
	maxOffset := int64(0)
	minOffset := int64(1<<62 - 1)
	offsets := map[int64]struct{}{}
	for _, r := range rows {
		if r.ObjectType != "Customer" {
			t.Fatalf("unexpected object type %q", r.ObjectType)
		}
		if r.BatchID != "tx-100" {
			t.Fatalf("expected BatchID tx-100, got %q", r.BatchID)
		}
		if r.UserID != "tester" {
			t.Fatalf("expected UserID tester, got %q", r.UserID)
		}
		if _, dup := offsets[r.PatchOffset]; dup {
			t.Fatalf("duplicate __patch_offset %d", r.PatchOffset)
		}
		offsets[r.PatchOffset] = struct{}{}
		if r.PatchOffset > maxOffset {
			maxOffset = r.PatchOffset
		}
		if r.PatchOffset < minOffset {
			minOffset = r.PatchOffset
		}
		if r.TimestampMs != ts.UnixMilli() {
			t.Fatalf("timestamp mismatch: got %d want %d", r.TimestampMs, ts.UnixMilli())
		}
		switch r.EditType {
		case "DELETE":
			if !r.IsDeleted {
				t.Fatalf("DELETE row has __is_deleted=false (pk %s)", r.PrimaryKey)
			}
			deletedSeen++
		case "CREATE", "MODIFY":
			if r.IsDeleted {
				t.Fatalf("%s row has __is_deleted=true (pk %s)", r.EditType, r.PrimaryKey)
			}
			if r.PropertiesJSON == "" {
				t.Fatalf("%s row has empty properties JSON (pk %s)", r.EditType, r.PrimaryKey)
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal([]byte(r.PropertiesJSON), &decoded); err != nil {
				t.Fatalf("decode properties JSON: %v", err)
			}
			if _, ok := decoded["name"]; !ok {
				t.Fatalf("expected name in decoded properties, got %v", decoded)
			}
		default:
			t.Fatalf("unexpected edit type %q", r.EditType)
		}
	}
	if deletedSeen == 0 {
		t.Fatalf("expected at least one DELETE row, got none")
	}
	if maxOffset-minOffset+1 != int64(n) {
		t.Fatalf("expected %d distinct offsets in a contiguous range, got [%d, %d]",
			n, minOffset, maxOffset)
	}
}

func TestMaterializer_DeleteRowSetsIsDeletedTrueAndOmitsProperties(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)
	ts := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)

	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-del",
		OntologyAPIName: "northwind",
		Timestamp:       ts,
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeDelete, ObjectType: "Customer", PrimaryKey: "C-9"},
		},
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	files := listParquetFiles(t, dir)
	rows := readParquet(t, files[0])
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]
	if !got.IsDeleted {
		t.Fatalf("expected __is_deleted=true on DELETE row")
	}
	if got.EditType != "DELETE" {
		t.Fatalf("expected edit_type=DELETE, got %q", got.EditType)
	}
	if got.PropertiesJSON != "" {
		t.Fatalf("expected empty properties JSON on DELETE, got %q", got.PropertiesJSON)
	}
}

func TestMaterializer_SkipsLinkEdits(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)

	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-link-only",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC),
		Edits: []funnel.Edit{
			{
				Type:             funnel.EditTypeLinkCreate,
				ObjectType:       "Customer",
				PrimaryKey:       "C-1",
				LinkTypeRID:      "ri.oms.main.linktype.x",
				TargetPrimaryKey: "O-1",
			},
			{
				Type:             funnel.EditTypeLinkDelete,
				ObjectType:       "Customer",
				PrimaryKey:       "C-1",
				LinkTypeRID:      "ri.oms.main.linktype.x",
				TargetPrimaryKey: "O-2",
			},
		},
	}); err != nil {
		t.Fatalf("materialize link-only batch: %v", err)
	}
	if files := listParquetFiles(t, dir); len(files) != 0 {
		t.Fatalf("expected 0 files for link-only batch, got %v", files)
	}
}

func TestMaterializer_SequentialBatchesAdvanceOffsets(t *testing.T) {
	dir := t.TempDir()
	m := NewMaterializer(dir)

	for i := 0; i < 3; i++ {
		ts := time.Date(2026, 5, 4, 15, i, 0, 0, time.UTC)
		if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
			ID:              fmt.Sprintf("tx-seq-%d", i),
			OntologyAPIName: "northwind",
			Timestamp:       ts,
			Edits: []funnel.Edit{
				{
					Type:       funnel.EditTypeCreate,
					ObjectType: "Customer",
					PrimaryKey: fmt.Sprintf("C-seq-%d", i),
					Properties: map[string]interface{}{"i": float64(i)},
				},
			},
		}); err != nil {
			t.Fatalf("materialize seq %d: %v", i, err)
		}
	}

	files := listParquetFiles(t, dir)
	if len(files) != 3 {
		t.Fatalf("expected 3 files (one per batch), got %d: %v", len(files), files)
	}
	allOffsets := []int64{}
	for _, p := range files {
		rows := readParquet(t, p)
		for _, r := range rows {
			allOffsets = append(allOffsets, r.PatchOffset)
		}
	}
	sort.Slice(allOffsets, func(i, j int) bool { return allOffsets[i] < allOffsets[j] })
	for i := 1; i < len(allOffsets); i++ {
		if allOffsets[i] <= allOffsets[i-1] {
			t.Fatalf("expected strictly monotonic offsets, got %v", allOffsets)
		}
	}
}
