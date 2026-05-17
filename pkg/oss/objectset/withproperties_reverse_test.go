package objectset_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// reverseCustomerOrderResolver is a directional resolver that mirrors a
// Northwind-shaped "Order -> Customer" link declared forward (each Order
// points at one Customer). Walking the link in reverse from a Customer PK
// yields the Order PKs that reference it. Implements
// DirectionalLinkTargetTypeResolver so the executor can ask for the
// per-direction "other end" ObjectType (forward target = "customer",
// reverse target = "order") when running numeric aggregations.
type reverseCustomerOrderResolver struct {
	// forwardEdges: orderPK -> []customerPK (each order points at exactly one
	// customer in this fixture, but the slice keeps the resolver shape uniform
	// with the existing perPKLinkResolver).
	forwardEdges map[string]map[string][]string
	// reverseEdges: customerPK -> []orderPK
	reverseEdges map[string]map[string][]string
	// targetsByDir: linkAPIName + "|" + dir -> targetObjectTypeAPIName.
	targetsByDir map[string]string
}

func (m *reverseCustomerOrderResolver) ResolveLinked(ctx context.Context, linkTypeKey string, pks []string, dir links.Direction) ([]string, error) {
	if dir == links.DirectionReverse {
		edges := m.reverseEdges[linkTypeKey]
		var out []string
		for _, pk := range pks {
			out = append(out, edges[pk]...)
		}
		return out, nil
	}
	edges := m.forwardEdges[linkTypeKey]
	var out []string
	for _, pk := range pks {
		out = append(out, edges[pk]...)
	}
	return out, nil
}

func (m *reverseCustomerOrderResolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkTypeRID, sourcePKs, links.DirectionForward)
}

func (m *reverseCustomerOrderResolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceOTAPIName, linkAPIName string, sourcePKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkAPIName, sourcePKs, links.DirectionForward)
}

func (m *reverseCustomerOrderResolver) ResolveLinkedReverseByAPIName(ctx context.Context, callerOTAPIName, linkAPIName string, callerPKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkAPIName, callerPKs, links.DirectionReverse)
}

func (m *reverseCustomerOrderResolver) ResolveTargetObjectType(ctx context.Context, sourceObjectType, linkTypeAPIName string) (string, error) {
	return m.targetsByDir[linkTypeAPIName+"|forward"], nil
}

func (m *reverseCustomerOrderResolver) ResolveTargetObjectTypeDir(ctx context.Context, callerObjectType, linkTypeAPIName string, dir links.Direction) (string, error) {
	return m.targetsByDir[linkTypeAPIName+"|"+dir.String()], nil
}

// setupReverseCustomerOrderExecutor stages a Northwind-shaped fixture where
// the link `orderCustomer` is declared forward from Order -> Customer:
//
//	o1, o2, o3 -> c1   (sums to 600)
//	o4         -> c2   (sums to 500)
//	c3         -> none (no incoming orders)
//
// Walking the link in reverse from a Customer PK should aggregate the
// matching Orders' totalAmount field — i.e. the canonical "每个 Customer 的
// 累计订单金额（反向）" case from PRD US-460.
func setupReverseCustomerOrderExecutor(t *testing.T) *objectset.Executor {
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
	for _, c := range []struct{ id, name string }{
		{"c1", "Alice"}, {"c2", "Bob"}, {"c3", "Carol"},
	} {
		if err := mgr.IndexDocument("customer", c.id, map[string]interface{}{
			"id":   c.id,
			"name": c.name,
		}); err != nil {
			t.Fatalf("IndexDocument customer %s: %v", c.id, err)
		}
	}

	orderProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "customerId", BaseType: "string", IsSearchable: true},
		{APIName: "totalAmount", BaseType: "double", IsSearchable: true},
		{APIName: "status", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("order", orderProps); err != nil {
		t.Fatalf("EnsureIndex order: %v", err)
	}
	orders := []struct {
		id, customer, status string
		amount               float64
	}{
		{"o1", "c1", "paid", 100},
		{"o2", "c1", "paid", 200},
		{"o3", "c1", "new", 300},
		{"o4", "c2", "paid", 500},
	}
	for _, o := range orders {
		if err := mgr.IndexDocument("order", o.id, map[string]interface{}{
			"id":          o.id,
			"customerId":  o.customer,
			"totalAmount": o.amount,
			"status":      o.status,
		}); err != nil {
			t.Fatalf("IndexDocument order %s: %v", o.id, err)
		}
	}

	resolver := &reverseCustomerOrderResolver{
		forwardEdges: map[string]map[string][]string{
			"orderCustomer": {
				"o1": {"c1"}, "o2": {"c1"}, "o3": {"c1"}, "o4": {"c2"},
			},
		},
		reverseEdges: map[string]map[string][]string{
			"orderCustomer": {
				"c1": {"o1", "o2", "o3"},
				"c2": {"o4"},
				// c3 deliberately absent — reverse walk yields zero orders.
			},
		},
		targetsByDir: map[string]string{
			// Forward: order -> customer. Reverse: customer -> order.
			"orderCustomer|forward": "customer",
			"orderCustomer|reverse": "order",
		},
	}
	store := objectset.NewStore(time.Hour)
	return objectset.NewExecutor(mgr, resolver, store)
}

// testWithPropertiesReverseNumericMetric drives the four numeric metrics
// (sum/avg/min/max) off the reverse Customer-Order fixture and asserts each
// customer's derived value matches the Northwind expectation.
func testWithPropertiesReverseNumericMetric(t *testing.T, metric string, want map[string]interface{}) {
	t.Helper()
	exec := setupReverseCustomerOrderExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:      "orderTotal",
				Link:      "orderCustomer",
				Direction: "reverse",
				Metric:    metric,
				Field:     "totalAmount",
			},
		},
	}
	result, err := exec.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute %s reverse: %v", metric, err)
	}
	if len(result.PrimaryKeys) != 3 {
		t.Fatalf("expected 3 customers, got %d: %v", len(result.PrimaryKeys), result.PrimaryKeys)
	}
	if result.DerivedValues == nil {
		t.Fatalf("%s reverse: expected DerivedValues to be populated", metric)
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

// TestWithPropertiesReverseSum is the canonical PRD acceptance criterion:
// "每个 Customer 的累计订单金额（反向）正确". c1 = 600, c2 = 500, c3 = 0.
func TestWithPropertiesReverseSum(t *testing.T) {
	testWithPropertiesReverseNumericMetric(t, "sum", map[string]interface{}{
		"c1": float64(600),
		"c2": float64(500),
		"c3": float64(0),
	})
}

// TestWithPropertiesReverseAvg — empty link set surfaces nil so the UI can
// distinguish "no rows" from "average of zero". c1 = 200, c2 = 500.
func TestWithPropertiesReverseAvg(t *testing.T) {
	testWithPropertiesReverseNumericMetric(t, "avg", map[string]interface{}{
		"c1": float64(200),
		"c2": float64(500),
		"c3": nil,
	})
}

// TestWithPropertiesReverseMin asserts min works over the reverse-walked
// orders' totalAmount; c1 = 100, c2 = 500, c3 = nil.
func TestWithPropertiesReverseMin(t *testing.T) {
	testWithPropertiesReverseNumericMetric(t, "min", map[string]interface{}{
		"c1": float64(100),
		"c2": float64(500),
		"c3": nil,
	})
}

// TestWithPropertiesReverseMax asserts max works over the reverse-walked
// orders' totalAmount; c1 = 300, c2 = 500, c3 = nil.
func TestWithPropertiesReverseMax(t *testing.T) {
	testWithPropertiesReverseNumericMetric(t, "max", map[string]interface{}{
		"c1": float64(300),
		"c2": float64(500),
		"c3": nil,
	})
}

// TestWithPropertiesReverseNumericTypeMismatch asserts that aiming a numeric
// metric at a non-numeric field via reverse direction still surfaces the
// existing DerivedPropertyTypeMismatch error path — i.e. the reverse path
// reads the correct "source" ObjectType's index, not the caller's.
func TestWithPropertiesReverseNumericTypeMismatch(t *testing.T) {
	exec := setupReverseCustomerOrderExecutor(t)
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{
				Name:      "orderTotal",
				Link:      "orderCustomer",
				Direction: "reverse",
				Metric:    "sum",
				Field:     "status", // string, not numeric
			},
		},
	}
	_, err := exec.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected type-mismatch error for non-numeric reverse field")
	}
	if !strings.Contains(err.Error(), "DerivedPropertyTypeMismatch") {
		t.Errorf("expected DerivedPropertyTypeMismatch, got %v", err)
	}
}

// TestWithPropertiesReverseNumericRequiresResolver asserts that when the
// underlying resolver does not implement reverseLinkFinder, a reverse-direction
// numeric metric still surfaces a clear "reverse" error (matching the
// pre-existing count-direction behaviour) instead of silently summing zeros.
func TestWithPropertiesReverseNumericRequiresResolver(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("customer", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("customer", "c1", map[string]interface{}{"id": "c1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	exec := objectset.NewExecutor(mgr, &forwardOnlyResolver{}, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "withProperties",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "customer"},
		DerivedProperties: []objectset.DerivedPropertyDef{
			{Name: "orderTotal", Link: "orderCustomer", Direction: "reverse", Metric: "sum", Field: "totalAmount"},
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
