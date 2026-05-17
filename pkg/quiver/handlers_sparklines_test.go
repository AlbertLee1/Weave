package quiver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// US-483 — Quiver Sparkline 多系列预加载. The /sparklines endpoint
// batches every series in a saved dashboard into a single HTTP
// round-trip so the SPA's dashboard load drops from N requests to 1.
//
// The PRD acceptance points exercised below:
//   - POST /api/v2/quiver/dashboards/{rid}/sparklines 批量返回 — one
//     response carries the points for every series in the saved
//     dashboard's config.
//   - 前端 dashboard load 改为单请求 — the request count gate (the
//     `seriesIDs` filter case below) proves the same handler can serve
//     a subset on demand so the frontend never has to fall back to
//     per-series fan-out.

// countingTimeSeriesReader wraps a fake reader and tracks how many
// StreamPoints calls were observed so the "single batch == single
// reader fan-out" invariant can be asserted by tests.
type countingTimeSeriesReader struct {
	mu     sync.Mutex
	series map[string][]TimeSeriesPoint
	calls  atomic.Int64
}

func newCountingReader() *countingTimeSeriesReader {
	return &countingTimeSeriesReader{series: map[string][]TimeSeriesPoint{}}
}

func (c *countingTimeSeriesReader) put(ontology, objectType, primaryKey, property string, pts []TimeSeriesPoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.series[fakeKey(ontology, objectType, primaryKey, property)] = pts
}

func (c *countingTimeSeriesReader) StreamPoints(_ context.Context, key TimeSeriesKey) ([]TimeSeriesPoint, error) {
	c.calls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.series[fakeKey(key.Ontology, key.ObjectType, key.PrimaryKey, key.Property)], nil
}

func (c *countingTimeSeriesReader) callCount() int64 { return c.calls.Load() }

// errReader returns an error for every StreamPoints call so the
// 5xx-on-reader-failure path is exercisable.
type errReader struct{}

func (errReader) StreamPoints(_ context.Context, _ TimeSeriesKey) ([]TimeSeriesPoint, error) {
	return nil, errors.New("synthetic reader failure")
}

func seedFiveSeriesDashboard(t *testing.T, store Store, owner string) string {
	t.Helper()
	cfg := json.RawMessage(`{
		"ontologyApiName": "ont",
		"series": [
			{"id": "s1", "objectType": "Host", "primaryKey": "h1", "property": "cpu", "label": "h1 cpu", "color": "#1"},
			{"id": "s2", "objectType": "Host", "primaryKey": "h2", "property": "cpu", "label": "h2 cpu", "color": "#2"},
			{"id": "s3", "objectType": "Host", "primaryKey": "h3", "property": "cpu", "label": "h3 cpu", "color": "#3"},
			{"id": "s4", "objectType": "Host", "primaryKey": "h4", "property": "cpu", "label": "h4 cpu", "color": "#4"},
			{"id": "s5", "objectType": "Host", "primaryKey": "h5", "property": "cpu", "label": "h5 cpu", "color": "#5", "branch": "feature/x"}
		]
	}`)
	row := newRow("ri.quiver.main.dashboard.us483", owner, "5-series dashboard")
	row.Config = cfg
	if err := store.Save(context.Background(), row); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	return row.RID
}

func seedFiveSeriesPoints(t *testing.T, reader *countingTimeSeriesReader, when time.Time) {
	t.Helper()
	for i, pk := range []string{"h1", "h2", "h3", "h4", "h5"} {
		pts := []TimeSeriesPoint{
			{Time: when, Value: float64(i + 1)},
			{Time: when.Add(time.Minute), Value: float64(i + 1)},
		}
		reader.put("ont", "Host", pk, "cpu", pts)
	}
}

// TestSparklines_BatchedAllSeriesInOneRequest is the headline PRD
// scenario: POST .../sparklines for a 5-series dashboard returns one
// envelope carrying every series' points. The frontend can mount the
// workbench without firing per-series queries.
func TestSparklines_BatchedAllSeriesInOneRequest(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedFiveSeriesDashboard(t, store, alice.ID)

	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reader := newCountingReader()
	seedFiveSeriesPoints(t, reader, when)

	r := newTestRouterWithReader(store, reader, alice)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /sparklines: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp SparklinesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RID != rid {
		t.Fatalf("rid echo: want %q, got %q", rid, resp.RID)
	}
	if len(resp.Series) != 5 {
		t.Fatalf("series count: want 5, got %d", len(resp.Series))
	}
	// Reader fan-out: exactly 5 calls — the single HTTP request maps
	// 1:1 to the 5 series, not N HTTP × N series.
	if got := reader.callCount(); got != 5 {
		t.Fatalf("reader fan-out: want 5 StreamPoints calls, got %d", got)
	}
	// Each series carries its points and the metadata needed by the
	// chart row (id/label/color/objectType/primaryKey/property/branch).
	byID := map[string]SparklineSeries{}
	for _, s := range resp.Series {
		byID[s.ID] = s
	}
	for _, want := range []struct {
		id, pk, label, branch string
	}{
		{"s1", "h1", "h1 cpu", ""},
		{"s2", "h2", "h2 cpu", ""},
		{"s3", "h3", "h3 cpu", ""},
		{"s4", "h4", "h4 cpu", ""},
		{"s5", "h5", "h5 cpu", "feature/x"},
	} {
		s, ok := byID[want.id]
		if !ok {
			t.Fatalf("series %q missing from response", want.id)
		}
		if s.Label != want.label {
			t.Fatalf("series %q label: want %q, got %q", want.id, want.label, s.Label)
		}
		if s.PrimaryKey != want.pk {
			t.Fatalf("series %q pk: want %q, got %q", want.id, want.pk, s.PrimaryKey)
		}
		if s.Branch != want.branch {
			t.Fatalf("series %q branch: want %q, got %q", want.id, want.branch, s.Branch)
		}
		if len(s.Points) != 2 {
			t.Fatalf("series %q point count: want 2, got %d", want.id, len(s.Points))
		}
	}
}

// TestSparklines_SeriesIDsFilter_SubsetOnly proves the optional
// seriesIds body filter restricts the fan-out to the requested subset,
// preserving the dashboard config's series order.
func TestSparklines_SeriesIDsFilter_SubsetOnly(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedFiveSeriesDashboard(t, store, alice.ID)
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reader := newCountingReader()
	seedFiveSeriesPoints(t, reader, when)

	r := newTestRouterWithReader(store, reader, alice)
	body := bytes.NewReader([]byte(`{"seriesIds": ["s5", "s2"]}`))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /sparklines (subset): want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp SparklinesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Series) != 2 {
		t.Fatalf("series count: want 2, got %d", len(resp.Series))
	}
	// Order must reflect dashboard config order, not the body's, so
	// the frontend doesn't have to re-sort: s2 declared before s5.
	if resp.Series[0].ID != "s2" || resp.Series[1].ID != "s5" {
		t.Fatalf("series order drift: want [s2, s5], got [%s, %s]",
			resp.Series[0].ID, resp.Series[1].ID)
	}
	if got := reader.callCount(); got != 2 {
		t.Fatalf("subset reader fan-out: want 2 StreamPoints calls, got %d", got)
	}
}

// TestSparklines_ShareSemantics — any authenticated caller who knows
// the RID can fetch sparklines, mirroring /view and /data.
func TestSparklines_ShareSemantics(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	rid := seedFiveSeriesDashboard(t, store, alice.ID)
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reader := newCountingReader()
	seedFiveSeriesPoints(t, reader, when)

	// Bob is not the owner but knows the RID.
	r := newTestRouterWithReader(store, reader, bob)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("non-owner /sparklines: want 200 (share), got %d (%s)", w.Code, w.Body.String())
	}
}

// TestSparklines_UnknownRID_404 — non-existent dashboard yields 404.
func TestSparklines_UnknownRID_404(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	reader := newCountingReader()

	r := newTestRouterWithReader(store, reader, alice)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/ri.quiver.main.dashboard.nope/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown rid: want 404, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestSparklines_NoReader_5xx — without a wired TimeSeriesReader the
// endpoint returns a structured 5xx so the SPA can hide the chart panel.
func TestSparklines_NoReader_5xx(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedFiveSeriesDashboard(t, store, alice.ID)

	r := newTestRouterWithReader(store, nil, alice)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code < 500 {
		t.Fatalf("no reader: want 5xx, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestSparklines_Unauthenticated_401 — share semantics still require
// any authenticated caller; anonymous access is denied.
func TestSparklines_Unauthenticated_401(t *testing.T) {
	store := NewMemoryStore()
	rid := seedFiveSeriesDashboard(t, store, "user:alice")
	reader := newCountingReader()

	r := newTestRouterWithReader(store, reader, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /sparklines: want 401, got %d", w.Code)
	}
}

// TestSparklines_EmptyBody_AllSeries — the SPA can POST with no body
// (or an empty object) and the handler treats it as "every series".
func TestSparklines_EmptyBody_AllSeries(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedFiveSeriesDashboard(t, store, alice.ID)
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reader := newCountingReader()
	seedFiveSeriesPoints(t, reader, when)

	r := newTestRouterWithReader(store, reader, alice)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		strings.NewReader("")) // wholly empty body — not even `{}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty body: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp SparklinesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Series) != 5 {
		t.Fatalf("series count: want 5, got %d", len(resp.Series))
	}
}

// TestSparklines_ReaderFailure_5xx — a partial reader failure surfaces
// the upstream error so the SPA can degrade the panel rather than
// silently emit half a chart.
func TestSparklines_ReaderFailure_5xx(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedFiveSeriesDashboard(t, store, alice.ID)

	r := newTestRouterWithReader(store, errReader{}, alice)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+rid+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code < 500 {
		t.Fatalf("reader failure: want 5xx, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestSparklines_NoSeriesInDashboard_EmptyArray — empty config series
// emits an empty (not nil) array so the SPA can render "no data" UI
// without special-casing the wire shape.
func TestSparklines_NoSeriesInDashboard_EmptyArray(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	row := newRow("ri.quiver.main.dashboard.empty", alice.ID, "empty")
	row.Config = json.RawMessage(`{"ontologyApiName":"ont","series":[]}`)
	if err := store.Save(context.Background(), row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	reader := newCountingReader()

	r := newTestRouterWithReader(store, reader, alice)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+row.RID+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty dashboard: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	// The wire MUST contain `"series":[]` (not `"series":null`) — the
	// frontend keys off Array.isArray and a null breaks the renderer.
	if !strings.Contains(w.Body.String(), `"series":[]`) {
		t.Fatalf("empty series wire shape: want literal []·, got %s", w.Body.String())
	}
	if got := reader.callCount(); got != 0 {
		t.Fatalf("reader fan-out on empty dashboard: want 0, got %d", got)
	}
}
