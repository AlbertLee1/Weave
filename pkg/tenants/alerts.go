package tenants

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// Threshold percentages at which alerts fire. 80 → warning notification
// via the US-429 multi-channel dispatcher; 100 → operational throttle
// (the existing CheckObjectQuota / CheckStorageQuota gates already deny
// at this level; we additionally fire one notification on the crossing).
const (
	AlertThresholdWarning = 80
	AlertThresholdLimit   = 100
)

// AlertThresholds is the set of thresholds the manager evaluates per
// metric. Iterated in ascending order so the warning fires before the
// limit on a single Evaluate call that crosses both.
var AlertThresholds = []int{AlertThresholdWarning, AlertThresholdLimit}

// Alert is a single triggered threshold crossing. The dedup ledger keys
// on (tenant, month, metric, threshold) so a tenant pushing past 80% and
// then 100% in the same month surfaces both alerts exactly once.
type Alert struct {
	Tenant     string    `json:"tenant"`
	Month      string    `json:"month"`
	Metric     string    `json:"metric"`
	Threshold  int       `json:"threshold"`
	Amount     int64     `json:"amount"`
	Cap        int64     `json:"cap"`
	Percent    int       `json:"percent"`
	RecordedAt time.Time `json:"recordedAt"`
}

// AlertStore persists the dedup ledger so each (tenant, month, metric,
// threshold) alert dispatches at most once per calendar month.
type AlertStore interface {
	// RecordAlert atomically inserts the (tenant, month, metric,
	// threshold) row IFF it does not already exist. Returns true when a
	// fresh row was inserted (caller should dispatch the notification),
	// false when the row already existed (already alerted this month).
	RecordAlert(ctx context.Context, a Alert) (bool, error)

	// ListAlerts returns every alert in the supplied month, sorted by
	// (tenant, metric, threshold). Pass zero month to return every alert.
	ListAlerts(ctx context.Context, month time.Time) ([]*Alert, error)
}

// UsageNotifier is the side-effect surface invoked when a fresh alert
// is recorded. The default LogUsageNotifier writes to log; production
// wiring routes this to the US-429 multi-channel Dispatcher.
type UsageNotifier interface {
	NotifyUsageAlert(ctx context.Context, a Alert) error
}

// LogUsageNotifier is the fallback notifier — emits one log line per
// alert and never errors. Used when no dispatcher is configured so the
// boot path stays branch-free.
type LogUsageNotifier struct{}

// NotifyUsageAlert logs a one-line summary of the alert.
func (LogUsageNotifier) NotifyUsageAlert(_ context.Context, a Alert) error {
	log.Printf("[TENANT-USAGE-ALERT] tenant=%s month=%s metric=%s threshold=%d%% amount=%d cap=%d percent=%d%%",
		a.Tenant, a.Month, a.Metric, a.Threshold, a.Amount, a.Cap, a.Percent)
	return nil
}

// ----------------------------------------------------------------------
// MemoryAlertStore — test-friendly in-memory dedup ledger.
// ----------------------------------------------------------------------

// MemoryAlertStore is a thread-safe in-memory AlertStore. RecordAlert
// inserts atomically; concurrent callers race cleanly so only the first
// caller observes (true, nil).
type MemoryAlertStore struct {
	mu    sync.Mutex
	rows  map[memAlertKey]Alert
	clock func() time.Time
}

type memAlertKey struct {
	tenant    string
	month     string
	metric    string
	threshold int
}

// NewMemoryAlertStore returns an empty in-memory AlertStore.
func NewMemoryAlertStore() *MemoryAlertStore {
	return &MemoryAlertStore{
		rows:  make(map[memAlertKey]Alert),
		clock: time.Now,
	}
}

func (s *MemoryAlertStore) RecordAlert(_ context.Context, a Alert) (bool, error) {
	if !IsValidMetric(a.Metric) {
		return false, fmt.Errorf("tenants: unknown metric %q", a.Metric)
	}
	if a.Threshold != AlertThresholdWarning && a.Threshold != AlertThresholdLimit {
		return false, fmt.Errorf("tenants: unsupported threshold %d", a.Threshold)
	}
	if a.Tenant == "" {
		return false, fmt.Errorf("tenants: tenant required")
	}
	k := memAlertKey{
		tenant:    a.Tenant,
		month:     a.Month,
		metric:    a.Metric,
		threshold: a.Threshold,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rows[k]; exists {
		return false, nil
	}
	if a.RecordedAt.IsZero() {
		a.RecordedAt = s.clock().UTC()
	}
	s.rows[k] = a
	return true, nil
}

func (s *MemoryAlertStore) ListAlerts(_ context.Context, month time.Time) ([]*Alert, error) {
	wantAll := month.IsZero()
	monthKey := FormatMonth(month)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Alert, 0, len(s.rows))
	for _, a := range s.rows {
		if !wantAll && a.Month != monthKey {
			continue
		}
		cp := a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tenant != out[j].Tenant {
			return out[i].Tenant < out[j].Tenant
		}
		if out[i].Metric != out[j].Metric {
			return out[i].Metric < out[j].Metric
		}
		return out[i].Threshold < out[j].Threshold
	})
	return out, nil
}
