package schema

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// InferJSON reads up to opts.SampleRows objects from a JSON-array
// payload and returns the inferred schema. The accepted top-level
// shapes are:
//
//	[ {"a": 1, "b": "x"}, {"a": 2, "b": "y"}, ... ]   (array of objects)
//	{"a": 1, "b": "x"}                                 (single object)
//
// For NDJSON (one JSON value per line) callers should use InferNDJSON.
func InferJSON(r io.Reader, opts Options) (*Result, error) {
	if r == nil {
		return nil, errors.New("schema: nil reader")
	}
	limit := effectiveSampleRows(opts.SampleRows)
	dec := json.NewDecoder(r)
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return emptyJSONResult(FormatJSON, limit), nil
		}
		return nil, fmt.Errorf("json: %w", err)
	}

	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '[':
			return inferJSONArray(dec, limit, FormatJSON)
		case '{':
			obj, err := decodeObjectAfterOpenBrace(dec)
			if err != nil {
				return nil, err
			}
			return inferJSONObjects([]map[string]any{obj}, limit, FormatJSON, false), nil
		default:
			return nil, fmt.Errorf("json: unsupported top-level delimiter %v", v)
		}
	default:
		return nil, errors.New("json: top-level value must be an array or object")
	}
}

// InferNDJSON reads newline-delimited JSON values, one per line, and
// returns the inferred schema. Empty lines are skipped; non-object
// lines (scalars, arrays) bump the WarningCount and are skipped.
func InferNDJSON(r io.Reader, opts Options) (*Result, error) {
	if r == nil {
		return nil, errors.New("schema: nil reader")
	}
	limit := effectiveSampleRows(opts.SampleRows)
	scanner := bufio.NewScanner(r)
	// Lift the line-length cap from the bufio default 64KiB to 1MiB
	// so realistic JSON rows survive (logs / nested objects).
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	accs := newOrderedAccumulators()
	rowsScanned := 0
	warns := 0
	for rowsScanned < limit && scanner.Scan() {
		line := scanner.Bytes()
		// Skip blank lines silently — they're a common artefact of
		// tools that append a trailing newline.
		if len(trimSpaceBytes(line)) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			warns++
			continue
		}
		accs.observeRow(obj)
		rowsScanned++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ndjson: %w", err)
	}
	truncated := false
	if rowsScanned == limit && scanner.Scan() {
		truncated = true
	}

	return &Result{
		Format:       FormatNDJSON,
		RowsScanned:  rowsScanned,
		Fields:       accs.fields(),
		SampleRows:   limit,
		Truncated:    truncated,
		WarningCount: warns,
	}, nil
}

func inferJSONArray(dec *json.Decoder, limit int, format Format) (*Result, error) {
	accs := newOrderedAccumulators()
	rowsScanned := 0
	warns := 0
	for dec.More() && rowsScanned < limit {
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
		obj, ok := v.(map[string]any)
		if !ok {
			warns++
			continue
		}
		accs.observeRow(obj)
		rowsScanned++
	}
	truncated := dec.More()
	if !truncated {
		// drain the closing bracket so further reads see EOF cleanly.
		_, _ = dec.Token()
	}
	return &Result{
		Format:       format,
		RowsScanned:  rowsScanned,
		Fields:       accs.fields(),
		SampleRows:   limit,
		Truncated:    truncated,
		WarningCount: warns,
	}, nil
}

func emptyJSONResult(f Format, limit int) *Result {
	return &Result{Format: f, SampleRows: limit, Fields: []Field{}}
}

// inferJSONObjects runs the inference over an in-memory slice. Used
// for the single-object top-level case.
func inferJSONObjects(rows []map[string]any, limit int, format Format, truncated bool) *Result {
	accs := newOrderedAccumulators()
	for i, obj := range rows {
		if i >= limit {
			truncated = true
			break
		}
		accs.observeRow(obj)
	}
	scanned := len(rows)
	if scanned > limit {
		scanned = limit
	}
	return &Result{
		Format:      format,
		RowsScanned: scanned,
		Fields:      accs.fields(),
		SampleRows:  limit,
		Truncated:   truncated,
	}
}

// decodeObjectAfterOpenBrace consumes the rest of an object whose
// opening `{` was already read off the decoder. Faster than rebuilding
// a streaming object decoder; we already have json.Decoder state.
func decodeObjectAfterOpenBrace(dec *json.Decoder) (map[string]any, error) {
	out := make(map[string]any)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("json: expected string key, got %T", keyTok)
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
		out[key] = v
	}
	// drain closing brace
	_, _ = dec.Token()
	return out, nil
}

// orderedAccumulators preserves field declaration order: the FIRST row
// that mentions a given key fixes that key's position in the output.
// Subsequent rows that introduce new keys append at the tail.
type orderedAccumulators struct {
	order []string
	byKey map[string]*fieldAccumulator
	rows  int
}

func newOrderedAccumulators() *orderedAccumulators {
	return &orderedAccumulators{byKey: make(map[string]*fieldAccumulator)}
}

func (o *orderedAccumulators) observeRow(row map[string]any) {
	o.rows++
	seen := make(map[string]bool, len(row))
	// Keys present in this row are observed; missing keys (relative
	// to the running set) are observed as null so cross-row
	// nullability inference is honest.
	// We need a deterministic per-row key order so per-row sample
	// retention is stable; iterate by the running order, then any
	// new keys (sorted by appearance below).
	for _, k := range o.order {
		if v, ok := row[k]; ok {
			o.observeValue(k, v)
			seen[k] = true
		} else {
			o.byKey[k].observe("", true)
			seen[k] = true
		}
	}
	for k := range row {
		if seen[k] {
			continue
		}
		// First time we see this key. Backfill prior rows' missing
		// observations as null so nullability stays honest.
		acc := newAccumulator(k)
		for i := 0; i < o.rows-1; i++ {
			acc.observe("", true)
		}
		o.byKey[k] = acc
		o.order = append(o.order, k)
		o.observeValue(k, row[k])
	}
}

func (o *orderedAccumulators) observeValue(k string, v any) {
	acc := o.byKey[k]
	raw, isNull := jsonValueToSample(v)
	acc.observe(raw, isNull)
}

func (o *orderedAccumulators) fields() []Field {
	out := make([]Field, 0, len(o.order))
	for _, k := range o.order {
		out = append(out, o.byKey[k].toField())
	}
	if out == nil {
		return []Field{}
	}
	return out
}

// jsonValueToSample turns a decoded JSON value into the (sampleString,
// isNull) shape the accumulator expects. JSON gives us native types;
// we re-render numbers / strings / bools as their canonical string
// form so the candidate-narrow lattice can run uniformly with CSV.
//
// Composite values (objects, arrays) collapse to their JSON
// stringification and are observed as "string" — pipeline schema
// inference v1 doesn't introspect nested structures (Struct / Array
// candidate types), keeping the wire shape simple. A future story
// can add struct-shape inference on top.
func jsonValueToSample(v any) (string, bool) {
	if v == nil {
		return "", true
	}
	switch x := v.(type) {
	case json.Number:
		return x.String(), false
	case string:
		return x, false
	case bool:
		if x {
			return "true", false
		}
		return "false", false
	case float64:
		// json.Decoder without UseNumber() lands floats here.
		return strconv.FormatFloat(x, 'f', -1, 64), false
	case int64:
		return strconv.FormatInt(x, 10), false
	case map[string]any, []any:
		// Composite: encode to canonical JSON for the sample, but
		// observe as a string-only value so the column infers to
		// string. The UI can let the operator override to
		// struct/array if they choose.
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		// Spike the candidate to string immediately by handing back a
		// token guaranteed to fail every tighter parser.
		return string(b), false
	default:
		return fmt.Sprintf("%v", v), false
	}
}

func trimSpaceBytes(b []byte) []byte {
	start := 0
	end := len(b)
	for start < end && isSpaceByte(b[start]) {
		start++
	}
	for end > start && isSpaceByte(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
