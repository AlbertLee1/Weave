package quiver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

// fakeTimeSeriesReader satisfies TimeSeriesReader for the /data handler
// tests. The series are keyed by (ontology, objectType, primaryKey,
// property) so a single fake reader can back a multi-series dashboard.
type fakeTimeSeriesReader struct {
	series map[string][]TimeSeriesPoint
}

func newFakeReader() *fakeTimeSeriesReader {
	return &fakeTimeSeriesReader{series: map[string][]TimeSeriesPoint{}}
}

func (f *fakeTimeSeriesReader) put(ontology, objectType, primaryKey, property string, pts []TimeSeriesPoint) {
	f.series[fakeKey(ontology, objectType, primaryKey, property)] = pts
}

func (f *fakeTimeSeriesReader) StreamPoints(_ context.Context, key TimeSeriesKey) ([]TimeSeriesPoint, error) {
	return f.series[fakeKey(key.Ontology, key.ObjectType, key.PrimaryKey, key.Property)], nil
}

func fakeKey(ontology, objectType, primaryKey, property string) string {
	return ontology + "\x00" + objectType + "\x00" + primaryKey + "\x00" + property
}

func newTestRouterWithReader(store Store, reader TimeSeriesReader, user *auth.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			if user != nil {
				ctx = auth.WithUser(ctx, user)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h := NewHandler(store)
	if reader != nil {
		h.SetTimeSeriesReader(reader)
	}
	h.RegisterRoutes(r)
	return r
}

// seedDashboardWithSeries persists a dashboard whose config wires up
// three series so /data is exercised at the literal PRD scale.
func seedDashboardWithSeries(t *testing.T, store Store, owner string) string {
	t.Helper()
	cfg := json.RawMessage(`{
		"ontologyApiName": "ont",
		"series": [
			{"id": "s1", "objectType": "Host", "primaryKey": "h1", "property": "cpu", "label": "CPU h1", "color": "#1"},
			{"id": "s2", "objectType": "Host", "primaryKey": "h2", "property": "cpu", "label": "CPU h2", "color": "#2"},
			{"id": "s3", "objectType": "Host", "primaryKey": "h3", "property": "cpu", "label": "CPU h3", "color": "#3"}
		]
	}`)
	row := newRow("ri.quiver.main.dashboard.d1", owner, "3-series dashboard")
	row.Config = cfg
	if err := store.Save(context.Background(), row); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	return row.RID
}

// TestHandler_Data_ThreeSeries_OneDay_FiveMinute is the literal PRD
// "3 series / 1d range / 5min step" acceptance scenario. Seeds 3 series
// with one point per minute over 24h; expects 24h/5min = 288 buckets
// per series, all in ascending time order with the right aggregated
// value.
func TestHandler_Data_ThreeSeries_OneDay_FiveMinute(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedDashboardWithSeries(t, store, alice.ID)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	reader := newFakeReader()
	for _, pk := range []string{"h1", "h2", "h3"} {
		pts := make([]TimeSeriesPoint, 0, 24*60)
		for m := 0; m < 24*60; m++ {
			pts = append(pts, TimeSeriesPoint{
				Time:  from.Add(time.Duration(m) * time.Minute),
				Value: float64(m),
			})
		}
		reader.put("ont", "Host", pk, "cpu", pts)
	}

	r := newTestRouterWithReader(store, reader, alice)
	url := "/api/v2/quiver/dashboards/" + rid + "/data" +
		"?from=" + from.Format(time.RFC3339) +
		"&to=" + to.Format(time.RFC3339) +
		"&step=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /data: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp DataResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Step != "5m" {
		t.Fatalf("step echo: want 5m, got %q", resp.Step)
	}
	if !resp.From.Equal(from) || !resp.To.Equal(to) {
		t.Fatalf("from/to echo drifted: from=%s to=%s", resp.From, resp.To)
	}
	if len(resp.Series) != 3 {
		t.Fatalf("series count: want 3, got %d", len(resp.Series))
	}
	wantIDs := map[string]string{"s1": "CPU h1", "s2": "CPU h2", "s3": "CPU h3"}
	for _, s := range resp.Series {
		if wantIDs[s.ID] != s.Label {
			t.Fatalf("series %q: label drift, want %q got %q", s.ID, wantIDs[s.ID], s.Label)
		}
		if len(s.Points) != 288 { // 24h * 60min / 5min
			t.Fatalf("series %q: want 288 buckets, got %d", s.ID, len(s.Points))
		}
		// Verify ascending time order + first bucket aligns to from.
		for i := 1; i < len(s.Points); i++ {
			if !s.Points[i].Time.After(s.Points[i-1].Time) {
				t.Fatalf("series %q points not ascending at %d", s.ID, i)
			}
		}
		if !s.Points[0].Time.Equal(from) {
			t.Fatalf("series %q first bucket: want %s, got %s", s.ID, from, s.Points[0].Time)
		}
		// First bucket holds minutes 0..4 → avg = (0+1+2+3+4)/5 = 2.0
		first, ok := s.Points[0].Value.(float64)
		if !ok {
			t.Fatalf("series %q first value not float64: %T", s.ID, s.Points[0].Value)
		}
		if first != 2.0 {
			t.Fatalf("series %q first bucket avg: want 2.0, got %v", s.ID, first)
		}
	}
}

func TestHandler_Data_FromAfterTo_400(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedDashboardWithSeries(t, store, alice.ID)
	reader := newFakeReader()

	r := newTestRouterWithReader(store, reader, alice)
	url := "/api/v2/quiver/dashboards/" + rid + "/data?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z&step=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("from>to: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_Data_BadStep_400(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedDashboardWithSeries(t, store, alice.ID)
	reader := newFakeReader()

	r := newTestRouterWithReader(store, reader, alice)
	url := "/api/v2/quiver/dashboards/" + rid + "/data?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&step=not-a-duration"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad step: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_Data_UnknownDashboard_404(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	reader := newFakeReader()

	r := newTestRouterWithReader(store, reader, alice)
	url := "/api/v2/quiver/dashboards/ri.quiver.main.dashboard.unknown/data?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&step=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown rid: want 404, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestHandler_Data_ShareSemantics — anyone authenticated with the RID can
// read /data, mirroring the read-only `/view` share surface.
func TestHandler_Data_ShareSemantics(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	rid := seedDashboardWithSeries(t, store, alice.ID)
	reader := newFakeReader()
	reader.put("ont", "Host", "h1", "cpu", []TimeSeriesPoint{
		{Time: time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC), Value: 42.0},
	})

	r := newTestRouterWithReader(store, reader, bob)
	url := "/api/v2/quiver/dashboards/" + rid + "/data?from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&step=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("non-owner /data: want 200 (share), got %d (%s)", w.Code, w.Body.String())
	}
}

// TestHandler_Data_NoReaderConfigured_500 — when the deployment lacks a
// TimeSeriesReader (degraded mode), /data returns a structured 5xx so
// the SPA can hide the chart panel.
func TestHandler_Data_NoReaderConfigured_500(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	rid := seedDashboardWithSeries(t, store, alice.ID)

	r := newTestRouterWithReader(store, nil, alice)
	url := "/api/v2/quiver/dashboards/" + rid + "/data?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&step=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code < 500 {
		t.Fatalf("no reader: want 5xx, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestHandler_Data_Unauthenticated_401 — /data still requires an
// authenticated caller. Share-link semantics only mean any
// authenticated caller is allowed, not anonymous public access.
func TestHandler_Data_Unauthenticated_401(t *testing.T) {
	store := NewMemoryStore()
	rid := seedDashboardWithSeries(t, store, "user:alice")
	reader := newFakeReader()

	r := newTestRouterWithReader(store, reader, nil)
	url := "/api/v2/quiver/dashboards/" + rid + "/data?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&step=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /data: want 401, got %d", w.Code)
	}
}

