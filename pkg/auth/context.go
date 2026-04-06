package auth

import "context"

// User represents an authenticated user resolved from a request.
//
// Roles holds global role grants (e.g. "admin", "editor", "viewer"); these
// are checked by the static permission matrix in permissions.go.
//
// OntologyRoles holds per-ontology scoped grants keyed by ontology RID
// (e.g. {"ri.ontology.main.ontology.northwind": "ontology-owner"}); these
// are checked by EnforceOntologyScope for resource-scoped writes.
type User struct {
	ID            string
	Roles         []string
	OntologyRoles map[string]string
}

type contextKey string

const userKey contextKey = "auth-user"

// WithUser stores a User in the context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFromContext retrieves the User from the context. Returns nil if not set.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userKey).(*User)
	return u
}
