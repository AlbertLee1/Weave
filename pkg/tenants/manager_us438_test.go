package tenants

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeNotifier records every alert it sees in dispatch order.
type fakeNotifier struct {
	mu      sync.Mutex
	alerts  []Alert
	failErr error
}

func (f *fakeNotifier) NotifyUsageAlert(_ context.Context, a Alert) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, a)
	return f.failErr
}

func (f *fakeNotifier) seen() []Alert {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Alert, len(f.alerts))
	copy(out, f.alerts)
	return out
}

// fixedClock returns a fixed instant. useful for reproducing month
// rollover semantics.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func newAlertingManager(t *testing.T, q *Quota, now time.Time) (*Manager, *fakeNotifier, *MemoryAlertStore) {
	t.Helper()
	ctx := context.Background()
	quotas := NewMemoryStore()
	if q != nil {
		if err := quotas.CreateQuota(ctx, q); err != nil {
			t.Fatalf("CreateQuota: %v", err)
		}
	}
	usage := NewMemoryUsageStore(quotas)
	alerts := NewMemoryAlertStore()
	notifier := &fakeNotifier{}
	mgr := NewManager(quotas).
		WithUsageStore(usage).
		WithAlertStore(alerts).
		WithNotifier(notifier)
	mgr.SetClock(fixedClock(now))
	return mgr, notifier, alerts
}

func TestManager_RecordUsage_FiresWarningAtThreshold(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	mgr, notifier, _ := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxObjects: 100},
		now,
	)

	// 79% — below the threshold; no alert.
	fired, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 79)
	if err != nil {
		t.Fatalf("RecordUsage 79: %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("at 79%% expected no alerts, got %d", len(fired))
	}
	if len(notifier.seen()) != 0 {
		t.Fatal("notifier called below threshold")
	}

	// Cross to 81% — exactly one warning alert.
	fired, err = mgr.RecordUsage(ctx, "acme", MetricObjects, 2)
	if err != nil {
		t.Fatalf("RecordUsage 81: %v", err)
	}
	if len(fired) != 1 {
		t.Fatalf("at 81%% expected 1 alert, got %d", len(fired))
	}
	if fired[0].Threshold != AlertThresholdWarning {
		t.Fatalf("expected warning threshold, got %d", fired[0].Threshold)
	}
	if got := notifier.seen(); len(got) != 1 || got[0].Threshold != AlertThresholdWarning {
		t.Fatalf("notifier saw %+v, want one warning", got)
	}

	// Another increment that stays in 80–99% must NOT re-fire (dedup).
	fired, err = mgr.RecordUsage(ctx, "acme", MetricObjects, 5)
	if err != nil {
		t.Fatalf("RecordUsage +5: %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("dedup: expected no fresh alerts, got %d", len(fired))
	}
	if got := notifier.seen(); len(got) != 1 {
		t.Fatalf("dedup: notifier should have only 1 call, got %d", len(got))
	}
}

func TestManager_RecordUsage_FiresLimitAtCap(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	mgr, notifier, _ := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxStorage: 1000},
		now,
	)

	// Big jump that crosses BOTH 80% and 100% in one call → both fire,
	// in ascending threshold order.
	fired, err := mgr.RecordUsage(ctx, "acme", MetricStorage, 1500)
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if len(fired) != 2 {
		t.Fatalf("expected 2 alerts (warning+limit), got %d (%+v)", len(fired), fired)
	}
	if fired[0].Threshold != AlertThresholdWarning {
		t.Fatalf("first alert threshold = %d, want 80", fired[0].Threshold)
	}
	if fired[1].Threshold != AlertThresholdLimit {
		t.Fatalf("second alert threshold = %d, want 100", fired[1].Threshold)
	}
	if fired[1].Percent != 100 {
		t.Fatalf("limit alert percent = %d, want clamped 100", fired[1].Percent)
	}
	if got := notifier.seen(); len(got) != 2 {
		t.Fatalf("notifier dispatched %d alerts, want 2", len(got))
	}
}

func TestManager_RecordUsage_NoCap_NeverFires(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	// Tenant has a row but MaxObjects=0 (unlimited) — must not alert.
	mgr, notifier, _ := newAlertingManager(t,
		&Quota{Tenant: "acme"},
		now,
	)

	fired, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 1_000_000)
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("no cap → no alert, got %d", len(fired))
	}
	if len(notifier.seen()) != 0 {
		t.Fatal("no cap → notifier should not fire")
	}
}

func TestManager_RecordUsage_MissingUsageStore_NoOp(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore())
	fired, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 100)
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if len(fired) != 0 {
		t.Fatalf("expected no alerts without usage store, got %d", len(fired))
	}
}

func TestManager_RecordUsage_RejectsUnknownMetric(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore()).WithUsageStore(NewMemoryUsageStore(nil))
	_, err := mgr.RecordUsage(ctx, "acme", "garbage", 1)
	if err == nil {
		t.Fatal("expected error for unknown metric")
	}
}

func TestManager_RecordUsage_MissingTenant_NoOp(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(NewMemoryStore()).WithUsageStore(NewMemoryUsageStore(nil))
	fired, err := mgr.RecordUsage(ctx, "", MetricObjects, 1)
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if len(fired) != 0 {
		t.Fatal("empty tenant must be a no-op")
	}
}

func TestManager_RecordUsage_PropagatesNotifierError_ButRecordsAlert(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	mgr, notifier, alerts := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxObjects: 100},
		now,
	)
	notifier.failErr = errors.New("transport down")

	fired, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 85)
	if err != nil {
		t.Fatalf("RecordUsage should swallow notifier errors, got %v", err)
	}
	if len(fired) != 1 {
		t.Fatalf("expected 1 alert recorded, got %d", len(fired))
	}
	// Ledger row must exist so we don't re-dispatch on the next call —
	// fail-closed on dedup is the right contract because retries via a
	// background sweep can re-attempt notifier delivery later.
	rows, err := alerts.ListAlerts(ctx, time.Time{})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(rows))
	}
}

func TestManager_EvaluateThresholds_FiresWithoutIncrement(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	mgr, notifier, _ := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxObjects: 100, MaxStorage: 1000},
		now,
	)

	// Seed usage directly, bypassing RecordUsage so the alert path
	// hasn't fired yet.
	month := MonthStart(now)
	if err := mgr.usage.AddUsage(ctx, "acme", month, MetricObjects, 90); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	if err := mgr.usage.AddUsage(ctx, "acme", month, MetricStorage, 999); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	fired, err := mgr.EvaluateThresholds(ctx, "acme")
	if err != nil {
		t.Fatalf("EvaluateThresholds: %v", err)
	}
	// objects 90% (warning); storage 99% (warning) — 2 alerts.
	if len(fired) != 2 {
		t.Fatalf("expected 2 alerts, got %d (%+v)", len(fired), fired)
	}
	if got := notifier.seen(); len(got) != 2 {
		t.Fatalf("notifier dispatches = %d, want 2", len(got))
	}
}

func TestManager_IsBlocked_AtCap(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	mgr, _, _ := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxObjects: 100},
		now,
	)

	if mgr.IsBlocked(ctx, "acme", MetricObjects) {
		t.Fatal("IsBlocked at 0 usage should be false")
	}

	if _, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 100); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if !mgr.IsBlocked(ctx, "acme", MetricObjects) {
		t.Fatal("IsBlocked at 100% should be true")
	}

	// Different metric / no cap → not blocked.
	if mgr.IsBlocked(ctx, "acme", MetricStorage) {
		t.Fatal("IsBlocked on uncapped metric should be false")
	}
	// Unknown tenant → false.
	if mgr.IsBlocked(ctx, "ghost", MetricObjects) {
		t.Fatal("IsBlocked on unknown tenant should be false")
	}
	// Empty tenant → false.
	if mgr.IsBlocked(ctx, "", MetricObjects) {
		t.Fatal("IsBlocked on empty tenant should be false")
	}
}

func TestManager_RecordUsage_MonthRollover_FiresAgainNextMonth(t *testing.T) {
	ctx := context.Background()
	mgr, notifier, _ := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxObjects: 100},
		time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
	)

	// Cross 80% in May.
	if _, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 85); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	// Move clock to June; usage counters are per-month so the new
	// counter starts at 0. A bump that re-crosses 80% must alert again.
	mgr.SetClock(fixedClock(time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)))
	if _, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 85); err != nil {
		t.Fatalf("RecordUsage June: %v", err)
	}

	got := notifier.seen()
	if len(got) != 2 {
		t.Fatalf("notifier saw %d alerts, want 2 (one per month)", len(got))
	}
	if got[0].Month == got[1].Month {
		t.Fatalf("month rollover should produce distinct months, got %s/%s", got[0].Month, got[1].Month)
	}
}

func TestManager_MonthlyUsageFor_ReadsThroughCurrentMonth(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	mgr, _, _ := newAlertingManager(t,
		&Quota{Tenant: "acme", MaxObjects: 1000},
		now,
	)

	if _, err := mgr.RecordUsage(ctx, "acme", MetricObjects, 250); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	rows, err := mgr.MonthlyUsageFor(ctx, "acme")
	if err != nil {
		t.Fatalf("MonthlyUsageFor: %v", err)
	}
	if len(rows) != len(ValidMetrics) {
		t.Fatalf("rows = %d, want %d", len(rows), len(ValidMetrics))
	}
	for _, r := range rows {
		if r.Metric == MetricObjects {
			if r.Amount != 250 || r.Cap != 1000 || r.Percent != 25 {
				t.Fatalf("objects row = %+v", r)
			}
		}
	}
}
