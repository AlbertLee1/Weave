package objectset_test

import (
	"context"
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
}

func (m *perPKLinkResolver) ResolveLinked(ctx context.Context, linkTypeKey string, pks []string, dir links.Direction) ([]string, error) {
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

// setupCustomerOrderExecutor stages a Northwind-style "customer has orders"
// fixture with three customers having 3, 1 and 0 orders respectively.
func setupCustomerOrderExecutor(t *testing.T) *objectset.Executor {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("customer", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
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

	resolver := &perPKLinkResolver{
		edges: map[string]map[string][]string{
			"customerOrders": {
				"c1": {"o1", "o2", "o3"},
				"c2": {"o4"},
				// c3 deliberately absent — should resolve to zero orders.
			},
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
	resolver := &perPKLinkResolver{edges: map[string]map[string][]string{"customerOrders": {}}}
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
