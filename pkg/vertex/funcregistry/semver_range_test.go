package funcregistry_test

import (
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/funcregistry"
)

func TestParseSemverRange_Given_Empty_When_Parse_Then_MatchAny(t *testing.T) {
	r, err := funcregistry.ParseSemverRange("")
	if err != nil {
		t.Fatalf("empty range should parse: %v", err)
	}
	v, _ := oms.ParseSemver("9.9.9")
	if !r.Matches(v) {
		t.Errorf("empty range should match anything")
	}
}

func TestParseSemverRange_Given_Star_When_Parse_Then_MatchAny(t *testing.T) {
	r, err := funcregistry.ParseSemverRange("*")
	if err != nil {
		t.Fatalf("'*' should parse: %v", err)
	}
	v, _ := oms.ParseSemver("0.0.1")
	if !r.Matches(v) {
		t.Errorf("'*' should match anything")
	}
}

func TestParseSemverRange_Given_ExactVersion_When_Match_Then_OnlyExact(t *testing.T) {
	r, _ := funcregistry.ParseSemverRange("1.2.3")
	cases := map[string]bool{
		"1.2.3": true,
		"1.2.4": false,
		"1.3.0": false,
		"2.0.0": false,
	}
	for ver, want := range cases {
		v, _ := oms.ParseSemver(ver)
		if got := r.Matches(v); got != want {
			t.Errorf("ParseSemverRange(1.2.3).Matches(%s) = %v, want %v", ver, got, want)
		}
	}
}

func TestParseSemverRange_Given_CaretRange_When_Match_Then_SameMajor(t *testing.T) {
	r, _ := funcregistry.ParseSemverRange("^1.0.0")
	cases := map[string]bool{
		"0.9.9":   false, // below caret floor
		"1.0.0":   true,  // floor inclusive
		"1.0.5":   true,
		"1.5.0":   true,
		"1.99.99": true,
		"2.0.0":   false, // major bump excluded
		"3.0.0":   false,
	}
	for ver, want := range cases {
		v, _ := oms.ParseSemver(ver)
		if got := r.Matches(v); got != want {
			t.Errorf("^1.0.0.Matches(%s) = %v, want %v", ver, got, want)
		}
	}
}

func TestParseSemverRange_Given_CaretRangeWithMinor_When_Match_Then_FloorEnforced(t *testing.T) {
	r, _ := funcregistry.ParseSemverRange("^1.2.3")
	cases := map[string]bool{
		"1.2.2":  false, // below floor
		"1.2.3":  true,
		"1.5.0":  true,
		"1.99.0": true,
		"2.0.0":  false,
	}
	for ver, want := range cases {
		v, _ := oms.ParseSemver(ver)
		if got := r.Matches(v); got != want {
			t.Errorf("^1.2.3.Matches(%s) = %v, want %v", ver, got, want)
		}
	}
}

func TestParseSemverRange_Given_TildeRange_When_Match_Then_SameMinor(t *testing.T) {
	r, _ := funcregistry.ParseSemverRange("~1.2.3")
	cases := map[string]bool{
		"1.2.2": false,
		"1.2.3": true,
		"1.2.9": true,
		"1.3.0": false,
		"2.0.0": false,
	}
	for ver, want := range cases {
		v, _ := oms.ParseSemver(ver)
		if got := r.Matches(v); got != want {
			t.Errorf("~1.2.3.Matches(%s) = %v, want %v", ver, got, want)
		}
	}
}

func TestParseSemverRange_Given_GreaterEqual_When_Match_Then_OnlyEqualOrHigher(t *testing.T) {
	r, _ := funcregistry.ParseSemverRange(">=1.2.3")
	cases := map[string]bool{
		"1.2.2": false,
		"1.2.3": true,
		"2.0.0": true,
		"9.9.9": true,
	}
	for ver, want := range cases {
		v, _ := oms.ParseSemver(ver)
		if got := r.Matches(v); got != want {
			t.Errorf(">=1.2.3.Matches(%s) = %v, want %v", ver, got, want)
		}
	}
}

func TestParseSemverRange_Given_GarbageInput_When_Parse_Then_Error(t *testing.T) {
	for _, s := range []string{"foo", "^abc", "1.x.0", "^1", "~1"} {
		if _, err := funcregistry.ParseSemverRange(s); err == nil {
			t.Errorf("ParseSemverRange(%q) should have errored", s)
		}
	}
}

func TestResolveLatestInRange_Given_MultipleVersions_When_CaretRange_Then_Latest1X(t *testing.T) {
	candidates := []oms.Function{
		{RID: "f1", Name: "hello", Version: "1.0.0"},
		{RID: "f2", Name: "hello", Version: "1.5.0"},
		{RID: "f3", Name: "hello", Version: "1.2.0"},
		{RID: "f4", Name: "hello", Version: "2.0.0"},
		{RID: "f5", Name: "hello", Version: "0.9.0"},
	}
	r, _ := funcregistry.ParseSemverRange("^1.0.0")
	winner, ok := funcregistry.ResolveLatestInRange(r, candidates)
	if !ok {
		t.Fatalf("expected match")
	}
	if winner.Version != "1.5.0" {
		t.Errorf("winner version = %q, want 1.5.0", winner.Version)
	}
	if winner.RID != "f2" {
		t.Errorf("winner RID = %q, want f2", winner.RID)
	}
}

func TestResolveLatestInRange_Given_NoMatch_When_Resolve_Then_FalseReturned(t *testing.T) {
	candidates := []oms.Function{
		{RID: "f1", Name: "hello", Version: "2.0.0"},
		{RID: "f2", Name: "hello", Version: "3.0.0"},
	}
	r, _ := funcregistry.ParseSemverRange("^1.0.0")
	if _, ok := funcregistry.ResolveLatestInRange(r, candidates); ok {
		t.Errorf("expected no match, got one")
	}
}

func TestResolveLatestInRange_Given_EmptyCandidates_When_Resolve_Then_FalseReturned(t *testing.T) {
	r, _ := funcregistry.ParseSemverRange("*")
	if _, ok := funcregistry.ResolveLatestInRange(r, nil); ok {
		t.Errorf("empty candidates should yield no match")
	}
}

func TestResolveLatestInRange_Given_UnparseableCandidateVersion_When_Resolve_Then_Skipped(t *testing.T) {
	candidates := []oms.Function{
		{RID: "bad", Name: "hello", Version: "not-a-semver"},
		{RID: "f1", Name: "hello", Version: "1.0.0"},
	}
	r, _ := funcregistry.ParseSemverRange("^1.0.0")
	winner, ok := funcregistry.ResolveLatestInRange(r, candidates)
	if !ok || winner.RID != "f1" {
		t.Errorf("expected to skip malformed candidate; winner=%+v ok=%v", winner, ok)
	}
}
