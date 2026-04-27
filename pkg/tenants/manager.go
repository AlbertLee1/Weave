package tenants

import (
	"context"
	"sync"

	"golang.org/x/time/rate"

	"github.com/liyang/weave/pkg/auth"
)

// TenantAttributeKey is the User.Attributes key carrying the caller's
// tenant identifier. We re-use the "realm" claim already populated by
// pkg/auth so a single JWT field gates feature flags AND quotas.
const TenantAttributeKey = "realm"

// TenantFromUser returns the caller's tenant identifier or "" when the
// claim is absent. nil-User → "".
func TenantFromUser(u *auth.User) string {
	if u == nil || u.Attributes == nil {
		return ""
	}
	v, ok := u.Attributes[TenantAttributeKey]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Manager is the read-side facade around Store. It caches per-tenant
// rate limiters keyed by tenant name so the QPS bucket is shared across
// every request from that tenant. Cache is populated lazily and
// invalidated when Reload is called (admins flip a quota → call Reload
// to drop the stale buckets).
type Manager struct {
	store Store

	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
}

// NewManager returns a Manager wired to store. Pass nil to build a
// permissive "no quotas" manager that allows everything — used in
// degraded-mode deployments without PG.
func NewManager(store Store) *Manager {
	return &Manager{
		store:    store,
		limiters: make(map[string]*rate.Limiter),
	}
}

// CheckQPS returns true iff the tenant is allowed to make one more
// request right now. Tenants without a quota row, the empty tenant
// (anonymous / no realm claim), and managers with a nil store all
// pass — the absence of an explicit cap means "unlimited".
func (m *Manager) CheckQPS(ctx context.Context, tenant string) bool {
	if m == nil || m.store == nil || tenant == "" {
		return true
	}
	lim := m.limiterFor(ctx, tenant)
	if lim == nil {
		return true
	}
	return lim.Allow()
}

// CheckObjectQuota returns true iff the tenant is allowed to add
// `delta` more objects given the current usage. delta=1 is the typical
// per-write check; pass larger values for batch creates.
func (m *Manager) CheckObjectQuota(ctx context.Context, tenant string, currentCount, delta int64) bool {
	if m == nil || m.store == nil || tenant == "" {
		return true
	}
	q, err := m.store.GetQuota(ctx, tenant)
	if err != nil || q == nil || q.MaxObjects <= 0 {
		return true
	}
	return currentCount+delta <= q.MaxObjects
}

// CheckStorageQuota returns true iff the tenant is allowed to consume
// `delta` more bytes given the current footprint.
func (m *Manager) CheckStorageQuota(ctx context.Context, tenant string, currentBytes, delta int64) bool {
	if m == nil || m.store == nil || tenant == "" {
		return true
	}
	q, err := m.store.GetQuota(ctx, tenant)
	if err != nil || q == nil || q.MaxStorage <= 0 {
		return true
	}
	return currentBytes+delta <= q.MaxStorage
}

// Reload invalidates the cached QPS limiters so the next request for
// each tenant rebuilds from the current quota row. Admin Update / Delete
// handlers call this after persisting changes.
func (m *Manager) Reload() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.limiters = make(map[string]*rate.Limiter)
	m.mu.Unlock()
}

func (m *Manager) limiterFor(ctx context.Context, tenant string) *rate.Limiter {
	m.mu.RLock()
	if lim, ok := m.limiters[tenant]; ok {
		m.mu.RUnlock()
		return lim
	}
	m.mu.RUnlock()

	q, err := m.store.GetQuota(ctx, tenant)
	if err != nil || q == nil || q.MaxQPS <= 0 {
		return nil
	}
	burst := q.Burst
	if burst <= 0 {
		// Default burst = 2x sustained rate, minimum 1, mirroring the
		// per-endpoint defaults in cmd/server/rate_limit.go.
		burst = int(q.MaxQPS * 2)
		if burst < 1 {
			burst = 1
		}
	}
	lim := rate.NewLimiter(rate.Limit(q.MaxQPS), burst)

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.limiters[tenant]; ok {
		// Lost the race; return the limiter the other goroutine cached.
		return existing
	}
	m.limiters[tenant] = lim
	return lim
}

type managerContextKey struct{}

// WithManager stamps mgr onto ctx so middleware-installed managers are
// reachable from any handler via ManagerFromContext.
func WithManager(ctx context.Context, mgr *Manager) context.Context {
	return context.WithValue(ctx, managerContextKey{}, mgr)
}

// ManagerFromContext returns the Manager on ctx or nil when none is
// installed. nil receivers on the helpers below behave as "no quotas".
func ManagerFromContext(ctx context.Context) *Manager {
	m, _ := ctx.Value(managerContextKey{}).(*Manager)
	return m
}
