package auth

// Role names. Flat 4-role hierarchy described in
// .omc/scientist/reports/20260406_104203_rbac_design.md.
const (
	RoleViewer        = "viewer"
	RoleEditor        = "editor"
	RoleOntologyOwner = "ontology-owner"
	RoleAdmin         = "admin"
)

// Permission codes. Each is "<resource>.<verb>" and is referenced by
// route-level RequirePermission middleware and handler-level scope checks.
const (
	PermOntologyRead            = "ontology.read"
	PermOntologyWrite           = "ontology.write"
	PermObjectTypeRead          = "objectType.read"
	PermObjectTypeWrite         = "objectType.write"
	PermLinkTypeRead            = "linkType.read"
	PermLinkTypeWrite           = "linkType.write"
	PermActionTypeRead          = "actionType.read"
	PermActionTypeWrite         = "actionType.write"
	PermInterfaceRead           = "interface.read"
	PermInterfaceWrite          = "interface.write"
	PermSharedPropertyRead      = "sharedProperty.read"
	PermSharedPropertyWrite     = "sharedProperty.write"
	PermTypeGroupRead           = "typeGroup.read"
	PermTypeGroupWrite          = "typeGroup.write"
	PermValueTypeRead           = "valueType.read"
	PermValueTypeWrite          = "valueType.write"
	PermQueryTypeRead           = "queryType.read"
	PermQueryTypeWrite          = "queryType.write"
	PermObjectRead              = "object.read"
	PermObjectWrite             = "object.write"
	PermActionExecute           = "action.execute"
	PermDatasourceBindingManage = "datasourceBinding.manage"
	PermSecurityPolicyManage    = "securityPolicy.manage"
	PermSnapshotManage          = "snapshot.manage"
	PermActionLogRead           = "actionLog.read"
	PermUserManage              = "user.manage"
	PermStreamIngest            = "stream.ingest"
)

// rolePermissions is the static role-to-permission matrix. Lookups go through
// RolePermissions, never the map directly, so callers cannot mutate it.
var rolePermissions = map[string][]string{
	RoleViewer: {
		PermOntologyRead,
		PermObjectTypeRead,
		PermLinkTypeRead,
		PermActionTypeRead,
		PermInterfaceRead,
		PermSharedPropertyRead,
		PermTypeGroupRead,
		PermValueTypeRead,
		PermQueryTypeRead,
		PermObjectRead,
		PermActionLogRead,
	},
	RoleEditor: {
		PermOntologyRead,
		PermObjectTypeRead,
		PermLinkTypeRead,
		PermActionTypeRead,
		PermInterfaceRead,
		PermSharedPropertyRead,
		PermTypeGroupRead,
		PermValueTypeRead,
		PermQueryTypeRead,
		PermObjectRead,
		PermObjectWrite,
		PermActionExecute,
		PermActionLogRead,
	},
	RoleOntologyOwner: {
		PermOntologyRead,
		PermOntologyWrite,
		PermObjectTypeRead,
		PermObjectTypeWrite,
		PermLinkTypeRead,
		PermLinkTypeWrite,
		PermActionTypeRead,
		PermActionTypeWrite,
		PermInterfaceRead,
		PermInterfaceWrite,
		PermSharedPropertyRead,
		PermSharedPropertyWrite,
		PermTypeGroupRead,
		PermTypeGroupWrite,
		PermValueTypeRead,
		PermValueTypeWrite,
		PermQueryTypeRead,
		PermQueryTypeWrite,
		PermObjectRead,
		PermObjectWrite,
		PermActionExecute,
		PermDatasourceBindingManage,
		PermSnapshotManage,
		PermActionLogRead,
		PermStreamIngest,
	},
	RoleAdmin: {
		PermOntologyRead,
		PermOntologyWrite,
		PermObjectTypeRead,
		PermObjectTypeWrite,
		PermLinkTypeRead,
		PermLinkTypeWrite,
		PermActionTypeRead,
		PermActionTypeWrite,
		PermInterfaceRead,
		PermInterfaceWrite,
		PermSharedPropertyRead,
		PermSharedPropertyWrite,
		PermTypeGroupRead,
		PermTypeGroupWrite,
		PermValueTypeRead,
		PermValueTypeWrite,
		PermQueryTypeRead,
		PermQueryTypeWrite,
		PermObjectRead,
		PermObjectWrite,
		PermActionExecute,
		PermDatasourceBindingManage,
		PermSecurityPolicyManage,
		PermSnapshotManage,
		PermActionLogRead,
		PermUserManage,
		PermStreamIngest,
	},
}

// AllPermissions returns the canonical list of every permission code.
func AllPermissions() []string {
	return []string{
		PermOntologyRead, PermOntologyWrite,
		PermObjectTypeRead, PermObjectTypeWrite,
		PermLinkTypeRead, PermLinkTypeWrite,
		PermActionTypeRead, PermActionTypeWrite,
		PermInterfaceRead, PermInterfaceWrite,
		PermSharedPropertyRead, PermSharedPropertyWrite,
		PermTypeGroupRead, PermTypeGroupWrite,
		PermValueTypeRead, PermValueTypeWrite,
		PermQueryTypeRead, PermQueryTypeWrite,
		PermObjectRead, PermObjectWrite,
		PermActionExecute,
		PermDatasourceBindingManage,
		PermSecurityPolicyManage,
		PermSnapshotManage,
		PermActionLogRead,
		PermUserManage,
		PermStreamIngest,
	}
}

// RolePermissions returns the permission list granted by a single role.
// Returns an empty slice for unknown roles. Callers should treat the result
// as read-only.
func RolePermissions(role string) []string {
	perms, ok := rolePermissions[role]
	if !ok {
		return nil
	}
	out := make([]string, len(perms))
	copy(out, perms)
	return out
}

// HasPermission returns true if any of the given roles grants the permission.
func HasPermission(roles []string, perm string) bool {
	for _, role := range roles {
		for _, p := range rolePermissions[role] {
			if p == perm {
				return true
			}
		}
	}
	return false
}

// PermissionsForRoles returns the union of permissions granted by the given
// roles, deduplicated. Order is not guaranteed.
func PermissionsForRoles(roles []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, role := range roles {
		for _, p := range rolePermissions[role] {
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
