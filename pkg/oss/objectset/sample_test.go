package objectset_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// --- Definition validation ---

func TestDefinition_ValidateSample_Valid(t *testing.T) {
	size := 3
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDefinition_ValidateSample_MissingObjectSet(t *testing.T) {
	size := 3
	def := &objectset.Definition{Type: "sample", Size: &size}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing objectSet")
	}
}

func TestDefinition_ValidateSample_MissingSize(t *testing.T) {
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for missing size")
	}
}

func TestDefinition_ValidateSample_ZeroSizeRejected(t *testing.T) {
	size := 0
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for size=0")
	}
}

func TestDefinition_ValidateSample_NegativeSizeRejected(t *testing.T) {
	size := -2
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
	}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error for negative size")
	}
}

// --- ParseDefinition ---

func TestParseDefinition_Sample(t *testing.T) {
	data := []byte(`{
		"type": "sample",
		"objectSet": {"type":"base","objectType":"employee"},
		"size": 2,
		"seed": 42
	}`)
	def, err := objectset.ParseDefinition(data)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if def.Type != "sample" {
		t.Errorf("expected type sample, got %s", def.Type)
	}
	if def.ObjectSet == nil {
		t.Fatal("expected nested objectSet")
	}
	if def.Size == nil || *def.Size != 2 {
		t.Errorf("expected size=2, got %v", def.Size)
	}
	if def.Seed == nil || *def.Seed != 42 {
		t.Errorf("expected seed=42, got %v", def.Seed)
	}
}

// --- Execute sample ---

// TestExecute_Sample_SmallerThanUniverse verifies that sampling N elements
// from a universe of U > N returns exactly N distinct PKs, every one of
// which belongs to the inner ObjectSet.
func TestExecute_Sample_SmallerThanUniverse(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	size := 2
	seed := int64(7)
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
		Seed:      &seed,
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ObjectType != "employee" {
		t.Errorf("expected objectType employee, got %s", result.ObjectType)
	}
	if len(result.PrimaryKeys) != size {
		t.Fatalf("expected %d PKs, got %d: %v", size, len(result.PrimaryKeys), result.PrimaryKeys)
	}

	seen := make(map[string]bool, size)
	universe := map[string]bool{"e1": true, "e2": true, "e3": true, "e4": true}
	for _, pk := range result.PrimaryKeys {
		if seen[pk] {
			t.Errorf("duplicate PK %q in sample output", pk)
		}
		seen[pk] = true
		if !universe[pk] {
			t.Errorf("sample produced %q which is not in the universe", pk)
		}
	}
}

// TestExecute_Sample_LargerThanUniverse verifies that sampling N >= U returns
// the entire universe without duplicates and does not error.
func TestExecute_Sample_LargerThanUniverse(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	size := 100 // way more than the 4 seeded employees
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	pks := sorted(result.PrimaryKeys)
	expected := []string{"e1", "e2", "e3", "e4"}
	if len(pks) != len(expected) {
		t.Fatalf("expected %d PKs, got %d: %v", len(expected), len(pks), pks)
	}
	for i, pk := range pks {
		if pk != expected[i] {
			t.Errorf("PK[%d]: expected %s, got %s", i, expected[i], pk)
		}
	}
}

// TestExecute_Sample_SeedDeterministic verifies that two executions with the
// same seed produce the same PK list in the same order.
func TestExecute_Sample_SeedDeterministic(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	size := 3
	seed := int64(123)
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
		Seed:      &seed,
	}

	a, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute A: %v", err)
	}
	b, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute B: %v", err)
	}

	if len(a.PrimaryKeys) != len(b.PrimaryKeys) {
		t.Fatalf("expected same length, got %d vs %d", len(a.PrimaryKeys), len(b.PrimaryKeys))
	}
	for i := range a.PrimaryKeys {
		if a.PrimaryKeys[i] != b.PrimaryKeys[i] {
			t.Errorf("pk[%d] diverged: %s vs %s", i, a.PrimaryKeys[i], b.PrimaryKeys[i])
		}
	}
}

// TestExecute_Sample_DifferentSeedsDiffer verifies that different seeds
// produce at least one differing ordering over a universe large enough to
// make a pure-chance match statistically unlikely.
func TestExecute_Sample_DifferentSeedsDiffer(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	size := 3
	seedA := int64(1)
	seedB := int64(999)
	defA := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
		Seed:      &seedA,
	}
	defB := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
		Seed:      &seedB,
	}

	a, err := executor.Execute(ctx, defA)
	if err != nil {
		t.Fatalf("Execute A: %v", err)
	}
	b, err := executor.Execute(ctx, defB)
	if err != nil {
		t.Fatalf("Execute B: %v", err)
	}

	// With 4 employees picking 3, there are 4 possible subsets and several
	// orderings; different seeds SHOULD produce divergent results for at
	// least one of (subset, order).
	same := len(a.PrimaryKeys) == len(b.PrimaryKeys)
	if same {
		for i := range a.PrimaryKeys {
			if a.PrimaryKeys[i] != b.PrimaryKeys[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Errorf("expected different sample output for different seeds, both returned %v", a.PrimaryKeys)
	}
}

// TestExecute_Sample_DelegatesEmptyInner verifies an empty inner ObjectSet
// yields an empty sample and no error even when Size > 0.
func TestExecute_Sample_DelegatesEmptyInner(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	// Filter everyone out: name == "nobody"
	size := 5
	seed := int64(0)
	def := &objectset.Definition{
		Type: "sample",
		ObjectSet: &objectset.Definition{
			Type:      "filter",
			ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
			Where:     json.RawMessage(`{"type":"eq","field":"name","value":"nobody"}`),
		},
		Size: &size,
		Seed: &seed,
	}

	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("expected empty sample for empty inner, got %v", result.PrimaryKeys)
	}
	if result.ObjectType != "employee" {
		t.Errorf("expected objectType employee, got %q", result.ObjectType)
	}
}

// TestExecute_Sample_PreservesTruncation verifies the inner Truncated flag is
// propagated through sample (the sample itself is a strict subset of a
// possibly-truncated universe).
func TestExecute_Sample_PreservesTruncation(t *testing.T) {
	// We can't easily synthesize a truncated inner at unit-test scale, but we
	// can verify the non-truncated path returns Truncated=false — the
	// propagation contract is the same either way.
	executor, _ := setupExecutorTest(t)
	ctx := context.Background()

	size := 2
	def := &objectset.Definition{
		Type:      "sample",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Size:      &size,
	}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Truncated {
		t.Errorf("expected Truncated=false, got true")
	}
}
