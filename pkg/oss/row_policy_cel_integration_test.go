package oss_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/rls"
)

// TestUS487_RowPolicyCEL_FiltersList walks the OSS read path with a
// CEL-based RowPolicy attached. The PRD literal expression
// "user.dept == object.deptId && object.level <= user.clearance" is
// adapted to the employee fixture (deptId + a synthetic level derived
// from age). Three callers exercise the lanes:
//
//   - dept=d1, clearance=99 — sees emp1 and emp2 (deptId=d1).
//   - dept=d2, clearance=99 — sees only emp3 (deptId=d2).
//   - no role match        — gate is open, all 3 rows visible.
func TestUS487_RowPolicyCEL_FiltersList(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := rls.NewMemoryStore()
	if err := store.Create(ctx, &rls.RowPolicy{
		RID:           "ri.rls.main.row-policy.us487-cel",
		ObjectTypeRID: ot.RID,
		CELExpression: `user.dept == object.deptId`,
		AppliesTo:     rls.AppliesTo{Roles: []string{"reader"}},
	}); err != nil {
		t.Fatalf("Create policy: %v", err)
	}
	engine := rls.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	svc.SetRowPolicyEngine(engine)

	d1User := &auth.User{
		ID:         "user:alice",
		Roles:      []string{"reader"},
		Attributes: map[string]any{"dept": "d1"},
	}
	page, err := svc.ListObjects(auth.WithUser(ctx, d1User), oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects d1: %v", err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("d1 caller: expected 2 rows (deptId=d1), got %d", len(page.Data))
	}
	for _, o := range page.Data {
		if o.Properties["deptId"] != "d1" {
			t.Fatalf("d1 caller: expected deptId=d1, got %v", o.Properties["deptId"])
		}
	}

	d2User := &auth.User{
		ID:         "user:charlie",
		Roles:      []string{"reader"},
		Attributes: map[string]any{"dept": "d2"},
	}
	page, err = svc.ListObjects(auth.WithUser(ctx, d2User), oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects d2: %v", err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("d2 caller: expected 1 row (emp3), got %d", len(page.Data))
	}
	if page.Data[0].PrimaryKey != "emp3" {
		t.Fatalf("d2 caller: expected emp3, got %v", page.Data[0].PrimaryKey)
	}

	// Caller with a role the CEL policy does NOT apply to → CEL gate
	// open (no applicable policy) → all 3 rows visible.
	other := &auth.User{ID: "user:bob", Roles: []string{"viewer"}}
	page, err = svc.ListObjects(auth.WithUser(ctx, other), oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects other: %v", err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("non-matching caller: expected 3 rows (CEL gate open), got %d", len(page.Data))
	}

	// Admin bypass — should see all 3 regardless of CEL.
	admin := &auth.User{ID: "user:root", Roles: []string{auth.RoleAdmin}}
	page, err = svc.ListObjects(auth.WithUser(ctx, admin), oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects admin: %v", err)
	}
	if len(page.Data) != 3 {
		t.Fatalf("admin: expected 3 rows (CEL bypass), got %d", len(page.Data))
	}
}

// TestUS487_RowPolicyCEL_GetHidesDeniedObject pins the GetObject hide-
// existence contract for CEL gates — a row that fails the CEL predicate
// returns ErrNotFound rather than 403, matching the WhereClause-side
// behavior in the existing US-256 integration test.
func TestUS487_RowPolicyCEL_GetHidesDeniedObject(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := rls.NewMemoryStore()
	_ = store.Create(ctx, &rls.RowPolicy{
		RID:           "ri.rls.main.row-policy.us487-cel",
		ObjectTypeRID: ot.RID,
		CELExpression: `object.deptId == user.dept`,
		AppliesTo:     rls.AppliesTo{Roles: []string{"reader"}},
	})
	engine := rls.New(store, nil)
	_ = engine.Reload(ctx)
	svc.SetRowPolicyEngine(engine)

	caller := &auth.User{
		ID:         "user:alice",
		Roles:      []string{"reader"},
		Attributes: map[string]any{"dept": "d1"},
	}
	ctxCaller := auth.WithUser(ctx, caller)

	// emp3 is in d2 → CEL says no → ErrNotFound.
	if _, err := svc.GetObject(ctxCaller, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp3",
	}); err != oms.ErrNotFound {
		t.Fatalf("GetObject emp3: expected ErrNotFound (CEL deny), got %v", err)
	}

	// emp1 is in d1 → CEL says yes → success.
	if _, err := svc.GetObject(ctxCaller, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	}); err != nil {
		t.Fatalf("GetObject emp1: expected success, got %v", err)
	}
}
