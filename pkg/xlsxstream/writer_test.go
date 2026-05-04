package xlsxstream

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriter_SingleSheet_HeadersAndRows(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.SetHeaders([]string{"id", "name", "score"}); err != nil {
		t.Fatalf("SetHeaders: %v", err)
	}
	for i := 1; i <= 3; i++ {
		if err := w.WriteRow([]any{i, fmt.Sprintf("name-%d", i), float64(i) * 1.5}); err != nil {
			t.Fatalf("WriteRow %d: %v", i, err)
		}
	}

	var buf bytes.Buffer
	n, err := w.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected positive byte count, got %d", n)
	}

	rows := readSheetRows(t, buf.Bytes(), "Data")
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (1 header + 3 data), got %d: %v", len(rows), rows)
	}
	if !equalStringRow(rows[0], []string{"id", "name", "score"}) {
		t.Errorf("header mismatch: %v", rows[0])
	}
	if rows[2][1] != "name-2" {
		t.Errorf("expected name-2 at row 3 col B, got %q", rows[2][1])
	}
}

func TestWriter_MultiSheet_RolloverPreservesHeader(t *testing.T) {
	w, err := New(Options{MaxRowsPerSheet: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.SetHeaders([]string{"k", "v"}); err != nil {
		t.Fatalf("SetHeaders: %v", err)
	}
	for i := 1; i <= 7; i++ {
		if err := w.WriteRow([]any{i, fmt.Sprintf("v%d", i)}); err != nil {
			t.Fatalf("WriteRow %d: %v", i, err)
		}
	}
	if got := w.SheetCount(); got != 3 {
		t.Fatalf("SheetCount = %d, want 3 (3+3+1)", got)
	}

	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	for sheetIdx, want := range []int{4, 4, 2} {
		name := sheetNameForIndex(sheetIdx)
		rows := readSheetRows(t, buf.Bytes(), name)
		if len(rows) != want {
			t.Errorf("sheet %s: got %d rows, want %d", name, len(rows), want)
		}
		if !equalStringRow(rows[0], []string{"k", "v"}) {
			t.Errorf("sheet %s: header mismatch %v", name, rows[0])
		}
	}
}

func TestWriter_NoHeaders_AllowsRowsDirectly(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.WriteRow([]any{"a", 1}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}
	if err := w.WriteRow([]any{"b", 2}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}

	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	rows := readSheetRows(t, buf.Bytes(), "Data")
	if len(rows) != 2 {
		t.Fatalf("expected 2 data rows, got %d: %v", len(rows), rows)
	}
}

func TestWriter_AfterWriteTo_RejectsFurtherWrites(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.WriteRow([]any{1}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if err := w.WriteRow([]any{2}); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
	if err := w.SetHeaders([]string{"a"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed on SetHeaders, got %v", err)
	}
}

func TestWriter_SetHeaders_AfterFirstRow_Errors(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.WriteRow([]any{"x"}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}
	if err := w.SetHeaders([]string{"col"}); err == nil {
		t.Fatalf("expected SetHeaders to error after WriteRow, got nil")
	}
}

func TestWriter_SheetNaming(t *testing.T) {
	cases := []struct {
		idx  int
		name string
	}{
		{0, "Data"},
		{1, "Data2"},
		{2, "Data3"},
	}
	for _, c := range cases {
		if got := sheetNameForIndex(c.idx); got != c.name {
			t.Errorf("idx %d: got %q want %q", c.idx, got, c.name)
		}
	}
}

func TestWriter_RolloverBoundaryExact(t *testing.T) {
	// MaxRowsPerSheet = 2, write exactly 2 rows -> single sheet, no spillover.
	w, err := New(Options{MaxRowsPerSheet: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.SetHeaders([]string{"k"}); err != nil {
		t.Fatalf("SetHeaders: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if err := w.WriteRow([]any{i}); err != nil {
			t.Fatalf("WriteRow %d: %v", i, err)
		}
	}
	if got := w.SheetCount(); got != 1 {
		t.Fatalf("SheetCount = %d, want 1", got)
	}
	if err := w.WriteRow([]any{3}); err != nil {
		t.Fatalf("WriteRow 3: %v", err)
	}
	if got := w.SheetCount(); got != 2 {
		t.Fatalf("after 3rd row SheetCount = %d, want 2", got)
	}
}

// helpers

func readSheetRows(t *testing.T, data []byte, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("GetRows(%s): %v", sheet, err)
	}
	return rows
}

func equalStringRow(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if strings.TrimSpace(got[i]) != want[i] {
			return false
		}
	}
	return true
}
