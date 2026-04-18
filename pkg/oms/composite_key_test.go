package oms_test

import (
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-211 ParseCompositeKey: single-PK case must be opaque (the legacy PK
// value may itself contain ':'); multi-PK case must split on ':' and require
// exactly the declared number of non-empty parts.

func TestParseCompositeKey_SinglePKIsOpaque(t *testing.T) {
	// A single-PK ObjectType expects exactly 1 part — no splitting. This
	// preserves the pre-US-211 behaviour where a key may contain colons
	// (e.g. RIDs like ri.ontology.main.ontology.123).
	parts, err := oms.ParseCompositeKey("ri.ontology.main.foo.123", 1)
	if err != nil {
		t.Fatalf("expected no error for opaque single PK, got %v", err)
	}
	if len(parts) != 1 || parts[0] != "ri.ontology.main.foo.123" {
		t.Errorf("expected [ri.ontology.main.foo.123], got %v", parts)
	}
}

func TestParseCompositeKey_SinglePKEmpty(t *testing.T) {
	if _, err := oms.ParseCompositeKey("", 1); err == nil {
		t.Fatal("expected error for empty single PK")
	}
}

func TestParseCompositeKey_CompositeHappyPath(t *testing.T) {
	parts, err := oms.ParseCompositeKey("10248:11", 2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(parts) != 2 || parts[0] != "10248" || parts[1] != "11" {
		t.Errorf("expected [10248 11], got %v", parts)
	}
}

func TestParseCompositeKey_WrongPartCount(t *testing.T) {
	if _, err := oms.ParseCompositeKey("10248", 2); err == nil {
		t.Fatal("expected error for too-few parts")
	}
	_, err := oms.ParseCompositeKey("a:b:c", 2)
	if err == nil {
		t.Fatal("expected error for too-many parts")
	}
	if !strings.Contains(err.Error(), "2 parts") {
		t.Errorf("expected error to mention expected count, got %q", err.Error())
	}
}

func TestParseCompositeKey_RejectsEmptyPart(t *testing.T) {
	if _, err := oms.ParseCompositeKey("10248:", 2); err == nil {
		t.Fatal("expected error for trailing empty part")
	}
	if _, err := oms.ParseCompositeKey(":11", 2); err == nil {
		t.Fatal("expected error for leading empty part")
	}
}

func TestParseCompositeKey_ZeroExpected(t *testing.T) {
	if _, err := oms.ParseCompositeKey("anything", 0); err == nil {
		t.Fatal("expected error when ObjectType has no PK declared")
	}
}

func TestJoinCompositeKey_SingleAndMulti(t *testing.T) {
	if got := oms.JoinCompositeKey([]string{"abc"}); got != "abc" {
		t.Errorf("single-element join should return the lone value, got %q", got)
	}
	if got := oms.JoinCompositeKey([]string{"10248", "11"}); got != "10248:11" {
		t.Errorf("multi join should colon-delimit, got %q", got)
	}
	if got := oms.JoinCompositeKey(nil); got != "" {
		t.Errorf("nil parts should produce empty string, got %q", got)
	}
}

func TestParseCompositeKey_RoundTripSingle(t *testing.T) {
	original := []string{"some:value:with:colons"}
	joined := oms.JoinCompositeKey(original)
	parts, err := oms.ParseCompositeKey(joined, len(original))
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if len(parts) != 1 || parts[0] != original[0] {
		t.Errorf("round-trip lost data: original=%v joined=%q parts=%v", original, joined, parts)
	}
}

func TestParseCompositeKey_RoundTripMulti(t *testing.T) {
	original := []string{"10248", "11", "42"}
	joined := oms.JoinCompositeKey(original)
	if joined != "10248:11:42" {
		t.Fatalf("unexpected joined form %q", joined)
	}
	parts, err := oms.ParseCompositeKey(joined, len(original))
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if len(parts) != len(original) {
		t.Errorf("part count mismatch: %v vs %v", original, parts)
	}
	for i := range original {
		if parts[i] != original[i] {
			t.Errorf("part[%d] mismatch: got %q want %q", i, parts[i], original[i])
		}
	}
}

// US-211 ObjectType.EffectivePrimaryKeys / IsCompositeKey: the model exposes
// a single canonical accessor regardless of which storage column populated
// the value, so downstream code never has to branch on "did the row predate
// migration 000037?".

func TestObjectType_EffectivePrimaryKeys_LegacySingle(t *testing.T) {
	ot := &oms.ObjectType{PrimaryKey: "employeeId"}
	pks := ot.EffectivePrimaryKeys()
	if len(pks) != 1 || pks[0] != "employeeId" {
		t.Errorf("expected [employeeId], got %v", pks)
	}
	if ot.IsCompositeKey() {
		t.Error("single-element key is not composite")
	}
}

func TestObjectType_EffectivePrimaryKeys_PreferComposite(t *testing.T) {
	ot := &oms.ObjectType{
		PrimaryKey:  "orderId",
		PrimaryKeys: []string{"orderId", "lineNumber"},
	}
	pks := ot.EffectivePrimaryKeys()
	if len(pks) != 2 || pks[0] != "orderId" || pks[1] != "lineNumber" {
		t.Errorf("expected [orderId lineNumber], got %v", pks)
	}
	if !ot.IsCompositeKey() {
		t.Error("two-element key should report composite")
	}
}

func TestObjectType_EffectivePrimaryKeys_Empty(t *testing.T) {
	ot := &oms.ObjectType{}
	if pks := ot.EffectivePrimaryKeys(); pks != nil {
		t.Errorf("expected nil for fully-unset PKs, got %v", pks)
	}
	if ot.IsCompositeKey() {
		t.Error("no PK declared cannot be composite")
	}
}
