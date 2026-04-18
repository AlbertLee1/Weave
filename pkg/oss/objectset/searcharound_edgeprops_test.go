package objectset_test

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// US-210: the executor's searchAround path surfaces per-edge properties when
// an EdgePropertiesProvider is attached. The map is keyed by the "other end"
// PK — target PK for forward direction, source PK for reverse.

type stubEdgePropsProvider struct {
	byLink map[string]map[string]map[string]interface{} // linkAPIName -> pk -> props
	lastDir links.Direction
	calls  int
}

func (s *stubEdgePropsProvider) ResolveEdgeProperties(_ context.Context, _, linkAPIName string, _ []string, dir links.Direction) (map[string]map[string]interface{}, error) {
	s.lastDir = dir
	s.calls++
	return s.byLink[linkAPIName], nil
}

func TestExecute_SearchAround_SurfacesEdgeProperties(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("user", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("user", "u1", map[string]interface{}{"id": "u1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := &mockLinkResolver{
		results: map[string][]string{"membership": {"g1", "g2"}},
	}
	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, resolver, store)

	provider := &stubEdgePropsProvider{
		byLink: map[string]map[string]map[string]interface{}{
			"membership": {
				"g1": {"role": "admin"},
				"g2": {"role": "member"},
			},
		},
	}
	executor.SetEdgePropertiesProvider(provider)

	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "user"},
		Link:      "membership",
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 2 {
		t.Fatalf("expected 2 PKs, got %v", result.PrimaryKeys)
	}
	if result.EdgeProperties == nil {
		t.Fatalf("expected EdgeProperties populated, got nil")
	}
	if result.EdgeProperties["g1"]["role"] != "admin" {
		t.Errorf("g1.role: got %v", result.EdgeProperties["g1"])
	}
	if result.EdgeProperties["g2"]["role"] != "member" {
		t.Errorf("g2.role: got %v", result.EdgeProperties["g2"])
	}
	if provider.lastDir != links.DirectionForward {
		t.Errorf("expected forward direction, got %v", provider.lastDir)
	}
}

func TestExecute_SearchAround_NoProvider_NoEdgeProperties(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Link:      "employeeDept",
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.EdgeProperties != nil {
		t.Errorf("expected nil EdgeProperties when no provider is wired, got %v", result.EdgeProperties)
	}
}

func TestExecute_SearchAround_ProviderSkippedWhenEmptyInnerSet(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	if _, err := mgr.EnsureIndex("user", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	// Intentionally seed nothing — inner base set is empty.

	resolver := &mockLinkResolver{results: map[string][]string{"membership": {}}}
	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, resolver, store)
	provider := &stubEdgePropsProvider{}
	executor.SetEdgePropertiesProvider(provider)

	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "user"},
		Link:      "membership",
	}
	if _, err := executor.Execute(context.Background(), def); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if provider.calls != 0 {
		t.Errorf("expected provider to be skipped when inner set is empty, got %d calls", provider.calls)
	}
}
