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
