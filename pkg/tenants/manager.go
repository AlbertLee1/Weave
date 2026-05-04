package tenants

import (
	"context"
	"log"
	"sync"
	"time"

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

	// US-438 billing + alerting hooks. Optional — a Manager built
	// without these still throttles QPS / object / storage at 100% via
	// the existing Check* gates, but does not record usage or fire
	// crossing notifications.
	usage    UsageStore
	alerts   AlertStore
	notifier UsageNotifier
	clock    func() time.Time
}

// NewManager returns a Manager wired to store. Pass nil to build a
// permissive "no quotas" manager that allows everything — used in
// degraded-mode deployments without PG.
func NewManager(store Store) *Manager {
	return &Manager{
		store:    store,
		limiters: make(map[string]*rate.Limiter),
		clock:    time.Now,
	}
}

// WithUsageStore attaches the per-tenant monthly usage counter store.
// Returns m for chaining. nil is safe — the manager continues to skip
// usage tracking, RecordUsage becomes a no-op.
func (m *Manager) WithUsageStore(s UsageStore) *Manager {
	if m == nil {
		return nil
	}
	m.usage = s
	return m
}

// WithAlertStore attaches the dedup ledger. Without an alert store the
// notifier fires on every threshold crossing (no dedup); the production
// wiring always pairs WithAlertStore + WithNotifier.
func (m *Manager) WithAlertStore(s AlertStore) *Manager {
	if m == nil {
		return nil
	}
	m.alerts = s
	return m
}

// WithNotifier attaches the side-effect surface invoked when a fresh
// alert is recorded. Without a notifier wired the alert is recorded in
// the ledger but no notification fires. Returns m for chaining.
func (m *Manager) WithNotifier(n UsageNotifier) *Manager {
	if m == nil {
		return nil
	}
	m.notifier = n
	return m
}

// SetClock overrides the wall-clock used to derive the current
// calendar month for usage/alert writes. Tests inject a fixed clock so
// month rollover behaviour is reproducible. Production wiring should
// not call this — the default time.Now is correct.
func (m *Manager) SetClock(now func() time.Time) {
	if m == nil || now == nil {
		return
	}
	m.clock = now
}

// Now returns the manager's wall-clock instant, defaulting to time.Now
// when SetClock was never called. Exposed so unit tests on derived
// helpers can inspect the same clock.
func (m *Manager) Now() time.Time {
	if m == nil || m.clock == nil {
		return time.Now()
	}
	return m.clock()
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

// ----------------------------------------------------------------------
// US-438 — usage + alerting helpers.
// ----------------------------------------------------------------------

// RecordUsage atomically increments the monthly usage counter for
// (tenant, metric) by delta and evaluates threshold crossings. Returns
// the alerts that fired on this call (newly-recorded only — already-
// notified thresholds in the same calendar month return nothing).
//
// Empty tenant, nil receiver, missing UsageStore → silent no-op.
// Unknown metric returns an error WITHOUT incrementing — the caller
// would otherwise silently drop the metric on a typo.
//
// Per-store atomicity: AddUsage runs first; threshold evaluation reads
// the post-increment value. Two concurrent RecordUsage calls that both
// cross a threshold race cleanly because the AlertStore.RecordAlert
// dedup is the single source of truth for "did anyone fire this alert
// already this month".
func (m *Manager) RecordUsage(ctx context.Context, tenant, metric string, delta int64) ([]Alert, error) {
	if m == nil || m.usage == nil || tenant == "" {
		return nil, nil
	}
	if !IsValidMetric(metric) {
		return nil, errInvalidMetric{metric: metric}
	}
	month := MonthStart(m.Now())
	if err := m.usage.AddUsage(ctx, tenant, month, metric, delta); err != nil {
		return nil, err
	}
	return m.evaluateMetric(ctx, tenant, metric, month)
}

// EvaluateThresholds re-checks every metric for the tenant and fires
// any pending alert without changing the recorded usage. Used by the
// admin "force a sweep" surface and by the periodic monthly sweep job
// so a freshly-loaded quota row doesn't have to wait for the next
// AddUsage call to surface the alert.
func (m *Manager) EvaluateThresholds(ctx context.Context, tenant string) ([]Alert, error) {
	if m == nil || m.usage == nil || tenant == "" {
		return nil, nil
	}
	month := MonthStart(m.Now())
	out := []Alert{}
	for _, metric := range ValidMetrics {
		fired, err := m.evaluateMetric(ctx, tenant, metric, month)
		if err != nil {
			return out, err
		}
		out = append(out, fired...)
	}
	return out, nil
}

// IsBlocked reports whether the tenant is currently at or above the
// 100% cap for the named metric. Returns false on missing store / nil
// receiver / empty tenant / unconfigured cap so the call site can
// short-circuit gracefully — same "absence of explicit cap means
// unlimited" contract the existing Check* helpers use.
func (m *Manager) IsBlocked(ctx context.Context, tenant, metric string) bool {
	if m == nil || m.usage == nil || m.store == nil || tenant == "" {
		return false
	}
	if !IsValidMetric(metric) {
		return false
	}
	q, err := m.store.GetQuota(ctx, tenant)
	if err != nil || q == nil {
		return false
	}
	cap := QuotaForMetric(q, metric)
	if cap <= 0 {
		return false
	}
	month := MonthStart(m.Now())
	rows, err := m.usage.GetMonthlyUsage(ctx, tenant, month)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Metric == metric {
			return r.Amount >= cap
		}
	}
	return false
}

// MonthlyUsageFor returns the per-metric MonthlyUsage rows for the
// supplied tenant in the manager's current calendar month. nil store
// → empty slice. Caps reflect the live tenant_quotas row.
func (m *Manager) MonthlyUsageFor(ctx context.Context, tenant string) ([]*MonthlyUsage, error) {
	if m == nil || m.usage == nil || tenant == "" {
		return nil, nil
	}
	return m.usage.GetMonthlyUsage(ctx, tenant, MonthStart(m.Now()))
}

// errInvalidMetric is the typed error RecordUsage returns when the
// caller passes an unknown metric — exposes the rejected name so the
// HTTP layer can put it on the wire.
type errInvalidMetric struct {
	metric string
}

func (e errInvalidMetric) Error() string {
	return "tenants: unknown metric " + e.metric
}

// evaluateMetric inspects (tenant, metric) post-increment and fires the
// 80% / 100% alerts as needed. Each crossing is recorded through the
// AlertStore for dedup so even a thrashing usage counter that crosses
// the threshold many times in a month notifies once.
func (m *Manager) evaluateMetric(ctx context.Context, tenant, metric string, month time.Time) ([]Alert, error) {
	if m == nil || m.usage == nil {
		return nil, nil
	}
	rows, err := m.usage.GetMonthlyUsage(ctx, tenant, month)
	if err != nil {
		return nil, err
	}
	var current *MonthlyUsage
	for _, r := range rows {
		if r.Metric == metric {
			current = r
			break
		}
	}
	if current == nil {
		return nil, nil
	}
	if current.Cap <= 0 {
		return nil, nil
	}
	monthKey := FormatMonth(month)
	fired := []Alert{}
	for _, threshold := range AlertThresholds {
		if current.Percent < threshold {
			continue
		}
		alert := Alert{
			Tenant:    tenant,
			Month:     monthKey,
			Metric:    metric,
			Threshold: threshold,
			Amount:    current.Amount,
			Cap:       current.Cap,
			Percent:   current.Percent,
		}
		if m.alerts != nil {
			fresh, err := m.alerts.RecordAlert(ctx, alert)
			if err != nil {
				return fired, err
			}
			if !fresh {
				continue
			}
		}
		if m.notifier != nil {
			if err := m.notifier.NotifyUsageAlert(ctx, alert); err != nil {
				log.Printf("[TENANT-USAGE-ALERT] notifier failed tenant=%s metric=%s threshold=%d err=%v",
					tenant, metric, threshold, err)
			}
		}
		fired = append(fired, alert)
	}
	return fired, nil
}
