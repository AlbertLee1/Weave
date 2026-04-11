package index

import "context"

// US-044: ontology-scoped Bleve indexes are uniform across the codebase. The
// scope rides on the request context so we don't have to thread an extra
// parameter through every search call. Handlers stamp the scope at the request
// edge via WithOntologyScope, and downstream packages (oss, oss/objectset,
// links) read it via OntologyScopeFromContext when computing index keys.

type ontologyScopeKey struct{}

// WithOntologyScope returns a child context tagged with the ontology API name.
// Empty input is a no-op so callers can pass through optional values without
// branching.
func WithOntologyScope(ctx context.Context, ontologyAPIName string) context.Context {
	if ontologyAPIName == "" {
		return ctx
	}
	return context.WithValue(ctx, ontologyScopeKey{}, ontologyAPIName)
}

// OntologyScopeFromContext extracts the ontology API name previously stamped
// by WithOntologyScope. Returns "" when no scope is set.
func OntologyScopeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ontologyScopeKey{}).(string); ok {
		return v
	}
	return ""
}

// KeyForCtx returns the per-ontology Bleve key for objectType using the
// scope stamped on ctx, falling back to the bare objectType when no scope is
// set OR when the manager has no scoped index for that key. The fallback
// preserves backwards compatibility for legacy callers (older test fixtures
// that pre-create unscoped indexes) while routing production traffic — which
// always seeds the scoped index via the funnel consumer — to the per-ontology
// key.
func KeyForCtx(ctx context.Context, mgr *Manager, objectType string) string {
	scope := OntologyScopeFromContext(ctx)
	if scope == "" || mgr == nil {
		return objectType
	}
	scoped := ScopedKey(scope, objectType)
	if mgr.GetIndex(scoped) != nil {
		return scoped
	}
	return objectType
}
