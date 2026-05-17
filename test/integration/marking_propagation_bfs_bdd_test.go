//go:build integration

package integration_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// US-474 BDD scenarios for multi-hop transitive marking propagation. The
// suite stands up a real PG container + the production
// linkPropagationResolver/linkPropagationTraverser pair so the test sees
// the same wiring the funnel consumer runs in prod (the in-process
// equivalents live under pkg/funnel/marking_propagation_bfs_test.go).
//
// Each scenario seeds:
//   - 4 ObjectTypes (a/b/c/d) + extras as needed for fork/truncation
//   - 3 LinkTypes wired through the OMS admin path with PropagateMarkings
//     toggled per scenario
//   - link_edges rows representing the downstream graph before the
//     LINK_CREATE under test is applied
//   - bleve docs seeded with starting `_markings` per node
//
// The Given/When/Then pivot is `consumer.applyEdit(LINK_CREATE A→B)`; the
// Then asserts every downstream node's `_markings` matches the
// transitively-expected set.

// TestBDD_US474_ThreeHopTransitivePropagation walks a chain a1→b1→c1→d1
// where all links carry PropagateMarkings=true. The consumer must
// propagate a1's markings to b1 AND further down to c1 + d1 in one pass
// via BFS over the existing link_edges graph.
func TestBDD_US474_ThreeHopTransitivePropagation(t *testing.T) {
	fix := setupBDDPropagationFixture(t, []linkSpec{
		{api: "ab", source: "a", target: "b", propagate: true},
		{api: "bc", source: "b", target: "c", propagate: true},
		{api: "cd", source: "c", target: "d", propagate: true},
	})

	// Given: a1 has markings {SECRET}; b1, c1, d1 are empty. Existing
	// link_edges connect b1→c1 and c1→d1 so the BFS has a downstream
	// graph to walk.
	fix.seedMarkings(t, "a", "a1", []string{"SECRET"})
	fix.ensureBleve(t, "b", "b1")
	fix.ensureBleve(t, "c", "c1")
	fix.ensureBleve(t, "d", "d1")
	fix.upsertEdge(t, "bc", "b1", "c1")
	fix.upsertEdge(t, "cd", "c1", "d1")

	// When: the user-facing action that triggers propagation is the
	// LINK_CREATE event for a1→b1.
	fix.applyLinkCreate(t, "ab", "a1", "b1")

	// Then: SECRET cascades from a1 through b1, c1, d1.
	for _, pair := range []struct{ ot, pk string }{{"b", "b1"}, {"c", "c1"}, {"d", "d1"}} {
		got := fix.readMarkings(t, pair.ot, pair.pk)
		if !reflect.DeepEqual(got, []string{"SECRET"}) {
			t.Fatalf("%s/%s markings\n  got:  %v\n  want: [SECRET]", pair.ot, pair.pk, got)
		}
	}
}

// TestBDD_US474_ForkPropagatesToAllBranches covers the fork case: b1 has
// outgoing propagating links to BOTH c1 and d1. After LINK_CREATE A→B
// the BFS must visit each branch independently and tag both.
func TestBDD_US474_ForkPropagatesToAllBranches(t *testing.T) {
	fix := setupBDDPropagationFixture(t, []linkSpec{
		{api: "ab", source: "a", target: "b", propagate: true},
		{api: "bc", source: "b", target: "c", propagate: true},
		{api: "bd", source: "b", target: "d", propagate: true},
	})

	fix.seedMarkings(t, "a", "a1", []string{"PHI"})
	fix.ensureBleve(t, "b", "b1")
	fix.ensureBleve(t, "c", "c1")
	fix.ensureBleve(t, "d", "d1")
	fix.upsertEdge(t, "bc", "b1", "c1")
	fix.upsertEdge(t, "bd", "b1", "d1")

	fix.applyLinkCreate(t, "ab", "a1", "b1")

	for _, pair := range []struct{ ot, pk string }{{"b", "b1"}, {"c", "c1"}, {"d", "d1"}} {
		got := fix.readMarkings(t, pair.ot, pair.pk)
		if !reflect.DeepEqual(got, []string{"PHI"}) {
			t.Fatalf("%s/%s markings\n  got:  %v\n  want: [PHI]", pair.ot, pair.pk, got)
		}
	}
}

// TestBDD_US474_TruncationAtNonPropagatingLink seeds a chain a1→b1→c1
// where the second hop (b→c) has PropagateMarkings=false. The BFS must
// reach b1 but stop short of c1 — the non-propagating LinkType is
// invisible to the traverser by design.
func TestBDD_US474_TruncationAtNonPropagatingLink(t *testing.T) {
	fix := setupBDDPropagationFixture(t, []linkSpec{
		{api: "ab", source: "a", target: "b", propagate: true},
		{api: "bc", source: "b", target: "c", propagate: false}, // intentionally false
	})

	fix.seedMarkings(t, "a", "a1", []string{"CONFIDENTIAL"})
	fix.ensureBleve(t, "b", "b1")
	fix.ensureBleve(t, "c", "c1")
	// The b1→c1 edge exists but the LinkType has propagate=false; the
	// traverser must omit it.
	fix.upsertEdge(t, "bc", "b1", "c1")

	fix.applyLinkCreate(t, "ab", "a1", "b1")

	if got := fix.readMarkings(t, "b", "b1"); !reflect.DeepEqual(got, []string{"CONFIDENTIAL"}) {
		t.Fatalf("b/b1 markings\n  got:  %v\n  want: [CONFIDENTIAL]", got)
	}
	if got := fix.readMarkings(t, "c", "c1"); got != nil {
		t.Fatalf("c/c1 must stay clean (non-propagating b→c), got %v", got)
	}
}

// ---------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------

type linkSpec struct {
	api       string // LinkType api_name (used for the link_edges row + assertions)
	source    string // source ObjectType api_name (a/b/c/...)
	target    string // target ObjectType api_name
	propagate bool
}

// bddPropagationFixture bundles every dependency the BDD test needs: a
// real PG-backed repo (so ListEdgeTargets sees a real link_edges table),
// a per-test Bleve index manager, and the production traverser/resolver
// pair wired together. The fixture exposes per-objectType helpers to
// reduce per-test boilerplate.
type bddPropagationFixture struct {
	ctx       context.Context
	repo      *oms.PGRepository
	mgr       *index.Manager
	consumer  *funnel.Consumer
	ontology  string
	linkRIDs  map[string]string // api -> LinkType RID
	otRIDs    map[string]string // api -> ObjectType RID
}

func setupBDDPropagationFixture(t *testing.T, links []linkSpec) *bddPropagationFixture {
	t.Helper()
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	// Discover every distinct ObjectType referenced by the link specs.
	otSet := map[string]struct{}{}
	for _, l := range links {
		otSet[l.source] = struct{}{}
		otSet[l.target] = struct{}{}
	}
	otAPINames := make([]string, 0, len(otSet))
	for k := range otSet {
		otAPINames = append(otAPINames, k)
	}
	sort.Strings(otAPINames)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "us474_bdd",
		DisplayName: "US-474 BDD",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	otRIDs := map[string]string{}
	for _, apiName := range otAPINames {
		ot := &oms.ObjectType{
			RID:         rid.NewObjectTypeRID(),
			OntologyRID: ont.RID,
			APIName:     apiName,
			DisplayName: apiName,
			PrimaryKey:  "id",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := repo.CreateObjectType(ctx, ot); err != nil {
			t.Fatalf("create object type %s: %v", apiName, err)
		}
		otRIDs[apiName] = ot.RID
	}

	linkRIDs := map[string]string{}
	for _, l := range links {
		lt := &oms.LinkType{
			RID:               rid.NewLinkTypeRID(),
			OntologyRID:       ont.RID,
			APIName:           l.api,
			DisplayName:       l.api,
			SourceObjectType:  otRIDs[l.source],
			TargetObjectType:  otRIDs[l.target],
			Cardinality:       "MANY_TO_MANY",
			PropagateMarkings: l.propagate,
		}
		if err := repo.CreateLinkType(ctx, lt); err != nil {
			t.Fatalf("create link type %s: %v", l.api, err)
		}
		linkRIDs[l.api] = lt.RID
	}

	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotAnalyzed},
	}
	for _, apiName := range otAPINames {
		if _, err := mgr.EnsureIndex(index.ScopedKey(ont.APIName, apiName), props); err != nil {
			t.Fatalf("ensure index %s: %v", apiName, err)
		}
	}

	// Consumer wired with the PRODUCTION resolver + traverser. The same
	// adapters main.go installs are used here so the BDD covers the
	// prod-wiring path end-to-end. cmd/server's adapters live in the main
	// package; this BDD instead uses pkg/funnel's own narrow interfaces
	// directly, with closures over the real PG repo, to keep the
	// test-time dependency graph free of import cycles.
	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetLinkEdgeWriter(repo)
	consumer.SetLinkEdgeDeleter(repo)
	consumer.SetLinkPropagationResolver(&bddLinkPropagationResolver{repo: repo})
	consumer.SetLinkPropagationTraverser(&bddLinkPropagationTraverser{repo: repo, ontologyRID: ont.RID})

	return &bddPropagationFixture{
		ctx:      ctx,
		repo:     repo,
		mgr:      mgr,
		consumer: consumer,
		ontology: ont.APIName,
		linkRIDs: linkRIDs,
		otRIDs:   otRIDs,
	}
}

func (f *bddPropagationFixture) ensureBleve(t *testing.T, ot, pk string) {
	t.Helper()
	doc := map[string]interface{}{"id": pk}
	if err := f.mgr.IndexDocument(index.ScopedKey(f.ontology, ot), pk, doc); err != nil {
		t.Fatalf("seed %s/%s: %v", ot, pk, err)
	}
}

func (f *bddPropagationFixture) seedMarkings(t *testing.T, ot, pk string, marks []string) {
	t.Helper()
	doc := map[string]interface{}{"id": pk, "_markings": marks}
	if err := f.mgr.IndexDocument(index.ScopedKey(f.ontology, ot), pk, doc); err != nil {
		t.Fatalf("seed %s/%s: %v", ot, pk, err)
	}
}

func (f *bddPropagationFixture) upsertEdge(t *testing.T, linkAPI, src, tgt string) {
	t.Helper()
	rid, ok := f.linkRIDs[linkAPI]
	if !ok {
		t.Fatalf("unknown link api %q", linkAPI)
	}
	if err := f.repo.UpsertLinkEdge(f.ctx, &oms.LinkEdge{
		LinkTypeRID:    rid,
		SourceObjectPK: src,
		TargetObjectPK: tgt,
	}); err != nil {
		t.Fatalf("upsert edge %s %s→%s: %v", linkAPI, src, tgt, err)
	}
}

func (f *bddPropagationFixture) applyLinkCreate(t *testing.T, linkAPI, src, tgt string) {
	t.Helper()
	rid, ok := f.linkRIDs[linkAPI]
	if !ok {
		t.Fatalf("unknown link api %q", linkAPI)
	}
	batch := funnel.EditBatch{
		ID:              "us474-" + linkAPI + "-" + src + "-" + tgt,
		OntologyAPIName: f.ontology,
		Edits: []funnel.Edit{{
			Type:             funnel.EditTypeLinkCreate,
			LinkTypeRID:      rid,
			PrimaryKey:       src,
			TargetPrimaryKey: tgt,
		}},
	}
	if err := f.consumer.ApplyBatch(f.ctx, batch); err != nil {
		t.Fatalf("applyBatch LINK_CREATE %s: %v", linkAPI, err)
	}
}

func (f *bddPropagationFixture) readMarkings(t *testing.T, ot, pk string) []string {
	t.Helper()
	q := bleve.NewDocIDQuery([]string{pk})
	req := bleve.NewSearchRequest(q)
	req.Fields = []string{"*"}
	req.Size = 1
	res, err := f.mgr.Search(index.ScopedKey(f.ontology, ot), req)
	if err != nil {
		t.Fatalf("search %s/%s: %v", ot, pk, err)
	}
	if res.Total == 0 {
		return nil
	}
	raw := res.Hits[0].Fields["_markings"]
	if raw == nil {
		return nil
	}
	var out []string
	switch v := raw.(type) {
	case string:
		out = []string{v}
	case []string:
		out = append(out, v...)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// bddLinkPropagationResolver mirrors cmd/server's linkPropagationResolver
// but is duplicated here to avoid an import cycle into package main. It
// adapts a real PG-backed oms.Repository to the narrow funnel resolver
// interface for the BDD.
type bddLinkPropagationResolver struct {
	repo interface {
		GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error)
		GetObjectType(ctx context.Context, rid string) (*oms.ObjectType, error)
	}
}

func (r *bddLinkPropagationResolver) LookupLinkPropagation(ctx context.Context, linkTypeRID string) (funnel.LinkPropagation, bool, error) {
	lt, err := r.repo.GetLinkType(ctx, linkTypeRID)
	if err != nil {
		return funnel.LinkPropagation{}, false, nil
	}
	src, err := r.repo.GetObjectType(ctx, lt.SourceObjectType)
	if err != nil {
		return funnel.LinkPropagation{}, false, nil
	}
	tgt, err := r.repo.GetObjectType(ctx, lt.TargetObjectType)
	if err != nil {
		return funnel.LinkPropagation{}, false, nil
	}
	return funnel.LinkPropagation{
		PropagateMarkings:       lt.PropagateMarkings,
		SourceObjectTypeAPIName: src.APIName,
		TargetObjectTypeAPIName: tgt.APIName,
	}, true, nil
}

// bddLinkPropagationTraverser implements funnel.LinkPropagationTraverser
// against a real PG repo. Enumerates propagating LinkTypes for the
// ontology by sourceObjectType API name and walks downstream via
// ListEdgeTargets — same shape as the cmd/server traverser but inlined
// here so the BDD has no main-package dependency.
type bddLinkPropagationTraverser struct {
	repo        *oms.PGRepository
	ontologyRID string
}

func (t *bddLinkPropagationTraverser) ListPropagatingOutgoingEdges(
	ctx context.Context,
	sourceObjectTypeAPIName string,
	sourcePKs []string,
) ([]funnel.PropagatingOutgoingEdge, error) {
	if sourceObjectTypeAPIName == "" || len(sourcePKs) == 0 {
		return nil, nil
	}
	lts, err := t.repo.ListLinkTypes(ctx, t.ontologyRID)
	if err != nil {
		return nil, err
	}
	var out []funnel.PropagatingOutgoingEdge
	for _, lt := range lts {
		if !lt.PropagateMarkings {
			continue
		}
		srcOT, err := t.repo.GetObjectType(ctx, lt.SourceObjectType)
		if err != nil {
			continue
		}
		if srcOT.APIName != sourceObjectTypeAPIName {
			continue
		}
		tgtOT, err := t.repo.GetObjectType(ctx, lt.TargetObjectType)
		if err != nil {
			continue
		}
		targets, err := t.repo.ListEdgeTargets(ctx, lt.RID, sourcePKs)
		if err != nil {
			return nil, err
		}
		for _, pk := range targets {
			out = append(out, funnel.PropagatingOutgoingEdge{
				LinkTypeRID:             lt.RID,
				TargetObjectTypeAPIName: tgtOT.APIName,
				TargetPK:                pk,
			})
		}
	}
	return out, nil
}
