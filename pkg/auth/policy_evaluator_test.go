package auth_test

import (
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// mustRules marshals a SecurityPolicyRules struct to a json.RawMessage.
// Panics on error since these are test fixtures.
func mustRules(t *testing.T, r auth.SecurityPolicyRules) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	return b
}

// makePolicy is a small helper to build an OBJECT-scope policy from a rules
// struct. The caller picks the policy type explicitly.
func makePolicy(t *testing.T, policyType string, rules auth.SecurityPolicyRules) oms.SecurityPolicy {
	t.Helper()
	return oms.SecurityPolicy{
		RID:           "ri.ontology.main.security-policy.test",
		ObjectTypeRID: "ri.ontology.main.object-type.test",
		PolicyType:    policyType,
		Rules:         mustRules(t, rules),
	}
}

func TestPolicyEvaluator_NoPolicies_DefaultDeny(t *testing.T) {
	// With no policies at all, default-deny means a non-admin user gets
	// denied even when the object exists.
	eval := auth.NewPolicyEvaluator(nil)
	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	obj := map[string]interface{}{"id": "o1"}

	allow, masks, err := eval.Evaluate(user, obj)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if allow {
		t.Errorf("expected default-deny for empty policy set, got allow")
	}
	if len(masks) != 0 {
		t.Errorf("expected no masks on deny, got %v", masks)
	}
}

func TestPolicyEvaluator_Allow_AdminRoleMatches(t *testing.T) {
	// A single allow policy that matches the admin role should grant access.
	pol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{
			Roles: []string{auth.RoleAdmin},
		},
		Condition: auth.ConditionSpec{Op: "always"},
	})
	eval := auth.NewPolicyEvaluator([]oms.SecurityPolicy{pol})

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleAdmin}}
	obj := map[string]interface{}{"id": "o1"}

	allow, _, err := eval.Evaluate(user, obj)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !allow {
		t.Errorf("expected allow for admin user with matching policy")
	}
}

func TestPolicyEvaluator_Deny_ViewerRoleBlocked(t *testing.T) {
	// Two policies: an allow for viewer and an explicit deny for viewer.
	// Deny must take precedence.
	allowPol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{
			Roles: []string{auth.RoleViewer},
		},
		Condition: auth.ConditionSpec{Op: "always"},
	})
	denyPol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "deny",
		Subjects: auth.SubjectSpec{
			Roles: []string{auth.RoleViewer},
		},
		Condition: auth.ConditionSpec{Op: "always"},
	})
	eval := auth.NewPolicyEvaluator([]oms.SecurityPolicy{allowPol, denyPol})

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	obj := map[string]interface{}{"id": "o1"}

	allow, _, err := eval.Evaluate(user, obj)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if allow {
		t.Errorf("expected deny when an explicit deny policy matches")
	}
}

func TestPolicyEvaluator_AnonymousSubject(t *testing.T) {
	// A policy with Anonymous=true should match a nil user.
	pol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{
			Anonymous: true,
		},
		Condition: auth.ConditionSpec{Op: "always"},
	})
	eval := auth.NewPolicyEvaluator([]oms.SecurityPolicy{pol})

	allow, _, err := eval.Evaluate(nil, map[string]interface{}{"id": "o1"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !allow {
		t.Errorf("expected allow for nil user with Anonymous policy")
	}

	// And the same policy must NOT match an authenticated user.
	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	allow, _, err = eval.Evaluate(user, map[string]interface{}{"id": "o1"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if allow {
		t.Errorf("expected deny for authenticated user against Anonymous-only policy")
	}
}

func TestPolicyEvaluator_PropertyEquals_Condition(t *testing.T) {
	// Policy: allow viewer when classification == "PUBLIC".
	pol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{
			Op:    "propertyEquals",
			Field: "classification",
			Value: "PUBLIC",
		},
	})
	eval := auth.NewPolicyEvaluator([]oms.SecurityPolicy{pol})

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}

	// Public object: should be visible.
	publicObj := map[string]interface{}{"id": "o1", "classification": "PUBLIC"}
	allow, _, err := eval.Evaluate(user, publicObj)
	if err != nil {
		t.Fatalf("Evaluate public: %v", err)
	}
	if !allow {
		t.Errorf("expected allow for PUBLIC object")
	}

	// Restricted object: should be hidden.
	restrictedObj := map[string]interface{}{"id": "o2", "classification": "SECRET"}
	allow, _, err = eval.Evaluate(user, restrictedObj)
	if err != nil {
		t.Fatalf("Evaluate restricted: %v", err)
	}
	if allow {
		t.Errorf("expected deny for SECRET object")
	}
}

func TestPolicyEvaluator_AndOrNot_Conditions(t *testing.T) {
	// AND: both children must be true.
	andPol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{
			Op: "and",
			Children: []auth.ConditionSpec{
				{Op: "propertyEquals", Field: "region", Value: "US"},
				{Op: "propertyEquals", Field: "active", Value: true},
			},
		},
	})
	eval := auth.NewPolicyEvaluator([]oms.SecurityPolicy{andPol})

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}

	matches := map[string]interface{}{"region": "US", "active": true}
	allow, _, _ := eval.Evaluate(user, matches)
	if !allow {
		t.Errorf("expected and-condition to match when both children true")
	}

	noMatch := map[string]interface{}{"region": "EU", "active": true}
	allow, _, _ = eval.Evaluate(user, noMatch)
	if allow {
		t.Errorf("expected and-condition to fail when one child false")
	}

	// OR: at least one child must be true.
	orPol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{
			Op: "or",
			Children: []auth.ConditionSpec{
				{Op: "propertyEquals", Field: "region", Value: "US"},
				{Op: "propertyEquals", Field: "region", Value: "EU"},
			},
		},
	})
	eval = auth.NewPolicyEvaluator([]oms.SecurityPolicy{orPol})

	allow, _, _ = eval.Evaluate(user, map[string]interface{}{"region": "EU"})
	if !allow {
		t.Errorf("expected or-condition to match EU")
	}
	allow, _, _ = eval.Evaluate(user, map[string]interface{}{"region": "APAC"})
	if allow {
		t.Errorf("expected or-condition to fail for APAC")
	}

	// NOT: invert single child.
	notPol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version: 1,
		Effect:  "allow",
		Subjects: auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{
			Op: "not",
			Children: []auth.ConditionSpec{
				{Op: "propertyEquals", Field: "secret", Value: true},
			},
		},
	})
	eval = auth.NewPolicyEvaluator([]oms.SecurityPolicy{notPol})

	allow, _, _ = eval.Evaluate(user, map[string]interface{}{"secret": false})
	if !allow {
		t.Errorf("expected not(secret==true) to match when secret=false")
	}
	allow, _, _ = eval.Evaluate(user, map[string]interface{}{"secret": true})
	if allow {
		t.Errorf("expected not(secret==true) to fail when secret=true")
	}
}

func TestPolicyEvaluator_PropertyMasks_ReturnedOnAllow(t *testing.T) {
	// PROPERTY-scope policies declare propertyMasks. On allow, the returned
	// mask list is the union across all matching property policies.
	maskPol := makePolicy(t, "PROPERTY", auth.SecurityPolicyRules{
		Version:       1,
		Effect:        "allow",
		Subjects:      auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition:     auth.ConditionSpec{Op: "always"},
		PropertyMasks: []string{"salary", "ssn"},
	})
	objPol := makePolicy(t, "OBJECT", auth.SecurityPolicyRules{
		Version:   1,
		Effect:    "allow",
		Subjects:  auth.SubjectSpec{Roles: []string{auth.RoleViewer}},
		Condition: auth.ConditionSpec{Op: "always"},
	})
	eval := auth.NewPolicyEvaluator([]oms.SecurityPolicy{objPol, maskPol})

	user := &auth.User{ID: "u1", Roles: []string{auth.RoleViewer}}
	allow, masks, err := eval.Evaluate(user, map[string]interface{}{"id": "o1"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !allow {
		t.Fatalf("expected allow with object policy")
	}
	if len(masks) != 2 {
		t.Fatalf("expected 2 masks, got %v", masks)
	}
	got := map[string]bool{}
	for _, m := range masks {
		got[m] = true
	}
	if !got["salary"] || !got["ssn"] {
		t.Errorf("expected masks salary and ssn, got %v", masks)
	}
}
