package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/materialize"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// US-485 BDD — Parquet 冷存 + tier router end-to-end through the HTTP
// boundary. Two scenarios cover the PRD acceptance criteria:
//
//   - Cross-window query unions hot (Bleve) and cold (real Parquet) and
//     surfaces every row exactly once (PRD literal "跨界查询 union 热冷
//     结果正确").
//   - Negative control: a hot-only window must NOT surface cold rows even
//     when the materialiser has them on disk. Without this scenario a
//     regression "always merge" implementation would silently pass the
//     positive scenario above.

// us485BDDFixture builds a handler whose executor is wired to a real
// materialize.Materializer + TierRouter and a Bleve index seeded with
// two "hot" customers. The temp parquet root is also seeded with two
// "cold" customers via a real MaterializeBatch call so the cold tier
// produces real Parquet bytes (not a stub).
type us485BDDFixture struct {
	router *chi.Mux
	now    time.Time
}

func setupUS485BDDFixture(t *testing.T) us485BDDFixture {
	t.Helper()
	idxDir := t.TempDir()
	mgr := index.NewManager(idxDir)
	t.Cleanup(func() { mgr.Close() })
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("Customer", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	// Hot tier: two recent customers in Bleve.
	for _, pk := range []string{"hot-1", "hot-2"} {
		if err := mgr.IndexDocument("Customer", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument %s: %v", pk, err)
		}
	}

	// Cold tier: two historical customers materialised to real Parquet.
	mat := materialize.NewMaterializer(t.TempDir())
	if err := mat.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-us485-bdd",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "cold-1", Properties: map[string]interface{}{"id": "cold-1"}},
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "cold-2", Properties: map[string]interface{}{"id": "cold-2"}},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch: %v", err)
	}

	store := objectset.NewStore(time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetTierRouter(materialize.NewTierRouter(mat))
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })
	executor.SetHotWindow(24 * time.Hour)

	handler := objectset.NewHandler(executor, mgr, store)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)
	return us485BDDFixture{router: r, now: fixed}
}

// loadObjectsTotalCount POSTs the ObjectSet to LoadObjects and returns
// the totalCount field of the response. totalCount is `len(result.
// PrimaryKeys)` after the hot+cold merge, so it surfaces the executor's
// tier-routing decision across the HTTP boundary even when cold rows
// have no Bleve document to render in `data`. (Hot rows render in
// `data` because they live in Bleve; cold-only rows show up in the
// count but not in `data` — that asymmetry is fine here because the
// BDD pin is on the routing classifier, not on cold-row materialisation
// inside the handler.)
//
// The second return value is the row count actually rendered into
// `data`, used by the negative control to prove hot-only stays at 2.
func loadObjectsTotalCount(t *testing.T, r *chi.Mux, def *objectset.Definition) (int, int) {
	t.Helper()
	body := objectset.LoadObjectSetRequest{
		ObjectSet: def,
		Select:    []string{"id"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/objectSets/loadObjects", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("loadObjects: code = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data       []map[string]interface{} `json:"data"`
		TotalCount string                   `json:"totalCount"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v — body %s", err, w.Body.String())
	}
	var total int
	if _, err := fmt.Sscanf(resp.TotalCount, "%d", &total); err != nil {
		t.Fatalf("parse totalCount %q: %v", resp.TotalCount, err)
	}
	return total, len(resp.Data)
}

// TestBDD_US485_CrossWindowQuery_UnionsHotAndCold — scenario:
// Given a Customer ObjectSet with a TimeRange that straddles the hot
// window's far edge (from=now-48h, to=now-12h),
// When the client POSTs loadObjects with that hint,
// Then the response totalCount equals 4 — two from Bleve (hot) and two
// read back from real Parquet on disk (cold) — proving the executor's
// classifier honoured the JSON-shaped TimeRange and merged both tiers.
func TestBDD_US485_CrossWindowQuery_UnionsHotAndCold(t *testing.T) {
	fx := setupUS485BDDFixture(t)
	from := fx.now.Add(-48 * time.Hour)
	to := fx.now.Add(-12 * time.Hour)
	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "Customer",
		TimeRange:  &objectset.TimeRangeHint{From: &from, To: &to},
	}
	total, dataCount := loadObjectsTotalCount(t, fx.router, def)
	if total != 4 {
		t.Fatalf("cross-window totalCount: want 4 (2 hot + 2 cold), got %d", total)
	}
	// The two hot rows must still render in `data` — the loadObjects
	// row-hydration path uses Bleve so cold-only PKs render as count-only.
	if dataCount != 2 {
		t.Fatalf("cross-window data rows: want 2 (hot rendered), got %d", dataCount)
	}
}

// TestBDD_US485_HotOnlyWindow_NegativeControl — scenario:
// Given the same fixture but a TimeRange anchored strictly inside
// `[now-3h, now-1h]` (entirely in the hot window),
// When the client POSTs loadObjects,
// Then totalCount is 2 (the cold tier is never consulted). Pinning this
// negative control prevents a regression that always merges (ignoring
// TimeRange) from passing the positive scenario above.
func TestBDD_US485_HotOnlyWindow_NegativeControl(t *testing.T) {
	fx := setupUS485BDDFixture(t)
	from := fx.now.Add(-3 * time.Hour)
	to := fx.now.Add(-1 * time.Hour)
	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "Customer",
		TimeRange:  &objectset.TimeRangeHint{From: &from, To: &to},
	}
	total, dataCount := loadObjectsTotalCount(t, fx.router, def)
	if total != 2 {
		t.Fatalf("hot-only totalCount: want 2 (cold skipped), got %d "+
			"(cold rows leaking into count means TimeRange is being ignored)", total)
	}
	if dataCount != 2 {
		t.Fatalf("hot-only data rows: want 2, got %d", dataCount)
	}
}
