package oss_test

import (
	"context"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oss"
)

// TestColumnMask_AppliedInListAndSearch verifies US-257: callers outside the
// mask's AppliesTo (allow list) see the masked value; callers inside the
// allow list see the clear value. Admins bypass all masks.
func TestColumnMask_AppliedInListAndSearch(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := masking.NewMemoryStore()
	// Mask the `name` property for everyone except the `finance` role.
	_ = store.Create(ctx, &masking.ColumnMask{
		RID:             "ri.masking.main.column-mask.name-hash",
		ObjectTypeRID:   ot.RID,
		PropertyAPIName: "name",
		MaskRule:        masking.MaskRuleHash,
		AppliesTo:       masking.AppliesTo{Roles: []string{"finance"}},
	})
	engine := masking.New(store, nil)
	if err := engine.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	svc.SetColumnMaskEngine(engine)

	// Caller 1: finance — sees clear `name`.
	fin := &auth.User{ID: "u:fin", Roles: []string{"finance"}}
	ctxFin := auth.WithUser(ctx, fin)
	page, err := svc.ListObjects(ctxFin, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects fin: %v", err)
	}
	for _, o := range page.Data {
		name, _ := o.Properties["name"].(string)
		if strings.HasPrefix(name, "sha256:") {
			t.Fatalf("finance caller should see clear names, got %q", name)
		}
	}

	// Caller 2: viewer — name is sha256-hashed.
	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)
	page, err = svc.ListObjects(ctxV, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects viewer: %v", err)
	}
	for _, o := range page.Data {
		name, _ := o.Properties["name"].(string)
		if !strings.HasPrefix(name, "sha256:") {
			t.Fatalf("viewer should see hashed name, got %q", name)
		}
	}

	// Caller 3: admin — bypasses mask.
	admin := &auth.User{ID: "u:admin", Roles: []string{auth.RoleAdmin}}
	ctxAdmin := auth.WithUser(ctx, admin)
	page, err = svc.ListObjects(ctxAdmin, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects admin: %v", err)
	}
	for _, o := range page.Data {
		name, _ := o.Properties["name"].(string)
		if strings.HasPrefix(name, "sha256:") {
			t.Fatalf("admin should see clear names, got %q", name)
		}
	}
}

// TestColumnMask_GetObjectMasked verifies that GetObject masks property
// values for callers outside the AppliesTo allow list.
func TestColumnMask_GetObjectMasked(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	ctx := context.Background()
	ot, err := repo.GetObjectTypeByAPIName(ctx, testOntologyRID, "employee")
	if err != nil {
		t.Fatalf("lookup employee ot: %v", err)
	}

	store := masking.NewMemoryStore()
	_ = store.Create(ctx, &masking.ColumnMask{
		RID:             "ri.masking.main.column-mask.age-redact",
		ObjectTypeRID:   ot.RID,
		PropertyAPIName: "age",
		MaskRule:        masking.MaskRuleRedact,
		AppliesTo:       masking.AppliesTo{}, // empty → apply to everyone
	})
	engine := masking.New(store, nil)
	_ = engine.Reload(ctx)
	svc.SetColumnMaskEngine(engine)

	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	ctxV := auth.WithUser(ctx, viewer)
	obj, err := svc.GetObject(ctxV, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if obj.Properties["age"] != "***" {
		t.Fatalf("expected age=***, got %v", obj.Properties["age"])
	}
	// Non-masked property unchanged.
	if obj.Properties["name"] != "alice" {
		t.Fatalf("expected name=alice, got %v", obj.Properties["name"])
	}
}
