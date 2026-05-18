package xlsxstream

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/liyang/weave/internal/testprofile"
)

// TestWriter_LargeFileMemoryBudget targets the US-431 envelope: stream a
// large multi-sheet workbook and confirm the steady-state heap allocation
// stays well below the 100 MiB budget. excelize's StreamWriter spills row
// chunks to a temp dir once they exceed 16 MiB, so the in-process heap
// should remain modest even at multi-million-row scale.
//
// Skipped under -short so the default unit suite stays fast.
func TestWriter_LargeFileMemoryBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-file budget test under -short")
	}
	const (
		colsPerRow   = 25
		maxHeapBytes = 100 << 20 // 100 MiB peak heap delta from baseline
	)
	rows := 120_000
	minFileBytes := int64(6 << 20)
	maxRowsPer := 75_000
	if testprofile.Instrumented(testing.CoverMode()) {
		rows = 50_000
		minFileBytes = 2 << 20
		maxRowsPer = 30_000
	}

	headers := make([]string, colsPerRow)
	for i := 0; i < colsPerRow; i++ {
		headers[i] = fmt.Sprintf("col%d", i)
	}

	tmp, err := os.CreateTemp("", "xlsxstream-bench-*.xlsx")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmp.Name())

	w, err := New(Options{MaxRowsPerSheet: maxRowsPer})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.SetHeaders(headers); err != nil {
		t.Fatalf("SetHeaders: %v", err)
	}

	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)

	row := make([]any, colsPerRow)
	for i := 0; i < rows; i++ {
		// Per-cell unique content defeats sharedStrings dedup so the
		// on-disk size scales with row count.
		for c := 0; c < colsPerRow; c++ {
			row[c] = fmt.Sprintf("r%d-c%d-%s", i, c, lipsum)
		}
		if err := w.WriteRow(row); err != nil {
			t.Fatalf("WriteRow %d: %v", i, err)
		}
	}

	var peak runtime.MemStats
	runtime.ReadMemStats(&peak)

	if _, err := w.WriteTo(tmp); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("Close tmp: %v", err)
	}

	st, err := os.Stat(tmp.Name())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if got := w.SheetCount(); got < 2 {
		t.Errorf("expected rollover (>=2 sheets) at %d rows / %d cap; got %d sheets",
			rows, maxRowsPer, got)
	}

	delta := int64(peak.HeapAlloc) - int64(base.HeapAlloc)
	if delta < 0 {
		delta = 0
	}
	t.Logf("xlsx size=%d bytes (%.1f MiB), sheets=%d, peak heap delta=%d bytes (%.1f MiB)",
		st.Size(), float64(st.Size())/(1<<20), w.SheetCount(),
		delta, float64(delta)/(1<<20))

	if st.Size() < minFileBytes {
		t.Errorf("file size %d bytes below %d-byte exercise floor", st.Size(), minFileBytes)
	}
	if delta > maxHeapBytes {
		t.Fatalf("heap delta %d bytes exceeds %d-byte budget", delta, maxHeapBytes)
	}
}

// lipsum is a 40-byte fragment used as a per-cell padding payload in the
// large-file budget test; the row/col index prefix in front of it keeps
// every cell unique so sharedStrings dedup can't collapse them.
const lipsum = "lorem ipsum dolor sit amet consectetur"

func BenchmarkWriter_RowsPerSecond(b *testing.B) {
	const colsPerRow = 20
	row := make([]any, colsPerRow)
	for i := 0; i < colsPerRow; i++ {
		row[i] = "lorem ipsum dolor sit amet"
	}
	headers := make([]string, colsPerRow)
	for i := 0; i < colsPerRow; i++ {
		headers[i] = fmt.Sprintf("col%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w, err := New(Options{})
		if err != nil {
			b.Fatalf("New: %v", err)
		}
		if err := w.SetHeaders(headers); err != nil {
			b.Fatalf("SetHeaders: %v", err)
		}
		for j := 0; j < 10_000; j++ {
			if err := w.WriteRow(row); err != nil {
				b.Fatalf("WriteRow: %v", err)
			}
		}
		if _, err := w.WriteTo(io.Discard); err != nil {
			b.Fatalf("WriteTo: %v", err)
		}
	}
}
