package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/security"
)

// TestPolicyAppliedToLoadSearch verifies that ServiceImpl merges the
// pkg/security policy engine query into every Bleve read (ListObjects +
// SearchObjects + GetObject). The engine is wired via SetPolicyEngine; a
// single eq rule on "deptId" scopes the caller to their own department.
//
// Fixture: 3 employees (emp1 d1, emp2 d1, emp3 d2). Caller sits in d1.
//   - ListObjects  -> emp1, emp2
//   - SearchObjects(where name=alice) -> emp1
//   - SearchObjects(where name=charlie) -> 0 rows (policy-denied even though
//     the where clause matches the doc)
//   - GetObject(emp3)  -> ErrNotFound (policy denies; existence hidden)
//   - GetObject(emp1)  -> success
func TestPolicyAppliedToLoadSearch(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)

	// Build a policy engine with an eq rule on deptId and wire it onto the
	// service. The ObjectType RID for "employee" is seeded by setupOSSTest.
	ot, err := repo.GetObjectTypeByAPIName(context.Background(), testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}
	engine := security.NewEngine()
	engine.SetPolicies(ot.RID, []security.Policy{{
		RID:           "ri.ontology.main.security-policy.dept-scope",
		ObjectTypeRID: ot.RID,
		PolicyType:    security.PolicyTypeObject,
		Rules: []security.Rule{{
			Type:           security.RuleTypeEq,
			UserAttr:       "deptId",
			ObjectProperty: "deptId",
		}},
	}})
	svc.SetPolicyEngine(engine)

	user := &auth.User{
		ID:         "u1",
		Roles:      []string{auth.RoleViewer},
		Attributes: map[string]any{"deptId": "d1"},
	}
	ctx := auth.WithUser(context.Background(), user)

	// ---- ListObjects: policy trims the match-all query to d1 only. ----
	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("ListObjects: expected 2 policy-visible objects, got %d", len(page.Data))
	}
	for _, o := range page.Data {
		if got := o.Properties["deptId"]; got != "d1" {
			t.Errorf("ListObjects: expected deptId=d1, got %v", got)
		}
	}

	// ---- SearchObjects (matches row inside policy): alice ok. ----
	page, err = svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "name",
			Value: json.RawMessage(`"alice"`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects alice: %v", err)
	}
	if len(page.Data) != 1 || !primaryKeyEquals(page.Data[0].PrimaryKey, "emp1") {
		t.Fatalf("SearchObjects alice: expected [emp1], got %+v", primaryKeys(page.Data))
	}

	// ---- SearchObjects (matches row OUTSIDE policy): charlie denied. ----
	page, err = svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "name",
			Value: json.RawMessage(`"charlie"`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects charlie: %v", err)
	}
	if len(page.Data) != 0 {
		t.Fatalf("SearchObjects charlie: expected 0 rows (policy deny), got %+v", primaryKeys(page.Data))
	}

	// ---- GetObject: denied target hidden with ErrNotFound. ----
	if _, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp3",
	}); err != oms.ErrNotFound {
		t.Fatalf("GetObject emp3: expected ErrNotFound (policy deny), got %v", err)
	}

	obj, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject emp1: %v", err)
	}
	if !primaryKeyEquals(obj.PrimaryKey, "emp1") {
		t.Errorf("GetObject emp1: expected emp1, got %v", obj.PrimaryKey)
	}
}

// TestPolicyAppliedToLoadSearch_FailClosed verifies that a user missing the
// referenced attribute is denied every row (the engine compiles to
// MatchNone). Without policy-query merging the match-all Bleve query would
// still return 3 rows.
func TestPolicyAppliedToLoadSearch_FailClosed(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)

	ot, err := repo.GetObjectTypeByAPIName(context.Background(), testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}
	engine := security.NewEngine()
	engine.SetPolicies(ot.RID, []security.Policy{{
		RID:           "ri.ontology.main.security-policy.dept-scope",
		ObjectTypeRID: ot.RID,
		PolicyType:    security.PolicyTypeObject,
		Rules: []security.Rule{{
			Type:           security.RuleTypeEq,
			UserAttr:       "deptId",
			ObjectProperty: "deptId",
		}},
	}})
	svc.SetPolicyEngine(engine)

	// No Attributes["deptId"] -> compilePolicies returns MatchNone.
	ctx := auth.WithUser(context.Background(), &auth.User{
		ID:    "u2",
		Roles: []string{auth.RoleViewer},
	})

	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(page.Data) != 0 {
		t.Fatalf("expected fail-closed (0 rows), got %d", len(page.Data))
	}
}

func primaryKeys(objs []*oss.WireObject) []interface{} {
	out := make([]interface{}, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.PrimaryKey)
	}
	return out
}

func primaryKeyEquals(pk interface{}, want string) bool {
	switch v := pk.(type) {
	case string:
		return v == want
	default:
		return false
	}
}
