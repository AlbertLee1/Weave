package objectset

import (
	"reflect"
	"testing"
)

// US-382: applyDerivedExcludedItems mirrors engine.applyExcludedItems for the
// derived-field aggregation path. Same semantics: dedup input, ignore blank
// ids and out-of-scope ids, and report the count of PKs that actually
// belonged to the resolved ObjectSet.

func TestApplyDerivedExcludedItems_NoExclusion(t *testing.T) {
	pks := []string{"a", "b", "c"}
	out, count := applyDerivedExcludedItems(pks, nil)
	if !reflect.DeepEqual(out, pks) {
		t.Errorf("pks: got %v want %v", out, pks)
	}
	if count != 0 {
		t.Errorf("count: got %d want 0", count)
	}
}

func TestApplyDerivedExcludedItems_RemovesAndCounts(t *testing.T) {
	pks := []string{"a", "b", "c", "d"}
	out, count := applyDerivedExcludedItems(pks, []string{"b", "d"})
	if !reflect.DeepEqual(out, []string{"a", "c"}) {
		t.Errorf("pks: got %v want [a c]", out)
	}
	if count != 2 {
		t.Errorf("count: got %d want 2", count)
	}
}

func TestApplyDerivedExcludedItems_IgnoresOutOfScopeAndDuplicates(t *testing.T) {
	pks := []string{"a", "b"}
	// "a" duplicated, "" blank, "z" not in pks — only "a" matches.
	out, count := applyDerivedExcludedItems(pks, []string{"a", "a", "", "z"})
	if !reflect.DeepEqual(out, []string{"b"}) {
		t.Errorf("pks: got %v want [b]", out)
	}
	if count != 1 {
		t.Errorf("count: got %d want 1", count)
	}
}

func TestApplyDerivedExcludedItems_AllBlankIsNoOp(t *testing.T) {
	pks := []string{"a", "b"}
	out, count := applyDerivedExcludedItems(pks, []string{"", ""})
	if !reflect.DeepEqual(out, pks) {
		t.Errorf("pks: got %v want unchanged", out)
	}
	if count != 0 {
		t.Errorf("count: got %d want 0", count)
	}
}
