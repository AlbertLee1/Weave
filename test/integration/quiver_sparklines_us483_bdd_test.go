//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/quiver"
)

// US-483 BDD — Quiver Sparkline 多系列预加载.
//
// PRD acceptance:
//   - POST /api/v2/quiver/dashboards/{rid}/sparklines 批量返回
//   - 前端 dashboard load 改为单请求
//
// Two scenarios cover the contract:
//   - Given a 5-series dashboard, when the SPA POSTs /sparklines once,
//     then exactly one HTTP request returns all 5 series' points AND
//     the TimeSeriesReader is invoked exactly 5 times (not 5×N due to
//     accidental fan-out at the handler).
//   - Negative control: unknown RID → 404, so an "always 200 + empty"
//     regression doesn't silently pass the positive scenario.

type us483FakeTSReader struct {
	mu     sync.Mutex
	series map[string][]quiver.TimeSeriesPoint
	calls  atomic.Int64
}

func (f *us483FakeTSReader) StreamPoints(_ context.Context, key quiver.TimeSeriesKey) ([]quiver.TimeSeriesPoint, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.series[us483Key(key)], nil
}

func us483Key(k quiver.TimeSeriesKey) string {
	return k.Ontology + "|" + k.ObjectType + "|" + k.PrimaryKey + "|" + k.Property
}

// setupUS483Fixture stands up a chi router with a real PG container
// (to keep this suite environment-symmetric with US-482 BDD), a
// quiver.MemoryStore (PG-backed Quiver store is covered by cmd/server
// unit tests), and a counting fake TimeSeriesReader.
func setupUS483Fixture(t *testing.T) (*chi.Mux, quiver.Store, *us483FakeTSReader) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := quiver.NewMemoryStore()
	reader := &us483FakeTSReader{series: map[string][]quiver.TimeSeriesPoint{}}
	h := quiver.NewHandler(store)
	h.SetTimeSeriesReader(reader)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.WithUser(req.Context(), &auth.User{ID: "user:alice"})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	h.RegisterRoutes(r)
	return r, store, reader
}

func TestBDD_US483_QuiverSparklines_BatchedFiveSeriesInOneRequest(t *testing.T) {
	router, store, reader := setupUS483Fixture(t)
	ctx := context.Background()

	// Given a 5-series dashboard persisted in the store, with one of
	// the series pinned to a non-main branch (proves the optional
	// branch is round-tripped, mirroring /data semantics).
	cfg := json.RawMessage(`{
		"ontologyApiName": "us483",
		"series": [
			{"id": "s1", "objectType": "Sensor", "primaryKey": "p-001", "property": "temp", "label": "Sensor 1 temp", "color": "#1"},
			{"id": "s2", "objectType": "Sensor", "primaryKey": "p-002", "property": "temp", "label": "Sensor 2 temp", "color": "#2"},
			{"id": "s3", "objectType": "Sensor", "primaryKey": "p-003", "property": "temp", "label": "Sensor 3 temp", "color": "#3"},
			{"id": "s4", "objectType": "Sensor", "primaryKey": "p-004", "property": "temp", "label": "Sensor 4 temp", "color": "#4"},
			{"id": "s5", "objectType": "Sensor", "primaryKey": "p-005", "property": "temp", "label": "Sensor 5 temp", "color": "#5", "branch": "feature/dr"}
		]
	}`)
	row := &quiver.Dashboard{
		RID:    "ri.quiver.main.dashboard.us483",
		Owner:  "user:alice",
		Name:   "US-483 BDD",
		Config: cfg,
	}
	if err := store.Save(ctx, row); err != nil {
		t.Fatalf("seed dashboard: %v", err)
	}

	// And 24h of per-minute readings for each of the 5 sensors so the
	// "single batch carries real data" check is non-trivial (an empty
	// fan-out would also "pass" a callCount=5 check but would emit
	// zero points per series).
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, pk := range []string{"p-001", "p-002", "p-003", "p-004", "p-005"} {
		pts := make([]quiver.TimeSeriesPoint, 0, 24*60)
		for m := 0; m < 24*60; m++ {
			pts = append(pts, quiver.TimeSeriesPoint{
				Time:  when.Add(time.Duration(m) * time.Minute),
				Value: float64(m + i),
			})
		}
		reader.series[us483Key(quiver.TimeSeriesKey{
			Ontology: "us483", ObjectType: "Sensor", PrimaryKey: pk, Property: "temp",
		})] = pts
	}

	// When the SPA POSTs once to /sparklines (empty body = "all series").
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/"+row.RID+"/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Then 200 with the 5-series batch shape.
	if w.Code != http.StatusOK {
		t.Fatalf("POST /sparklines: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp quiver.SparklinesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RID != row.RID {
		t.Fatalf("rid echo: want %q, got %q", row.RID, resp.RID)
	}
	if len(resp.Series) != 5 {
		t.Fatalf("series count: want 5, got %d", len(resp.Series))
	}
	// Frontend single-request invariant: one HTTP POST yields exactly
	// 5 reader calls, not 5×N or N². A regression that fans out via
	// per-series HTTP would either inflate the call count or split
	// across requests.
	if got := reader.calls.Load(); got != 5 {
		t.Fatalf("batch fan-out: want 5 reader calls, got %d", got)
	}
	// Each series carries the seeded 1440 raw points (sparkline path
	// is unbucketed by design — the SPA chart picks the resampling).
	wantOrder := []string{"s1", "s2", "s3", "s4", "s5"}
	for i, s := range resp.Series {
		if s.ID != wantOrder[i] {
			t.Fatalf("series order drift at index %d: want %q, got %q", i, wantOrder[i], s.ID)
		}
		if len(s.Points) != 24*60 {
			t.Fatalf("series %q point count: want 1440, got %d", s.ID, len(s.Points))
		}
	}
	// s5 must round-trip its branch override.
	if resp.Series[4].Branch != "feature/dr" {
		t.Fatalf("series s5 branch: want %q, got %q", "feature/dr", resp.Series[4].Branch)
	}
}

// TestBDD_US483_QuiverSparklines_UnknownRID_404 is the negative
// control. Without it an "always 200 + empty array" regression would
// silently pass the positive scenario above.
func TestBDD_US483_QuiverSparklines_UnknownRID_404(t *testing.T) {
	router, _, _ := setupUS483Fixture(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/quiver/dashboards/ri.quiver.main.dashboard.does-not-exist/sparklines",
		bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown rid: want 404, got %d (%s)", w.Code, w.Body.String())
	}
}
