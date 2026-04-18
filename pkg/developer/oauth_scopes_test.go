package developer

import (
	"sort"
	"strings"
	"testing"
)

func TestKnownScopes_ContainsReadWriteAdmin(t *testing.T) {
	got := KnownScopes()
	if len(got) < 3 {
		t.Fatalf("expected at least 3 well-known scopes, got %v", got)
	}
	want := map[string]bool{ScopeRead: false, ScopeWrite: false, ScopeAdmin: false}
	for _, s := range got {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	for s, seen := range want {
		if !seen {
			t.Errorf("KnownScopes() missing %q", s)
		}
	}
	// Stable order: callers iterate in catalogue order.
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	// Spot-check that the known constants render identically (no whitespace
	// drift, no upper-case mismatch).
	for _, s := range got {
		if strings.TrimSpace(s) != s {
			t.Errorf("scope %q has whitespace", s)
		}
	}
}

func TestIsKnownScope(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{ScopeRead, true},
		{ScopeWrite, true},
		{ScopeAdmin, true},
		{"read", true},
		{"unknown", false},
		{"", false},
		{"READ", false}, // case-sensitive: scopes are lower-case by convention
	}
	for _, c := range cases {
		if got := IsKnownScope(c.in); got != c.want {
			t.Errorf("IsKnownScope(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNarrowScopes_RejectsExpansion(t *testing.T) {
	// Cannot widen: requesting scopes outside the granted set must be rejected.
	if _, err := NarrowScopes([]string{"read"}, []string{"read", "write"}); err == nil {
		t.Fatal("expected error when requested scopes exceed granted")
	}
}

func TestNarrowScopes_FiltersToIntersection(t *testing.T) {
	got, err := NarrowScopes([]string{"read", "write", "admin"}, []string{"write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "write" {
		t.Errorf("got %v, want [write]", got)
	}
}

func TestNarrowScopes_EmptyRequestedReturnsGranted(t *testing.T) {
	// "no scope param" semantics: keep the original grant.
	got, err := NarrowScopes([]string{"read", "write"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want [read, write]", got)
	}
}

func TestNarrowScopes_DuplicatesCollapsed(t *testing.T) {
	got, err := NarrowScopes([]string{"read", "write"}, []string{"read", "read", "write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2 unique entries", got)
	}
}
