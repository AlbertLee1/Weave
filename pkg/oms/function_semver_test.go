package oms_test

import (
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

func TestParseSemver_HappyPath(t *testing.T) {
	cases := []struct {
		in       string
		expected oms.Semver
	}{
		{"0.0.0", oms.Semver{}},
		{"1.0.0", oms.Semver{Major: 1}},
		{"2.10.30", oms.Semver{Major: 2, Minor: 10, Patch: 30}},
		{"1.2.3-beta", oms.Semver{Major: 1, Minor: 2, Patch: 3, Pre: "beta"}},
		{"10.0.0", oms.Semver{Major: 10}},
	}
	for _, c := range cases {
		got, err := oms.ParseSemver(c.in)
		if err != nil {
			t.Fatalf("ParseSemver(%q) returned err: %v", c.in, err)
		}
		if got != c.expected {
			t.Errorf("ParseSemver(%q) = %+v, want %+v", c.in, got, c.expected)
		}
	}
}

func TestParseSemver_Rejects(t *testing.T) {
	bad := []string{"", "1", "1.0", "1.0.0.0", "a.b.c", "-1.0.0", "1..0"}
	for _, in := range bad {
		if _, err := oms.ParseSemver(in); err == nil {
			t.Errorf("ParseSemver(%q): expected error, got nil", in)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.10.0", "1.2.0", 1}, // numeric, not lexical
		{"1.0.10", "1.0.2", 1},
		{"1.0.0-beta", "1.0.0", -1},      // pre-release < normal
		{"1.0.0", "1.0.0-beta", 1},       // normal > pre-release
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"10.0.0", "2.0.0", 1},           // anti-lexical: "10" > "2"
	}
	for _, c := range cases {
		av, _ := oms.ParseSemver(c.a)
		bv, _ := oms.ParseSemver(c.b)
		got := oms.CompareSemver(av, bv)
		if got != c.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSortFunctionsByVersionDesc(t *testing.T) {
	in := []oms.Function{
		{Name: "b", Version: "1.0.0"},
		{Name: "a", Version: "2.0.0"},
		{Name: "a", Version: "10.0.0"},
		{Name: "a", Version: "1.0.0"},
		{Name: "b", Version: "2.5.0"},
	}
	oms.SortFunctionsByVersionDesc(in)
	want := []struct {
		Name, Version string
	}{
		{"a", "10.0.0"},
		{"a", "2.0.0"},
		{"a", "1.0.0"},
		{"b", "2.5.0"},
		{"b", "1.0.0"},
	}
	if len(in) != len(want) {
		t.Fatalf("len mismatch")
	}
	for i, w := range want {
		if in[i].Name != w.Name || in[i].Version != w.Version {
			t.Errorf("position %d: got (%q, %q), want (%q, %q)",
				i, in[i].Name, in[i].Version, w.Name, w.Version)
		}
	}
}

func TestSortFunctionsByVersionDesc_UnparseableSinksToBottom(t *testing.T) {
	in := []oms.Function{
		{Name: "a", Version: "garbage"},
		{Name: "a", Version: "1.0.0"},
		{Name: "a", Version: "2.0.0"},
	}
	oms.SortFunctionsByVersionDesc(in)
	if in[0].Version != "2.0.0" || in[1].Version != "1.0.0" || in[2].Version != "garbage" {
		t.Fatalf("unexpected order: %+v", in)
	}
}
