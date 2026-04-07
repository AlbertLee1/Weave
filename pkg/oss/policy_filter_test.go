package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// buildPolicy is a tiny constructor for SecurityPolicy fixtures used by the
// filter tests. The rules JSON is built from the typed schema so a typo in
// the structural shape fails the test rather than being silently accepted.
func buildPolicy(t *testing.T, objectTypeRID, policyType string, rules auth.SecurityPolicyRules) oms.SecurityPolicy {
	t.Helper()
	b, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	return oms.SecurityPolicy{
		RID:           "ri.ontology.main.security-policy.test",
		ObjectTypeRID: objectTypeRID,
		PolicyType:    policyType,
		Rules:         b,
	}
}

func TestFilterObjects_AdminBypass(t *testing.T) {
	// Admin role short-circuits the filter and never calls the repo.
	repo := newMockOmsRepo()
	filter := oss.NewPolicyFilter(repo)

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleAdmin}}
	objs := []*oss.WireObject{
		oss.FormatObject("employee", "e1", map[string]interface{}{"name": "alice"}),
		oss.FormatObject("employee", "e2", map[string]interface{}{"name": "bob"}),
	}

	out, err := filter.FilterObjects(context.Background(), user, testOntologyRID, "employee", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("expected admin to see all 2 objects, got %d", len(out))
	}
}

func TestFilterObjects_DropsDeniedObjects(t *testing.T) {
	// Policy: allow viewer when classification == "PUBLIC".
	// Two objects, one PUBLIC, one SECRET. The SECRET one must be dropped.
	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.doc",
		OntologyRID: testOntologyRID,
		APIName:     "doc",
		PrimaryKey:  "id",
	})
	repo.securityPolicies["ri.ontology.main.object-type.doc"] = []oms.SecurityPolicy{
		buildPolicy(t, "ri.ontology.main.object-type.doc", "OBJECT", auth.SecurityPolicyRules{
			Version:  1,
			Effect:   "allow",
			Subjects: auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
			Condition: auth.ConditionSpec{
				Op:    "propertyEquals",
				Field: "classification",
				Value: "PUBLIC",
			},
		}),
	}

	filter := oss.NewPolicyFilter(repo)
	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	objs := []*oss.WireObject{
		oss.FormatObject("doc", "d1", map[string]interface{}{"id": "d1", "classification": "PUBLIC"}),
		oss.FormatObject("doc", "d2", map[string]interface{}{"id": "d2", "classification": "SECRET"}),
	}

	out, err := filter.FilterObjects(context.Background(), user, testOntologyRID, "doc", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 object after filtering, got %d", len(out))
	}
	if out[0].PrimaryKey != "d1" {
		t.Errorf("expected d1 to remain, got %v", out[0].PrimaryKey)
	}
}

func TestFilterObjects_RedactsMaskedProperties(t *testing.T) {
	// PROPERTY-scope mask: salary is hidden from viewers.
	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: testOntologyRID,
		APIName:     "employee",
		PrimaryKey:  "employeeId",
	})
	repo.securityPolicies["ri.ontology.main.object-type.employee"] = []oms.SecurityPolicy{
		buildPolicy(t, "ri.ontology.main.object-type.employee", "OBJECT", auth.SecurityPolicyRules{
			Version:   1,
			Effect:    "allow",
			Subjects:  auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
			Condition: auth.ConditionSpec{Op: "always"},
		}),
		buildPolicy(t, "ri.ontology.main.object-type.employee", "PROPERTY", auth.SecurityPolicyRules{
			Version:       1,
			Effect:        "allow",
			Subjects:      auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
			Condition:     auth.ConditionSpec{Op: "always"},
			PropertyMasks: []string{"salary"},
		}),
	}

	filter := oss.NewPolicyFilter(repo)
	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	objs := []*oss.WireObject{
		oss.FormatObject("employee", "e1", map[string]interface{}{
			"employeeId": "e1",
			"name":       "alice",
			"salary":     float64(100000),
		}),
	}

	out, err := filter.FilterObjects(context.Background(), user, testOntologyRID, "employee", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 object, got %d", len(out))
	}
	if _, ok := out[0].Properties["salary"]; ok {
		t.Errorf("expected salary to be redacted, got %v", out[0].Properties["salary"])
	}
	if out[0].Properties["name"] != "alice" {
		t.Errorf("expected non-masked field 'name' to be retained, got %v", out[0].Properties["name"])
	}
}

func TestFilterObjects_EmptyList(t *testing.T) {
	// An empty input slice is returned as-is regardless of policies.
	repo := newMockOmsRepo()
	filter := oss.NewPolicyFilter(repo)
	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}

	out, err := filter.FilterObjects(context.Background(), user, testOntologyRID, "employee", nil)
	if err != nil {
		t.Fatalf("FilterObjects nil: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(out))
	}

	out, err = filter.FilterObjects(context.Background(), user, testOntologyRID, "employee", []*oss.WireObject{})
	if err != nil {
		t.Fatalf("FilterObjects empty: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(out))
	}
}

func TestFilterObjects_NoPolicies(t *testing.T) {
	// When ListSecurityPolicies returns an empty slice, the filter is a no-op
	// and ALL objects pass through. This preserves backwards compat for
	// ObjectTypes that have never had a policy attached.
	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.employee",
		OntologyRID: testOntologyRID,
		APIName:     "employee",
		PrimaryKey:  "employeeId",
	})

	filter := oss.NewPolicyFilter(repo)
	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	objs := []*oss.WireObject{
		oss.FormatObject("employee", "e1", map[string]interface{}{"employeeId": "e1"}),
		oss.FormatObject("employee", "e2", map[string]interface{}{"employeeId": "e2"}),
	}

	out, err := filter.FilterObjects(context.Background(), user, testOntologyRID, "employee", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("expected pass-through behavior with no policies, got %d", len(out))
	}
}
