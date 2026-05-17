package objectset_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// US-464 hardens searchAround cycle-detection coverage:
//   - diamond fan-in (A→B + A→C → D collapses to one D, not two)
//   - self-loop (A→A returns nothing because the seed is in the visited set)
//   - triangle (A→B→C→A returns nothing, matching US-366 already, but
//     here we additionally lock down the "no duplicate PK" invariant by
//     asserting the result set equals the dedup of itself).
//
// All three tests reuse the multiHopResolver fixture from
// searcharound_path_test.go to stay consistent with the rest of the
// searchAround test surface.

// TestExecute_SearchAround_Path_Diamond_NoDuplicates asserts that when two
// upstream nodes fan in to the same downstream node, the executor returns
// that downstream node exactly once. The classic shape:
//
//	    A ─┐
//	       ├─> D
//	    B ─┘
//	    C ─> D  (third path, same sink)
//
// Walking [A,B,C] → "leadsTo" must collapse on D. This pins the dedupe
// invariant that searchAround output never contains duplicate (objectType,
// primaryKey) tuples — a regression here would inflate every analytics
// rollup downstream.
func TestExecute_SearchAround_Path_Diamond_NoDuplicates(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"node", "sink"})
	// Seed three distinct upstream "node" rows. They all flow into the
	// single "sink" d1 via a single-hop forward link.
	for _, pk := range []string{"a1", "b1", "c1"} {
		if err := mgr.IndexDocument("node", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}

	resolver := newMultiHopResolver()
	resolver.addForward("node", "leadsTo", "sink", map[string][]string{
		"a1": {"d1"},
		"b1": {"d1"},
		"c1": {"d1"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "node"},
		Path: []objectset.PathStep{
			{Link: "leadsTo"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "sink" {
		t.Errorf("ObjectType: want sink, got %q", result.ObjectType)
	}
	if got, want := fmt.Sprintf("%v", sorted(result.PrimaryKeys)), fmt.Sprintf("%v", []string{"d1"}); got != want {
		t.Errorf("diamond fan-in: want %s, got %s", want, got)
	}
	// Explicitly assert "no duplicates" by comparing against the dedup of
	// the result. If dedupeStrings ever silently regresses (or if cycle
	// pruning emits a key twice) this catches it independently of length.
	if hasDuplicateStrings(result.PrimaryKeys) {
		t.Errorf("diamond result contains duplicate PKs: %v", result.PrimaryKeys)
	}
}

// TestExecute_SearchAround_Path_Diamond_TwoHop covers the deeper diamond:
//
//	     ┌─> B1 ─┐
//	A ───┤        ├─> D
//	     └─> B2 ─┘
//
// Walking A → branchTo (forward) → mergeTo (forward) must return D exactly
// once. This exercises the hop-2 dedupeStrings + cycle-prune interaction
// rather than the single-hop short-circuit covered by the wide-diamond test.
func TestExecute_SearchAround_Path_Diamond_TwoHop(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"root", "mid", "sink"})
	if err := mgr.IndexDocument("root", "a1", map[string]interface{}{"id": "a1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := newMultiHopResolver()
	resolver.addForward("root", "branchTo", "mid", map[string][]string{
		"a1": {"b1", "b2"},
	})
	resolver.addForward("mid", "mergeTo", "sink", map[string][]string{
		"b1": {"d1"},
		"b2": {"d1"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "root"},
		Path: []objectset.PathStep{
			{Link: "branchTo"},
			{Link: "mergeTo"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "sink" {
		t.Errorf("ObjectType: want sink, got %q", result.ObjectType)
	}
	if got, want := fmt.Sprintf("%v", sorted(result.PrimaryKeys)), fmt.Sprintf("%v", []string{"d1"}); got != want {
		t.Errorf("two-hop diamond: want %s, got %s", want, got)
	}
	if hasDuplicateStrings(result.PrimaryKeys) {
		t.Errorf("two-hop diamond result contains duplicate PKs: %v", result.PrimaryKeys)
	}
}

// TestExecute_SearchAround_Path_SelfLoop_Reflexive asserts that a reflexive
// link (A→A) on the seed PK is pruned by cycle detection: the visited set
// is seeded with the source set, so the seed itself is already "visited"
// and the result is empty. Crucially, a different PK linked to itself
// (b1→b1) reached via a forward hop should still be pruned at the *second*
// step but not before.
func TestExecute_SearchAround_Path_SelfLoop_Reflexive(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"node"})
	if err := mgr.IndexDocument("node", "a1", map[string]interface{}{"id": "a1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := newMultiHopResolver()
	// a1 links to itself via a reflexive "loopsTo" link.
	resolver.addForward("node", "loopsTo", "node", map[string][]string{
		"a1": {"a1"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "node"},
		Path: []objectset.PathStep{
			{Link: "loopsTo"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "node" {
		t.Errorf("ObjectType: want node, got %q", result.ObjectType)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("self-loop should yield empty result (seed in visited set), got %v", result.PrimaryKeys)
	}
}

// TestExecute_SearchAround_Path_SelfLoop_TwoHopDownstream asserts that a
// reflexive self-loop reached via a downstream hop is also pruned. Shape:
//
//	A → B → B (reflexive)
//
// Hop 1 yields {b1}; hop 2 over loopsTo would yield {b1} again, which the
// cycle-prune set must drop because b1 was just added at "mid" in hop 1.
func TestExecute_SearchAround_Path_SelfLoop_TwoHopDownstream(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"root", "mid"})
	if err := mgr.IndexDocument("root", "a1", map[string]interface{}{"id": "a1"}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	resolver := newMultiHopResolver()
	resolver.addForward("root", "stepTo", "mid", map[string][]string{
		"a1": {"b1"},
	})
	resolver.addForward("mid", "loopsTo", "mid", map[string][]string{
		"b1": {"b1"},
	})

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type:      "searchAround",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "root"},
		Path: []objectset.PathStep{
			{Link: "stepTo"},
			{Link: "loopsTo"},
		},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "mid" {
		t.Errorf("ObjectType: want mid, got %q", result.ObjectType)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("downstream self-loop should yield empty result (b1 in visited set after hop 1), got %v", result.PrimaryKeys)
	}
}

// TestExecute_SearchAround_Path_Triangle_NoDuplicates is the US-464
// hardening companion to US-366's TestExecute_..._Triangle: same A→B→C→A
// shape but here we additionally pin down "result.PrimaryKeys has no
// duplicates" via hasDuplicateStrings, independent of length. PRD US-464
// lists 三角图 explicitly as an acceptance criterion — even though the
// implementation already returns an empty set, this assertion stops a
// future "len==0 but duplicates leaked through" regression.
func TestExecute_SearchAround_Path_Triangle_NoDuplicates(t *testing.T) {
	mgr := seedMultiHopIndex(t, []string{"a", "b", "c"})
	for _, pk := range []string{"a1", "a2"} {
		if err := mgr.IndexDocument("a", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}

	resolver := newMultiHopResolver()
	resolver.addForward("a", "ab", "b", map[string][]string{
		"a1": {"b1"},
		"a2": {"b2"},
	})
	resolver.addForward("b", "bc", "c", map[string][]string{
		"b1": {"c1"},
		"b2": {"c1"}, // diamond + triangle: both b's collapse on c1
	})
	resolver.addForward("c", "ca", "a", map[string][]string{
		"c1": {"a1", "a2"},
	})

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
	if hasDuplicateStrings(result.PrimaryKeys) {
		t.Errorf("triangle result contains duplicate PKs: %v", result.PrimaryKeys)
	}
}

// hasDuplicateStrings reports whether s contains any element more than
// once. Returns false for empty / nil / single-element slices.
func hasDuplicateStrings(s []string) bool {
	if len(s) < 2 {
		return false
	}
	seen := make(map[string]struct{}, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}
