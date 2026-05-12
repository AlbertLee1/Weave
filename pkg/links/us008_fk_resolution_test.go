package links_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// --- US-008 fixture helpers ---

type chainOT struct {
	rid     string
	apiName string
	pk      string
	extra   []string // additional searchable string fields (e.g. FK columns)
	docs    []map[string]any
}

type chainLT struct {
	rid                string
	apiName            string
	sourceOT, targetOT string
	fkSource, fkTarget string
}

// setupChain builds an in-memory Bleve + countingLinkRepo backed Resolver from
// a declarative ObjectType + LinkType list. Each ObjectType produces a Bleve
// index named by apiName; each LinkType installs a MANY_TO_ONE FK link
// (source.fkSource → target.fkTarget) in the mock repo.
func setupChain(t *testing.T, ots []chainOT, lts []chainLT) (*links.Resolver, *countingLinkRepo) {
	t.Helper()

	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })

	objectTypes := make(map[string]*oms.ObjectType, len(ots))
	for _, ot := range ots {
		props := []index.Property{{APIName: ot.pk, BaseType: "string", IsSearchable: true}}
		for _, f := range ot.extra {
			props = append(props, index.Property{APIName: f, BaseType: "string", IsSearchable: true})
		}
		if _, err := mgr.EnsureIndex(ot.apiName, props); err != nil {
			t.Fatalf("ensure index %s: %v", ot.apiName, err)
		}
		for _, d := range ot.docs {
			pkVal, _ := d[ot.pk].(string)
			if err := mgr.IndexDocument(ot.apiName, pkVal, d); err != nil {
				t.Fatalf("index %s/%s: %v", ot.apiName, pkVal, err)
			}
		}
		objectTypes[ot.rid] = &oms.ObjectType{RID: ot.rid, APIName: ot.apiName, PrimaryKey: ot.pk}
	}

	linkTypes := make(map[string]*oms.LinkType, len(lts))
	outgoing := make(map[string][]oms.LinkType)
	for _, l := range lts {
		lt := oms.LinkType{
			RID:              l.rid,
			APIName:          l.apiName,
			SourceObjectType: l.sourceOT,
			TargetObjectType: l.targetOT,
			Cardinality:      "MANY_TO_ONE",
			ForeignKeyConfig: mustJSON(links.FKConfig{SourceProperty: l.fkSource, TargetProperty: l.fkTarget}),
		}
		linkTypes[l.rid] = &lt
		outgoing[l.sourceOT] = append(outgoing[l.sourceOT], lt)
	}

	repo := &countingLinkRepo{
		mockRepo: &mockRepo{
			objectTypes: objectTypes,
			linkTypes:   linkTypes,
			outgoing:    outgoing,
		},
	}
	return links.NewResolver(repo, mgr), repo
}

func sortedStrs(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// --- 1. FK resolution cache: hit, invalidation, TTL expiry ---

func TestUS008_LinkTypeCache_FKResolution_HitInvalidationTTL(t *testing.T) {
	resolver, repo := setupChain(t,
		[]chainOT{
			{rid: "ri.ot.a", apiName: "a", pk: "id", extra: []string{"bid"}, docs: []map[string]any{
				{"id": "a1", "bid": "b1"},
			}},
			{rid: "ri.ot.b", apiName: "b", pk: "id", docs: []map[string]any{
				{"id": "b1"},
			}},
		},
		[]chainLT{
			{rid: "ri.lt.ab", apiName: "ab", sourceOT: "ri.ot.a", targetOT: "ri.ot.b", fkSource: "bid", fkTarget: "id"},
		},
	)
	cache := links.NewLinkTypeCache(60 * time.Second)
	resolver.SetLinkTypeCache(cache)
	ctx := context.Background()
	hops := []links.Hop{{LinkTypeRID: "ri.lt.ab"}}

	t.Run("warm_cache_collapses_repeated_traversals_to_one_repo_call", func(t *testing.T) {
		atomic.StoreInt64(&repo.getLinkTypeCalls, 0)
		for i := 0; i < 5; i++ {
			out, _, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, hops, links.TraverseOptions{})
			if err != nil {
				t.Fatalf("TraverseHops: %v", err)
			}
			if fmt.Sprintf("%v", out) != fmt.Sprintf("%v", []string{"b1"}) {
				t.Fatalf("expected [b1], got %v", out)
			}
		}
		if got := atomic.LoadInt64(&repo.getLinkTypeCalls); got != 1 {
			t.Fatalf("repo.GetLinkType calls: want 1 (cold then 4 cache hits), got %d", got)
		}
	})

	t.Run("InvalidateAll_forces_refetch", func(t *testing.T) {
		atomic.StoreInt64(&repo.getLinkTypeCalls, 0)
		// First call hits the still-warm cache (from previous subtest), so 0 repo calls.
		_, _, _ = resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, hops, links.TraverseOptions{})
		if got := atomic.LoadInt64(&repo.getLinkTypeCalls); got != 0 {
			t.Fatalf("warm cache call: want 0 repo calls, got %d", got)
		}
		cache.InvalidateAll()
		_, _, _ = resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, hops, links.TraverseOptions{})
		if got := atomic.LoadInt64(&repo.getLinkTypeCalls); got != 1 {
			t.Fatalf("post-invalidation: want 1 fresh repo call, got %d", got)
		}
	})

	t.Run("TTL_expiry_triggers_refetch", func(t *testing.T) {
		cache.InvalidateAll()
		now := time.Unix(0, 0)
		cache.SetNowFunc(func() time.Time { return now })

		atomic.StoreInt64(&repo.getLinkTypeCalls, 0)
		_, _, _ = resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, hops, links.TraverseOptions{})
		if got := atomic.LoadInt64(&repo.getLinkTypeCalls); got != 1 {
			t.Fatalf("cold call: want 1 repo call, got %d", got)
		}
		// Advance the test clock beyond TTL → next call must hit the repo again.
		now = now.Add(61 * time.Second)
		_, _, _ = resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, hops, links.TraverseOptions{})
		if got := atomic.LoadInt64(&repo.getLinkTypeCalls); got != 2 {
			t.Fatalf("post-TTL: want 2 repo calls, got %d", got)
		}
	})
}

// --- 2. Cycle detection: direct A→B→A loop, triangle A→B→C→A, opt-out ---

func TestUS008_TraverseHops_CycleDetection(t *testing.T) {
	// Two-type fixture with both forward links: a.bid → b.id and b.aid → a.id.
	// Walking [ab forward, ba forward] returns the original a1, which the cycle
	// guard must prune because (ri.ot.a, a1) is seeded.
	loopResolver, _ := setupChain(t,
		[]chainOT{
			{rid: "ri.ot.a", apiName: "a", pk: "id", extra: []string{"bid"}, docs: []map[string]any{
				{"id": "a1", "bid": "b1"},
			}},
			{rid: "ri.ot.b", apiName: "b", pk: "id", extra: []string{"aid"}, docs: []map[string]any{
				{"id": "b1", "aid": "a1"},
			}},
		},
		[]chainLT{
			{rid: "ri.lt.ab", apiName: "ab", sourceOT: "ri.ot.a", targetOT: "ri.ot.b", fkSource: "bid", fkTarget: "id"},
			{rid: "ri.lt.ba", apiName: "ba", sourceOT: "ri.ot.b", targetOT: "ri.ot.a", fkSource: "aid", fkTarget: "id"},
		},
	)
	ctx := context.Background()

	t.Run("direct_loop_a_to_b_to_a_prunes_seed", func(t *testing.T) {
		out, audit, err := loopResolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.ba"},
		}, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected empty result after direct loop, got %v", out)
		}
		if got := audit.Pruned[1]; got != 1 {
			t.Fatalf("hop 1 pruned: want 1 (a1 already visited), got %d (audit=%+v)", got, audit)
		}
	})

	t.Run("triangle_a_b_c_a_prunes_at_third_hop", func(t *testing.T) {
		triResolver, _ := setupChain(t,
			[]chainOT{
				{rid: "ri.ot.a3", apiName: "a3", pk: "id", extra: []string{"bid"}, docs: []map[string]any{
					{"id": "a1", "bid": "b1"},
				}},
				{rid: "ri.ot.b3", apiName: "b3", pk: "id", extra: []string{"cid"}, docs: []map[string]any{
					{"id": "b1", "cid": "c1"},
				}},
				{rid: "ri.ot.c3", apiName: "c3", pk: "id", extra: []string{"aid"}, docs: []map[string]any{
					{"id": "c1", "aid": "a1"},
				}},
			},
			[]chainLT{
				{rid: "ri.lt.abc", apiName: "abc", sourceOT: "ri.ot.a3", targetOT: "ri.ot.b3", fkSource: "bid", fkTarget: "id"},
				{rid: "ri.lt.bca", apiName: "bca", sourceOT: "ri.ot.b3", targetOT: "ri.ot.c3", fkSource: "cid", fkTarget: "id"},
				{rid: "ri.lt.caa", apiName: "caa", sourceOT: "ri.ot.c3", targetOT: "ri.ot.a3", fkSource: "aid", fkTarget: "id"},
			},
		)
		out, audit, err := triResolver.TraverseHops(ctx, "ri.ot.a3", []string{"a1"}, []links.Hop{
			{LinkTypeRID: "ri.lt.abc"},
			{LinkTypeRID: "ri.lt.bca"},
			{LinkTypeRID: "ri.lt.caa"},
		}, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("triangle cycle should yield empty, got %v", out)
		}
		if got := audit.Pruned[2]; got != 1 {
			t.Fatalf("hop 2 pruned: want 1, got %d (audit=%+v)", got, audit)
		}
	})

	t.Run("DisableCycleGuard_returns_revisited_pks", func(t *testing.T) {
		out, audit, err := loopResolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.ba"},
		}, links.TraverseOptions{DisableCycleGuard: true})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if fmt.Sprintf("%v", out) != fmt.Sprintf("%v", []string{"a1"}) {
			t.Fatalf("disabled cycle guard should return [a1], got %v", out)
		}
		for i, p := range audit.Pruned {
			if p != 0 {
				t.Fatalf("hop %d Pruned should be 0 when guard disabled, got %d", i, p)
			}
		}
	})
}

// --- 3. Multi-hop propagation: 2-hop forward chain, fan-in dedup, short-circuit ---

func TestUS008_TraverseHops_MultiHopPropagation(t *testing.T) {
	resolver, _ := setupChain(t,
		[]chainOT{
			{rid: "ri.ot.a", apiName: "a", pk: "id", extra: []string{"bid"}, docs: []map[string]any{
				{"id": "a1", "bid": "b1"},
				{"id": "a2", "bid": "b2"},
				{"id": "a3", "bid": "b1"},
			}},
			{rid: "ri.ot.b", apiName: "b", pk: "id", extra: []string{"cid"}, docs: []map[string]any{
				{"id": "b1", "cid": "c1"},
				{"id": "b2", "cid": "c2"},
			}},
			{rid: "ri.ot.c", apiName: "c", pk: "id", docs: []map[string]any{
				{"id": "c1"},
				{"id": "c2"},
			}},
		},
		[]chainLT{
			{rid: "ri.lt.ab", apiName: "ab", sourceOT: "ri.ot.a", targetOT: "ri.ot.b", fkSource: "bid", fkTarget: "id"},
			{rid: "ri.lt.bc", apiName: "bc", sourceOT: "ri.ot.b", targetOT: "ri.ot.c", fkSource: "cid", fkTarget: "id"},
		},
	)
	ctx := context.Background()

	t.Run("two_hops_yield_distinct_targets", func(t *testing.T) {
		out, audit, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1", "a2"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.bc"},
		}, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if want := []string{"c1", "c2"}; fmt.Sprintf("%v", sortedStrs(out)) != fmt.Sprintf("%v", want) {
			t.Fatalf("two_hops: want %v, got %v", want, sortedStrs(out))
		}
		if audit.Inputs[0] != 2 || audit.Outputs[0] != 2 {
			t.Fatalf("hop0 expected inputs=2 outputs=2, got %d/%d", audit.Inputs[0], audit.Outputs[0])
		}
		if audit.Inputs[1] != 2 || audit.Outputs[1] != 2 {
			t.Fatalf("hop1 expected inputs=2 outputs=2, got %d/%d", audit.Inputs[1], audit.Outputs[1])
		}
	})

	t.Run("fan_in_dedup_collapses_intermediate_to_single_pk", func(t *testing.T) {
		// a1 and a3 both reference b1, so after hop 0 the working set is {b1}.
		out, audit, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1", "a3"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
		}, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if want := []string{"b1"}; fmt.Sprintf("%v", out) != fmt.Sprintf("%v", want) {
			t.Fatalf("fan_in: want %v, got %v", want, out)
		}
		if audit.Outputs[0] != 1 {
			t.Fatalf("hop0 outputs after dedup: want 1, got %d", audit.Outputs[0])
		}
	})

	t.Run("empty_intermediate_short_circuits_remaining_hops", func(t *testing.T) {
		// Starting from an unindexed PK produces zero output at hop 0; remaining
		// hops must be filled with empty audit slots, never invoking the resolver.
		out, audit, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a-missing"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.bc"},
		}, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected empty, got %v", out)
		}
		if len(audit.Inputs) != 2 {
			t.Fatalf("audit slots: want 2 (one per hop), got %d", len(audit.Inputs))
		}
		if audit.Inputs[1] != 0 || audit.Outputs[1] != 0 {
			t.Fatalf("hop1 short-circuit: inputs %d outputs %d", audit.Inputs[1], audit.Outputs[1])
		}
	})
}

// --- 4. Permission propagation trimming ---

func TestUS008_TraverseHops_PermissionTrim(t *testing.T) {
	resolver, _ := setupChain(t,
		[]chainOT{
			{rid: "ri.ot.a", apiName: "a", pk: "id", extra: []string{"bid"}, docs: []map[string]any{
				{"id": "a1", "bid": "b1"},
				{"id": "a2", "bid": "b2"},
			}},
			{rid: "ri.ot.b", apiName: "b", pk: "id", extra: []string{"cid"}, docs: []map[string]any{
				{"id": "b1", "cid": "c1"},
				{"id": "b2", "cid": "c2"},
			}},
			{rid: "ri.ot.c", apiName: "c", pk: "id", docs: []map[string]any{
				{"id": "c1"},
				{"id": "c2"},
			}},
		},
		[]chainLT{
			{rid: "ri.lt.ab", apiName: "ab", sourceOT: "ri.ot.a", targetOT: "ri.ot.b", fkSource: "bid", fkTarget: "id"},
			{rid: "ri.lt.bc", apiName: "bc", sourceOT: "ri.ot.b", targetOT: "ri.ot.c", fkSource: "cid", fkTarget: "id"},
		},
	)
	ctx := context.Background()

	t.Run("deny_target_pk_drops_from_final_set", func(t *testing.T) {
		denied := map[string]bool{"c1": true}
		perm := func(_ context.Context, _ int, _, pk string) bool { return !denied[pk] }
		out, audit, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1", "a2"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.bc"},
		}, links.TraverseOptions{Permission: perm})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if want := []string{"c2"}; fmt.Sprintf("%v", out) != fmt.Sprintf("%v", want) {
			t.Fatalf("deny c1: want %v, got %v", want, out)
		}
		if audit.Denied[1] != 1 {
			t.Fatalf("hop1 denied: want 1, got %d", audit.Denied[1])
		}
	})

	t.Run("deny_intermediate_pk_breaks_propagation", func(t *testing.T) {
		// Denying b1 at hop 0 prunes the propagation chain a1→b1→c1 entirely.
		perm := func(_ context.Context, hopIdx int, outOT, pk string) bool {
			if hopIdx == 0 && outOT == "ri.ot.b" && pk == "b1" {
				return false
			}
			return true
		}
		out, audit, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1", "a2"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.bc"},
		}, links.TraverseOptions{Permission: perm})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if want := []string{"c2"}; fmt.Sprintf("%v", out) != fmt.Sprintf("%v", want) {
			t.Fatalf("deny b1 at hop0: want %v, got %v", want, out)
		}
		if audit.Denied[0] != 1 {
			t.Fatalf("hop0 denied: want 1, got %d", audit.Denied[0])
		}
	})

	t.Run("deny_all_short_circuits_subsequent_hops", func(t *testing.T) {
		perm := func(_ context.Context, _ int, _, _ string) bool { return false }
		out, audit, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1", "a2"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.bc"},
		}, links.TraverseOptions{Permission: perm})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("deny-all should yield empty, got %v", out)
		}
		if audit.Inputs[1] != 0 || audit.Outputs[1] != 0 || audit.Denied[1] != 0 {
			t.Fatalf("hop1 short-circuit: inputs %d outputs %d denied %d", audit.Inputs[1], audit.Outputs[1], audit.Denied[1])
		}
	})
}

// --- 5. Boundary / error paths ---

func TestUS008_TraverseHops_BoundaryConditions(t *testing.T) {
	resolver, _ := setupChain(t,
		[]chainOT{
			{rid: "ri.ot.a", apiName: "a", pk: "id", extra: []string{"bid"}, docs: []map[string]any{
				{"id": "a1", "bid": "b1"},
				{"id": "a2", "bid": "b2"},
			}},
			{rid: "ri.ot.b", apiName: "b", pk: "id", docs: []map[string]any{
				{"id": "b1"},
				{"id": "b2"},
			}},
		},
		[]chainLT{
			{rid: "ri.lt.ab", apiName: "ab", sourceOT: "ri.ot.a", targetOT: "ri.ot.b", fkSource: "bid", fkTarget: "id"},
		},
	)

	t.Run("empty_start_pks_returns_nil_no_hop_invoked", func(t *testing.T) {
		out, audit, err := resolver.TraverseHops(context.Background(), "ri.ot.a", nil, []links.Hop{{LinkTypeRID: "ri.lt.ab"}}, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if out != nil {
			t.Fatalf("expected nil result, got %v", out)
		}
		if len(audit.Inputs) != 0 {
			t.Fatalf("audit should be empty for short-circuit, got %d slots", len(audit.Inputs))
		}
	})

	t.Run("empty_hops_returns_unique_start_pks", func(t *testing.T) {
		out, _, err := resolver.TraverseHops(context.Background(), "ri.ot.a", []string{"a1", "a1", "a2"}, nil, links.TraverseOptions{})
		if err != nil {
			t.Fatalf("TraverseHops: %v", err)
		}
		if want := []string{"a1", "a2"}; fmt.Sprintf("%v", sortedStrs(out)) != fmt.Sprintf("%v", want) {
			t.Fatalf("empty hops should dedupe startPKs: want %v, got %v", want, sortedStrs(out))
		}
	})

	t.Run("MaxHops_exceeded_returns_ErrTooManyHops", func(t *testing.T) {
		_, _, err := resolver.TraverseHops(context.Background(), "ri.ot.a", []string{"a1"}, []links.Hop{
			{LinkTypeRID: "ri.lt.ab"},
			{LinkTypeRID: "ri.lt.ab"},
		}, links.TraverseOptions{MaxHops: 1})
		if err == nil {
			t.Fatal("expected ErrTooManyHops, got nil")
		}
		if !errors.Is(err, links.ErrTooManyHops) {
			t.Fatalf("expected ErrTooManyHops, got %v", err)
		}
	})

	t.Run("ctx_canceled_before_first_hop_returns_ctx_error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := resolver.TraverseHops(ctx, "ri.ot.a", []string{"a1"}, []links.Hop{{LinkTypeRID: "ri.lt.ab"}}, links.TraverseOptions{})
		if err == nil {
			t.Fatal("expected ctx.Canceled, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected ctx.Canceled, got %v", err)
		}
	})

	t.Run("unknown_link_type_propagates_wrapped_error", func(t *testing.T) {
		_, _, err := resolver.TraverseHops(context.Background(), "ri.ot.a", []string{"a1"}, []links.Hop{
			{LinkTypeRID: "ri.lt.nonexistent"},
		}, links.TraverseOptions{})
		if err == nil {
			t.Fatal("expected error for unknown link type, got nil")
		}
	})
}
