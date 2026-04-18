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

// multiHopResolver is a deterministic LinkResolver that remembers
// per-(sourceType, linkAPIName) fanout for forward traversal and per-link
// target ObjectTypes. It also records calls so tests can assert the order
// and payloads of each hop.
type multiHopResolver struct {
	// forward[sourceOT + "|" + linkAPIName][sourcePK] = targets
	forward map[string]map[string][]string
	// reverse[targetOT + "|" + linkAPIName][targetPK] = sources
	reverse map[string]map[string][]string
	// targetType[sourceOT + "|" + linkAPIName] = targetOT (forward)
	// for reverse the "target" of a reverse walk is the declared source, so
	// keyed by callerOT (the target in forward terms).
	targetType map[string]string

	calls []struct {
		SourceOT string
		Link     string
		PKs      []string
		Dir      string
	}
}

func newMultiHopResolver() *multiHopResolver {
	return &multiHopResolver{
		forward:    map[string]map[string][]string{},
		reverse:    map[string]map[string][]string{},
		targetType: map[string]string{},
	}
}

func (m *multiHopResolver) addForward(sourceOT, link, targetOT string, edges map[string][]string) {
	m.forward[sourceOT+"|"+link] = edges
	m.targetType[sourceOT+"|"+link] = targetOT
}

func (m *multiHopResolver) addReverse(targetOT, link, sourceOT string, edges map[string][]string) {
	m.reverse[targetOT+"|"+link] = edges
	m.targetType[targetOT+"|"+link+"|reverse"] = sourceOT
}

func (m *multiHopResolver) ResolveLinkedObjectsByAPIName(_ context.Context, sourceOT, link string, sourcePKs []string) ([]string, error) {
	m.calls = append(m.calls, struct {
		SourceOT string
		Link     string
		PKs      []string
		Dir      string
	}{sourceOT, link, append([]string(nil), sourcePKs...), "forward"})
	edges, ok := m.forward[sourceOT+"|"+link]
	if !ok {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, pk := range sourcePKs {
		for _, t := range edges[pk] {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func (m *multiHopResolver) ResolveLinkedReverseByAPIName(_ context.Context, callerOT, link string, callerPKs []string) ([]string, error) {
	m.calls = append(m.calls, struct {
		SourceOT string
		Link     string
		PKs      []string
		Dir      string
	}{callerOT, link, append([]string(nil), callerPKs...), "reverse"})
	edges, ok := m.reverse[callerOT+"|"+link]
	if !ok {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, pk := range callerPKs {
		for _, s := range edges[pk] {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (m *multiHopResolver) ResolveLinked(_ context.Context, _ string, _ []string, _ links.Direction) ([]string, error) {
	return nil, nil
}

func (m *multiHopResolver) ResolveLinkedObjects(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}

func (m *multiHopResolver) ResolveTargetObjectType(_ context.Context, sourceOT, link string) (string, error) {
	return m.targetType[sourceOT+"|"+link], nil
}

func (m *multiHopResolver) ResolveTargetObjectTypeDir(_ context.Context, callerOT, link string, dir links.Direction) (string, error) {
	if dir == links.DirectionReverse {
		return m.targetType[callerOT+"|"+link+"|reverse"], nil
	}
	return m.targetType[callerOT+"|"+link], nil
}

func seedMultiHopIndex(t *testing.T, ots []string) *index.Manager {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
	}
	for _, ot := range ots {
		if _, err := mgr.EnsureIndex(ot, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}
	return mgr
}

// TestExecute_SearchAround_Path_ThreeHops exercises a 3-hop forward traversal
// employee -> department -> building -> city and asserts the resolver receives
// each intermediate sourceType / PKs exactly once per hop.
func TestExecute_SearchAround_Path_ThreeHops(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"employee", "department", "building", "city"})
	// Seed the employee index so executeBase has something to walk from.
	for _, pk := range []string{"e1", "e2", "e3"} {
		if err := mgr.IndexDocument("employee", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument %s: %v", pk, err)
		}
	}

	resolver := newMultiHopResolver()
	resolver.addForward("employee", "worksInDept", "department", map[string][]string{
		"e1": {"d1"},
		"e2": {"d1", "d2"},
		"e3": {"d3"},
	})
	resolver.addForward("department", "housedInBuilding", "building", map[string][]string{
		"d1": {"b1"},
		"d2": {"b2"},
		"d3": {"b3"},
	})
	resolver.addForward("building", "locatedInCity", "city", map[string][]string{
		"b1": {"c1"},
		"b2": {"c1"},
		"b3": {"c2"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(1*time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Path: []objectset.PathStep{
			{Link: "worksInDept"},
			{Link: "housedInBuilding"},
			{Link: "locatedInCity"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "city" {
		t.Errorf("ObjectType: want city, got %q", result.ObjectType)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"c1", "c2"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("PKs: want %v, got %v", want, got)
	}

	// Verify each hop fed the right sourceType + deduped PKs forward.
	if len(resolver.calls) != 3 {
		t.Fatalf("expected 3 hops, got %d: %+v", len(resolver.calls), resolver.calls)
	}
	if resolver.calls[0].SourceOT != "employee" || resolver.calls[0].Link != "worksInDept" {
		t.Errorf("hop 0: got %+v", resolver.calls[0])
	}
	if resolver.calls[1].SourceOT != "department" || resolver.calls[1].Link != "housedInBuilding" {
		t.Errorf("hop 1: got %+v", resolver.calls[1])
	}
	// Hop 1 must see deduped department PKs (d1, d2, d3) even though e1+e2 both hit d1.
	if strings.Join(sorted(resolver.calls[1].PKs), ",") != "d1,d2,d3" {
		t.Errorf("hop 1 PKs: got %v", resolver.calls[1].PKs)
	}
	if resolver.calls[2].SourceOT != "building" || resolver.calls[2].Link != "locatedInCity" {
		t.Errorf("hop 2: got %+v", resolver.calls[2])
	}
}

// TestExecute_SearchAround_Path_MixedDirection checks a reverse step in the
// middle of the path: base employee -> dept (forward) -> back to employee
// (reverse). The result should be the original employees plus peers that
// share the same dept.
func TestExecute_SearchAround_Path_MixedDirection(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"employee", "department"})
	for _, pk := range []string{"e1"} {
		if err := mgr.IndexDocument("employee", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}

	resolver := newMultiHopResolver()
	resolver.addForward("employee", "worksInDept", "department", map[string][]string{
		"e1": {"d1"},
	})
	// Reverse walk of worksInDept from department d1 yields e1, e2, e3.
	resolver.addReverse("department", "worksInDept", "employee", map[string][]string{
		"d1": {"e1", "e2", "e3"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(1*time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Path: []objectset.PathStep{
			{Link: "worksInDept"},
			{Link: "worksInDept", Direction: "reverse"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "employee" {
		t.Errorf("ObjectType: want employee, got %q", result.ObjectType)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"e1", "e2", "e3"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("PKs: want %v, got %v", want, got)
	}
}

// TestExecute_SearchAround_Path_IntermediateTypeMismatch asserts that when
// a PathStep declares expectedObjectType, the executor validates it against
// the resolver's target-type lookup and rejects mismatches.
func TestExecute_SearchAround_Path_IntermediateTypeMismatch(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"employee"})
	if err := mgr.IndexDocument("employee", "e1", map[string]interface{}{"id": "e1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := newMultiHopResolver()
	resolver.addForward("employee", "worksInDept", "department", map[string][]string{
		"e1": {"d1"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(1*time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Path: []objectset.PathStep{
			{Link: "worksInDept", ExpectedObjectType: "building"},
		},
	}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "expectedObjectType") {
		t.Errorf("error should mention expectedObjectType, got: %v", err)
	}
}

// TestDefinition_ValidateSearchAround_PathAndLinkMutex asserts that Path and
// legacy Link cannot both be set on the same definition.
func TestDefinition_ValidateSearchAround_PathAndLinkMutex(t *testing.T) {
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Link:      "worksInDept",
		Path: []objectset.PathStep{
			{Link: "worksInDept"},
		},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error when both link and path set, got nil")
	}
}

// TestDefinition_ValidateSearchAround_PathStepMissingLink asserts that every
// step requires a non-empty link API name.
func TestDefinition_ValidateSearchAround_PathStepMissingLink(t *testing.T) {
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Path: []objectset.PathStep{
			{Link: "worksInDept"},
			{Link: ""},
		},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error when path step has empty link, got nil")
	}
}

// TestDefinition_ValidateSearchAround_PathNeitherLinkNorPath asserts that
// at least one of link / path must be supplied.
func TestDefinition_ValidateSearchAround_PathNeitherLinkNorPath(t *testing.T) {
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error when neither link nor path set, got nil")
	}
}
