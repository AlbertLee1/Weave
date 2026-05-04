package tenants

import (
	"context"
	"testing"
	"time"
)

func TestMemoryAlertStore_RecordAlert_InsertsOnce(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAlertStore()

	a := Alert{
		Tenant:    "acme",
		Month:     "2026-05-01",
		Metric:    MetricObjects,
		Threshold: AlertThresholdWarning,
		Amount:    800,
		Cap:       1000,
		Percent:   80,
	}
	fresh, err := store.RecordAlert(ctx, a)
	if err != nil {
		t.Fatalf("RecordAlert first: %v", err)
	}
	if !fresh {
		t.Fatal("first RecordAlert should report fresh=true")
	}

	fresh, err = store.RecordAlert(ctx, a)
	if err != nil {
		t.Fatalf("RecordAlert second: %v", err)
	}
	if fresh {
		t.Fatal("second RecordAlert should report fresh=false (dedup)")
	}

	// Different threshold for the same key is a separate insert.
	a.Threshold = AlertThresholdLimit
	a.Amount = 1000
	a.Percent = 100
	fresh, err = store.RecordAlert(ctx, a)
	if err != nil {
		t.Fatalf("RecordAlert third: %v", err)
	}
	if !fresh {
		t.Fatal("limit threshold should be a separate fresh insert")
	}
}

func TestMemoryAlertStore_RecordAlert_RejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAlertStore()

	if _, err := store.RecordAlert(ctx, Alert{Tenant: "acme", Metric: "garbage", Threshold: 80}); err == nil {
		t.Fatal("expected error for unknown metric")
	}
	if _, err := store.RecordAlert(ctx, Alert{Tenant: "acme", Metric: MetricObjects, Threshold: 50}); err == nil {
		t.Fatal("expected error for unsupported threshold")
	}
	if _, err := store.RecordAlert(ctx, Alert{Tenant: "", Metric: MetricObjects, Threshold: 80}); err == nil {
		t.Fatal("expected error for empty tenant")
	}
}

func TestMemoryAlertStore_ListAlerts_FiltersByMonth(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAlertStore()

	for _, a := range []Alert{
		{Tenant: "acme", Month: "2026-04-01", Metric: MetricObjects, Threshold: AlertThresholdWarning},
		{Tenant: "acme", Month: "2026-05-01", Metric: MetricObjects, Threshold: AlertThresholdWarning},
		{Tenant: "acme", Month: "2026-05-01", Metric: MetricObjects, Threshold: AlertThresholdLimit},
		{Tenant: "globex", Month: "2026-05-01", Metric: MetricStorage, Threshold: AlertThresholdWarning},
	} {
		if _, err := store.RecordAlert(ctx, a); err != nil {
			t.Fatalf("RecordAlert: %v", err)
		}
	}

	may := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	mayRows, err := store.ListAlerts(ctx, may)
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(mayRows) != 3 {
		t.Fatalf("may rows = %d, want 3", len(mayRows))
	}

	all, err := store.ListAlerts(ctx, time.Time{})
	if err != nil {
		t.Fatalf("ListAlerts(zero): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("all rows = %d, want 4", len(all))
	}
}

func TestLogUsageNotifier_NeverErrors(t *testing.T) {
	if err := (LogUsageNotifier{}).NotifyUsageAlert(context.Background(), Alert{
		Tenant: "acme", Metric: MetricObjects, Threshold: 80,
	}); err != nil {
		t.Fatalf("LogUsageNotifier.NotifyUsageAlert: %v", err)
	}
}
