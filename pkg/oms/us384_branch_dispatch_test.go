package oms

import (
	"context"
	"encoding/json"
	"testing"
)

func jsonMarshalSafe(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// US-384: branch_id column on action_types / functions enables independent
// versions per branch. The unit tests below cover the package-local
// helpers that drive the routing decision; PG round-trip coverage lives
// in pg_repository_us384_test.go (integration build tag).

func TestNormalizeBranchID(t *testing.T) {
	cases := map[string]string{
		"":         DefaultBranch,
		"main":     "main",
		"feature":  "feature",
		"feat-foo": "feat-foo",
	}
	for in, want := range cases {
		got := NormalizeBranchID(in)
		if got != want {
			t.Errorf("NormalizeBranchID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBranchScopeFromContext_DefaultsToMain(t *testing.T) {
	if got := BranchScopeFromContext(context.Background()); got != DefaultBranch {
		t.Errorf("BranchScopeFromContext(empty ctx) = %q, want %q", got, DefaultBranch)
	}
}

func TestWithBranchScope_RoundTrips(t *testing.T) {
	ctx := WithBranchScope(context.Background(), "feature-x")
	if got := BranchScopeFromContext(ctx); got != "feature-x" {
		t.Errorf("BranchScopeFromContext = %q, want %q", got, "feature-x")
	}
}

func TestWithBranchScope_EmptyAndMainAreNoOps(t *testing.T) {
	for _, in := range []string{"", DefaultBranch} {
		ctx := WithBranchScope(context.Background(), in)
		if got := BranchScopeFromContext(ctx); got != DefaultBranch {
			t.Errorf("WithBranchScope(%q) leaked %q, want %q", in, got, DefaultBranch)
		}
	}
}

func TestPreferBranchFunctions_SuppressesMainDuplicate(t *testing.T) {
	in := []Function{
		{Name: "fn", Version: "1.0.0", BranchID: "main"},
		{Name: "fn", Version: "2.0.0", BranchID: "main"},
		{Name: "fn", Version: "2.0.0", BranchID: "feature"},
	}
	out := preferBranchFunctions(in, "feature")
	// 2.0.0/main should be suppressed; 1.0.0/main inherited; 2.0.0/feature kept.
	wantSet := map[string]bool{"1.0.0|main": true, "2.0.0|feature": true}
	if len(out) != len(wantSet) {
		t.Fatalf("preferBranchFunctions returned %d rows, want %d: %#v", len(out), len(wantSet), out)
	}
	for _, fn := range out {
		key := fn.Version + "|" + fn.BranchID
		if !wantSet[key] {
			t.Errorf("unexpected entry %s in result", key)
		}
	}
}

func TestPreferBranchFunctions_NoOpForMainScope(t *testing.T) {
	in := []Function{
		{Name: "fn", Version: "1.0.0", BranchID: "main"},
		{Name: "fn", Version: "2.0.0", BranchID: "feature"},
	}
	out := preferBranchFunctions(in, DefaultBranch)
	if len(out) != len(in) {
		t.Errorf("main-scope filter dropped rows: got %d, want %d", len(out), len(in))
	}
}

func TestPreferBranchFunctions_NoBranchOverride_KeepsMain(t *testing.T) {
	in := []Function{
		{Name: "fn", Version: "1.0.0", BranchID: "main"},
		{Name: "fn", Version: "2.0.0", BranchID: "main"},
	}
	out := preferBranchFunctions(in, "feature")
	if len(out) != 2 {
		t.Errorf("expected fall-through to keep both main rows, got %d", len(out))
	}
}

func TestPreferBranchFunctionsByName_KeyedOnNameAndVersion(t *testing.T) {
	in := []Function{
		{Name: "alpha", Version: "1.0.0", BranchID: "main"},
		{Name: "alpha", Version: "1.0.0", BranchID: "feature"},
		{Name: "beta", Version: "1.0.0", BranchID: "main"},
		{Name: "beta", Version: "2.0.0", BranchID: "main"},
		{Name: "beta", Version: "2.0.0", BranchID: "feature"},
	}
	out := preferBranchFunctionsByName(in, "feature")
	wantKeys := map[string]bool{
		"alpha@1.0.0|feature": true,
		"beta@1.0.0|main":     true,
		"beta@2.0.0|feature":  true,
	}
	if len(out) != len(wantKeys) {
		t.Fatalf("got %d rows, want %d: %#v", len(out), len(wantKeys), out)
	}
	for _, fn := range out {
		key := fn.Name + "@" + fn.Version + "|" + fn.BranchID
		if !wantKeys[key] {
			t.Errorf("unexpected entry %s", key)
		}
	}
}

func TestActionType_BranchIDJSONOmitemptyOnDefault(t *testing.T) {
	at := ActionType{APIName: "createOrder", Status: "ACTIVE"}
	raw, err := jsonMarshalSafe(at)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if contains(raw, `"branchId"`) {
		t.Errorf("BranchID empty should omit from JSON; got %s", raw)
	}
}

func TestActionType_BranchIDJSONIncludedWhenSet(t *testing.T) {
	at := ActionType{APIName: "createOrder", Status: "ACTIVE", BranchID: "feature-x"}
	raw, err := jsonMarshalSafe(at)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !contains(raw, `"branchId":"feature-x"`) {
		t.Errorf("expected branchId in JSON, got %s", raw)
	}
}

func TestFunction_BranchIDJSONIncludedWhenSet(t *testing.T) {
	fn := Function{Name: "compute", Version: "1.0.0", BranchID: "feature-x"}
	raw, err := jsonMarshalSafe(fn)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !contains(raw, `"branchId":"feature-x"`) {
		t.Errorf("expected branchId in JSON, got %s", raw)
	}
}

