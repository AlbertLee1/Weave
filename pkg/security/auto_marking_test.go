package security

import (
	"context"
	"testing"

	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// TestEvaluate_AutoMarkingClause_NoOtherPolicies — US-051 Red:
// when an ObjectType is registered as markings-enabled, Evaluate must
// automatically append a marking-subset clause against the reserved
// `_markings` field even if no explicit OBJECT-scope policy rows exist.
func TestEvaluate_AutoMarkingClause_NoOtherPolicies(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.report", APIName: "Report"}
	engine := NewEngine()
	engine.SetMarkingsEnabled(ot.RID, true)

	user := &auth.User{
		ID: "u-cleared",
		Attributes: map[string]any{
			"markings": []any{"ACME", "TOP_SECRET"},
		},
	}

	q, err := engine.Evaluate(context.Background(), user, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	bq, ok := q.(*query.BooleanQuery)
	if !ok {
		t.Fatalf("expected *query.BooleanQuery, got %T", q)
	}
	shouldTerms, shouldFields := extractBooleanShouldTerms(t, bq)
	if got, want := shouldTerms, []string{"ACME", "TOP_SECRET"}; !stringSliceEq(got, want) {
		t.Errorf("auto-marking should terms = %v, want %v", got, want)
	}
	for i, f := range shouldFields {
		if f != MarkingField {
			t.Errorf("should clause %d field = %q, want %q", i, f, MarkingField)
		}
	}
}

// TestEvaluate_AutoMarkingClause_ConjunctionWithEq — US-051 Red:
// when the ObjectType has BOTH an explicit eq rule AND markings enabled,
// Evaluate must return a ConjunctionQuery whose clauses are the eq
// TermQuery AND the auto-appended marking BooleanQuery.
func TestEvaluate_AutoMarkingClause_ConjunctionWithEq(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.employee", APIName: "Employee"}
	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p-eq",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeEq,
			UserAttr:       "dept",
			ObjectProperty: "owner_dept",
		}},
	}})
	engine.SetMarkingsEnabled(ot.RID, true)

	user := &auth.User{
		ID: "u-alice",
		Attributes: map[string]any{
			"dept":     "ENG",
			"markings": []any{"ACME"},
		},
	}

	q, err := engine.Evaluate(context.Background(), user, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	conj, ok := q.(*query.ConjunctionQuery)
	if !ok {
		t.Fatalf("expected *query.ConjunctionQuery, got %T", q)
	}
	if got := len(conj.Conjuncts); got != 2 {
		t.Fatalf("Conjunction has %d clauses, want 2", got)
	}
	// Clause 1: eq TermQuery on owner_dept.
	tq, ok := conj.Conjuncts[0].(*query.TermQuery)
	if !ok {
		t.Fatalf("clause 0 is %T, want *query.TermQuery", conj.Conjuncts[0])
	}
	if tq.FieldVal != "owner_dept" || tq.Term != "ENG" {
		t.Errorf("clause 0 = (%s=%s), want (owner_dept=ENG)", tq.FieldVal, tq.Term)
	}
	// Clause 2: auto marking BooleanQuery on _markings.
	bq, ok := conj.Conjuncts[1].(*query.BooleanQuery)
	if !ok {
		t.Fatalf("clause 1 is %T, want *query.BooleanQuery", conj.Conjuncts[1])
	}
	shouldTerms, shouldFields := extractBooleanShouldTerms(t, bq)
	if got, want := shouldTerms, []string{"ACME"}; !stringSliceEq(got, want) {
		t.Errorf("auto-marking should terms = %v, want %v", got, want)
	}
	if shouldFields[0] != MarkingField {
		t.Errorf("auto-marking field = %q, want %q", shouldFields[0], MarkingField)
	}
}

// TestEvaluate_AutoMarkingClause_UserWithoutMarkings — US-051 Red:
// fail-closed semantics. A user with zero markings in Attributes must see
// MatchNone when the ObjectType is markings-enabled, even when there is
// no other policy in play. The whole Evaluate result collapses to
// MatchNone (not ConjunctionQuery{..., MatchNone}).
func TestEvaluate_AutoMarkingClause_UserWithoutMarkings(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.report", APIName: "Report"}
	engine := NewEngine()
	engine.SetMarkingsEnabled(ot.RID, true)

	q, err := engine.Evaluate(context.Background(), &auth.User{ID: "u-none"}, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchNoneQuery); !ok {
		t.Fatalf("expected MatchNoneQuery when markings-enabled user lacks markings, got %T", q)
	}
}

// TestEvaluate_AutoMarkingClause_DisabledType — regression guard:
// ObjectTypes that never call SetMarkingsEnabled continue to return
// MatchAllQuery when no other policy is registered. Un-marked back-compat
// traffic must not pick up a marking clause.
func TestEvaluate_AutoMarkingClause_DisabledType(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.plain", APIName: "Plain"}
	engine := NewEngine()

	q, err := engine.Evaluate(context.Background(), &auth.User{
		ID:         "u-noop",
		Attributes: map[string]any{"markings": []any{"ACME"}},
	}, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchAllQuery); !ok {
		t.Fatalf("expected MatchAllQuery for un-policied, markings-disabled type, got %T", q)
	}
}

// TestEvaluate_AutoMarkingClause_Disable — SetMarkingsEnabled(..., false)
// must remove a previously-registered RID from the marking-enabled set so
// callers can turn off markings without tearing down the Engine.
func TestEvaluate_AutoMarkingClause_Disable(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.toggle", APIName: "Toggle"}
	engine := NewEngine()
	engine.SetMarkingsEnabled(ot.RID, true)
	engine.SetMarkingsEnabled(ot.RID, false)

	q, err := engine.Evaluate(context.Background(), &auth.User{
		ID:         "u-cleared",
		Attributes: map[string]any{"markings": []any{"ACME"}},
	}, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchAllQuery); !ok {
		t.Fatalf("expected MatchAllQuery after disabling markings, got %T", q)
	}
}
