package security

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// TestEqualityRule — Red test for US-043: the eq rule type compiles into a
// bleve TermQuery that narrows results to objects whose ObjectProperty value
// matches the caller's user attribute.
func TestEqualityRule(t *testing.T) {
	ot := oms.ObjectType{
		RID:     "ri.ontology.main.object-type.employee",
		APIName: "Employee",
	}

	policy := Policy{
		RID:           "ri.ontology.main.security-policy.dept-match",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeEq,
			UserAttr:       "dept",
			ObjectProperty: "owner_dept",
		}},
	}

	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{policy})

	user := &auth.User{
		ID: "u-alice",
		Attributes: map[string]any{
			"dept": "ENG",
		},
	}

	q, err := engine.Evaluate(context.Background(), user, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if q == nil {
		t.Fatal("Evaluate returned a nil query")
	}

	tq, ok := q.(*query.TermQuery)
	if !ok {
		t.Fatalf("expected *query.TermQuery, got %T", q)
	}
	if got, want := tq.Term, "ENG"; got != want {
		t.Errorf("TermQuery.Term = %q, want %q", got, want)
	}
	if got, want := tq.FieldVal, "owner_dept"; got != want {
		t.Errorf("TermQuery.Field = %q, want %q", got, want)
	}
}

// TestEqualityRule_NoPolicies — when no policies are registered for the
// object type the engine must return a MatchAllQuery so callers can unconditionally
// AND-combine the result into the query pipeline.
func TestEqualityRule_NoPolicies(t *testing.T) {
	engine := NewEngine()
	q, err := engine.Evaluate(context.Background(), &auth.User{ID: "u-bob"}, oms.ObjectType{
		RID:     "ri.ontology.main.object-type.order",
		APIName: "Order",
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchAllQuery); !ok {
		t.Fatalf("expected *query.MatchAllQuery for un-policied type, got %T", q)
	}
}

// TestEqualityRule_MissingUserAttr — when the user lacks the referenced
// attribute the engine must return a MatchNoneQuery so the caller sees zero
// rows rather than falling through to an un-filtered AND clause.
func TestEqualityRule_MissingUserAttr(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.employee", APIName: "Employee"}
	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p1",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeEq,
			UserAttr:       "dept",
			ObjectProperty: "owner_dept",
		}},
	}})

	q, err := engine.Evaluate(context.Background(), &auth.User{ID: "u-attrless"}, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchNoneQuery); !ok {
		t.Fatalf("expected *query.MatchNoneQuery when user attribute missing, got %T", q)
	}
}

// TestInRule — Red test for US-044: the `in` rule type accepts a user
// attribute that holds a list of values, and compiles into a Bleve boolean
// query whose Should clauses are TermQuery(value, field=objectProperty) for
// each user value, MinShould=1. Semantically this expresses
// "user.attr ∈ object.objectProperty" when object.objectProperty is a
// single-value keyword field, or "user.attr ∩ object.objectProperty ≠ ∅"
// when the field is multi-valued.
func TestInRule(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.document", APIName: "Document"}
	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p-in",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeIn,
			UserAttr:       "departments",
			ObjectProperty: "allowed_departments",
		}},
	}})

	user := &auth.User{
		ID: "u-alice",
		Attributes: map[string]any{
			"departments": []any{"ENG", "SEC"},
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
	if got, want := shouldTerms, []string{"ENG", "SEC"}; !stringSliceEq(got, want) {
		t.Errorf("should terms = %v, want %v", got, want)
	}
	for _, f := range shouldFields {
		if f != "allowed_departments" {
			t.Errorf("should clause field = %q, want %q", f, "allowed_departments")
		}
	}
}

// TestInRule_MissingAttribute — fail-closed semantics: when the user lacks
// the list attribute the `in` rule compiles to MatchNone so zero rows are
// returned rather than the un-filtered chain leaking through.
func TestInRule_MissingAttribute(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.document", APIName: "Document"}
	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p-in",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeIn,
			UserAttr:       "departments",
			ObjectProperty: "allowed_departments",
		}},
	}})

	q, err := engine.Evaluate(context.Background(), &auth.User{ID: "u-attrless"}, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchNoneQuery); !ok {
		t.Fatalf("expected *query.MatchNoneQuery when list attribute missing, got %T", q)
	}
}

// TestMarkingSubsetRule — Red test for US-044: the `markingSubset` rule type
// expresses "object.markings ⊆ user.markings". User markings are sourced from
// user.Attributes["markings"] (which US-059 will populate from JWT claims).
// The compiler emits a Bleve BooleanQuery whose Should clauses are
// TermQuery(m, field=objectProperty) for each marking the user holds. The
// caller's query pipeline AND-combines this clause so that only objects whose
// marking is one the user possesses remain in the result set.
func TestMarkingSubsetRule(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.report", APIName: "Report"}
	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p-ms",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeMarkingSubset,
			ObjectProperty: "marking",
		}},
	}})

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
		t.Errorf("should terms = %v, want %v", got, want)
	}
	for _, f := range shouldFields {
		if f != "marking" {
			t.Errorf("should clause field = %q, want %q", f, "marking")
		}
	}
}

// TestMarkingSubsetRule_NoUserMarkings — fail-closed semantics: a user with
// no markings at all cannot see any objects governed by a markingSubset rule.
func TestMarkingSubsetRule_NoUserMarkings(t *testing.T) {
	ot := oms.ObjectType{RID: "ri.ontology.main.object-type.report", APIName: "Report"}
	engine := NewEngine()
	engine.SetPolicies(ot.RID, []Policy{{
		RID:           "p-ms",
		ObjectTypeRID: ot.RID,
		PolicyType:    PolicyTypeObject,
		Rules: []Rule{{
			Type:           RuleTypeMarkingSubset,
			ObjectProperty: "marking",
		}},
	}})

	q, err := engine.Evaluate(context.Background(), &auth.User{ID: "u-marklessly"}, ot)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if _, ok := q.(*query.MatchNoneQuery); !ok {
		t.Fatalf("expected *query.MatchNoneQuery when user has no markings, got %T", q)
	}
}

// TestInRule_JSONRoundtrip — the `in` rule shape must survive through the
// JSONB rules column without schema changes.
func TestInRule_JSONRoundtrip(t *testing.T) {
	raw := []byte(`{"type":"in","userAttr":"departments","objectProperty":"allowed_departments"}`)
	var r Rule
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Type != RuleTypeIn || r.UserAttr != "departments" || r.ObjectProperty != "allowed_departments" {
		t.Fatalf("unexpected round-tripped rule: %+v", r)
	}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("json marshal mismatch:\n got: %s\nwant: %s", out, raw)
	}
}

// TestMarkingSubsetRule_JSONRoundtrip — the `markingSubset` rule shape must
// survive through the JSONB rules column. Note the omitempty tag on UserAttr
// keeps the serialised form minimal since this rule type reads user.markings
// implicitly rather than via a named user attribute.
func TestMarkingSubsetRule_JSONRoundtrip(t *testing.T) {
	raw := []byte(`{"type":"markingSubset","objectProperty":"marking"}`)
	var r Rule
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Type != RuleTypeMarkingSubset || r.ObjectProperty != "marking" || r.UserAttr != "" {
		t.Fatalf("unexpected round-tripped rule: %+v", r)
	}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("json marshal mismatch:\n got: %s\nwant: %s", out, raw)
	}
}

// extractBooleanShouldTerms returns the term strings and field names of every
// TermQuery Should clause on bq, preserving order. Any non-TermQuery should
// clause triggers t.Fatalf.
func extractBooleanShouldTerms(t *testing.T, bq *query.BooleanQuery) ([]string, []string) {
	t.Helper()
	dj, ok := bq.Should.(*query.DisjunctionQuery)
	if !ok {
		t.Fatalf("Boolean.Should is not a DisjunctionQuery: %T", bq.Should)
	}
	terms := make([]string, 0, len(dj.Disjuncts))
	fields := make([]string, 0, len(dj.Disjuncts))
	for i, sub := range dj.Disjuncts {
		tq, ok := sub.(*query.TermQuery)
		if !ok {
			t.Fatalf("should clause %d is not a TermQuery: %T", i, sub)
		}
		terms = append(terms, tq.Term)
		fields = append(fields, tq.FieldVal)
	}
	return terms, fields
}

func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRule_JSONRoundtrip — security_policies.rules is a JSONB column; the Rule
// type must (de)serialise with tag names that match the DSL spelled out in the
// story (`type`, `userAttr`, `objectProperty`).
func TestRule_JSONRoundtrip(t *testing.T) {
	raw := []byte(`{"type":"eq","userAttr":"dept","objectProperty":"owner_dept"}`)
	var r Rule
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Type != RuleTypeEq || r.UserAttr != "dept" || r.ObjectProperty != "owner_dept" {
		t.Fatalf("unexpected round-tripped rule: %+v", r)
	}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("json marshal mismatch:\n got: %s\nwant: %s", out, raw)
	}
}
