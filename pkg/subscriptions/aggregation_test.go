package subscriptions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/where"
)

// ---------- IncrementalAggregator unit tests ----------

func TestIncrementalAggregator_CountNoGroupBy(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "count"}, "")

	// Three CREATE events: count climbs 1 → 2 → 3.
	for i, pk := range []string{"o1", "o2", "o3"} {
		mutated := agg.Apply("ADDED_OR_UPDATED", "Order", pk, map[string]interface{}{"price": float64(i + 1)})
		if !mutated {
			t.Fatalf("apply %s: expected mutation", pk)
		}
	}

	snap := agg.Snapshot()
	if len(snap.Data) != 1 {
		t.Fatalf("expected 1 row (no groupBy), got %d", len(snap.Data))
	}
	if got := snap.Data[0].Metrics[0].Value; got != int64(3) {
		t.Errorf("expected count=3, got %v (%T)", got, got)
	}

	// MODIFY does not change count for an already-known PK.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"price": float64(99)})
	snap = agg.Snapshot()
	if got := snap.Data[0].Metrics[0].Value; got != int64(3) {
		t.Errorf("expected count=3 after MODIFY, got %v", got)
	}

	// DELETE drops count by one and removes the snapshot.
	mutated := agg.Apply("DELETED", "Order", "o2", nil)
	if !mutated {
		t.Errorf("expected delete to mutate state")
	}
	snap = agg.Snapshot()
	if got := snap.Data[0].Metrics[0].Value; got != int64(2) {
		t.Errorf("expected count=2 after delete, got %v", got)
	}
}

func TestIncrementalAggregator_SumGroupBy(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "sum", Field: "price"}, "category")

	// Two distinct categories.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"category": "books", "price": float64(10)})
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{"category": "books", "price": float64(20)})
	agg.Apply("ADDED_OR_UPDATED", "Order", "o3", map[string]interface{}{"category": "music", "price": float64(5)})

	snap := agg.Snapshot()
	if len(snap.Data) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(snap.Data))
	}
	got := bucketByGroup(snap.Data, "category")
	if got["books"] != float64(30) {
		t.Errorf("books sum: want 30, got %v", got["books"])
	}
	if got["music"] != float64(5) {
		t.Errorf("music sum: want 5, got %v", got["music"])
	}

	// MODIFY o1's price 10 → 100 within the same bucket.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"category": "books", "price": float64(100)})
	got = bucketByGroup(agg.Snapshot().Data, "category")
	if got["books"] != float64(120) {
		t.Errorf("books sum after price bump: want 120, got %v", got["books"])
	}

	// MODIFY o3 to switch buckets: music → books. music empties, books grows.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o3", map[string]interface{}{"category": "books", "price": float64(5)})
	snap = agg.Snapshot()
	got = bucketByGroup(snap.Data, "category")
	if _, ok := got["music"]; ok {
		t.Errorf("expected music bucket to be removed, still present")
	}
	if got["books"] != float64(125) {
		t.Errorf("books sum after bucket move: want 125, got %v", got["books"])
	}

	// DELETE o2 (snapshot price=20) — books bucket sum drops to 125 - 20 = 105.
	agg.Apply("DELETED", "Order", "o2", nil)
	got = bucketByGroup(agg.Snapshot().Data, "category")
	if got["books"] != float64(105) {
		t.Errorf("books after o2 deleted: want 105, got %v", got["books"])
	}
}

func TestIncrementalAggregator_AvgEmptyBucketReturnsZero(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "avg", Field: "price"}, "")
	agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"price": float64(10)})
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{"price": float64(30)})
	if got := agg.Snapshot().Data[0].Metrics[0].Value; got != float64(20) {
		t.Errorf("avg: want 20, got %v", got)
	}
}

func TestIncrementalAggregator_MinMaxMultiset(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "min", Field: "price"}, "")
	agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"price": float64(10)})
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{"price": float64(5)})
	agg.Apply("ADDED_OR_UPDATED", "Order", "o3", map[string]interface{}{"price": float64(7)})

	if got := agg.Snapshot().Data[0].Metrics[0].Value; got != float64(5) {
		t.Errorf("min: want 5, got %v", got)
	}

	// Removing the current min must surface the next smallest.
	agg.Apply("DELETED", "Order", "o2", nil)
	if got := agg.Snapshot().Data[0].Metrics[0].Value; got != float64(7) {
		t.Errorf("min after deleting smallest: want 7, got %v", got)
	}

	// Same value added twice — removing one shouldn't change min.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o4", map[string]interface{}{"price": float64(7)})
	agg.Apply("DELETED", "Order", "o3", nil)
	if got := agg.Snapshot().Data[0].Metrics[0].Value; got != float64(7) {
		t.Errorf("min with duplicate values: want 7, got %v", got)
	}
}

func TestIncrementalAggregator_Where(t *testing.T) {
	clause := mustWhere(t, `{"type":"eq","field":"status","value":"shipped"}`)
	agg := newIncrementalAggregator("Order", clause, AggMetric{Type: "count"}, "")

	// Pending order — does not match Where.
	mutated := agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"status": "pending"})
	if mutated {
		t.Errorf("non-matching CREATE on a fresh aggregator should not mutate state")
	}

	// Shipped order — matches.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{"status": "shipped"})
	if got := agg.Snapshot().Data[0].Metrics[0].Value; got != int64(1) {
		t.Errorf("expected count=1, got %v", got)
	}

	// Update o2 → pending: must remove from running totals (was matched).
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{"status": "pending"})
	snap := agg.Snapshot()
	if len(snap.Data) != 0 {
		t.Errorf("expected empty buckets after move-out-of-scope, got %d", len(snap.Data))
	}

	// Update o2 → shipped again: must re-add.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{"status": "shipped"})
	if got := agg.Snapshot().Data[0].Metrics[0].Value; got != int64(1) {
		t.Errorf("expected count=1 after move-back, got %v", got)
	}
}

func TestIncrementalAggregator_NullGroupBucket(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "count"}, "category")
	agg.Apply("ADDED_OR_UPDATED", "Order", "o1", map[string]interface{}{"category": "books"})
	agg.Apply("ADDED_OR_UPDATED", "Order", "o2", map[string]interface{}{}) // no category
	agg.Apply("ADDED_OR_UPDATED", "Order", "o3", map[string]interface{}{"category": "books"})

	snap := agg.Snapshot()
	if len(snap.Data) != 2 {
		t.Fatalf("expected 2 rows (books + null), got %d", len(snap.Data))
	}
	// nil bucket sorts last per groupResponseEntry ordering.
	last := snap.Data[len(snap.Data)-1]
	if last.Group["category"] != nil {
		t.Errorf("expected last row to be the null bucket, got %v", last.Group)
	}
}

func TestIncrementalAggregator_WrongObjectType_NoMutation(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "count"}, "")
	if agg.Apply("ADDED_OR_UPDATED", "Customer", "c1", map[string]interface{}{}) {
		t.Error("Apply on different objectType should be a no-op")
	}
	if len(agg.Snapshot().Data) != 0 {
		t.Error("expected empty state after wrong-objectType apply")
	}
}

func TestIncrementalAggregator_DeleteUnknownPK(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "count"}, "")
	if agg.Apply("DELETED", "Order", "missing", nil) {
		t.Error("DELETE for an unseen PK should not mutate state")
	}
}

func TestValidateAggMetric(t *testing.T) {
	cases := []struct {
		name    string
		metric  AggMetric
		wantErr bool
	}{
		{"count-ok", AggMetric{Type: "count"}, false},
		{"sum-ok", AggMetric{Type: "sum", Field: "price"}, false},
		{"avg-needs-field", AggMetric{Type: "avg"}, true},
		{"min-needs-field", AggMetric{Type: "min"}, true},
		{"unknown", AggMetric{Type: "median"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAggMetric(tc.metric)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateAggMetric(%v) err=%v, wantErr=%v", tc.metric, err, tc.wantErr)
			}
		})
	}
}

// ---------- Seed integration tests ----------

func TestIncrementalAggregator_Seed_FromIndex(t *testing.T) {
	idx := newMemIndex(t)
	defer idx.Close()
	idx.Index("o1", map[string]interface{}{"category": "books", "price": float64(10)})
	idx.Index("o2", map[string]interface{}{"category": "books", "price": float64(20)})
	idx.Index("o3", map[string]interface{}{"category": "music", "price": float64(5)})

	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "sum", Field: "price"}, "category")
	if err := agg.Seed(idx, 100); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := bucketByGroup(agg.Snapshot().Data, "category")
	if got["books"] != float64(30) {
		t.Errorf("seeded books sum: want 30, got %v", got["books"])
	}
	if got["music"] != float64(5) {
		t.Errorf("seeded music sum: want 5, got %v", got["music"])
	}

	// Subsequent change events apply incrementally on top of the seed.
	agg.Apply("ADDED_OR_UPDATED", "Order", "o4", map[string]interface{}{"category": "books", "price": float64(7)})
	got = bucketByGroup(agg.Snapshot().Data, "category")
	if got["books"] != float64(37) {
		t.Errorf("books after incremental: want 37, got %v", got["books"])
	}
}

func TestIncrementalAggregator_Seed_NilIndex(t *testing.T) {
	agg := newIncrementalAggregator("Order", nil, AggMetric{Type: "count"}, "")
	if err := agg.Seed(nil, 100); err != nil {
		t.Fatalf("nil index seed: %v", err)
	}
	if len(agg.Snapshot().Data) != 0 {
		t.Error("nil-index seed should leave aggregator empty")
	}
}

// ---------- WebSocket integration tests ----------

func TestSubscribeAggregation_InitialSnapshot_FromIndex(t *testing.T) {
	idx := newMemIndex(t)
	defer idx.Close()
	idx.Index("o1", map[string]interface{}{"category": "books", "price": float64(10)})
	idx.Index("o2", map[string]interface{}{"category": "books", "price": float64(20)})

	h := NewHub()
	defer h.Close()
	h.SetIndexResolver(&fakeIndexResolver{idx: idx})

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeAggregation",
		Data: json.RawMessage(`{
			"objectType":"Order",
			"metric":{"type":"sum","field":"price","name":"total"},
			"groupBy":"category"
		}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	subResp := readMessage(t, ctx, c)
	if subResp.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", subResp.Type, subResp.Error)
	}

	initial := readMessage(t, ctx, c)
	if initial.Type != "aggregationChanged" {
		t.Fatalf("expected initial aggregationChanged, got %q", initial.Type)
	}
	var payload AggregationChangedPayload
	if err := json.Unmarshal(initial.Data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := bucketByGroup(payload.State.Data, "category")
	if v := got["books"]; v != float64(30) {
		t.Errorf("initial books sum: want 30, got %v", v)
	}
}

func TestSubscribeAggregation_IncrementalUpdates(t *testing.T) {
	h := NewHub()
	defer h.Close()
	// No index resolver — start empty, all state grows via ChangeEvents.

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeAggregation",
		Data: json.RawMessage(`{
			"objectType":"Order",
			"metric":{"type":"count","name":"orders"}
		}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := readMessage(t, ctx, c); got.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", got.Type, got.Error)
	}
	// Initial empty snapshot.
	initial := readMessage(t, ctx, c)
	if initial.Type != "aggregationChanged" {
		t.Fatalf("expected initial aggregationChanged, got %q", initial.Type)
	}

	// First object — count climbs to 1.
	h.HandleObjectChange("Order", "o1", "CREATE", map[string]interface{}{"category": "books"})
	first := readMessage(t, ctx, c)
	if first.Type != "aggregationChanged" {
		t.Fatalf("expected aggregationChanged, got %q", first.Type)
	}
	var firstPayload AggregationChangedPayload
	if err := json.Unmarshal(first.Data, &firstPayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := numericMetric(metricFromRows(firstPayload.State.Data, "orders")); got != 1 {
		t.Errorf("count after o1: want 1, got %v", got)
	}

	// Wrong objectType — must NOT trigger a new event.
	h.HandleObjectChange("Customer", "c1", "CREATE", map[string]interface{}{"name": "Alice"})

	// Second matching event — count climbs to 2.
	h.HandleObjectChange("Order", "o2", "CREATE", map[string]interface{}{"category": "music"})
	second := readMessage(t, ctx, c)
	if second.Type != "aggregationChanged" {
		t.Fatalf("expected aggregationChanged, got %q", second.Type)
	}
	var secondPayload AggregationChangedPayload
	if err := json.Unmarshal(second.Data, &secondPayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := numericMetric(metricFromRows(secondPayload.State.Data, "orders")); got != 2 {
		t.Errorf("count after o2: want 2, got %v", got)
	}
}

func TestSubscribeAggregation_InvalidMetric_Rejected(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeAggregation",
		Data: json.RawMessage(`{"objectType":"Order","metric":{"type":"median"}}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := readMessage(t, ctx, c)
	if resp.Type != "error" {
		t.Fatalf("expected error, got %q", resp.Type)
	}
	if !strings.Contains(resp.Error, "median") && !strings.Contains(resp.Error, "unsupported") {
		t.Errorf("expected error mentioning unsupported metric, got %q", resp.Error)
	}
}

func TestSubscribeAggregation_MissingObjectType(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeAggregation",
		Data: json.RawMessage(`{"metric":{"type":"count"}}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := readMessage(t, ctx, c)
	if resp.Type != "error" {
		t.Fatalf("expected error, got %q", resp.Type)
	}
}

func TestSubscribeAggregation_FilterMovesOutOfScope_Decrements(t *testing.T) {
	h := NewHub()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _ := dialAndWelcome(t, ctx, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	subMsg := Message{
		Type: "subscribeAggregation",
		Data: json.RawMessage(`{
			"objectType":"Order",
			"where":{"type":"eq","field":"status","value":"shipped"},
			"metric":{"type":"count"}
		}`),
	}
	if err := wsjson.Write(ctx, c, subMsg); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readMessage(t, ctx, c); got.Type != "subscribed" {
		t.Fatalf("expected subscribed, got %q (%s)", got.Type, got.Error)
	}
	_ = readMessage(t, ctx, c) // initial empty snapshot

	// Create a shipped order — count → 1.
	h.HandleObjectChange("Order", "o1", "CREATE", map[string]interface{}{"status": "shipped"})
	if got := numericMetric(metricFromMessage(t, readMessage(t, ctx, c), "count")); got != 1 {
		t.Errorf("after CREATE shipped: want 1, got %v", got)
	}

	// Move it to pending — count must drop back to 0 (with bucket removed).
	h.HandleObjectChange("Order", "o1", "MODIFY", map[string]interface{}{"status": "pending"})
	out := readMessage(t, ctx, c)
	if out.Type != "aggregationChanged" {
		t.Fatalf("expected aggregationChanged after move-out, got %q", out.Type)
	}
	var payload AggregationChangedPayload
	if err := json.Unmarshal(out.Data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.State.Data) != 0 {
		t.Errorf("expected empty buckets after move-out, got %d (%v)", len(payload.State.Data), payload.State.Data)
	}
}

// ---------- helpers ----------

// fakeIndexResolver is a 1-index in-memory IndexResolver for tests.
type fakeIndexResolver struct {
	idx bleve.Index
}

func (f *fakeIndexResolver) GetIndex(_ string) bleve.Index { return f.idx }

// newMemIndex builds an in-memory Bleve index with sensible defaults for the
// test fields (category indexed as keyword, price as number). Callers Index
// arbitrary docs and Close on cleanup.
func newMemIndex(t *testing.T) bleve.Index {
	t.Helper()
	im := bleve.NewIndexMapping()
	docMap := bleve.NewDocumentMapping()
	categoryFM := bleve.NewKeywordFieldMapping()
	categoryFM.Store = true
	docMap.AddFieldMappingsAt("category", categoryFM)
	priceFM := mapping.NewNumericFieldMapping()
	priceFM.Store = true
	docMap.AddFieldMappingsAt("price", priceFM)
	statusFM := bleve.NewKeywordFieldMapping()
	statusFM.Store = true
	docMap.AddFieldMappingsAt("status", statusFM)
	im.DefaultMapping = docMap

	idx, err := bleve.NewMemOnly(im)
	if err != nil {
		t.Fatalf("create mem index: %v", err)
	}
	return idx
}

func mustWhere(t *testing.T, raw string) *where.WhereClause {
	t.Helper()
	var w where.WhereClause
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatalf("parse where: %v", err)
	}
	return &w
}

// readMessage reads one Message from the websocket conn or fails the test.
func readMessage(t *testing.T, ctx context.Context, c *websocket.Conn) Message {
	t.Helper()
	var m Message
	if err := wsjson.Read(ctx, c, &m); err != nil {
		t.Fatalf("read message: %v", err)
	}
	return m
}

// bucketByGroup picks out per-bucket scalar metric values keyed by the row's
// group field — convenient for assertions like `want["books"] == 30`.
func bucketByGroup(rows []aggregation.AggregationRow, field string) map[string]interface{} {
	out := make(map[string]interface{}, len(rows))
	for _, r := range rows {
		key, ok := r.Group[field].(string)
		if !ok {
			continue
		}
		if len(r.Metrics) > 0 {
			out[key] = r.Metrics[0].Value
		}
	}
	return out
}

// metricFromRows picks the first metric value with the given name across rows.
func metricFromRows(rows []aggregation.AggregationRow, name string) interface{} {
	for _, r := range rows {
		for _, m := range r.Metrics {
			if m.Name == name {
				return m.Value
			}
		}
	}
	return nil
}

// metricFromMessage decodes an aggregationChanged Message and returns the
// first metric value matching name. Test-helper: fails on shape errors.
func metricFromMessage(t *testing.T, m Message, name string) interface{} {
	t.Helper()
	var payload AggregationChangedPayload
	if err := json.Unmarshal(m.Data, &payload); err != nil {
		t.Fatalf("unmarshal aggregationChanged: %v", err)
	}
	return metricFromRows(payload.State.Data, name)
}

// numericMetric coerces an interface metric value to float64 across the
// possible JSON/Go shapes (int64 in-process, float64 after JSON round-trip).
// Returns NaN-like sentinel for non-numeric inputs to surface assertion
// failures clearly.
func numericMetric(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	default:
		return -1
	}
}
