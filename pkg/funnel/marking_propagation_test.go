package funnel

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// stubLinkPropagationResolver is a tiny in-memory LinkPropagationResolver
// keyed by linkTypeRID. The lookupErr field lets a test assert the consumer
// logs+swallows resolver failures rather than aborting the LINK_CREATE.
type stubLinkPropagationResolver struct {
	entries   map[string]LinkPropagation
	lookupErr error
	calls     int
}

func (s *stubLinkPropagationResolver) LookupLinkPropagation(_ context.Context, linkTypeRID string) (LinkPropagation, bool, error) {
	s.calls++
	if s.lookupErr != nil {
		return LinkPropagation{}, false, s.lookupErr
	}
	info, ok := s.entries[linkTypeRID]
	return info, ok, nil
}

// fakeNoopLinkEdgeWriter satisfies LinkEdgeWriter without persisting edges
// — every test in this file only cares about the propagation side-effect on
// Bleve, so we keep the writer minimal.
type fakeNoopLinkEdgeWriter struct{}

func (fakeNoopLinkEdgeWriter) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error {
	return nil
}

// setupPropagationConsumer wires a Consumer with a real index.Manager that
// has both `parent` and `child` indexes, plus a stub LinkPropagationResolver
// preloaded with one LinkType. Returns helpers to seed source/target docs
// and read the target's `_markings` after a LINK_CREATE.
func setupPropagationConsumer(t *testing.T, info LinkPropagation) (*Consumer, *index.Manager, *stubLinkPropagationResolver) {
	t.Helper()
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
		{APIName: "name", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
	}
	for _, ot := range []string{info.SourceObjectTypeAPIName, info.TargetObjectTypeAPIName} {
		if ot == "" {
			continue
		}
		if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, ot), props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}

	resolver := &stubLinkPropagationResolver{
		entries: map[string]LinkPropagation{
			"ri.lt.parent-child": info,
		},
	}

	consumer := &Consumer{
		indexMgr:      mgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
	consumer.SetLinkEdgeWriter(fakeNoopLinkEdgeWriter{})
	consumer.SetLinkPropagationResolver(resolver)
	return consumer, mgr, resolver
}

// readMarkings returns the target's `_markings` field (sorted) after a
// re-index. nil means the field is absent.
func readMarkings(t *testing.T, mgr *index.Manager, ot, pk string) []string {
	t.Helper()
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := mgr.Search(index.ScopedKey(testOntology, ot), req)
	if err != nil {
		t.Fatalf("search %s/%s: %v", ot, pk, err)
	}
	if res.Total == 0 {
		t.Fatalf("expected hit for %s/%s, got none", ot, pk)
	}
	raw := res.Hits[0].Fields["_markings"]
	out := decodeMarkings(map[string]interface{}{"_markings": raw})
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func TestPropagateMarkings_BasicInheritance(t *testing.T) {
	consumer, mgr, _ := setupPropagationConsumer(t, LinkPropagation{
		PropagateMarkings:       true,
		SourceObjectTypeAPIName: "parent",
		TargetObjectTypeAPIName: "child",
	})

	// Seed parent with markings, child without.
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"id":         "p1",
		"name":       "Parent One",
		"_markings":  []string{"ALPHA", "BETA"},
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "child"), "c1", map[string]interface{}{
		"id":   "c1",
		"name": "Child One",
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.parent-child",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}

	got := readMarkings(t, mgr, "child", "c1")
	want := []string{"ALPHA", "BETA"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("child markings\n  got:  %v\n  want: %v", got, want)
	}
}

func TestPropagateMarkings_MergesWithExisting(t *testing.T) {
	consumer, mgr, _ := setupPropagationConsumer(t, LinkPropagation{
		PropagateMarkings:       true,
		SourceObjectTypeAPIName: "parent",
		TargetObjectTypeAPIName: "child",
	})

	// Child already has GAMMA — propagation should add ALPHA without
	// dropping the pre-existing marking.
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"id":        "p1",
		"_markings": []string{"ALPHA"},
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "child"), "c1", map[string]interface{}{
		"id":        "c1",
		"_markings": []string{"GAMMA"},
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.parent-child",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}

	got := readMarkings(t, mgr, "child", "c1")
	want := []string{"ALPHA", "GAMMA"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("child markings\n  got:  %v\n  want: %v", got, want)
	}
}

func TestPropagateMarkings_DisabledIsNoop(t *testing.T) {
	consumer, mgr, _ := setupPropagationConsumer(t, LinkPropagation{
		PropagateMarkings:       false,
		SourceObjectTypeAPIName: "parent",
		TargetObjectTypeAPIName: "child",
	})

	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"id":        "p1",
		"_markings": []string{"ALPHA"},
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "child"), "c1", map[string]interface{}{
		"id": "c1",
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.parent-child",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}

	if got := readMarkings(t, mgr, "child", "c1"); got != nil {
		t.Fatalf("expected child markings unchanged (nil), got %v", got)
	}
}

func TestPropagateMarkings_SourceWithoutMarkings(t *testing.T) {
	consumer, mgr, _ := setupPropagationConsumer(t, LinkPropagation{
		PropagateMarkings:       true,
		SourceObjectTypeAPIName: "parent",
		TargetObjectTypeAPIName: "child",
	})

	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"id": "p1",
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "child"), "c1", map[string]interface{}{
		"id":        "c1",
		"_markings": []string{"GAMMA"},
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.parent-child",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}

	got := readMarkings(t, mgr, "child", "c1")
	want := []string{"GAMMA"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("child markings\n  got:  %v\n  want: %v", got, want)
	}
}

func TestPropagateMarkings_LinkTypeNotFoundSilentSkip(t *testing.T) {
	consumer, mgr, resolver := setupPropagationConsumer(t, LinkPropagation{
		PropagateMarkings:       true,
		SourceObjectTypeAPIName: "parent",
		TargetObjectTypeAPIName: "child",
	})

	// Seed both objects but use a LinkTypeRID the stub does NOT know about.
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"id":        "p1",
		"_markings": []string{"ALPHA"},
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "child"), "c1", map[string]interface{}{
		"id": "c1",
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.unknown",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected 1 resolver call, got %d", resolver.calls)
	}
	if got := readMarkings(t, mgr, "child", "c1"); got != nil {
		t.Fatalf("expected no propagation for unknown link type, got %v", got)
	}
}

func TestPropagateMarkings_TargetNotIndexedSkip(t *testing.T) {
	consumer, mgr, _ := setupPropagationConsumer(t, LinkPropagation{
		PropagateMarkings:       true,
		SourceObjectTypeAPIName: "parent",
		TargetObjectTypeAPIName: "child",
	})

	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"id":        "p1",
		"_markings": []string{"ALPHA"},
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	// Child intentionally not indexed.

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.parent-child",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c-missing",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE should succeed even when target missing: %v", err)
	}
}

func TestPropagateMarkings_ResolverErrorIsLoggedNotReturned(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })

	consumer := &Consumer{
		indexMgr:      mgr,
		maxDeliveries: DefaultMaxDeliveries,
	}
	consumer.SetLinkEdgeWriter(fakeNoopLinkEdgeWriter{})
	consumer.SetLinkPropagationResolver(&stubLinkPropagationResolver{
		lookupErr: errors.New("boom"),
	})

	// Even though the resolver errors, applyLinkCreate must succeed (the
	// edge upsert is the source of truth; propagation is best-effort).
	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.x",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE must swallow resolver errors, got: %v", err)
	}
}

func TestPropagateMarkings_NoResolverIsNoop(t *testing.T) {
	consumer, mgr := setupTestConsumer(t)
	consumer.SetLinkEdgeWriter(fakeNoopLinkEdgeWriter{})
	// no SetLinkPropagationResolver — propagation must be silently disabled.

	if _, err := mgr.EnsureIndex(index.ScopedKey(testOntology, "parent"), nil); err != nil {
		t.Fatalf("EnsureIndex parent: %v", err)
	}
	if err := mgr.IndexDocument(index.ScopedKey(testOntology, "parent"), "p1", map[string]interface{}{
		"_markings": []string{"ALPHA"},
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}

	if err := consumer.applyEdit(testOntology, Edit{
		Type:             EditTypeLinkCreate,
		LinkTypeRID:      "ri.lt.x",
		PrimaryKey:       "p1",
		TargetPrimaryKey: "c1",
	}); err != nil {
		t.Fatalf("applyEdit LINK_CREATE: %v", err)
	}
}

func TestDecodeMarkings_Shapes(t *testing.T) {
	cases := []struct {
		name string
		raw  interface{}
		want []string
	}{
		{"nil", nil, nil},
		{"single string", "ALPHA", []string{"ALPHA"}},
		{"[]string", []string{"BETA", "ALPHA", "ALPHA"}, []string{"ALPHA", "BETA"}},
		{"[]interface{}", []interface{}{"ALPHA", "GAMMA"}, []string{"ALPHA", "GAMMA"}},
		{"empty []interface{}", []interface{}{}, nil},
		{"non-string entries dropped", []interface{}{"ALPHA", 42, true}, []string{"ALPHA"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := map[string]interface{}{"_markings": c.raw}
			got := decodeMarkings(doc)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("decodeMarkings(%v)\n  got:  %v\n  want: %v", c.raw, got, c.want)
			}
		})
	}
}

func TestMergeMarkings_DedupAndSort(t *testing.T) {
	got := mergeMarkings([]string{"BETA", "ALPHA"}, []string{"GAMMA", "ALPHA"})
	want := []string{"ALPHA", "BETA", "GAMMA"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeMarkings\n  got:  %v\n  want: %v", got, want)
	}
}
