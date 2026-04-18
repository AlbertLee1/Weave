package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/rls"
)

// TestRowPolicy_FiltersListAndSearch exercises the US-256 row_policies
// integration on ServiceImpl. An EU-only policy applied to employee for
// role=eu-reader narrows the visible set to rows whose deptId is "d1"
// (stand-in for the EU dept in the fixture). Non-matching roles flow
// through unchanged because the policy does not apply to them.
func TestRowPolicy_FiltersListAndSearch(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := rls.NewMemoryStore()
	_ = store.Create(ctx, &rls.RowPolicy{
		RID:           "ri.rls.main.row-policy.d1-only",
		ObjectTypeRID: ot.RID,
		Predicate:     json.RawMessage(`{"type":"eq","field":"deptId","value":"d1"}`),
		AppliesTo:     rls.AppliesTo{Roles: []string{"eu-reader"}},
	})
	engine := rls.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	svc.SetRowPolicyEngine(engine)

	// Caller 1: holds the role — sees only d1 rows (emp1, emp2).
	eu := &auth.User{ID: "u:eu", Roles: []string{"eu-reader"}}
	ctxEU := auth.WithUser(ctx, eu)
	page, err := svc.ListObjects(ctxEU, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects eu: %v", err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("eu caller: expected 2 rows (d1), got %d", len(page.Data))
	}
	for _, o := range page.Data {
		if o.Properties["deptId"] != "d1" {
			t.Fatalf("eu caller: expected deptId=d1, got %v", o.Properties["deptId"])
		}
	}

	// Caller 2: role does not match the policy's AppliesTo — flow through
	// unchanged (permissive: policies that don't apply add no filter).
	other := &auth.User{ID: "u:other", Roles: []string{"viewer"}}
	ctxOther := auth.WithUser(ctx, other)
	page, err = svc.ListObjects(ctxOther, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects other: %v", err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("non-matching caller: expected 3 rows (no filter), got %d", len(page.Data))
	}

	// Caller 3: admin bypass.
	admin := &auth.User{ID: "u:admin", Roles: []string{auth.RoleAdmin}}
	ctxAdmin := auth.WithUser(ctx, admin)
	page, err = svc.ListObjects(ctxAdmin, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects admin: %v", err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("admin caller: expected 3 rows (bypass), got %d", len(page.Data))
	}
}

// TestRowPolicy_GetHidesDeniedObject verifies that GetObject returns
// ErrNotFound for a row that falls outside the policy's predicate. Hiding
// existence (rather than 403) is the same choice pkg/security makes for
// the rule-based engine.
func TestRowPolicy_GetHidesDeniedObject(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := rls.NewMemoryStore()
	_ = store.Create(ctx, &rls.RowPolicy{
		RID:           "ri.rls.main.row-policy.d1-only",
		ObjectTypeRID: ot.RID,
		Predicate:     json.RawMessage(`{"type":"eq","field":"deptId","value":"d1"}`),
		AppliesTo:     rls.AppliesTo{Roles: []string{"eu-reader"}},
	})
	engine := rls.New(store, nil)
	_ = engine.Reload(ctx)
	svc.SetRowPolicyEngine(engine)

	eu := &auth.User{ID: "u:eu", Roles: []string{"eu-reader"}}
	ctxEU := auth.WithUser(ctx, eu)

	// emp3 is in d2 → policy rejects, returns ErrNotFound.
	if _, err := svc.GetObject(ctxEU, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp3",
	}); err != oms.ErrNotFound {
		t.Fatalf("GetObject emp3: expected ErrNotFound (policy deny), got %v", err)
	}

	// emp1 is in d1 → policy permits.
	if _, err := svc.GetObject(ctxEU, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	}); err != nil {
		t.Fatalf("GetObject emp1: expected success, got %v", err)
	}
}
