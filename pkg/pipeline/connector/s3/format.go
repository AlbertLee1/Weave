package s3

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"
)

// CSVOptions tunes the CSV decoder. The zero value is "RFC 4180 with
// comma delimiter and a header row", matching encoding/csv defaults.
type CSVOptions struct {
	// Comma is the field delimiter. Defaults to ',' when zero.
	Comma rune
	// Comment, if non-zero, marks lines that csv.Reader skips entirely.
	// Common choice: '#'.
	Comment rune
	// NoHeader skips header-row interpretation; rows are keyed by
	// "col_0", "col_1", … instead of declared header names. Useful for
	// header-less exports.
	NoHeader bool
	// LazyQuotes mirrors csv.Reader.LazyQuotes — accept malformed
	// quoting in field values rather than erroring. Off by default for
	// strict parsing.
	LazyQuotes bool
	// FieldsPerRecord controls the per-row field-count check. 0 (the
	// default) requires every row to match the first row's count.
	// Negative disables the check (any width). Positive enforces an
	// exact count.
	FieldsPerRecord int
}

// decodeObject dispatches to the format-specific decoder. Centralising
// the switch keeps the connector logic format-agnostic.
func decodeObject(format Format, data []byte, csvOpts CSVOptions) ([]map[string]any, error) {
	switch format {
	case FormatCSV:
		return decodeCSV(data, csvOpts)
	case FormatParquet:
		return decodeParquet(data)
	default:
		return nil, fmt.Errorf("s3: unsupported format %q", format)
	}
}

// decodeCSV parses bytes into row maps. With NoHeader=false (the
// default) the first row supplies column names; with NoHeader=true the
// rows are keyed by positional names "col_0", "col_1", ….
//
// Empty objects return ([]map[string]any{}, nil) — empty result, not
// an error. A header-only object (NoHeader=false, one row) likewise
// returns the empty slice.
func decodeCSV(data []byte, opts CSVOptions) ([]map[string]any, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	if opts.Comma != 0 {
		reader.Comma = opts.Comma
	}
	if opts.Comment != 0 {
		reader.Comment = opts.Comment
	}
	reader.LazyQuotes = opts.LazyQuotes
	reader.FieldsPerRecord = opts.FieldsPerRecord

	rows := []map[string]any{}
	var header []string
	first := true
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
		if first {
			first = false
			if !opts.NoHeader {
				header = make([]string, len(rec))
				copy(header, rec)
				continue
			}
			header = make([]string, len(rec))
			for i := range rec {
				header[i] = fmt.Sprintf("col_%d", i)
			}
		}
		row := make(map[string]any, len(header))
		for i, v := range rec {
			if i >= len(header) {
				// Extra fields without a header slot get a positional
				// name so they aren't silently dropped. Matches the
				// "tolerate ragged rows but flag them" pattern from
				// US-290 schema inference.
				row[fmt.Sprintf("col_%d", i)] = v
				continue
			}
			row[header[i]] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// parquetReadBatch caps the per-call ReadRows batch. Tuned to match
// the JDBC connector's DefaultPageSize default — large enough to
// amortise per-row overhead, small enough to bound transient memory
// during decode.
const parquetReadBatch = 1024

// decodeParquet reads bytes as a parquet file and returns one map per
// row, keyed by dotted column path. The whole object is read into
// memory because parquet requires random access — bounded by
// ObjectInfo.Size on the producer side.
//
// Decoded value types follow parquet-go's Value.Kind() mapping:
// boolean→bool, int32/int64→int32/int64, float→float32, double→
// float64, byte_array→string ([]byte for non-utf8 binary types), null
// →nil. Nested groups are flattened via dotted column paths
// ("address.city").
func decodeParquet(data []byte) ([]map[string]any, error) {
	if len(data) == 0 {
		return []map[string]any{}, nil
	}
	reader := parquet.NewReader(bytes.NewReader(data))
	defer reader.Close()

	schema := reader.Schema()
	if schema == nil {
		return nil, errors.New("parquet: file has no schema")
	}
	colPaths := schema.Columns()
	colNames := make([]string, len(colPaths))
	for i, p := range colPaths {
		colNames[i] = joinPath(p)
	}

	out := []map[string]any{}
	buf := make([]parquet.Row, parquetReadBatch)
	for {
		n, err := reader.ReadRows(buf)
		for i := 0; i < n; i++ {
			row := buf[i]
			m := make(map[string]any, len(row))
			for _, val := range row {
				idx := val.Column()
				if idx < 0 || idx >= len(colNames) {
					continue
				}
				m[colNames[idx]] = parquetValueToGo(val)
			}
			out = append(out, m)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parquet: read rows: %w", err)
		}
	}
	return out, nil
}

// joinPath flattens a column path ([]string) into a dotted column
// name. Top-level columns produce single-segment names; nested groups
// produce "group.field". Empty path is impossible per parquet-go but
// guarded anyway.
func joinPath(p []string) string {
	switch len(p) {
	case 0:
		return ""
	case 1:
		return p[0]
	default:
		// strings.Join via byte buffer to avoid the import dance.
		var b bytes.Buffer
		for i, seg := range p {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(seg)
		}
		return b.String()
	}
}

// parquetValueToGo coerces a parquet.Value to a Go-native scalar via
// the value's Kind(). Null values surface as nil; integer / float /
// bool / string follow the obvious mapping; byte_array values are
// returned as string (parquet's UTF-8 logical type covers most cases)
// — callers that need raw []byte for non-UTF-8 binary should
// post-process via Value.ByteArray().
func parquetValueToGo(v parquet.Value) any {
	if v.IsNull() {
		return nil
	}
	switch v.Kind() {
	case parquet.Boolean:
		return v.Boolean()
	case parquet.Int32:
		return v.Int32()
	case parquet.Int64:
		return v.Int64()
	case parquet.Int96:
		// Int96 is the legacy timestamp encoding; surface raw bytes
		// since the canonical Go representation is driver-specific.
		b := v.ByteArray()
		out := make([]byte, len(b))
		copy(out, b)
		return out
	case parquet.Float:
		return v.Float()
	case parquet.Double:
		return v.Double()
	case parquet.ByteArray, parquet.FixedLenByteArray:
		// Most string / binary parquet columns use ByteArray with
		// UTF-8 logical type. Returning string keeps map[string]any
		// usable from JSON-marshalling pipelines without a second
		// coercion pass.
		return string(v.ByteArray())
	default:
		return v.String()
	}
}
