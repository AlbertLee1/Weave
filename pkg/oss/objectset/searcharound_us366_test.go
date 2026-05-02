package objectset_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestExecute_SearchAround_Path_CycleDetection_DirectLoop asserts that an
// A→B→A path does not re-emit the original source PKs. The visited set is
// seeded with the inner ObjectSet so hop 2's reverse traversal back to A
// must prune them entirely.
func TestExecute_SearchAround_Path_CycleDetection_DirectLoop(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"a", "b"})
	for _, pk := range []string{"a1", "a2"} {
		if err := mgr.IndexDocument("a", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}

	resolver := newMultiHopResolver()
	resolver.addForward("a", "ab", "b", map[string][]string{
		"a1": {"b1"},
		"a2": {"b1", "b2"},
	})
	// Reverse walk from b back to a returns the original sources plus a3 — a
	// peer that should remain in the result because it was not in the seed.
	resolver.addReverse("b", "ab", "a", map[string][]string{
		"b1": {"a1", "a2", "a3"},
		"b2": {"a2"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "a"},
		Path: []objectset.PathStep{
			{Link: "ab"},
			{Link: "ab", Direction: "reverse"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "a" {
		t.Errorf("ObjectType: want a, got %q", result.ObjectType)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"a3"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("PKs after cycle prune: want %v, got %v (a1, a2 are seeds and must be pruned)", want, got)
	}
}

// TestExecute_SearchAround_Path_CycleDetection_Triangle exercises a longer
// loop A→B→C→A. The third hop lands on the original source, which the
// visited set must prune so the result is empty rather than re-walking A.
func TestExecute_SearchAround_Path_CycleDetection_Triangle(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"a", "b", "c"})
	if err := mgr.IndexDocument("a", "a1", map[string]interface{}{"id": "a1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := newMultiHopResolver()
	resolver.addForward("a", "ab", "b", map[string][]string{"a1": {"b1"}})
	resolver.addForward("b", "bc", "c", map[string][]string{"b1": {"c1"}})
	resolver.addForward("c", "ca", "a", map[string][]string{"c1": {"a1"}})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "a"},
		Path: []objectset.PathStep{
			{Link: "ab"},
			{Link: "bc"},
			{Link: "ca"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "a" {
		t.Errorf("ObjectType: want a, got %q", result.ObjectType)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("triangle cycle should yield empty result, got %v", result.PrimaryKeys)
	}
}

// TestExecute_SearchAround_Path_CycleDetection_DoesNotPruneFreshNodes makes
// sure cycle pruning only fires for actually-visited (objectType, pk) pairs.
// A path that re-visits the *same* ObjectType but with a different PK must
// not be incorrectly pruned.
func TestExecute_SearchAround_Path_CycleDetection_DoesNotPruneFreshNodes(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"a", "b"})
	if err := mgr.IndexDocument("a", "a1", map[string]interface{}{"id": "a1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := newMultiHopResolver()
	resolver.addForward("a", "ab", "b", map[string][]string{"a1": {"b1"}})
	// Reverse walk yields a fresh PK a2 that has never been visited at "a".
	resolver.addReverse("b", "ab", "a", map[string][]string{
		"b1": {"a2"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "a"},
		Path: []objectset.PathStep{
			{Link: "ab"},
			{Link: "ab", Direction: "reverse"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fmt.Sprintf("%v", result.PrimaryKeys) != fmt.Sprintf("%v", []string{"a2"}) {
		t.Errorf("fresh node a2 must survive cycle prune, got %v", result.PrimaryKeys)
	}
}

// TestExecute_SearchAround_Path_TooLarge_AbortsHop asserts that an
// intermediate working set above SearchAroundIntermediateCap aborts the
// walk with the typed ErrQueryTooLarge sentinel.
func TestExecute_SearchAround_Path_TooLarge_AbortsHop(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"a", "b"})
	if err := mgr.IndexDocument("a", "a1", map[string]interface{}{"id": "a1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// Resolver returns one more PK than the cap to force the guard.
	overflow := objectset.SearchAroundIntermediateCap + 1
	bigEdges := make([]string, overflow)
	for i := 0; i < overflow; i++ {
		bigEdges[i] = fmt.Sprintf("b%d", i)
	}
	resolver := newMultiHopResolver()
	resolver.addForward("a", "ab", "b", map[string][]string{"a1": bigEdges})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "a"},
		Path: []objectset.PathStep{
			{Link: "ab"},
		},
	}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected ErrQueryTooLarge, got nil")
	}
	if !errors.Is(err, objectset.ErrQueryTooLarge) {
		t.Fatalf("expected ErrQueryTooLarge, got: %v", err)
	}
	if !strings.Contains(err.Error(), "WEAVE_QUERY_TOO_LARGE") {
		t.Errorf("error message should embed WEAVE_QUERY_TOO_LARGE marker, got: %v", err)
	}
}

// BenchmarkExecute_SearchAround_Path_ThreeHops_10K builds a 3-hop forward
// chain rooted in a 10K-PK static ObjectSet and measures one Execute call.
// The PRD threshold is "northwind 3 跳 < 200ms（10K 对象）"; the inner set
// is "static" rather than "base" so the bench isolates the path traversal
// (cycle prune + dedup + fanout) without Bleve query overhead.
func BenchmarkExecute_SearchAround_Path_ThreeHops_10K(b *testing.B) {
	const n = 10000
	mgr := seedMultiHopIndex(b, []string{"e", "d", "p", "c"})

	resolver := newMultiHopResolver()
	hop1 := make(map[string][]string, n)
	hop2 := make(map[string][]string, 200)
	hop3 := make(map[string][]string, 50)
	seedPKs := make([]string, n)
	for i := 0; i < n; i++ {
		pk := fmt.Sprintf("e%d", i)
		seedPKs[i] = pk
		// Each employee → one department, fanning into a smaller pool so the
		// dedup path actually fires between hops.
		hop1[pk] = []string{fmt.Sprintf("d%d", i%200)}
	}
	for i := 0; i < 200; i++ {
		hop2[fmt.Sprintf("d%d", i)] = []string{fmt.Sprintf("p%d", i%50)}
	}
	for i := 0; i < 50; i++ {
		hop3[fmt.Sprintf("p%d", i)] = []string{fmt.Sprintf("c%d", i%5)}
	}
	resolver.addForward("e", "worksInDept", "d", hop1)
	resolver.addForward("d", "housedInBuilding", "p", hop2)
	resolver.addForward("p", "locatedInCity", "c", hop3)

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type: "searchAround",
		ObjectSet: &objectset.Definition{
			Type:        "static",
			ObjectType:  "e",
			PrimaryKeys: seedPKs,
		},
		Path: []objectset.PathStep{
			{Link: "worksInDept"},
			{Link: "housedInBuilding"},
			{Link: "locatedInCity"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset the resolver call log between iterations so the benchmark
		// doesn't allocate unbounded slices for it.
		resolver.calls = resolver.calls[:0]
		if _, err := executor.Execute(context.Background(), def); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
}

