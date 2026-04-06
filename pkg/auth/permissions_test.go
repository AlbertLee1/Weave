package auth

import (
	"slices"
	"testing"
)

func TestRolePermissions_ViewerReadOnly(t *testing.T) {
	perms := RolePermissions("viewer")
	if len(perms) == 0 {
		t.Fatal("viewer should have at least the read permissions")
	}
	if !slices.Contains(perms, PermOntologyRead) {
		t.Errorf("viewer must have %s", PermOntologyRead)
	}
	if slices.Contains(perms, PermOntologyWrite) {
		t.Error("viewer must NOT have ontology.write")
	}
	if slices.Contains(perms, PermActionExecute) {
		t.Error("viewer must NOT have action.execute")
	}
	if slices.Contains(perms, PermSecurityPolicyManage) {
		t.Error("viewer must NOT have securityPolicy.manage")
	}
}

func TestRolePermissions_EditorCanExecute(t *testing.T) {
	perms := RolePermissions("editor")
	if !slices.Contains(perms, PermActionExecute) {
		t.Error("editor must have action.execute")
	}
	if slices.Contains(perms, PermOntologyWrite) {
		t.Error("editor must NOT have ontology.write (that is ontology-owner+)")
	}
	if slices.Contains(perms, PermSecurityPolicyManage) {
		t.Error("editor must NOT have securityPolicy.manage")
	}
}

func TestRolePermissions_OntologyOwnerWritesMetadata(t *testing.T) {
	perms := RolePermissions("ontology-owner")
	for _, p := range []string{
		PermOntologyWrite, PermObjectTypeWrite, PermLinkTypeWrite, PermActionTypeWrite,
		PermInterfaceWrite, PermSnapshotManage, PermDatasourceBindingManage,
	} {
		if !slices.Contains(perms, p) {
			t.Errorf("ontology-owner must have %s", p)
		}
	}
	if slices.Contains(perms, PermSecurityPolicyManage) {
		t.Error("ontology-owner must NOT have securityPolicy.manage (admin only)")
	}
	if slices.Contains(perms, PermUserManage) {
		t.Error("ontology-owner must NOT have user.manage (admin only)")
	}
}

func TestRolePermissions_AdminAll(t *testing.T) {
	perms := RolePermissions("admin")
	for _, p := range AllPermissions() {
		if !slices.Contains(perms, p) {
			t.Errorf("admin must have %s", p)
		}
	}
}

func TestRolePermissions_UnknownRole(t *testing.T) {
	if perms := RolePermissions("not-a-role"); len(perms) != 0 {
		t.Errorf("unknown role must return empty slice, got %v", perms)
	}
}

func TestHasPermission(t *testing.T) {
	if !HasPermission([]string{"viewer"}, PermOntologyRead) {
		t.Error("viewer should grant ontology.read")
	}
	if HasPermission([]string{"viewer"}, PermOntologyWrite) {
		t.Error("viewer must not grant ontology.write")
	}
	if !HasPermission([]string{"viewer", "editor"}, PermActionExecute) {
		t.Error("union of roles should grant action.execute via editor")
	}
	if HasPermission(nil, PermOntologyRead) {
		t.Error("nil roles should grant nothing")
	}
}

func TestPermissionsForRoles_DeduplicatesAndUnions(t *testing.T) {
	got := PermissionsForRoles([]string{"viewer", "viewer", "editor"})
	if !slices.Contains(got, PermActionExecute) {
		t.Error("union should include editor permissions")
	}
	if !slices.Contains(got, PermOntologyRead) {
		t.Error("union should include viewer permissions")
	}
	// Check uniqueness
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p] {
			t.Errorf("duplicate permission %s in union", p)
		}
		seen[p] = true
	}
}
