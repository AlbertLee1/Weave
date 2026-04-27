package featureflags

import (
	"context"

	"github.com/liyang/weave/pkg/auth"
)

// Manager is the read-side facade around Store. Production wiring
// constructs one Manager at server boot and hands it to every handler /
// middleware that wants to gate on flag state. A nil Manager and a
// Manager with a nil Store both return false for every HasFlag call so
// degraded-mode deployments (no PG) never throw.
type Manager struct {
	store Store
}

// NewManager returns a Manager reading from store. Pass nil to build
// a "no flags configured" manager that always reports false.
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// HasFlag returns true iff the named flag is enabled for user.
// Store lookups that return an error (flag missing, PG offline, ...)
// map to false — feature flags fail closed.
func (m *Manager) HasFlag(ctx context.Context, name string, user *auth.User) bool {
	if m == nil || m.store == nil {
		return false
	}
	flag, err := m.store.GetFlag(ctx, name)
	if err != nil {
		return false
	}
	return flag.EnabledFor(user)
}

type managerContextKey struct{}

// WithManager returns a copy of ctx carrying mgr so downstream
// handlers / middleware can call HasFlag(ctx, ...) without re-threading
// the Manager explicitly.
func WithManager(ctx context.Context, mgr *Manager) context.Context {
	return context.WithValue(ctx, managerContextKey{}, mgr)
}

// ManagerFromContext returns the Manager stored on ctx, or nil when
// none has been wired (e.g. test routers that skip the middleware).
func ManagerFromContext(ctx context.Context) *Manager {
	m, _ := ctx.Value(managerContextKey{}).(*Manager)
	return m
}

// HasFlag is the context-backed convenience wrapper around
// Manager.HasFlag. Returns false when no Manager is installed on ctx —
// this fail-closed shape lets handlers in degraded-mode test routers
// call it without guarding for nil ctx values.
func HasFlag(ctx context.Context, name string, user *auth.User) bool {
	return ManagerFromContext(ctx).HasFlag(ctx, name, user)
}
