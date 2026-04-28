package schema

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
)

// InferCSV reads up to opts.SampleRows rows from r and returns the
// inferred schema. The first call to csvReader.Read can fail with
// io.EOF if r is empty; we surface that as an empty-but-valid Result
// rather than an error so HTTP callers don't see a 500 for "user
// pasted an empty file".
func InferCSV(r io.Reader, opts Options) (*Result, error) {
	if r == nil {
		return nil, errors.New("schema: nil reader")
	}
	limit := effectiveSampleRows(opts.SampleRows)
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // tolerate ragged rows
	reader.TrimLeadingSpace = true
	if opts.Delimiter != 0 {
		reader.Comma = opts.Delimiter
	}
	hasHeader := opts.HasHeader

	var (
		header  []string
		accs    []*fieldAccumulator
		rowsLen int
		warns   int
	)
	rowsScanned := 0
	for rowsScanned < limit {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv read: %w", err)
		}
		if header == nil {
			if hasHeader {
				header = append(header, record...)
				accs = make([]*fieldAccumulator, len(header))
				for i, name := range header {
					if name == "" {
						name = fmt.Sprintf("col%d", i+1)
					}
					accs[i] = newAccumulator(name)
				}
				rowsLen = len(header)
				continue
			}
			rowsLen = len(record)
			accs = make([]*fieldAccumulator, rowsLen)
			for i := range accs {
				accs[i] = newAccumulator(fmt.Sprintf("col%d", i+1))
			}
		}
		// Tolerate variable-width rows by padding short rows with
		// nulls and trimming over-long rows. Mismatches bump the
		// warning counter so the UI can flag "this CSV looks ragged".
		if len(record) < rowsLen {
			warns++
			padded := make([]string, rowsLen)
			copy(padded, record)
			record = padded
		} else if len(record) > rowsLen {
			warns++
			record = record[:rowsLen]
		}
		for i := 0; i < rowsLen; i++ {
			val := record[i]
			isNull := val == "" || isNullSentinel(val)
			accs[i].observe(val, isNull)
		}
		rowsScanned++
	}
	// Detect truncation by attempting one more read — if it succeeds
	// the input had at least one more row than our budget allowed.
	truncated := false
	if rowsScanned == limit {
		if _, err := reader.Read(); err == nil {
			truncated = true
		}
	}

	fields := make([]Field, 0, len(accs))
	for _, a := range accs {
		fields = append(fields, a.toField())
	}

	return &Result{
		Format:       FormatCSV,
		RowsScanned:  rowsScanned,
		Fields:       fields,
		SampleRows:   limit,
		HasHeader:    hasHeader,
		Truncated:    truncated,
		WarningCount: warns,
	}, nil
}

// isNullSentinel reports whether the raw value is one of the well-known
// null encodings users put in CSV exports. Case-insensitive. Note we
// purposefully accept ONLY the canonical forms — any other token is
// observed as a string value.
func isNullSentinel(raw string) bool {
	switch raw {
	case "NULL", "null", "Null":
		return true
	case "\\N":
		return true
	}
	return false
}
