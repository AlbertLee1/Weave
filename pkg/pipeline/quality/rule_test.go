package quality

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestRuleValidate_NotNull(t *testing.T) {
	r := Rule{Name: "email_present", Type: RuleNotNull, Field: "email"}
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid notNull rule, got %v", err)
	}

	bad := Rule{Name: "email_present", Type: RuleNotNull, Field: ""}
	if err := bad.Validate(); err == nil {
		t.Fatalf("expected error for missing field on notNull rule")
	}
}

func TestRuleValidate_Range(t *testing.T) {
	t.Run("min only", func(t *testing.T) {
		r := Rule{Name: "amount", Type: RuleRange, Field: "amount", Min: ptr(0.0)}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("max only", func(t *testing.T) {
		r := Rule{Name: "amount", Type: RuleRange, Field: "amount", Max: ptr(100.0)}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("min > max rejected", func(t *testing.T) {
		r := Rule{Name: "amount", Type: RuleRange, Field: "amount", Min: ptr(10.0), Max: ptr(1.0)}
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for min>max")
		}
	})
	t.Run("min/max both nil rejected", func(t *testing.T) {
		r := Rule{Name: "amount", Type: RuleRange, Field: "amount"}
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for empty range bounds")
		}
	})
}

func TestRuleValidate_Regex(t *testing.T) {
	r := Rule{Name: "email_format", Type: RuleRegex, Field: "email", Pattern: `^[\w.]+@[\w.]+$`}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Rule{Name: "email_format", Type: RuleRegex, Field: "email", Pattern: ""}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for empty pattern")
	}
	worse := Rule{Name: "email_format", Type: RuleRegex, Field: "email", Pattern: "[invalid("}
	if err := worse.Validate(); err == nil {
		t.Fatal("expected error for malformed pattern")
	}
}

func TestRuleValidate_ForeignKey(t *testing.T) {
	r := Rule{Name: "user_ref", Type: RuleForeignKey, Field: "user_id", Lookup: "users"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Rule{Name: "user_ref", Type: RuleForeignKey, Field: "user_id", Lookup: ""}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for missing lookup")
	}
}

func TestRuleValidate_BadName(t *testing.T) {
	cases := []string{"", "1starts-with-digit", strings.Repeat("a", 65), "has spaces"}
	for _, name := range cases {
		r := Rule{Name: name, Type: RuleNotNull, Field: "x"}
		if err := r.Validate(); err == nil {
			t.Errorf("expected invalid name %q to fail Validate", name)
		}
	}
}

func TestRuleValidate_UnknownType(t *testing.T) {
	r := Rule{Name: "x", Type: "garbage", Field: "x"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for unknown rule type")
	}
}

func TestValidateRules_DuplicateName(t *testing.T) {
	rules := []Rule{
		{Name: "x", Type: RuleNotNull, Field: "a"},
		{Name: "x", Type: RuleNotNull, Field: "b"},
	}
	if err := ValidateRules(rules); err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func TestAllRuleTypes_Coverage(t *testing.T) {
	got := AllRuleTypes()
	want := []RuleType{RuleNotNull, RuleRange, RuleUnique, RuleRegex, RuleForeignKey}
	if len(got) != len(want) {
		t.Fatalf("AllRuleTypes returned %d entries, want %d", len(got), len(want))
	}
	for i, kt := range want {
		if got[i] != kt {
			t.Errorf("AllRuleTypes[%d] = %q, want %q", i, got[i], kt)
		}
		if !IsKnownRuleType(kt) {
			t.Errorf("IsKnownRuleType(%q) = false, want true", kt)
		}
	}
	if IsKnownRuleType("notARule") {
		t.Error("IsKnownRuleType returned true for nonsense type")
	}
}
