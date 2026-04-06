package auth

import (
	"context"
	"testing"
)

func TestEnforceOntologyScope_AdminBypass(t *testing.T) {
	ctx := WithUser(context.Background(), &User{ID: "root", Roles: []string{RoleAdmin}})
	if err := EnforceOntologyScope(ctx, "ri.ontology.main.ontology.northwind", PermObjectTypeWrite); err != nil {
		t.Errorf("admin should be allowed, got %v", err)
	}
}

func TestEnforceOntologyScope_OwnerOfMatchingOntology(t *testing.T) {
	ctx := WithUser(context.Background(), &User{
		ID:    "alice",
		Roles: []string{RoleViewer},
		OntologyRoles: map[string]string{
			"ri.ontology.main.ontology.northwind": RoleOntologyOwner,
		},
	})
	if err := EnforceOntologyScope(ctx, "ri.ontology.main.ontology.northwind", PermObjectTypeWrite); err != nil {
		t.Errorf("owner of matching ontology should be allowed, got %v", err)
	}
}

func TestEnforceOntologyScope_OwnerOfDifferentOntology(t *testing.T) {
	ctx := WithUser(context.Background(), &User{
		ID:    "alice",
		Roles: []string{RoleViewer},
		OntologyRoles: map[string]string{
			"ri.ontology.main.ontology.northwind": RoleOntologyOwner,
		},
	})
	err := EnforceOntologyScope(ctx, "ri.ontology.main.ontology.chinook", PermObjectTypeWrite)
	if err == nil {
		t.Error("owner of different ontology should be denied")
	}
}

func TestEnforceOntologyScope_NoUserDenies(t *testing.T) {
	err := EnforceOntologyScope(context.Background(), "ri.ontology.main.ontology.x", PermObjectTypeWrite)
	if err == nil {
		t.Error("no user should be denied")
	}
}

func TestEnforceOntologyScope_ViewerDenied(t *testing.T) {
	ctx := WithUser(context.Background(), &User{ID: "v", Roles: []string{RoleViewer}})
	err := EnforceOntologyScope(ctx, "ri.ontology.main.ontology.x", PermObjectTypeWrite)
	if err == nil {
		t.Error("viewer should be denied write")
	}
}

func TestEnforceOntologyScope_ReadPermissionAllowedForViewer(t *testing.T) {
	ctx := WithUser(context.Background(), &User{ID: "v", Roles: []string{RoleViewer}})
	if err := EnforceOntologyScope(ctx, "ri.ontology.main.ontology.x", PermObjectTypeRead); err != nil {
		t.Errorf("viewer should be allowed read, got %v", err)
	}
}
