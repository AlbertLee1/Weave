package tenants

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Metric identifiers are wire-format constants — the migration's CHECK
// constraint uses the same lowercase strings, so keep them in lockstep.
const (
	MetricObjects  = "objects"
	MetricStorage  = "storage"
	MetricRequests = "requests"
)

// ValidMetrics is the canonical metric vocabulary. The order here is the
// same as the SQL `metrics` CTE so the view's GROUP BY / sort order is
// reproducible.
var ValidMetrics = []string{MetricObjects, MetricStorage, MetricRequests}

// IsValidMetric reports whether m is one of the canonical metric names.
func IsValidMetric(m string) bool {
	switch m {
	case MetricObjects, MetricStorage, MetricRequests:
		return true
	}
	return false
}

// MonthlyUsage is one row of the tenant_usage_monthly view: the
// current-calendar-month consumption of one metric for one tenant,
// alongside the relevant quota cap so the caller can compute a usage
// percentage without a round-trip to tenant_quotas.
//
// Cap == 0 means "no limit configured" — Percent is reported as 0 in
// that case (the threshold checker degrades to a no-op).
type MonthlyUsage struct {
	Tenant  string `json:"tenant"`
	Month   string `json:"month"` // YYYY-MM-01 (first day of month)
	Metric  string `json:"metric"`
	Amount  int64  `json:"amount"`
	Cap     int64  `json:"cap"`
	Percent int    `json:"percent"`
}

// UsageStore is the persistence interface for tenant_monthly_usage. The
// PG-backed implementation lives in cmd/server/tenant_usage_store.go;
// MemoryUsageStore below is the test-friendly default.
type UsageStore interface {
	// AddUsage atomically increments the (tenant, month, metric) counter
	// by delta. Negative deltas are valid (e.g. recording a row deletion
	// reduces objects_count) but the underlying counter is clamped at 0
	// to honour the SQL non-negative CHECK constraint. month MUST be the
	// first day of the calendar month — call MonthStart(t) to derive.
	AddUsage(ctx context.Context, tenant string, month time.Time, metric string, delta int64) error

	// GetMonthlyUsage returns every metric row for (tenant, month). The
	// implementation MUST surface zero-amount rows for any configured
	// metric so the threshold checker can iterate without nil guards.
	// month is the first day of the calendar month.
	GetMonthlyUsage(ctx context.Context, tenant string, month time.Time) ([]*MonthlyUsage, error)

	// ListMonthlyUsage walks every (tenant, month) row in the view, used
	// by the admin GET /api/admin/tenant-usage endpoint and the periodic
	// alert sweep. month is the first day of the calendar month.
	ListMonthlyUsage(ctx context.Context, month time.Time) ([]*MonthlyUsage, error)
}

// MonthStart truncates t to the first day of its calendar month in UTC.
// Zero t returns zero time so callers don't need a nil guard.
func MonthStart(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// FormatMonth renders t.UTC() as YYYY-MM-01 — the canonical wire shape
// for MonthlyUsage.Month.
func FormatMonth(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = MonthStart(t)
	return t.Format("2006-01-02")
}

// QuotaForMetric returns the configured cap for the named metric on q.
// Returns 0 (== unlimited) for unknown metrics or a nil quota.
func QuotaForMetric(q *Quota, metric string) int64 {
	if q == nil {
		return 0
	}
	switch metric {
	case MetricObjects:
		return q.MaxObjects
	case MetricStorage:
		return q.MaxStorage
	}
	return 0
}

// computePercent returns clamp(amount * 100 / cap, 0..100). cap <= 0
// reports 0 — "no limit configured".
func computePercent(amount, cap int64) int {
	if cap <= 0 || amount <= 0 {
		return 0
	}
	pct := amount * 100 / cap
	if pct > 100 {
		return 100
	}
	return int(pct)
}

// ----------------------------------------------------------------------
// MemoryUsageStore — test-friendly in-memory implementation.
// ----------------------------------------------------------------------

// MemoryUsageStore is a thread-safe in-memory UsageStore. It honours
// the same non-negative invariant the SQL CHECK constraint enforces.
type MemoryUsageStore struct {
	mu     sync.RWMutex
	quotas Store
	rows   map[memUsageKey]int64
	clock  func() time.Time
}

type memUsageKey struct {
	tenant string
	month  string // YYYY-MM-01
	metric string
}

// NewMemoryUsageStore returns an empty in-memory UsageStore. quotas is
// consulted at read time to fill in MonthlyUsage.Cap; pass nil to fall
// back to "no limits configured" — the percent column then always
// reports 0.
func NewMemoryUsageStore(quotas Store) *MemoryUsageStore {
	return &MemoryUsageStore{
		quotas: quotas,
		rows:   make(map[memUsageKey]int64),
		clock:  time.Now,
	}
}

func (s *MemoryUsageStore) AddUsage(_ context.Context, tenant string, month time.Time, metric string, delta int64) error {
	if !IsValidMetric(metric) {
		return fmt.Errorf("tenants: unknown metric %q", metric)
	}
	if tenant == "" {
		return fmt.Errorf("tenants: tenant required")
	}
	k := memUsageKey{
		tenant: tenant,
		month:  FormatMonth(month),
		metric: metric,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.rows[k] + delta
	if cur < 0 {
		cur = 0
	}
	s.rows[k] = cur
	return nil
}

func (s *MemoryUsageStore) GetMonthlyUsage(ctx context.Context, tenant string, month time.Time) ([]*MonthlyUsage, error) {
	monthKey := FormatMonth(month)
	s.mu.RLock()
	defer s.mu.RUnlock()
	q, _ := s.lookupQuota(ctx, tenant)
	out := make([]*MonthlyUsage, 0, len(ValidMetrics))
	for _, metric := range ValidMetrics {
		amount := s.rows[memUsageKey{tenant: tenant, month: monthKey, metric: metric}]
		cap := QuotaForMetric(q, metric)
		out = append(out, &MonthlyUsage{
			Tenant:  tenant,
			Month:   monthKey,
			Metric:  metric,
			Amount:  amount,
			Cap:     cap,
			Percent: computePercent(amount, cap),
		})
	}
	return out, nil
}

func (s *MemoryUsageStore) ListMonthlyUsage(ctx context.Context, month time.Time) ([]*MonthlyUsage, error) {
	monthKey := FormatMonth(month)
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := map[string]struct{}{}
	if s.quotas != nil {
		quotas, err := s.quotas.ListQuotas(ctx)
		if err != nil {
			return nil, err
		}
		for _, q := range quotas {
			tenants[q.Tenant] = struct{}{}
		}
	}
	for k := range s.rows {
		if k.month == monthKey {
			tenants[k.tenant] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(tenants))
	for t := range tenants {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)

	out := make([]*MonthlyUsage, 0, len(sorted)*len(ValidMetrics))
	for _, tenant := range sorted {
		q, _ := s.lookupQuota(ctx, tenant)
		for _, metric := range ValidMetrics {
			amount := s.rows[memUsageKey{tenant: tenant, month: monthKey, metric: metric}]
			cap := QuotaForMetric(q, metric)
			out = append(out, &MonthlyUsage{
				Tenant:  tenant,
				Month:   monthKey,
				Metric:  metric,
				Amount:  amount,
				Cap:     cap,
				Percent: computePercent(amount, cap),
			})
		}
	}
	return out, nil
}

func (s *MemoryUsageStore) lookupQuota(ctx context.Context, tenant string) (*Quota, error) {
	if s.quotas == nil {
		return nil, nil
	}
	q, err := s.quotas.GetQuota(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return q, nil
}
