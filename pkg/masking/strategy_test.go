package masking

import (
	"strings"
	"testing"
)

func TestNormalizeStrategy_LowercaseAndSpaces(t *testing.T) {
	cases := map[MaskStrategy]MaskStrategy{
		"redact":     MaskStrategyRedact,
		"  HASH  ":   MaskStrategyHash,
		"NULL":       MaskStrategyNull,
		"PartiAl":    MaskStrategyPartial,
		"":           "",
		"unknown":    "",
		"REPLACE":    "",
	}
	for in, want := range cases {
		got := NormalizeStrategy(in)
		if got != want {
			t.Fatalf("NormalizeStrategy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStrategyFromRule_AndBack(t *testing.T) {
	pairs := []struct {
		rule     MaskRule
		strategy MaskStrategy
	}{
		{MaskRuleHash, MaskStrategyHash},
		{MaskRuleRedact, MaskStrategyRedact},
		{MaskRulePartial, MaskStrategyPartial},
	}
	for _, p := range pairs {
		if got := StrategyFromRule(p.rule); got != p.strategy {
			t.Fatalf("StrategyFromRule(%q) = %q, want %q", p.rule, got, p.strategy)
		}
		if got := RuleFromStrategy(p.strategy); got != p.rule {
			t.Fatalf("RuleFromStrategy(%q) = %q, want %q", p.strategy, got, p.rule)
		}
	}
	if got := RuleFromStrategy(MaskStrategyNull); got != "" {
		t.Fatalf("RuleFromStrategy(NULL) should be empty (no rule equivalent), got %q", got)
	}
}

func TestApplyMaskStrategy_NULL(t *testing.T) {
	if got := ApplyMaskStrategy(MaskStrategyNull, "anything"); got != nil {
		t.Fatalf("NULL should rewrite to nil, got %v", got)
	}
	if got := ApplyMaskStrategy(MaskStrategyNull, nil); got != nil {
		t.Fatalf("NULL on nil should still be nil, got %v", got)
	}
	if got := ApplyMaskStrategy(MaskStrategyNull, 12345); got != nil {
		t.Fatalf("NULL on number should be nil, got %v", got)
	}
}

func TestApplyMaskStrategy_HASH(t *testing.T) {
	got := ApplyMaskStrategy(MaskStrategyHash, "secret")
	s, ok := got.(string)
	if !ok || !strings.HasPrefix(s, "sha256:") {
		t.Fatalf("HASH expected sha256: prefix, got %v", got)
	}
}

func TestApplyMaskStrategy_REDACTAndPARTIAL(t *testing.T) {
	// US-433: REDACT emits "***", PARTIAL keeps first/last 2 chars.
	if got := ApplyMaskStrategy(MaskStrategyRedact, "anything"); got != "***" {
		t.Fatalf("REDACT: expected ***, got %v", got)
	}
	if got := ApplyMaskStrategy(MaskStrategyPartial, "abcdefgh"); got != "ab****gh" {
		t.Fatalf("PARTIAL: expected ab****gh, got %v", got)
	}
}

func TestApplyStrategyTransforms_SkipsAbsentKeys(t *testing.T) {
	props := map[string]interface{}{"name": "Alice", "ssn": "123-45-6789"}
	transforms := map[string]MaskStrategy{
		"ssn":     MaskStrategyHash,
		"unknown": MaskStrategyRedact,
	}
	ApplyStrategyTransforms(props, transforms)
	if props["name"] != "Alice" {
		t.Fatalf("name should be untouched, got %v", props["name"])
	}
	if s, ok := props["ssn"].(string); !ok || !strings.HasPrefix(s, "sha256:") {
		t.Fatalf("ssn should be hashed, got %v", props["ssn"])
	}
	if _, exists := props["unknown"]; exists {
		t.Fatalf("missing-key transform should not add to props")
	}
}

func TestApplyStrategyTransforms_NULLClearsValue(t *testing.T) {
	props := map[string]interface{}{"email": "alice@ex.com"}
	ApplyStrategyTransforms(props, map[string]MaskStrategy{"email": MaskStrategyNull})
	if props["email"] != nil {
		t.Fatalf("NULL strategy should clear value, got %v", props["email"])
	}
}
