package oss_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/scenarios"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeOrder(id, status string, total float64) *oss.WireObject {
	return &oss.WireObject{
		PrimaryKey: id,
		APIName:    "Order",
		Properties: map[string]any{"status": status, "total": total},
	}
}

func metricByName(row aggregation.AggregationRow, name string) (float64, bool) {
	for _, m := range row.Metrics {
		if m.Name != name {
			continue
		}
		switch v := m.Value.(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// VTX-005 BDD: aggregation with X-Scenario-Id overlay
// ---------------------------------------------------------------------------

// BDD #1: 10 Orders 总价 1000 + scenario modifyProperty 加 500 → SUM 1500.
func TestScenarioAggregate_Given_ModifyEdit_When_GroupBySumTotal_Then_OverlayValue(t *testing.T) {
	base := make([]*oss.WireObject, 10)
	for i := 0; i < 10; i++ {
		base[i] = makeOrder(fmt.Sprintf("O-%d", i), "pending", 100) // 10 * 100 = 1000
	}
	edits := []scenarios.ScenarioEdit{
		// O-0.total 100 → 600. Net +500. Group by status=pending.
		{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-0", Property: "total", NewValue: raw(600)},
	}
	req := &aggregation.AggregationRequest{
		ObjectType:   "Order",
		Aggregations: []aggregation.AggregationSpec{{Type: "sum", Field: "total", Name: "total_sum"}},
		GroupBy:      []aggregation.GroupBySpec{{Type: "exact", Field: "status"}},
	}

	resp, err := oss.AggregateWithOverlay(base, edits, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 group, got %d", len(resp.Data))
	}
	row := resp.Data[0]
	if row.Group["status"] != "pending" {
		t.Errorf("group: got %v want pending", row.Group["status"])
	}
	v, ok := metricByName(row, "total_sum")
	if !ok {
		t.Fatalf("metric total_sum missing: %+v", row.Metrics)
	}
	if v != 1500 {
		t.Errorf("sum: got %v want 1500", v)
	}
}

// BDD #2: 10 Orders, scenario deletes 2 → COUNT 8.
func TestScenarioAggregate_Given_DeleteEdits_When_Count_Then_BaseMinusDeleted(t *testing.T) {
	base := make([]*oss.WireObject, 10)
	for i := 0; i < 10; i++ {
		base[i] = makeOrder(fmt.Sprintf("O-%d", i), "pending", 100)
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Order", ObjectID: "O-3"},
		{Seq: 2, Op: "deleteObject", ObjectType: "Order", ObjectID: "O-7"},
	}
	req := &aggregation.AggregationRequest{
		ObjectType:   "Order",
		Aggregations: []aggregation.AggregationSpec{{Type: "count", Name: "n"}},
	}

	resp, err := oss.AggregateWithOverlay(base, edits, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data len: %d", len(resp.Data))
	}
	v, _ := metricByName(resp.Data[0], "n")
	if v != 8 {
		t.Errorf("count: got %v want 8", v)
	}
}

// BDD #3: 10 Orders, scenario creates 3 new → COUNT 13.
func TestScenarioAggregate_Given_CreateEdits_When_Count_Then_BasePlusCreated(t *testing.T) {
	base := make([]*oss.WireObject, 10)
	for i := 0; i < 10; i++ {
		base[i] = makeOrder(fmt.Sprintf("O-%d", i), "pending", 100)
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "N-1", NewValue: raw(map[string]any{"status": "new", "total": 10})},
		{Seq: 2, Op: "createObject", ObjectType: "Order", ObjectID: "N-2", NewValue: raw(map[string]any{"status": "new", "total": 20})},
		{Seq: 3, Op: "createObject", ObjectType: "Order", ObjectID: "N-3", NewValue: raw(map[string]any{"status": "new", "total": 30})},
	}
	req := &aggregation.AggregationRequest{
		ObjectType:   "Order",
		Aggregations: []aggregation.AggregationSpec{{Type: "count", Name: "n"}},
	}

	resp, err := oss.AggregateWithOverlay(base, edits, req)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := metricByName(resp.Data[0], "n")
	if v != 13 {
		t.Errorf("count: got %v want 13", v)
	}
}

// Cross-cutting: SUM with delete + modify + create combined.
func TestScenarioAggregate_Given_MixedEdits_When_Sum_Then_AllAccountedFor(t *testing.T) {
	base := []*oss.WireObject{
		makeOrder("A", "pending", 10),
		makeOrder("B", "pending", 20),
		makeOrder("C", "shipped", 30),
		makeOrder("D", "shipped", 40),
	} // pending=30, shipped=70 → SUM 100
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "deleteObject", ObjectType: "Order", ObjectID: "A"},                                                                          // -10
		{Seq: 2, Op: "modifyProperty", ObjectType: "Order", ObjectID: "B", Property: "total", NewValue: raw(200)},                                 // +180
		{Seq: 3, Op: "createObject", ObjectType: "Order", ObjectID: "E", NewValue: raw(map[string]any{"status": "pending", "total": float64(5)})}, // +5
	}
	// post-overlay: B=200, C=30, D=40, E=5 → SUM 275
	req := &aggregation.AggregationRequest{
		ObjectType:   "Order",
		Aggregations: []aggregation.AggregationSpec{{Type: "sum", Field: "total", Name: "s"}},
	}
	resp, err := oss.AggregateWithOverlay(base, edits, req)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := metricByName(resp.Data[0], "s")
	if v != 275 {
		t.Errorf("sum: got %v want 275", v)
	}
}

// AVG metric — guards the average codepath since BDD focuses on COUNT/SUM.
func TestScenarioAggregate_Given_OverlayEdits_When_Avg_Then_RecomputedFromOverlay(t *testing.T) {
	base := []*oss.WireObject{
		makeOrder("A", "x", 100),
		makeOrder("B", "x", 200),
		makeOrder("C", "x", 300),
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "C", Property: "total", NewValue: raw(900)},
	}
	// After overlay: 100, 200, 900 → AVG 400
	req := &aggregation.AggregationRequest{
		ObjectType:   "Order",
		Aggregations: []aggregation.AggregationSpec{{Type: "avg", Field: "total", Name: "a"}},
	}
	resp, err := oss.AggregateWithOverlay(base, edits, req)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := metricByName(resp.Data[0], "a")
	if v != 400 {
		t.Errorf("avg: got %v want 400", v)
	}
}

// ---------------------------------------------------------------------------
// Handler integration: POST /aggregate with X-Scenario-Id routes through the
// overlay path. Without the header the existing Bleve engine path is used.
// ---------------------------------------------------------------------------

type listingService struct {
	fakeService
	rows []*oss.WireObject
}

func (l *listingService) ListObjects(_ context.Context, req oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	if req.ObjectType != "Order" {
		return nil, errors.New("unexpected objectType")
	}
	return &oss.ObjectPage{Data: l.rows}, nil
}

func TestScenarioAggregate_Given_HeaderPresent_When_PostAggregate_Then_OverlayPath(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	const scenarioRID = "ri.vertex.main.scenario.s1"

	rows := []*oss.WireObject{
		makeOrder("A", "pending", 10),
		makeOrder("B", "pending", 20),
	}
	svc := &listingService{rows: rows}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			scenarioRID: {RID: scenarioRID, ParentOntologyCommit: ontologyRID},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			scenarioRID: {
				{Seq: 1, Op: "createObject", ObjectType: "Order", ObjectID: "N", NewValue: raw(map[string]any{"status": "pending", "total": float64(70)})},
			},
		},
	}
	router := newTestRouter(svc, reader)

	body := []byte(`{"aggregation":[{"type":"sum","field":"total","name":"s"}]}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp aggregation.AggregationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data len: %d", len(resp.Data))
	}
	v, _ := metricByName(resp.Data[0], "s")
	if v != 100 {
		t.Errorf("sum (10+20+70): got %v want 100", v)
	}
}

func TestScenarioAggregate_Given_HeaderPresent_When_ScenarioMissing_Then_404(t *testing.T) {
	svc := &listingService{}
	reader := &fakeScenarioReader{scenarios: map[string]*scenarios.Scenario{}}
	router := newTestRouter(svc, reader)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/o1/objects/Order/aggregate",
		bytes.NewReader([]byte(`{"aggregation":[{"type":"count","name":"n"}]}`)))
	req.Header.Set("X-Scenario-Id", "ri.vertex.main.scenario.nope")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["errorName"] != "ScenarioNotFound" {
		t.Errorf("errorName: got %v", body["errorName"])
	}
}

// ---------------------------------------------------------------------------
// US-479 BDD: scenario modifies the groupBy property itself → folded value
// drives bucket assignment (not the base value).
// ---------------------------------------------------------------------------

// TestUS479_GroupByOnFoldedProperty_BucketsReflectScenarioEdits is the
// canonical PRD scenario: 10 base Orders are all status=pending; a scenario
// modifyProperty edit flips O-0.status to "shipped". The aggregation must
// produce TWO buckets (pending=9, shipped=1) — proving that groupBy reads
// post-fold property values, not the base index.
func TestUS479_GroupByOnFoldedProperty_BucketsReflectScenarioEdits(t *testing.T) {
	base := make([]*oss.WireObject, 10)
	for i := 0; i < 10; i++ {
		base[i] = makeOrder(fmt.Sprintf("O-%d", i), "pending", 100)
	}
	edits := []scenarios.ScenarioEdit{
		// Move O-0 from pending → shipped. groupBy=status should now place
		// O-0 in a separate bucket from the other 9 rows.
		{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-0", Property: "status", NewValue: raw("shipped")},
	}
	req := &aggregation.AggregationRequest{
		ObjectType:   "Order",
		Aggregations: []aggregation.AggregationSpec{{Type: "count", Name: "n"}, {Type: "sum", Field: "total", Name: "s"}},
		GroupBy:      []aggregation.GroupBySpec{{Type: "exact", Field: "status"}},
	}

	resp, err := oss.AggregateWithOverlay(base, edits, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 buckets after fold, got %d: %+v", len(resp.Data), resp.Data)
	}

	got := map[string]map[string]float64{}
	for _, row := range resp.Data {
		key := fmt.Sprintf("%v", row.Group["status"])
		n, _ := metricByName(row, "n")
		s, _ := metricByName(row, "s")
		got[key] = map[string]float64{"n": n, "s": s}
	}
	if got["pending"]["n"] != 9 {
		t.Errorf("pending bucket count: got %v want 9", got["pending"]["n"])
	}
	if got["pending"]["s"] != 900 {
		t.Errorf("pending bucket sum: got %v want 900", got["pending"]["s"])
	}
	if got["shipped"]["n"] != 1 {
		t.Errorf("shipped bucket count: got %v want 1", got["shipped"]["n"])
	}
	if got["shipped"]["s"] != 100 {
		t.Errorf("shipped bucket sum: got %v want 100", got["shipped"]["s"])
	}
}

// TestUS479_GroupByOnFoldedProperty_OverHTTP exercises the same PRD scenario
// through the chi router: X-Scenario-Id header → overlay path → groupBy by
// folded status. Guards the handler wiring + serialization in addition to
// the executor.
func TestUS479_GroupByOnFoldedProperty_OverHTTP(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.northwind"
	const scenarioRID = "ri.vertex.main.scenario.us479"

	rows := make([]*oss.WireObject, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, makeOrder(fmt.Sprintf("O-%d", i), "pending", 100))
	}
	svc := &listingService{rows: rows}
	reader := &fakeScenarioReader{
		scenarios: map[string]*scenarios.Scenario{
			scenarioRID: {RID: scenarioRID, ParentOntologyCommit: ontologyRID},
		},
		edits: map[string][]scenarios.ScenarioEdit{
			scenarioRID: {
				{Seq: 1, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-0", Property: "status", NewValue: raw("shipped")},
				{Seq: 2, Op: "modifyProperty", ObjectType: "Order", ObjectID: "O-1", Property: "status", NewValue: raw("shipped")},
			},
		},
	}
	router := newTestRouter(svc, reader)

	body := []byte(`{"aggregation":[{"type":"count","name":"n"}],"groupBy":[{"type":"exact","field":"status"}]}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontologyRID+"/objects/Order/aggregate",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scenario-Id", scenarioRID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp aggregation.AggregationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 buckets, got %d: %+v", len(resp.Data), resp.Data)
	}
	counts := map[string]float64{}
	for _, row := range resp.Data {
		k := fmt.Sprintf("%v", row.Group["status"])
		c, _ := metricByName(row, "n")
		counts[k] = c
	}
	if counts["pending"] != 3 {
		t.Errorf("pending: got %v want 3", counts["pending"])
	}
	if counts["shipped"] != 2 {
		t.Errorf("shipped: got %v want 2", counts["shipped"])
	}
}
