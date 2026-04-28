// Package schema implements sample-driven type inference for CSV / JSON
// pipeline inputs (US-290). The inference is deliberately conservative:
// every column starts at the most permissive candidate (string) and is
// narrowed only when EVERY non-null sample value matches a stricter
// type. The result is a wire-friendly descriptor a UI can render for
// user confirmation/adjustment before pinning the pipeline schema.
//
// Per-column type cascade (loosest → tightest):
//
//	string → boolean | date | timestamp | integer → long → double
//
// Integers always promote to double once a fractional sample appears,
// and integers that exceed 32-bit range promote to long. A column with
// at least one value that fails ALL tighter parsers stays at string.
//
// The package is connector-agnostic: callers feed it any io.Reader of
// CSV or JSON content. Network/HTTP fetching is out of scope — the
// connector layer (US-292+ JDBC/S3/REST) hands a Reader to InferCSV /
// InferJSON for sampling.
package schema

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	weavetypes "github.com/liyang/weave/pkg/types"
)

// DefaultSampleRows is the default scan budget. Matches the PRD's
// "扫描前 1000 行样本推断类型" acceptance criterion.
const DefaultSampleRows = 1000

// MaxSampleRows caps the scan to keep memory bounded and prevent
// pathological "just feed us your 10M-row CSV" requests at the HTTP
// layer. Callers that genuinely need more should split into batches
// and reduce locally.
const MaxSampleRows = 100000

// SampleValueLimit is the per-field cap on retained sample values. Keeps
// the wire response bounded regardless of SampleRows; the UI shows the
// first few sampled values as a sanity check.
const SampleValueLimit = 5

// Format identifies the input encoding.
type Format string

const (
	FormatCSV    Format = "csv"
	FormatJSON   Format = "json"
	FormatNDJSON Format = "ndjson"
)

// Options tunes the inference run. Zero-value Options is valid and
// applies the package defaults.
type Options struct {
	// SampleRows caps the number of rows scanned. <=0 means
	// DefaultSampleRows; values above MaxSampleRows are clamped to
	// MaxSampleRows. The cap is rows (not bytes) — JSON-array inputs
	// stop iterating after SampleRows decoded objects.
	SampleRows int

	// HasHeader applies to FormatCSV only. When true the first row is
	// treated as the header (column names). When false synthesised
	// names ("col1", "col2", …) are emitted. Default true.
	HasHeader bool

	// Delimiter applies to FormatCSV only. Zero value defaults to ','.
	// Tab-separated callers should pass '\t'.
	Delimiter rune
}

// Field describes one inferred column. The wire shape is intentionally
// flat so the UI can render a per-row checkbox/dropdown without first
// reshaping the response.
type Field struct {
	// Name is the column / object-key name as it appeared in the
	// source. CSV header rows are honoured when HasHeader is true;
	// JSON object keys are taken verbatim.
	Name string `json:"name"`

	// BaseType is the inferred Weave base type (one of pkg/types.BaseType).
	// The PRD guarantees at minimum: string, integer, long, double,
	// boolean, date, timestamp.
	BaseType weavetypes.BaseType `json:"baseType"`

	// Nullable reports whether the column had at least one observed
	// null/empty value across the sample window. Helps the UI default
	// "required" toggles on the resulting ObjectType.
	Nullable bool `json:"nullable"`

	// Samples carries up to SampleValueLimit raw sample strings (CSV)
	// or JSON-encoded values (JSON) so the UI can show the user what
	// the inference is generalising from. Cap'd to keep responses
	// small even when SampleRows is large.
	Samples []string `json:"samples,omitempty"`

	// NonNullCount is the count of non-null observations in the sample.
	NonNullCount int `json:"nonNullCount"`

	// NullCount is the count of null/empty observations in the sample.
	NullCount int `json:"nullCount"`
}

// Result is one inference response.
type Result struct {
	Format       Format  `json:"format"`
	RowsScanned  int     `json:"rowsScanned"`
	Fields       []Field `json:"fields"`
	SampleRows   int     `json:"sampleRows"`
	HasHeader    bool    `json:"hasHeader,omitempty"`
	Truncated    bool    `json:"truncated,omitempty"`
	WarningCount int     `json:"warningCount,omitempty"`
}

// candidateType represents the running inference state for one column.
// It tracks the LOOSEST type still consistent with every observation.
// Once it widens to widenString, no further observation can narrow it.
type candidateType int

const (
	candidateUnknown candidateType = iota
	candidateInteger
	candidateLong
	candidateDouble
	candidateBoolean
	candidateDate
	candidateTimestamp
	candidateString
)

func (c candidateType) baseType() weavetypes.BaseType {
	switch c {
	case candidateInteger:
		return weavetypes.Integer
	case candidateLong:
		return weavetypes.Long
	case candidateDouble:
		return weavetypes.Double
	case candidateBoolean:
		return weavetypes.Boolean
	case candidateDate:
		return weavetypes.Date
	case candidateTimestamp:
		return weavetypes.Timestamp
	default:
		return weavetypes.String
	}
}

// fieldAccumulator gathers per-column statistics across the sample.
type fieldAccumulator struct {
	Name         string
	Cand         candidateType
	NonNullCount int
	NullCount    int
	Samples      []string
}

func newAccumulator(name string) *fieldAccumulator {
	return &fieldAccumulator{Name: name, Cand: candidateUnknown}
}

func (a *fieldAccumulator) observe(raw string, isNull bool) {
	if isNull {
		a.NullCount++
		return
	}
	a.NonNullCount++
	if len(a.Samples) < SampleValueLimit {
		a.Samples = append(a.Samples, raw)
	}
	a.Cand = narrow(a.Cand, raw)
}

// narrow returns the widest candidate consistent with the previous
// state and the new observation. Once a column has reached String it
// stays there; otherwise we ratchet to the loosest accepting type.
func narrow(prev candidateType, raw string) candidateType {
	if prev == candidateString {
		return candidateString
	}
	obs := classify(raw)
	if prev == candidateUnknown {
		return obs
	}
	return mergeCandidates(prev, obs)
}

// classify returns the tightest candidate type that accepts raw on its
// own. Used to seed an empty column AND to feed mergeCandidates for
// the running narrow.
func classify(raw string) candidateType {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return candidateString
	}
	if isInteger(trimmed) {
		if fitsInt32(trimmed) {
			return candidateInteger
		}
		return candidateLong
	}
	if isFloat(trimmed) {
		return candidateDouble
	}
	if isBooleanLiteral(trimmed) {
		return candidateBoolean
	}
	if isDate(trimmed) {
		return candidateDate
	}
	if isTimestamp(trimmed) {
		return candidateTimestamp
	}
	return candidateString
}

// mergeCandidates picks the loosest type that accepts both inputs.
// The lattice is a narrow tree:
//
//	integer ⊆ long ⊆ double
//	boolean, date, timestamp are siblings (no widen path between them
//	without going through string)
//	any conflict → string.
func mergeCandidates(a, b candidateType) candidateType {
	if a == b {
		return a
	}
	// Numeric promotion lattice.
	if isNumeric(a) && isNumeric(b) {
		return widerNumeric(a, b)
	}
	return candidateString
}

func isNumeric(c candidateType) bool {
	return c == candidateInteger || c == candidateLong || c == candidateDouble
}

func widerNumeric(a, b candidateType) candidateType {
	rank := func(c candidateType) int {
		switch c {
		case candidateInteger:
			return 1
		case candidateLong:
			return 2
		case candidateDouble:
			return 3
		default:
			return 0
		}
	}
	if rank(a) > rank(b) {
		return a
	}
	return b
}

func isInteger(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' || s[0] == '+' {
		if len(s) == 1 {
			return false
		}
		i = 1
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func fitsInt32(s string) bool {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return false
	}
	return v >= -2147483648 && v <= 2147483647
}

func isFloat(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return false
	}
	// strconv.ParseFloat accepts pure integers — we already exclude
	// those at the call site, but for paranoia exclude the "no dot
	// and no e" case here too.
	if !strings.ContainsAny(s, ".eE") {
		return false
	}
	return true
}

func isBooleanLiteral(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false":
		return true
	}
	return false
}

// isDate accepts ISO-8601 calendar dates (YYYY-MM-DD). We deliberately
// do NOT accept slash- or dot-separated forms — the locale ambiguity
// (DD/MM vs MM/DD) makes silent inference dangerous. Users who want
// those can override the inferred type to date in the UI.
func isDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return false
	}
	return true
}

// isTimestamp accepts RFC3339 / ISO-8601 datetime forms with a 'T' or
// space separator.
func isTimestamp(s string) bool {
	for _, layout := range timestampLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05.000000",
	"2006-01-02 15:04:05Z07:00",
}

func (a *fieldAccumulator) toField() Field {
	cand := a.Cand
	if cand == candidateUnknown {
		// Column was either entirely absent or entirely null —
		// default to nullable string so downstream tooling has a
		// concrete type to work with.
		cand = candidateString
	}
	return Field{
		Name:         a.Name,
		BaseType:     cand.baseType(),
		Nullable:     a.NullCount > 0 || a.NonNullCount == 0,
		Samples:      a.Samples,
		NonNullCount: a.NonNullCount,
		NullCount:    a.NullCount,
	}
}

// effectiveSampleRows clamps n into [1, MaxSampleRows]. n<=0 returns
// DefaultSampleRows so the empty Options literal is valid.
func effectiveSampleRows(n int) int {
	switch {
	case n <= 0:
		return DefaultSampleRows
	case n > MaxSampleRows:
		return MaxSampleRows
	default:
		return n
	}
}

// validateFormat is a tiny sanity-check helper used by the HTTP layer.
func validateFormat(f Format) error {
	switch f {
	case FormatCSV, FormatJSON, FormatNDJSON:
		return nil
	default:
		return fmt.Errorf("unsupported format %q", f)
	}
}
