package timeseries

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// VMStore is a VictoriaMetrics-backed Store. Points are written via
// /api/v1/import (newline-delimited JSON) and read via /api/v1/export.
//
// VictoriaMetrics is single-value-per-(series,timestamp) numeric only —
// AppendPoint coerces incoming values to float64 and returns
// ErrNonNumericValue for unsupported payloads. Non-numeric series should
// continue to use the PG/memory backends.
//
// Series identity is encoded as a Prometheus-style metric:
//
//	weave_timeseries{ontology="...",object_type="...",primary_key="...",property="..."}
type VMStore struct {
	baseURL string
	client  *http.Client
}

// VMMetricName is the fixed Prometheus metric name VMStore writes/reads.
// Externalised so tests and ops can match it in /api/v1/series queries.
const VMMetricName = "weave_timeseries"

// ErrNonNumericValue is returned by VMStore.AppendPoint when the value
// payload cannot be coerced to a float64.
var ErrNonNumericValue = errors.New("timeseries: VictoriaMetrics backend supports numeric values only")

// NewVMStore wraps a VictoriaMetrics base URL (e.g. http://victoriametrics:8428)
// as a Store. The baseURL must NOT include a trailing slash; callers should
// pass the bare host:port form.
func NewVMStore(baseURL string) *VMStore {
	return &VMStore{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// AppendPoint writes one (timestamp, value) datapoint via /api/v1/import.
// VictoriaMetrics expects timestamps in milliseconds since the UNIX epoch.
func (s *VMStore) AppendPoint(ctx context.Context, key SeriesKey, p Point) error {
	v, ok := coerceFloat(p.Value)
	if !ok {
		return ErrNonNumericValue
	}
	body, err := encodeImportLine(key, p.Time, v)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v1/import", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/stream+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("vm import: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("vm import: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// FirstPoint returns the earliest point for the series, or ErrNoPoints.
func (s *VMStore) FirstPoint(ctx context.Context, key SeriesKey) (*Point, error) {
	points, err := s.StreamPoints(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, ErrNoPoints
	}
	p := points[0]
	return &p, nil
}

// LastPoint returns the most recent point for the series, or ErrNoPoints.
func (s *VMStore) LastPoint(ctx context.Context, key SeriesKey) (*Point, error) {
	points, err := s.StreamPoints(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(points) == 0 {
		return nil, ErrNoPoints
	}
	p := points[len(points)-1]
	return &p, nil
}

// StreamPoints returns every point for the series in ascending order.
//
// VictoriaMetrics /api/v1/export emits one JSON line per series with
// parallel timestamps[] and values[] arrays — the line carries every
// datapoint for the matching series in the requested time range.
func (s *VMStore) StreamPoints(ctx context.Context, key SeriesKey) ([]Point, error) {
	q := url.Values{}
	q.Set("match[]", buildMatcher(key))
	// VictoriaMetrics requires bounded windows for /api/v1/export. Use a
	// 50-year span to capture every point — this matches the historical
	// "all points" semantics of the in-memory store.
	q.Set("start", "0")
	q.Set("end", fmt.Sprintf("%d", time.Now().Add(50*365*24*time.Hour).UnixMilli()))
	exportURL := s.baseURL + "/api/v1/export?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, exportURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vm export: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("vm export: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var out []Point
	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var line exportLine
		if err := dec.Decode(&line); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("vm export decode: %w", err)
		}
		if len(line.Timestamps) != len(line.Values) {
			return nil, fmt.Errorf("vm export: timestamps/values length mismatch (%d vs %d)", len(line.Timestamps), len(line.Values))
		}
		for i, ts := range line.Timestamps {
			out = append(out, Point{
				Time:  time.UnixMilli(ts).UTC(),
				Value: line.Values[i],
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	if out == nil {
		out = []Point{}
	}
	return out, nil
}

type exportLine struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

type importLine struct {
	Metric     map[string]string `json:"metric"`
	Values     []float64         `json:"values"`
	Timestamps []int64           `json:"timestamps"`
}

func encodeImportLine(key SeriesKey, ts time.Time, value float64) ([]byte, error) {
	line := importLine{
		Metric: map[string]string{
			"__name__":    VMMetricName,
			"ontology":    key.Ontology,
			"object_type": key.ObjectType,
			"primary_key": key.PrimaryKey,
			"property":    key.Property,
		},
		Values:     []float64{value},
		Timestamps: []int64{ts.UnixMilli()},
	}
	body, err := json.Marshal(line)
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func buildMatcher(key SeriesKey) string {
	var b strings.Builder
	b.WriteString(VMMetricName)
	b.WriteByte('{')
	writeLabel(&b, "ontology", key.Ontology, false)
	writeLabel(&b, "object_type", key.ObjectType, true)
	writeLabel(&b, "primary_key", key.PrimaryKey, true)
	writeLabel(&b, "property", key.Property, true)
	b.WriteByte('}')
	return b.String()
}

func writeLabel(b *strings.Builder, name, value string, sep bool) {
	if sep {
		b.WriteByte(',')
	}
	b.WriteString(name)
	b.WriteString(`="`)
	b.WriteString(escapeLabelValue(value))
	b.WriteByte('"')
}

// escapeLabelValue escapes the three reserved characters in a Prometheus
// label value: backslash, double quote, and newline.
func escapeLabelValue(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// coerceFloat best-effort converts the typed payload to float64 so the
// caller can decide whether to forward to VictoriaMetrics or fail with
// ErrNonNumericValue.
func coerceFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
