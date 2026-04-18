package masking

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestApplyMaskRule_Hash_String(t *testing.T) {
	got := ApplyMaskRule(MaskRuleHash, "alice@example.com")
	sum := sha256.Sum256([]byte("alice@example.com"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("hash: got %q, want %q", got, want)
	}
}

func TestApplyMaskRule_Hash_NonString(t *testing.T) {
	got := ApplyMaskRule(MaskRuleHash, 42)
	sum := sha256.Sum256([]byte("42"))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("hash(int): got %v, want %s", got, want)
	}
}

func TestApplyMaskRule_Redact_String(t *testing.T) {
	got := ApplyMaskRule(MaskRuleRedact, "secret")
	if got != "[REDACTED]" {
		t.Fatalf("redact: got %v, want [REDACTED]", got)
	}
}

func TestApplyMaskRule_Redact_Nil(t *testing.T) {
	// Already nil should remain nil — nothing to redact.
	got := ApplyMaskRule(MaskRuleRedact, nil)
	if got != nil {
		t.Fatalf("redact(nil): got %v, want nil", got)
	}
}

func TestApplyMaskRule_Partial_Email(t *testing.T) {
	got := ApplyMaskRule(MaskRulePartial, "alice@example.com")
	s, ok := got.(string)
	if !ok {
		t.Fatalf("partial: expected string, got %T", got)
	}
	// Keep first/last visible; mask the middle.
	if !strings.HasPrefix(s, "a") {
		t.Fatalf("partial: expected prefix 'a', got %q", s)
	}
	if !strings.HasSuffix(s, "m") {
		t.Fatalf("partial: expected suffix 'm' (last char of 'com'), got %q", s)
	}
	if !strings.Contains(s, "*") {
		t.Fatalf("partial: expected stars in middle, got %q", s)
	}
}

func TestApplyMaskRule_Partial_ShortString(t *testing.T) {
	// Strings of length <= 2 get fully masked so no visible bytes leak.
	got := ApplyMaskRule(MaskRulePartial, "ab")
	if got != "**" {
		t.Fatalf("partial(short): got %v, want **", got)
	}
}

func TestApplyMaskRule_Partial_Empty(t *testing.T) {
	got := ApplyMaskRule(MaskRulePartial, "")
	if got != "" {
		t.Fatalf("partial(empty): expected empty string, got %v", got)
	}
}

func TestApplyMaskRule_UnknownRule_PassThrough(t *testing.T) {
	got := ApplyMaskRule("bogus", "value")
	if got != "value" {
		t.Fatalf("unknown rule should pass value through unchanged, got %v", got)
	}
}
