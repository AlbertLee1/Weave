package funnel

import (
	"context"
	"reflect"
	"testing"

	"github.com/liyang/weave/pkg/index"
)

// US-474 — multi-hop transitive marking propagation.
//
// When a LINK_CREATE A→B successfully propagates markings into B, the
// consumer must walk B's outgoing propagating edges forward via a BFS so
// the markings transitively reach C, D, ... up to the truncation boundary
// (a downstream link with PropagateMarkings=false) or a cycle visit.
//
// The traversal capability is supplied via a narrow optional interface
// (LinkPropagationTraverser); a nil traverser preserves the pre-US-474
// one-hop behaviour for callers that have not wired the new resolver.

// stubLinkPropagationTraverser is an in-memory LinkPropagationTraverser
// keyed by sourceObjectTypeAPIName. Each entry describes the propagating
// outgoing edges from any source PK of that ObjectType — the test fixture
// expands the per-PK list at call time so a single fixture can declare
// "object type X has outgoing propagating links to {Y@y1, Z@z1}" without
// caring which sourcePKs the consumer probes.
type stubLinkPropagationTraverser struct {
	edges map[string][]PropagatingOutgoingEdge
	calls int
}

func (s *stubLinkPropagationTraverser) ListPropagatingOutgoingEdges(
	_ context.Context,
	sourceObjectTypeAPIName string,
	sourcePKs []string,
) ([]PropagatingOutgoingEdge, error) {
	s.calls++
	if len(sourcePKs) == 0 {
		return nil, nil
	}
	out := s.edges[sourceObjectTypeAPIName]
	return out, nil
}

// setupBFSConsumer wires a Consumer with: per-OT bleve indexes, an
// in-memory LinkPropagationResolver pre-loaded with one or more
// (linkTypeRID, source/target OT, propagate flag) entries, and an
// in-memory LinkPropagationTraverser pre-loaded with downstream edges.
// Helper returns the consumer + index manager so tests can seed source
// docs and read back per-node markings.
func setupBFSConsumer(
	t *testing.T,
	objectTypes []string,
	resolver map[string]LinkPropagation,
	traverser map[string][]PropagatingOutgoingEdge,
) (*Consumer, *index.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
	}
	for _, ot := range objectTypes {
		if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, ot), props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}

	consumer := &Consumer{
		indexMgr:      mgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
	consumer.SetLinkEdgeWriter(fakeNoopLinkEdgeWriter{})
	consumer.SetLinkPropagationResolver(&stubLinkPropagationResolver{entries: resolver})
	consumer.SetLinkPropagationTraverser(&stubLinkPropagationTraverser{edges: traverser})
	return consumer, mgr
}

// TestPropagateMarkings_ThreeHopTransitive walks A→B→C→D where every link
// has PropagateMarkings=true. A LINK_CREATE A→B must transitively land A's
// markings on B, C, and D in one consumer pass.
func TestPropagateMarkings_ThreeHopTransitive(t *testing.T) {
	resolver := map[string]LinkPropagation{
		"ri.lt.AB": {PropagateMarkings: true, SourceObjectTypeAPIName: "a", TargetObjectTypeAPIName: "b"},
		"ri.lt.BC": {PropagateMarkings: true, SourceObjectTypeAPIName: "b", TargetObjectTypeAPIName: "c"},
		"ri.lt.CD": {PropagateMarkings: true, SourceObjectTypeAPIName: "c", TargetObjectTypeAPIName: "d"},
	}
	traverser := map[string][]PropagatingOutgoingEdge{
		"b": {{LinkTypeRID: "ri.lt.BC", TargetObjectTypeAPIName: "c", TargetPK: "c1"}},
		"c": {{LinkTypeRID: "ri.lt.CD", TargetObjectTypeAPIName: "d", TargetPK: "d1"}},
	}
	consumer, mgr := setupBFSConsumer(t, []string{"a", "b", "c", "d"}, resolver, traverser)

	mustSeed(t, mgr, "a", "a1", []string{"SECRET"})
	mustSeed(t, mgr, "b", "b1", nil)
	mustSeed(t, mgr, "c", "c1", nil)
	mustSeed(t, mgr, "d", "d1", nil)

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.AB",
		PrimaryKey:       "a1",
		TargetPrimaryKey: "b1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}

	wantMarks := []string{"SECRET"}
	for _, pair := range []struct {
		ot, pk string
	}{{"b", "b1"}, {"c", "c1"}, {"d", "d1"}} {
		got := readMarkings(t, mgr, pair.ot, pair.pk)
		if !reflect.DeepEqual(got, wantMarks) {
			t.Fatalf("%s/%s markings\n  got:  %v\n  want: %v", pair.ot, pair.pk, got, wantMarks)
		}
	}
}

// TestPropagateMarkings_ForkBranchesInheritIndependently covers A→B and
// A→C declared on disjoint downstream nodes. A LINK_CREATE A→B fans out
// to B; the same LINK_CREATE must NOT touch C, because the traversal
// only follows outgoing edges of the inserted target B (not of the
// original source A). C is reached when its own LINK_CREATE A→C fires.
func TestPropagateMarkings_ForkBranchesInheritIndependently(t *testing.T) {
	resolver := map[string]LinkPropagation{
		"ri.lt.AB": {PropagateMarkings: true, SourceObjectTypeAPIName: "a", TargetObjectTypeAPIName: "b"},
		"ri.lt.AC": {PropagateMarkings: true, SourceObjectTypeAPIName: "a", TargetObjectTypeAPIName: "c"},
		"ri.lt.BD": {PropagateMarkings: true, SourceObjectTypeAPIName: "b", TargetObjectTypeAPIName: "d"},
		"ri.lt.BE": {PropagateMarkings: true, SourceObjectTypeAPIName: "b", TargetObjectTypeAPIName: "e"},
	}
	traverser := map[string][]PropagatingOutgoingEdge{
		"b": {
			{LinkTypeRID: "ri.lt.BD", TargetObjectTypeAPIName: "d", TargetPK: "d1"},
			{LinkTypeRID: "ri.lt.BE", TargetObjectTypeAPIName: "e", TargetPK: "e1"},
		},
	}
	consumer, mgr := setupBFSConsumer(t, []string{"a", "b", "c", "d", "e"}, resolver, traverser)

	mustSeed(t, mgr, "a", "a1", []string{"PHI"})
	mustSeed(t, mgr, "b", "b1", nil)
	mustSeed(t, mgr, "c", "c1", nil)
	mustSeed(t, mgr, "d", "d1", nil)
	mustSeed(t, mgr, "e", "e1", nil)

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.AB",
		PrimaryKey:       "a1",
		TargetPrimaryKey: "b1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE A→B: %v", err)
	}

	// B and the fork D,E all inherit PHI.
	for _, pair := range []struct{ ot, pk string }{{"b", "b1"}, {"d", "d1"}, {"e", "e1"}} {
		got := readMarkings(t, mgr, pair.ot, pair.pk)
		want := []string{"PHI"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s/%s markings\n  got:  %v\n  want: %v", pair.ot, pair.pk, got, want)
		}
	}
	// C was NOT a target of this LINK_CREATE and not in B's outgoing edges; it stays clean.
	if got := readMarkings(t, mgr, "c", "c1"); got != nil {
		t.Fatalf("c/c1 must be untouched by A→B propagation, got %v", got)
	}
}

// TestPropagateMarkings_TruncationStopsAtNonPropagatingLink covers
// A→B (propagate=true) and B→C (propagate=false). The BFS frontier
// must stop at B; C remains unchanged. The truncation guarantee comes
// from the traverser only listing edges whose LinkType has
// PropagateMarkings=true — non-propagating links are invisible to the
// walk.
func TestPropagateMarkings_TruncationStopsAtNonPropagatingLink(t *testing.T) {
	resolver := map[string]LinkPropagation{
		"ri.lt.AB": {PropagateMarkings: true, SourceObjectTypeAPIName: "a", TargetObjectTypeAPIName: "b"},
		// BC exists but is intentionally absent from the traverser map below,
		// modelling the "propagate=false outgoing edge" filter.
	}
	traverser := map[string][]PropagatingOutgoingEdge{
		// "b" key absent — traverser returns no outgoing propagating edges.
	}
	consumer, mgr := setupBFSConsumer(t, []string{"a", "b", "c"}, resolver, traverser)

	mustSeed(t, mgr, "a", "a1", []string{"CONFIDENTIAL"})
	mustSeed(t, mgr, "b", "b1", nil)
	mustSeed(t, mgr, "c", "c1", nil)

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.AB",
		PrimaryKey:       "a1",
		TargetPrimaryKey: "b1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}

	gotB := readMarkings(t, mgr, "b", "b1")
	if !reflect.DeepEqual(gotB, []string{"CONFIDENTIAL"}) {
		t.Fatalf("b/b1 markings\n  got:  %v\n  want: [CONFIDENTIAL]", gotB)
	}
	if got := readMarkings(t, mgr, "c", "c1"); got != nil {
		t.Fatalf("c/c1 must stay clean (B→C is non-propagating), got %v", got)
	}
}

// TestPropagateMarkings_CycleSafe guards against infinite recursion when
// the propagating link graph contains a cycle (A→B and B→A both flagged
// propagate=true). The BFS must visit each node at most once.
func TestPropagateMarkings_CycleSafe(t *testing.T) {
	resolver := map[string]LinkPropagation{
		"ri.lt.AB": {PropagateMarkings: true, SourceObjectTypeAPIName: "a", TargetObjectTypeAPIName: "b"},
		"ri.lt.BA": {PropagateMarkings: true, SourceObjectTypeAPIName: "b", TargetObjectTypeAPIName: "a"},
	}
	traverser := map[string][]PropagatingOutgoingEdge{
		"b": {{LinkTypeRID: "ri.lt.BA", TargetObjectTypeAPIName: "a", TargetPK: "a1"}},
		"a": {{LinkTypeRID: "ri.lt.AB", TargetObjectTypeAPIName: "b", TargetPK: "b1"}},
	}
	consumer, mgr := setupBFSConsumer(t, []string{"a", "b"}, resolver, traverser)

	mustSeed(t, mgr, "a", "a1", []string{"TOPSECRET"})
	mustSeed(t, mgr, "b", "b1", nil)

	done := make(chan struct{})
	go func() {
		_ = consumer.applyEdit(testOntology, Edit{
			Type:             EditTypeLinkCreate,
			LinkTypeRID:      "ri.lt.AB",
			PrimaryKey:       "a1",
			TargetPrimaryKey: "b1",
		})
		close(done)
	}()
	<-done
	// B inherits TOPSECRET. A already has TOPSECRET so the back-edge is a no-op
	// (mergeMarkings sees no delta) and the BFS terminates.
	if got := readMarkings(t, mgr, "b", "b1"); !reflect.DeepEqual(got, []string{"TOPSECRET"}) {
		t.Fatalf("b/b1 markings\n  got:  %v\n  want: [TOPSECRET]", got)
	}
}

// TestPropagateMarkings_NilTraverserKeepsOneHop is the back-compat guard:
// a Consumer with no LinkPropagationTraverser set must continue to do
// one-hop propagation only.
func TestPropagateMarkings_NilTraverserKeepsOneHop(t *testing.T) {
	resolver := map[string]LinkPropagation{
		"ri.lt.AB": {PropagateMarkings: true, SourceObjectTypeAPIName: "a", TargetObjectTypeAPIName: "b"},
	}
	// setupBFSConsumer always installs a traverser; replicate the bare wiring here.
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	for _, ot := range []string{"a", "b", "c"} {
		if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, ot), nil); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}
	consumer := &Consumer{indexMgr: mgr, maxDeliveries: DefaultMaxDeliveries}
	consumer.SetLinkEdgeWriter(fakeNoopLinkEdgeWriter{})
	consumer.SetLinkPropagationResolver(&stubLinkPropagationResolver{entries: resolver})
	// No SetLinkPropagationTraverser — BFS degrades to single hop.

	mustSeed(t, mgr, "a", "a1", []string{"MARK"})
	mustSeed(t, mgr, "b", "b1", nil)
	mustSeed(t, mgr, "c", "c1", nil)

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.AB",
		PrimaryKey:       "a1",
		TargetPrimaryKey: "b1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}
	if got := readMarkings(t, mgr, "b", "b1"); !reflect.DeepEqual(got, []string{"MARK"}) {
		t.Fatalf("b/b1 markings\n  got:  %v\n  want: [MARK]", got)
	}
	// Without a traverser, C cannot be reached even if B had outgoing propagating
	// links — back-compat with pre-US-474 wiring.
	if got := readMarkings(t, mgr, "c", "c1"); got != nil {
		t.Fatalf("c/c1 must stay clean without traverser, got %v", got)
	}
}

// mustSeed writes a single object document with the optional markings slice.
// Empty marks means the `_markings` field is omitted entirely so the bleve
// search returns nil for the field (matching production behaviour).
func mustSeed(t *testing.T, mgr *index.Manager, ot, pk string, marks []string) {
	t.Helper()
	doc := map[string]interface{}{"id": pk}
	if len(marks) > 0 {
		doc["_markings"] = marks
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, ot), pk, doc); err != nil {
		t.Fatalf("seed %s/%s: %v", ot, pk, err)
	}
}
