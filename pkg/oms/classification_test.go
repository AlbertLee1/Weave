package oms_test

import (
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

func TestKnownClassifications_Order(t *testing.T) {
	want := []string{"Public", "Internal", "Confidential", "PII", "Secret"}
	got := oms.KnownClassifications()
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("KnownClassifications()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIsKnownClassification(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true}, // unspecified is allowed
		{"Public", true},
		{"Internal", true},
		{"Confidential", true},
		{"PII", true},
		{"Secret", true},
		{"public", false}, // case-sensitive
		{"TopSecret", false},
		{"pii", false},
		{" Secret", false},
	}
	for _, c := range cases {
		if got := oms.IsKnownClassification(c.in); got != c.want {
			t.Errorf("IsKnownClassification(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
