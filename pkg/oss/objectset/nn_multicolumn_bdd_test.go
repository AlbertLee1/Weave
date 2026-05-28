package objectset_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestBDD_NearestNeighbors_MultiColumn covers PRD-V2 Gap-Q4 close-out
// (round 49). Foundry's nearestNeighbors lets callers specify multiple
// vector columns and fuses the results so e.g. an Employee with two
// embedding columns (resumeEmbedding + skillsEmbedding) can be searched
// across both at once. Before this round Weave only accepted a single
// PropertyIdentifier — round 49 adds the plural PropertyIdentifiers
// field and a min-distance fusion strategy so a PK that is "close" on
// any one column floats to the top.
//
// Scenarios:
//   - Single-column path remains unchanged (regression).
//   - Two-column query runs one store call per column and the fused
//     result keeps the best (lowest) distance per PK.
//   - K is honored on the fused output, not per-column.
//   - Specifying BOTH the singular AND plural fields is rejected at
//     Validate() time so the call shape is unambiguous.
//   - Neither field set is rejected (existing behavior preserved).
type fusionFakeVectorStore struct {
	// matchesByProperty maps a property API name to the matches the
	// store should return for that property. Lets a single fake serve
	// many per-column queries.
	matchesByProperty map[string][]objectset.NearestNeighborMatch
	// Calls captures the property API name of every dispatched query,
	// in order, so tests can assert per-column dispatch happened.
	Calls []string
}

func (f *fusionFakeVectorStore) FindNearestNeighbors(_ context.Context, q objectset.NNVectorQuery) ([]objectset.NearestNeighborMatch, error) {
	f.Calls = append(f.Calls, q.PropertyAPIName)
	out := f.matchesByProperty[q.PropertyAPIName]
	// Copy so subsequent test runs cannot mutate the fixture.
	cp := make([]objectset.NearestNeighborMatch, len(out))
	copy(cp, out)
	return cp, nil
}

func TestBDD_NearestNeighbors_MultiColumn_FusesByMinDistance(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			// resumeEmbedding: e1 wins, e2 a distant second
			"resumeEmbedding": {
				{PrimaryKey: "e1", Distance: 0.10},
				{PrimaryKey: "e2", Distance: 0.40},
			},
			// skillsEmbedding: e3 wins, but e2 is much closer here than
			// on resumeEmbedding — min-distance fusion should pick its
			// skillsEmbedding score so e2 ranks ahead of e1's 0.10.
			"skillsEmbedding": {
				{PrimaryKey: "e3", Distance: 0.05},
				{PrimaryKey: "e2", Distance: 0.08},
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
			mkPropID("resumeEmbedding"),
			mkPropID("skillsEmbedding"),
		},
		NumNeighbors: &k,
		Query: &objectset.NNQuery{
			Vector: &objectset.VectorQuery{Value: []float64{0.1, 0.2, 0.3}},
		},
	}

	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// One store call per column, in declared order.
	if len(store.Calls) != 2 || store.Calls[0] != "resumeEmbedding" || store.Calls[1] != "skillsEmbedding" {
		t.Errorf("Calls = %v, want [resumeEmbedding skillsEmbedding]", store.Calls)
	}
	// Expected fused ranking by min distance:
	//   e3 = 0.05 (skillsEmbedding)
	//   e2 = 0.08 (skillsEmbedding — beats its 0.40 resumeEmbedding)
	//   e1 = 0.10 (resumeEmbedding)
	want := []string{"e3", "e2", "e1"}
	if len(res.PrimaryKeys) != len(want) {
		t.Fatalf("PrimaryKeys len = %d, want %d (%v)", len(res.PrimaryKeys), len(want), res.PrimaryKeys)
	}
	for i, w := range want {
		if res.PrimaryKeys[i] != w {
			t.Errorf("[%d] PrimaryKeys = %q, want %q (full=%v)", i, res.PrimaryKeys[i], w, res.PrimaryKeys)
		}
	}
}

func TestBDD_NearestNeighbors_MultiColumn_RespectsK(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			"a": {
				{PrimaryKey: "e1", Distance: 0.10},
				{PrimaryKey: "e2", Distance: 0.20},
			},
			"b": {
				{PrimaryKey: "e3", Distance: 0.15},
				{PrimaryKey: "e4", Distance: 0.30},
			},
		},
	}
	executor.SetVectorStore(store)

	k := 2
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifiers: []objectset.PropertyIdentifier{
			mkPropID("a"),
			mkPropID("b"),
		},
		NumNeighbors: &k,
		Query: &objectset.NNQuery{
			Vector: &objectset.VectorQuery{Value: []float64{0.0}},
		},
	}
	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// 4 PKs across columns but K=2 → keep best two: e1=0.10, e3=0.15.
	want := []string{"e1", "e3"}
	if len(res.PrimaryKeys) != len(want) {
		t.Fatalf("PrimaryKeys len = %d, want %d (%v)", len(res.PrimaryKeys), len(want), res.PrimaryKeys)
	}
	for i, w := range want {
		if res.PrimaryKeys[i] != w {
			t.Errorf("[%d] PrimaryKeys = %q, want %q", i, res.PrimaryKeys[i], w)
		}
	}
}

func TestBDD_NearestNeighbors_MultiColumn_PluralWithSingleEntryWorks(t *testing.T) {
	// A list with one entry should behave exactly like the singular
	// path — no fusion logic should kick in, just one store call.
	executor, _ := setupExecutorTest(t)
	store := &fusionFakeVectorStore{
		matchesByProperty: map[string][]objectset.NearestNeighborMatch{
			"embedding": {{PrimaryKey: "e1", Distance: 0.1}},
		},
	}
	executor.SetVectorStore(store)

	k := 1
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifiers: []objectset.PropertyIdentifier{mkPropID("embedding")},
		NumNeighbors:        &k,
		Query: &objectset.NNQuery{
			Vector: &objectset.VectorQuery{Value: []float64{0.1}},
		},
	}
	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(store.Calls) != 1 || store.Calls[0] != "embedding" {
		t.Errorf("Calls = %v, want [embedding]", store.Calls)
	}
	if len(res.PrimaryKeys) != 1 || res.PrimaryKeys[0] != "e1" {
		t.Errorf("PrimaryKeys = %v, want [e1]", res.PrimaryKeys)
	}
}

func TestBDD_NearestNeighbors_ValidateRejectsBothSingularAndPlural(t *testing.T) {
	k := 5
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{Property: struct {
			APIName string `json:"apiName"`
		}{APIName: "a"}},
		PropertyIdentifiers: []objectset.PropertyIdentifier{mkPropID("b")},
		NumNeighbors:        &k,
		Query:               &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.0}}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected validation error when both singular and plural are set")
	}
	if !strings.Contains(err.Error(), "propertyIdentifier") {
		t.Errorf("error %q should mention propertyIdentifier", err.Error())
	}
}

func TestBDD_NearestNeighbors_ValidateRejectsNeitherSingularNorPlural(t *testing.T) {
	k := 5
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		NumNeighbors: &k,
		Query:        &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.0}}},
	}
	err := def.Validate()
	if err == nil {
		t.Fatal("expected validation error when neither singular nor plural is set")
	}
}

// mkPropID is a brevity helper so the table-style tests don't have to
// repeat the anonymous-struct literal at every call site.
func mkPropID(apiName string) objectset.PropertyIdentifier {
	return objectset.PropertyIdentifier{Property: struct {
		APIName string `json:"apiName"`
	}{APIName: apiName}}
}
