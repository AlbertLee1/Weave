package auth

import "context"

// MarkingsAttributeKey is the User.Attributes key that carries the caller's
// held marking names for Foundry-style mandatory access control. The row-
// level policy engine (pkg/security RuleTypeMarkingSubset) reads the same
// key; keeping it exported here means middleware, handlers, and tests can
// populate it without duplicating the string literal.
const MarkingsAttributeKey = "markings"

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
	Email         string
	Name          string
	Roles         []string
	OntologyRoles map[string]string

	// Attributes carries key/value user attributes sourced from JWT claims or
	// directory lookups (dept, region, clearance, ...). The row-level policy
	// engine (pkg/security) reads these in eq/in/subset rule evaluation.
	// A nil map is equivalent to an empty map.
	Attributes map[string]any
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

// Markings returns the caller's marking set from the request context. It
// reads user.Attributes[MarkingsAttributeKey] and normalises the raw shape
// (native []string, JSON-decoded []any, or a scalar string) into a clean
// []string. Returns nil when there is no user or the attribute is absent.
//
// Both the JWT middleware (pkg/auth.handleJWT) and the ObjectSet read paths
// (pkg/oss.extractUserMarkings) depend on the canonical "markings" key, so
// any future surface that persists or consumes markings should go through
// this helper rather than poking the raw map.
func Markings(ctx context.Context) []string {
	u := UserFromContext(ctx)
	if u == nil || u.Attributes == nil {
		return nil
	}
	raw, ok := u.Attributes[MarkingsAttributeKey]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case nil:
		return nil
	case []string:
		if len(v) == 0 {
			return nil
		}
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}
