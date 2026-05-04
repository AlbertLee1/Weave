// Package xlsxstream provides a streaming Excel (xlsx) writer that
// transparently rolls over to a fresh sheet once a configurable row cap is
// hit (default 1,000,000 rows per sheet to match Excel's hard limit of
// 1,048,576 rows minus a comfortable safety margin).
//
// Each sheet is filled via excelize/v2's StreamWriter, which spills row
// chunks to disk after a 16 MiB in-memory threshold. Producing a
// half-gigabyte workbook costs only a few tens of MiB of resident heap.
package xlsxstream

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/xuri/excelize/v2"
)

// DefaultMaxRowsPerSheet is the per-sheet row ceiling used when Options
// leaves it zero. Set to 1,000,000 — comfortably below Excel's hard
// 1,048,576-row limit so consumers (Excel, LibreOffice) open the file
// without truncation.
const DefaultMaxRowsPerSheet = 1_000_000

// SheetNamePrefix is the canonical sheet name; subsequent sheets append a
// numeric suffix (Data, Data2, Data3, …).
const SheetNamePrefix = "Data"

// ErrClosed is returned when a Writer is used after WriteTo / Close.
var ErrClosed = errors.New("xlsxstream: writer is closed")

// Options tunes Writer construction. Zero values pick safe defaults.
type Options struct {
	// MaxRowsPerSheet is the per-sheet row cap (data rows + 1 header).
	// Zero or negative values fall back to DefaultMaxRowsPerSheet.
	MaxRowsPerSheet int
}

// Writer streams rows into a multi-sheet xlsx workbook.
//
// Lifecycle: New → optional SetHeaders → repeated WriteRow → WriteTo or
// Close. After WriteTo / Close the writer is sealed; further writes return
// ErrClosed.
type Writer struct {
	mu          sync.Mutex
	file        *excelize.File
	stream      *excelize.StreamWriter
	maxPerSheet int
	rowsInSheet int   // total rows on the active sheet (header + data) — drives cell coordinates
	dataInSheet int   // data rows on the active sheet (header excluded) — gates rollover
	totalSheets int   // sheets created so far (>=1 once a sheet exists)
	headers     []any // copy of headers; written as row 1 of every sheet
	hasRow      bool  // any data row written?
	closed      bool
}

// New constructs a Writer and primes the first sheet ("Data").
func New(opts Options) (*Writer, error) {
	max := opts.MaxRowsPerSheet
	if max <= 0 {
		max = DefaultMaxRowsPerSheet
	}
	if max < 1 {
		return nil, fmt.Errorf("xlsxstream: MaxRowsPerSheet must be >= 1, got %d", max)
	}
	f := excelize.NewFile()
	w := &Writer{file: f, maxPerSheet: max}
	if err := w.openFirstSheet(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

// openFirstSheet renames the default sheet and acquires its StreamWriter.
func (w *Writer) openFirstSheet() error {
	defaultSheet := w.file.GetSheetName(0)
	if defaultSheet != SheetNamePrefix {
		if err := w.file.SetSheetName(defaultSheet, SheetNamePrefix); err != nil {
			return fmt.Errorf("xlsxstream: rename default sheet: %w", err)
		}
	}
	sw, err := w.file.NewStreamWriter(SheetNamePrefix)
	if err != nil {
		return fmt.Errorf("xlsxstream: NewStreamWriter: %w", err)
	}
	w.stream = sw
	w.totalSheets = 1
	w.rowsInSheet = 0
	w.dataInSheet = 0
	return nil
}

// SetHeaders records column headers; written as row 1 of the active sheet
// and replayed verbatim on every rollover sheet. Must be called before the
// first WriteRow.
func (w *Writer) SetHeaders(cols []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	if w.hasRow {
		return errors.New("xlsxstream: SetHeaders must be called before WriteRow")
	}
	if len(cols) == 0 {
		w.headers = nil
		return nil
	}
	headers := make([]any, len(cols))
	for i, c := range cols {
		headers[i] = c
	}
	w.headers = headers
	return w.writeHeaderRowLocked()
}

// writeHeaderRowLocked stamps the cached headers as row 1 of the active
// sheet. Caller must hold w.mu.
func (w *Writer) writeHeaderRowLocked() error {
	if len(w.headers) == 0 {
		return nil
	}
	if err := w.stream.SetRow("A1", w.headers); err != nil {
		return fmt.Errorf("xlsxstream: write header: %w", err)
	}
	w.rowsInSheet = 1
	w.dataInSheet = 0
	return nil
}

// WriteRow appends a row to the active sheet, rolling over to a fresh
// sheet when the row cap is hit. row may contain any value excelize
// accepts (string, numeric, bool, time.Time, excelize.Cell, …).
func (w *Writer) WriteRow(row []any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}

	// Lazily emit headers on the first write if SetHeaders was called.
	if !w.hasRow && len(w.headers) > 0 && w.rowsInSheet == 0 {
		if err := w.writeHeaderRowLocked(); err != nil {
			return err
		}
	}

	if w.dataInSheet >= w.maxPerSheet {
		if err := w.rolloverLocked(); err != nil {
			return err
		}
	}

	cell, err := excelize.CoordinatesToCellName(1, w.rowsInSheet+1)
	if err != nil {
		return fmt.Errorf("xlsxstream: coords: %w", err)
	}
	if err := w.stream.SetRow(cell, row); err != nil {
		return fmt.Errorf("xlsxstream: SetRow: %w", err)
	}
	w.rowsInSheet++
	w.dataInSheet++
	w.hasRow = true
	return nil
}

// rolloverLocked flushes the active sheet, opens a new one, and replays
// the cached header row. Caller must hold w.mu.
func (w *Writer) rolloverLocked() error {
	if err := w.stream.Flush(); err != nil {
		return fmt.Errorf("xlsxstream: flush before rollover: %w", err)
	}
	w.totalSheets++
	name := sheetNameForIndex(w.totalSheets - 1)
	if _, err := w.file.NewSheet(name); err != nil {
		return fmt.Errorf("xlsxstream: NewSheet %s: %w", name, err)
	}
	sw, err := w.file.NewStreamWriter(name)
	if err != nil {
		return fmt.Errorf("xlsxstream: NewStreamWriter %s: %w", name, err)
	}
	w.stream = sw
	w.rowsInSheet = 0
	w.dataInSheet = 0
	return w.writeHeaderRowLocked()
}

// SheetCount returns the number of sheets produced so far (>=1).
func (w *Writer) SheetCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.totalSheets
}

// WriteTo flushes the active StreamWriter and emits the assembled xlsx
// bytes into out. Implies Close: subsequent writes return ErrClosed.
func (w *Writer) WriteTo(out io.Writer) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrClosed
	}
	if err := w.stream.Flush(); err != nil {
		return 0, fmt.Errorf("xlsxstream: final flush: %w", err)
	}
	w.closed = true
	defer func() {
		_ = w.file.Close()
	}()
	n, err := w.file.WriteTo(out)
	if err != nil {
		return n, fmt.Errorf("xlsxstream: write file: %w", err)
	}
	return n, nil
}

// Close flushes outstanding rows and releases the underlying file without
// emitting bytes. Useful for callers that abort an in-progress export.
// Safe to call after WriteTo (no-op).
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.stream != nil {
		_ = w.stream.Flush()
	}
	return w.file.Close()
}

// sheetNameForIndex maps 0 → "Data", 1 → "Data2", … so the first sheet
// keeps its bare canonical name and rollover sheets carry a 1-based suffix.
func sheetNameForIndex(idx int) string {
	if idx <= 0 {
		return SheetNamePrefix
	}
	return SheetNamePrefix + strconv.Itoa(idx+1)
}
