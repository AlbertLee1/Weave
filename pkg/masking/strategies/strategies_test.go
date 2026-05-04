package strategies

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestHash_String(t *testing.T) {
	got := Hash("alice@example.com")
	sum := sha256.Sum256([]byte("alice@example.com"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("Hash: got %q, want %q", got, want)
	}
}

func TestHash_NonString(t *testing.T) {
	got := Hash(42)
	sum := sha256.Sum256([]byte("42"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("Hash(int): got %q, want %q", got, want)
	}
}

func TestHash_Stable(t *testing.T) {
	a := Hash("x")
	b := Hash("x")
	if a != b {
		t.Fatalf("Hash should be deterministic, got %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("Hash output missing sha256: prefix, got %q", a)
	}
	// "sha256:" + 64 hex chars = 71
	if len(a) != 71 {
		t.Fatalf("Hash unexpected length %d, want 71", len(a))
	}
}

func TestRedact_LiteralTripleStar(t *testing.T) {
	if got := Redact(); got != "***" {
		t.Fatalf("Redact: got %q, want \"***\"", got)
	}
	if RedactReplacement != "***" {
		t.Fatalf("RedactReplacement constant drifted: %q", RedactReplacement)
	}
}

func TestNull_AlwaysNil(t *testing.T) {
	if Null() != nil {
		t.Fatalf("Null() must always return nil")
	}
}

func TestPartial_KeepsFirstAndLastTwo(t *testing.T) {
	cases := map[string]interface{}{
		"":                   "",
		"a":                  "*",
		"ab":                 "**",
		"abc":                "***",
		"abcd":               "****",
		"abcde":              "ab*de",
		"abcdef":             "ab**ef",
		"abcdefgh":           "ab****gh",
		"alice@example.com":  "al*************om",
	}
	for in, want := range cases {
		got := Partial(in)
		if got != want {
			t.Fatalf("Partial(%q): got %v, want %v", in, got, want)
		}
	}
}

func TestPartial_NonString(t *testing.T) {
	// 12345 stringifies to "12345" (5 chars). Keeps first/last 2 → "12*45".
	if got := Partial(12345); got != "12*45" {
		t.Fatalf("Partial(int): got %v, want %q", got, "12*45")
	}
	// Booleans stringify to "true" / "false" (4 / 5 chars).
	if got := Partial(true); got != "****" {
		t.Fatalf("Partial(true): got %v, want %q", got, "****")
	}
	if got := Partial(false); got != "fa*se" {
		t.Fatalf("Partial(false): got %v, want %q", got, "fa*se")
	}
}

func TestApply_Dispatch(t *testing.T) {
	if got := Apply(NameNull, "anything"); got != nil {
		t.Fatalf("Apply NULL: got %v, want nil", got)
	}
	if got := Apply(NameRedact, "secret"); got != "***" {
		t.Fatalf("Apply REDACT: got %v, want \"***\"", got)
	}
	if got := Apply(NamePartial, "abcdefgh"); got != "ab****gh" {
		t.Fatalf("Apply PARTIAL: got %v, want %q", got, "ab****gh")
	}
	got := Apply(NameHash, "secret")
	s, ok := got.(string)
	if !ok || !strings.HasPrefix(s, "sha256:") {
		t.Fatalf("Apply HASH: expected sha256: prefix, got %v", got)
	}
}

func TestApply_NilShortCircuit(t *testing.T) {
	// NULL collapses nil to nil.
	if got := Apply(NameNull, nil); got != nil {
		t.Fatalf("Apply NULL on nil: got %v, want nil", got)
	}
	// Non-NULL strategies pass nil through as nil — no transform on absent values.
	if got := Apply(NameHash, nil); got != nil {
		t.Fatalf("Apply HASH on nil: got %v, want nil", got)
	}
	if got := Apply(NameRedact, nil); got != nil {
		t.Fatalf("Apply REDACT on nil: got %v, want nil", got)
	}
	if got := Apply(NamePartial, nil); got != nil {
		t.Fatalf("Apply PARTIAL on nil: got %v, want nil", got)
	}
}

func TestApply_UnknownStrategyPassesThrough(t *testing.T) {
	if got := Apply(Name("BOGUS"), "value"); got != "value" {
		t.Fatalf("Apply unknown strategy should pass through, got %v", got)
	}
}
