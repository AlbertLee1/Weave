package objectset_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// perPKLinkResolver is a test resolver that returns per-source-PK link targets.
// The baseline mock in objectset_test.go ignores sourcePKs and returns a flat
// list for any caller, which is not enough for withProperties count metrics
// where we need the target count per individual base object.
type perPKLinkResolver struct {
	// linkAPIName -> (source PK -> target PKs)
	edges map[string]map[string][]string
	// linkAPIName -> (target PK -> source PKs) for reverse traversal. Populated
	// so the resolver can satisfy reverseLinkFinder for US-003 withProperties
	// reverse direction tests. Nil map means reverse not wired.
	reverseEdges map[string]map[string][]string
	// linkAPIName -> target ObjectType API name. Populated so the resolver
	// can satisfy LinkTargetTypeResolver for withProperties sum/avg/min/max,
	// which must re-query the target ObjectType's index for numeric fields.
	targets map[string]string
}

func (m *perPKLinkResolver) ResolveLinked(ctx context.Context, linkTypeKey string, pks []string, dir links.Direction) ([]string, error) {
	if dir == links.DirectionReverse {
		edges := m.reverseEdges[linkTypeKey]
		var out []string
		for _, pk := range pks {
			out = append(out, edges[pk]...)
		}
		return out, nil
	}
	edges := m.edges[linkTypeKey]
	var out []string
	for _, pk := range pks {
		out = append(out, edges[pk]...)
	}
	return out, nil
}

func (m *perPKLinkResolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkTypeRID, sourcePKs, links.DirectionForward)
}

func (m *perPKLinkResolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceOTAPIName, linkAPIName string, sourcePKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkAPIName, sourcePKs, links.DirectionForward)
}

// ResolveLinkedReverseByAPIName satisfies the executor's reverseLinkFinder
// interface: for a caller-OT that sits on the *target* end of the link, return
// the PKs of the source-end objects pointing at each caller PK.
func (m *perPKLinkResolver) ResolveLinkedReverseByAPIName(ctx context.Context, callerOTAPIName, linkAPIName string, callerPKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkAPIName, callerPKs, links.DirectionReverse)
}

func (m *perPKLinkResolver) ResolveTargetObjectType(ctx context.Context, sourceObjectType, linkTypeAPIName string) (string, error) {
	if t, ok := m.targets[linkTypeAPIName]; ok {
		return t, nil
	}
	return "", nil
}

// setupCustomerOrderExecutor stages a Northwind-style "customer has orders"
// fixture with three customers having 3, 1 and 0 orders respectively. Orders
// additionally carry a numeric totalAmount and a non-numeric status so sum /
// avg / min / max and the type-mismatch code paths can all be exercised.
func setupCustomerOrderExecutor(t *testing.T) *objectset.Executor {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	customerProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("customer", customerProps); err != nil {
		t.Fatalf("EnsureIndex customer: %v", err)
	}
	customers := []struct{ id, name string }{
		{"c1", "Alice"},
		{"c2", "Bob"},
		{"c3", "Carol"},
	}
	for _, c := range customers {
		if err := mgr.IndexDocument("customer", c.id, map[string]interface{}{
			"id":   c.id,
			"name": c.name,
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", c.id, err)
		}
	}

	orderProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "totalAmount", BaseType: "double", IsSearchable: true},
		{APIName: "status", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("order", orderProps); err != nil {
		t.Fatalf("EnsureIndex order: %v", err)
	}
	orders := []struct {
		id     string
		amount float64
		status string
	}{
		{"o1", 100, "new"},
		{"o2", 200, "paid"},
		{"o3", 300, "paid"},
		{"o4", 500, "paid"},
	}
	for _, o := range orders {
		if err := mgr.IndexDocument("order", o.id, map[string]interface{}{
			"id":          o.id,
			"totalAmount": o.amount,
			"status":      o.status,
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", o.id, err)
		}
	}

	resolver := &perPKLinkResolver{
		edges: map[string]map[string][]string{
			"customerOrders": {
				"c1": {"o1", "o2", "o3"},
				"c2": {"o4"},
				// c3 deliberately absent — should resolve to zero orders.
			},
		},
		targets: map[string]string{
			"customerOrders": "order",
		},
	}

	store := objectset.NewStore(time.Hour)
	return objectset.NewExecutor(mgr, resolver, store)
}

// TestWithPropertiesCount asserts that a withProperties ObjectSet with a single
// count-metric derived property attaches per-base-object counts matching the
// number of forward-linked target objects (US-001).
func TestWithPropertiesCount(t *testing.T) {
	exec := setupCustomerOrderExecutor(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:      "orderCount",
				Link:      "customerOrders",
				Direction: "forward",
				Metric:    "count",
			},
		},
	}

	result, err := exec.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "customer" {
		t.Errorf("expected objectType customer, got %q", result.ObjectType)
	}
	if len(result.PrimaryKeys) != 3 {
		t.Fatalf("expected 3 customers, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
	if result.DerivedValues == nil {
		t.Fatalf("expected DerivedValues to be populated, got nil")
	}

	expected := map[string]int64{
		"c1": 3,
		"c2": 1,
		"c3": 0,
	}
	for pk, want := range expected {
		perPK, ok := result.DerivedValues[pk]
		if !ok {
			t.Errorf("derived values missing for %s", pk)
			continue
		}
		raw, ok := perPK["orderCount"]
		if !ok {
			t.Errorf("orderCount missing for %s", pk)
			continue
		}
		got, ok := raw.(int64)
		if !ok {
			t.Errorf("orderCount for %s: want int64, got %T (%v)", pk, raw, raw)
			continue
		}
		if got != want {
			t.Errorf("%s orderCount: got %d, want %d", pk, got, want)
		}
	}
}

// TestWithPropertiesCount_EmptyBaseSet verifies that withProperties returns an
// empty DerivedValues map (not nil) when the inner ObjectSet yields zero
// objects — handlers need to be able to range over it safely.
func TestWithPropertiesCount_EmptyBaseSet(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("customer", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	resolver := &perPKLinkResolver{
		edges:   map[string]map[string][]string{"customerOrders": {}},
		targets: map[string]string{"customerOrders": "order"},
	}
	store := objectset.NewStore(time.Hour)
	exec := objectset.NewExecutor(mgr, resolver, store)

	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "orderCount", Link: "customerOrders", Direction: "forward", Metric: "count"},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected 0 customers, got %d", len(result.PrimaryKeys))
	}
	if result.DerivedValues == nil {
		t.Errorf("expected non-nil DerivedValues map on empty base set")
	}
}

// withPropertiesNumericMetric drives the four numeric metric sub-cases off a
// single Northwind-shaped fixture. c1 has orders 100/200/300, c2 has 500, c3
// has none — so the empty-link branch is exercised on every metric.
func testWithPropertiesNumericMetric(t *testing.T, metric string, want map[string]interface{}) {
	t.Helper()
	exec := setupCustomerOrderExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:      "orderTotal",
				Link:      "customerOrders",
				Direction: "forward",
				Metric:    metric,
				Field:     "totalAmount",
			},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute %s: %v", metric, err)
	}
	if result.DerivedValues == nil {
		t.Fatalf("%s: expected DerivedValues to be populated", metric)
	}
	for pk, wantVal := range want {
		got := result.DerivedValues[pk]["orderTotal"]
		if wantVal == nil {
			if got != nil {
				t.Errorf("%s %s: want nil, got %T (%v)", metric, pk, got, got)
			}
			continue
		}
		wantFloat, ok := wantVal.(float64)
		if !ok {
			t.Fatalf("test bug: non-nil expectation must be float64, got %T", wantVal)
		}
		gotFloat, ok := got.(float64)
		if !ok {
			t.Errorf("%s %s: want float64, got %T (%v)", metric, pk, got, got)
			continue
		}
		if gotFloat != wantFloat {
			t.Errorf("%s %s: got %v, want %v", metric, pk, gotFloat, wantFloat)
		}
	}
}

// TestWithPropertiesSum — c1 = 600, c2 = 500, c3 = 0 (empty link set → 0).
func TestWithPropertiesSum(t *testing.T) {
	testWithPropertiesNumericMetric(t, "sum", map[string]interface{}{
		"c1": float64(600),
		"c2": float64(500),
		"c3": float64(0),
	})
}

// TestWithPropertiesAvg — uses float64 arithmetic; empty link set → nil.
func TestWithPropertiesAvg(t *testing.T) {
	testWithPropertiesNumericMetric(t, "avg", map[string]interface{}{
		"c1": float64(200),
		"c2": float64(500),
		"c3": nil,
	})
}

// TestWithPropertiesMin — empty link set → nil.
func TestWithPropertiesMin(t *testing.T) {
	testWithPropertiesNumericMetric(t, "min", map[string]interface{}{
		"c1": float64(100),
		"c2": float64(500),
		"c3": nil,
	})
}

// TestWithPropertiesMax — empty link set → nil.
func TestWithPropertiesMax(t *testing.T) {
	testWithPropertiesNumericMetric(t, "max", map[string]interface{}{
		"c1": float64(300),
		"c2": float64(500),
		"c3": nil,
	})
}

// TestWithPropertiesNumericTypeMismatch asserts that pointing sum at a
// non-numeric target field fails with a DerivedPropertyTypeMismatch error
// rather than silently producing garbage.
func TestWithPropertiesNumericTypeMismatch(t *testing.T) {
	exec := setupCustomerOrderExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:      "orderTotal",
				Link:      "customerOrders",
				Direction: "forward",
				Metric:    "sum",
				Field:     "status", // string, not numeric
			},
		},
	}
	_, err := exec.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected type-mismatch error for non-numeric field")
	}
	if !strings.Contains(err.Error(), "DerivedPropertyTypeMismatch") {
		t.Errorf("expected DerivedPropertyTypeMismatch, got %v", err)
	}
}

// TestWithPropertiesNumericMissingField asserts that omitting Field on a
// numeric metric is rejected at Validate time so callers cannot smuggle a
// half-specified derived property through the executor.
func TestWithPropertiesNumericMissingField(t *testing.T) {
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "orderTotal", Link: "customerOrders", Direction: "forward", Metric: "sum"},
		},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected validation error for missing field on numeric metric")
	}
}

// setupProductOrderLineExecutor stages a Northwind-shaped "product ← orderLine"
// fixture for US-003 reverse-direction testing: the link `orderLineProduct` is
// declared forward from OrderLine to Product (each orderLine points at one
// product), so walking it in reverse from a Product PK should yield the PKs of
// the orderLines that reference it.
//
//	p1 ← ol1, ol2, ol4   (3 orderLines)
//	p2 ← ol3             (1 orderLine)
//	p3 ← —               (0 orderLines)
func setupProductOrderLineExecutor(t *testing.T) *objectset.Executor {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	productProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("product", productProps); err != nil {
		t.Fatalf("EnsureIndex product: %v", err)
	}
	products := []struct{ id, name string }{
		{"p1", "Alpha"},
		{"p2", "Beta"},
		{"p3", "Gamma"},
	}
	for _, p := range products {
		if err := mgr.IndexDocument("product", p.id, map[string]interface{}{
			"id":   p.id,
			"name": p.name,
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", p.id, err)
		}
	}

	olProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "productId", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("orderLine", olProps); err != nil {
		t.Fatalf("EnsureIndex orderLine: %v", err)
	}
	orderLines := []struct{ id, productID string }{
		{"ol1", "p1"},
		{"ol2", "p1"},
		{"ol3", "p2"},
		{"ol4", "p1"},
	}
	for _, ol := range orderLines {
		if err := mgr.IndexDocument("orderLine", ol.id, map[string]interface{}{
			"id":        ol.id,
			"productId": ol.productID,
		}); err != nil {
			t.Fatalf("IndexDocument %s: %v", ol.id, err)
		}
	}

	resolver := &perPKLinkResolver{
		edges: map[string]map[string][]string{
			"orderLineProduct": {
				"ol1": {"p1"},
				"ol2": {"p1"},
				"ol3": {"p2"},
				"ol4": {"p1"},
			},
		},
		reverseEdges: map[string]map[string][]string{
			"orderLineProduct": {
				"p1": {"ol1", "ol2", "ol4"},
				"p2": {"ol3"},
			},
		},
		targets: map[string]string{
			"orderLineProduct": "product",
		},
	}

	store := objectset.NewStore(time.Hour)
	return objectset.NewExecutor(mgr, resolver, store)
}

// TestWithPropertiesReverseCount asserts US-003: a withProperties definition
// with Direction="reverse" counts source-end objects pointing at each base PK
// via the reverseLinkFinder path instead of the forward ResolveLinked helper.
func TestWithPropertiesReverseCount(t *testing.T) {
	exec := setupProductOrderLineExecutor(t)
	ctx := context.Background()

	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "product"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:      "totalOrders",
				Link:      "orderLineProduct",
				Direction: "reverse",
				Metric:    "count",
			},
		},
	}

	result, err := exec.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 3 {
		t.Fatalf("expected 3 products, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
	if result.DerivedValues == nil {
		t.Fatalf("expected DerivedValues to be populated")
	}

	expected := map[string]int64{
		"p1": 3,
		"p2": 1,
		"p3": 0,
	}
	for pk, want := range expected {
		raw, ok := result.DerivedValues[pk]["totalOrders"]
		if !ok {
			t.Errorf("totalOrders missing for %s", pk)
			continue
		}
		got, ok := raw.(int64)
		if !ok {
			t.Errorf("%s totalOrders: want int64, got %T (%v)", pk, raw, raw)
			continue
		}
		if got != want {
			t.Errorf("%s totalOrders: got %d, want %d", pk, got, want)
		}
	}
}

// TestWithPropertiesReverseRequiresResolver asserts that when the underlying
// resolver does not implement reverseLinkFinder, a reverse-direction
// withProperties fails with a clear error instead of silently returning zeros.
func TestWithPropertiesReverseRequiresResolver(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("product", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("product", "p1", map[string]interface{}{"id": "p1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	exec := objectset.NewExecutor(mgr, &forwardOnlyResolver{}, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "product"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "totalOrders", Link: "orderLineProduct", Direction: "reverse", Metric: "count"},
		},
	}
	_, err := exec.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected error when resolver does not support reverse direction")
	}
	if !strings.Contains(err.Error(), "reverse") {
		t.Errorf("expected reverse-support error, got %v", err)
	}
}

// forwardOnlyResolver satisfies links.LinkResolver but deliberately omits
// ResolveLinkedReverseByAPIName so the executor's reverse path must bail out.
type forwardOnlyResolver struct{}

func (forwardOnlyResolver) ResolveLinked(ctx context.Context, linkTypeKey string, pks []string, dir links.Direction) ([]string, error) {
	return nil, nil
}
func (forwardOnlyResolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return nil, nil
}
func (forwardOnlyResolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceOT, linkAPIName string, sourcePKs []string) ([]string, error) {
	return nil, nil
}

// TestWithPropertiesCursorStability asserts US-005: when a withProperties
// ObjectSet is executed, the returned PrimaryKeys are sorted by
// (firstDerivedValue ASC, primaryKey ASC) so that offset-based pagination can
// slice the result into pages without ever losing or duplicating a base
// object, even when many base objects share the same derived value.
//
// Fixture: 50 customers with orderCount determined by id % 5 (so 10 customers
// share each of the 5 distinct counts). Pagination with pageSize=17 should
// walk the full set across 3 pages with no duplicates and no gaps, and each
// page's rows must respect the composite sort order.
func TestWithPropertiesCursorStability(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	custProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("customer", custProps); err != nil {
		t.Fatalf("EnsureIndex customer: %v", err)
	}
	orderProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("order", orderProps); err != nil {
		t.Fatalf("EnsureIndex order: %v", err)
	}

	edges := map[string][]string{}
	for i := 0; i < 50; i++ {
		cid := fmt.Sprintf("c%03d", i)
		if err := mgr.IndexDocument("customer", cid, map[string]interface{}{"id": cid}); err != nil {
			t.Fatalf("IndexDocument customer %s: %v", cid, err)
		}
		// orderCount bucket = i % 5 → ten customers per count (0..4) guarantees
		// large tie groups so the pk tiebreaker is actually exercised.
		count := i % 5
		orders := make([]string, 0, count)
		for j := 0; j < count; j++ {
			oid := fmt.Sprintf("o-%s-%d", cid, j)
			if err := mgr.IndexDocument("order", oid, map[string]interface{}{"id": oid}); err != nil {
				t.Fatalf("IndexDocument order %s: %v", oid, err)
			}
			orders = append(orders, oid)
		}
		edges[cid] = orders
	}

	resolver := &perPKLinkResolver{
		edges:   map[string]map[string][]string{"customerOrders": edges},
		targets: map[string]string{"customerOrders": "order"},
	}
	store := objectset.NewStore(time.Hour)
	exec := objectset.NewExecutor(mgr, resolver, store)

	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "orderCount", Link: "customerOrders", Direction: "forward", Metric: "count"},
		},
	}

	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 50 {
		t.Fatalf("expected 50 customers, got %d", len(result.PrimaryKeys))
	}

	// Assert composite sort order: non-decreasing (orderCount, pk).
	for i := 1; i < len(result.PrimaryKeys); i++ {
		prev := result.PrimaryKeys[i-1]
		cur := result.PrimaryKeys[i]
		prevV, ok := result.DerivedValues[prev]["orderCount"].(int64)
		if !ok {
			t.Fatalf("derived orderCount for %s missing or not int64", prev)
		}
		curV, ok := result.DerivedValues[cur]["orderCount"].(int64)
		if !ok {
			t.Fatalf("derived orderCount for %s missing or not int64", cur)
		}
		if curV < prevV {
			t.Errorf("not sorted by derivedValue at index %d: %s(%d) then %s(%d)", i, prev, prevV, cur, curV)
			continue
		}
		if curV == prevV && cur < prev {
			t.Errorf("pk tiebreaker violated at index %d: %s then %s (both count=%d)", i, prev, cur, curV)
		}
	}

	// Simulate offset-based pagination over 3 pages × 17 + overflow.
	pageSize := 17
	seen := make(map[string]bool, 50)
	totalPages := 0
	for start := 0; start < len(result.PrimaryKeys); start += pageSize {
		end := start + pageSize
		if end > len(result.PrimaryKeys) {
			end = len(result.PrimaryKeys)
		}
		for _, pk := range result.PrimaryKeys[start:end] {
			if seen[pk] {
				t.Errorf("duplicate %s across pages", pk)
			}
			seen[pk] = true
		}
		totalPages++
	}
	if len(seen) != 50 {
		t.Errorf("expected 50 unique customers across all pages, got %d", len(seen))
	}
	if totalPages != 3 {
		t.Errorf("expected 3 pages (17+17+16), got %d", totalPages)
	}
}

// TestWithPropertiesCount_ValidationMissingMetric asserts that a derived
// property definition missing the metric field is rejected by Validate so
// callers can't smuggle empty computations through the executor.
func TestWithPropertiesCount_ValidationMissingMetric(t *testing.T) {
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "orderCount", Link: "customerOrders", Direction: "forward"},
		},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected validation error for missing metric")
	}
}
