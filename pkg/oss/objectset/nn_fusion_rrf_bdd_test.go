package objectset_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestBDD_NearestNeighbors_FusionStrategy_* covers PRD-V2 Gap-Q4
// round-50 follow-up: round 49 added multi-column NN with min-distance
// fusion; this round adds an explicit FusionStrategy selector and the
// industry-standard Reciprocal Rank Fusion (RRF) strategy alongside it.
//
// The motivating Foundry pattern: when a PK appears in TWO column
// rankings (even at modest distances) it should outrank a PK that
// only appears once with a slightly better distance — min-distance
// fusion can't express this because it discards rank information.
// RRF rewards multi-column presence by summing reciprocal ranks.
//
// RRF formula (Cormack et al., k=60):
//
//	score(pk) = sum_c 1 / (60 + rank_c(pk))
//
// where rank starts at 1 and absent PKs contribute 0. Sort by score
// descending, ties broken on PK so output is deterministic.
//
// Scenarios:
//   - RRF reorders results so a two-column hit outranks a one-column
//     better-distance hit (the property min-distance cannot deliver).
//   - Empty FusionStrategy is treated as "min" — round-49 behavior
//     preserved bit-for-bit.
//   - Explicit "min" matches the default exactly.
//   - Unknown strategies are rejected at Validate() (fail-fast so
//     callers see the typo before any store call goes out).
//   - "rrf" on a single-column query is accepted (no Validate error)
//     and produces sane results — single-column RRF degenerates to
//     ranking by stored distance, same as the single-column fast path.
func TestBDD_NearestNeighbors_FusionStrategy_RRF_RewardsMultiColumnHits(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	// Fixture chosen so RRF produces a verifiably different ordering
	// from min-distance:
	//   col_a returns e1=0.1, e2=0.2, e3=0.3         ranks 1,2,3
	//   col_b returns e2=0.05, e3=0.15               ranks 1,2
	//
	// min-distance ranking:
	//   e2=0.05 < e1=0.10 < e3=0.15            -> [e2, e1, e3]
	//
	// RRF (k=60) ranking:
	//   e1 = 1/61            = 0.01639
	//   e2 = 1/62 + 1/61     = 0.03252
	//   e3 = 1/63 + 1/62     = 0.03200
	//   desc -> [e2, e3, e1]
	//
	// The key contrast: e3 (two columns, both worse distances) jumps
	// ahead of e1 (one column, best raw distance) under RRF.
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			"col_a": {
				{PrimaryKey: "e1", Distance: 0.10},
				{PrimaryKey: "e2", Distance: 0.20},
				{PrimaryKey: "e3", Distance: 0.30},
			},
			"col_b": {
				{PrimaryKey: "e2", Distance: 0.05},
				{PrimaryKey: "e3", Distance: 0.15},
			},
		},
	}
	executor.SetVectorStore(store)

	k := 3
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifiers: []objectset.PropertyIdentifier{
			mkPropID("col_a"),
			mkPropID("col_b"),
		},
		NumNeighbors:   &k,
		FusionStrategy: "rrf",
		Query: &objectset.NNQuery{
			Vector: &objectset.VectorQuery{Value: []float64{0.0}},
		},
	}
	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"e2", "e3", "e1"}
	if len(res.PrimaryKeys) != len(want) {
		t.Fatalf("PrimaryKeys len=%d want=%d (%v)", len(res.PrimaryKeys), len(want), res.PrimaryKeys)
	}
	for i, w := range want {
		if res.PrimaryKeys[i] != w {
			t.Errorf("[%d] PrimaryKeys=%q want=%q (full=%v)", i, res.PrimaryKeys[i], w, res.PrimaryKeys)
		}
	}
}

func TestBDD_NearestNeighbors_FusionStrategy_DefaultIsMinDistance(t *testing.T) {
	// Same fixture as above, no FusionStrategy set. Result must match
	// round-49 min-distance fusion: [e2, e1, e3].
	executor, _ := setupExecutorTest(t)
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			"col_a": {
				{PrimaryKey: "e1", Distance: 0.10},
				{PrimaryKey: "e2", Distance: 0.20},
				{PrimaryKey: "e3", Distance: 0.30},
			},
			"col_b": {
				{PrimaryKey: "e2", Distance: 0.05},
				{PrimaryKey: "e3", Distance: 0.15},
			},
		},
	}
	executor.SetVectorStore(store)

	k := 3
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifiers: []objectset.PropertyIdentifier{
			mkPropID("col_a"),
			mkPropID("col_b"),
		},
		NumNeighbors: &k,
		// FusionStrategy intentionally omitted.
		Query: &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.0}}},
	}
	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"e2", "e1", "e3"}
	for i, w := range want {
		if res.PrimaryKeys[i] != w {
			t.Errorf("[%d] PrimaryKeys=%q want=%q (full=%v)", i, res.PrimaryKeys[i], w, res.PrimaryKeys)
		}
	}
}

func TestBDD_NearestNeighbors_FusionStrategy_ExplicitMinMatchesDefault(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			"col_a": {
				{PrimaryKey: "e1", Distance: 0.10},
				{PrimaryKey: "e2", Distance: 0.20},
			},
			"col_b": {
				{PrimaryKey: "e2", Distance: 0.05},
			},
		},
	}
	executor.SetVectorStore(store)

	k := 2
	def := &objectset.Definition{
		Type:      "nearestNeighbors",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		PropertyIdentifiers: []objectset.PropertyIdentifier{
			mkPropID("col_a"),
			mkPropID("col_b"),
		},
		NumNeighbors:   &k,
		FusionStrategy: "min",
		Query:          &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.0}}},
	}
	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// min: e2=0.05, e1=0.10
	want := []string{"e2", "e1"}
	if len(res.PrimaryKeys) != len(want) ||
		res.PrimaryKeys[0] != want[0] || res.PrimaryKeys[1] != want[1] {
		t.Errorf("PrimaryKeys=%v want=%v", res.PrimaryKeys, want)
	}
}

func TestBDD_NearestNeighbors_ValidateRejectsUnknownFusionStrategy(t *testing.T) {
	k := 5
	def := &objectset.Definition{
		Type:      "nearestNeighbors",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		PropertyIdentifiers: []objectset.PropertyIdentifier{
			mkPropID("a"),
			mkPropID("b"),
		},
		NumNeighbors:   &k,
		FusionStrategy: "magic",
		Query:          &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.0}}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected validation error for unknown fusionStrategy")
	}
	if !strings.Contains(err.Error(), "fusionStrategy") {
		t.Errorf("error %q should mention fusionStrategy", err.Error())
	}
}

func TestBDD_NearestNeighbors_FusionStrategy_RRFOnSingleColumnIsHarmless(t *testing.T) {
	// Single column + "rrf" should NOT error at Validate, and must
	// produce a sensible result (the single-column fast path already
	// returns by ascending distance, which equals what RRF would
	// produce with one column ranked).
	executor, _ := setupExecutorTest(t)
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			"only": {
				{PrimaryKey: "e1", Distance: 0.1},
				{PrimaryKey: "e2", Distance: 0.2},
			},
		},
	}
	executor.SetVectorStore(store)

	k := 2
	def := &objectset.Definition{
		Type:                "nearestNeighbors",
		ObjectSet:           &objectset.Definition{Type: "base", ObjectType: "employee"},
		PropertyIdentifiers: []objectset.PropertyIdentifier{mkPropID("only")},
		NumNeighbors:        &k,
		FusionStrategy:      "rrf",
		Query:               &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.0}}},
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("Validate: %v (rrf with single column must be accepted)", err)
	}
	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.PrimaryKeys) != 2 || res.PrimaryKeys[0] != "e1" || res.PrimaryKeys[1] != "e2" {
		t.Errorf("PrimaryKeys=%v want=[e1 e2]", res.PrimaryKeys)
	}
}
