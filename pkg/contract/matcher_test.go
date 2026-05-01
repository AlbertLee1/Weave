package contract

import (
	"strings"
	"testing"
)

func TestMatchBody_ExactDeepEquality(t *testing.T) {
	expected := mustJSON(`{"status":"ok","count":3}`)
	actual := mustJSON(`{"status":"ok","count":3}`)
	if errs := MatchBody(expected, actual, nil, false); len(errs) != 0 {
		t.Errorf("expected match, got: %v", errs)
	}
}

func TestMatchBody_ActualMayHaveExtraKeysWhenNotStrict(t *testing.T) {
	expected := mustJSON(`{"status":"ok"}`)
	actual := mustJSON(`{"status":"ok","extra":"field"}`)
	if errs := MatchBody(expected, actual, nil, false); len(errs) != 0 {
		t.Errorf("expected forward-compat match, got: %v", errs)
	}
}

func TestMatchBody_StrictRejectsExtraKeys(t *testing.T) {
	expected := mustJSON(`{"status":"ok"}`)
	actual := mustJSON(`{"status":"ok","extra":"field"}`)
	errs := MatchBody(expected, actual, nil, true)
	if len(errs) == 0 {
		t.Fatal("strict mode should reject extra keys")
	}
}

func TestMatchBody_MissingExpectedKeyFails(t *testing.T) {
	expected := mustJSON(`{"status":"ok","count":3}`)
	actual := mustJSON(`{"status":"ok"}`)
	errs := MatchBody(expected, actual, nil, false)
	if len(errs) == 0 {
		t.Fatal("missing key should fail")
	}
	if !strings.Contains(errs[0].Error(), "count") {
		t.Errorf("error should mention path: %v", errs[0])
	}
}

func TestMatchBody_TypeMatcherAcceptsAnyValueOfType(t *testing.T) {
	expected := mustJSON(`{"id":"placeholder"}`)
	actual := mustJSON(`{"id":"actual-uuid-here"}`)
	matchers := map[string]MatcherRule{
		"$.id": {Match: "type", Value: "string"},
	}
	if errs := MatchBody(expected, actual, matchers, false); len(errs) != 0 {
		t.Errorf("type matcher should accept any string: %v", errs)
	}
}

func TestMatchBody_TypeMatcherRejectsWrongType(t *testing.T) {
	expected := mustJSON(`{"id":"placeholder"}`)
	actual := mustJSON(`{"id":42}`)
	matchers := map[string]MatcherRule{
		"$.id": {Match: "type", Value: "string"},
	}
	errs := MatchBody(expected, actual, matchers, false)
	if len(errs) == 0 {
		t.Fatal("type matcher should reject wrong type")
	}
}

func TestMatchBody_RegexMatcher(t *testing.T) {
	expected := mustJSON(`{"code":"placeholder"}`)
	matchers := map[string]MatcherRule{
		"$.code": {Match: "regex", Value: "^[A-Z]{2}[0-9]+$"},
	}
	actual := mustJSON(`{"code":"AB123"}`)
	if errs := MatchBody(expected, actual, matchers, false); len(errs) != 0 {
		t.Errorf("regex matcher should accept AB123: %v", errs)
	}
	bad := mustJSON(`{"code":"ab123"}`)
	if errs := MatchBody(expected, bad, matchers, false); len(errs) == 0 {
		t.Fatal("regex matcher should reject ab123")
	}
}

func TestMatchBody_PresenceMatcher(t *testing.T) {
	expected := mustJSON(`{"createdAt":""}`)
	matchers := map[string]MatcherRule{
		"$.createdAt": {Match: "presence"},
	}
	actual := mustJSON(`{"createdAt":"2026-05-02T10:00:00Z"}`)
	if errs := MatchBody(expected, actual, matchers, false); len(errs) != 0 {
		t.Errorf("presence matcher should accept any value: %v", errs)
	}
	missing := mustJSON(`{"other":"x"}`)
	if errs := MatchBody(expected, missing, matchers, false); len(errs) == 0 {
		t.Fatal("presence matcher should reject missing key")
	}
}

func TestMatchBody_NestedPath(t *testing.T) {
	expected := mustJSON(`{"data":{"nested":{"field":"x"}}}`)
	actual := mustJSON(`{"data":{"nested":{"field":"different"}}}`)
	matchers := map[string]MatcherRule{
		"$.data.nested.field": {Match: "type", Value: "string"},
	}
	if errs := MatchBody(expected, actual, matchers, false); len(errs) != 0 {
		t.Errorf("nested matcher should pass: %v", errs)
	}
}

func TestMatchBody_ArrayIndexPath(t *testing.T) {
	expected := mustJSON(`{"items":[{"id":"x"}]}`)
	actual := mustJSON(`{"items":[{"id":"abc"}]}`)
	matchers := map[string]MatcherRule{
		"$.items.0.id": {Match: "type", Value: "string"},
	}
	if errs := MatchBody(expected, actual, matchers, false); len(errs) != 0 {
		t.Errorf("array index matcher should pass: %v", errs)
	}
}

func TestMatchBody_IgnoreMatcherSkipsPath(t *testing.T) {
	expected := mustJSON(`{"keep":"a","skip":"b"}`)
	actual := mustJSON(`{"keep":"a","skip":"COMPLETELY DIFFERENT"}`)
	matchers := map[string]MatcherRule{
		"$.skip": {Match: "ignore"},
	}
	if errs := MatchBody(expected, actual, matchers, false); len(errs) != 0 {
		t.Errorf("ignore matcher should skip path: %v", errs)
	}
}

func TestMatchBody_TypeMatcherSupportedTypes(t *testing.T) {
	cases := []struct {
		name  string
		typ   string
		value string
		ok    bool
	}{
		{"string-ok", "string", `"x"`, true},
		{"string-bad", "string", `42`, false},
		{"number-int", "number", `42`, true},
		{"number-float", "number", `3.14`, true},
		{"number-string", "number", `"x"`, false},
		{"integer-ok", "integer", `42`, true},
		{"integer-float", "integer", `3.14`, false},
		{"boolean-ok", "boolean", `true`, true},
		{"boolean-bad", "boolean", `1`, false},
		{"array-ok", "array", `[1,2]`, true},
		{"array-bad", "array", `{}`, false},
		{"object-ok", "object", `{}`, true},
		{"object-bad", "object", `[]`, false},
		{"null-ok", "null", `null`, true},
		{"null-bad", "null", `0`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expected := mustJSON(`{"f":"placeholder"}`)
			actual := mustJSON(`{"f":` + c.value + `}`)
			matchers := map[string]MatcherRule{
				"$.f": {Match: "type", Value: c.typ},
			}
			errs := MatchBody(expected, actual, matchers, false)
			if c.ok && len(errs) != 0 {
				t.Errorf("expected match, got %v", errs)
			}
			if !c.ok && len(errs) == 0 {
				t.Errorf("expected mismatch, got pass")
			}
		})
	}
}
