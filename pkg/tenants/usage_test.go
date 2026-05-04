package tenants

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMonthStart_TruncatesToFirstOfMonthUTC(t *testing.T) {
	cases := []struct {
		in   time.Time
		want time.Time
	}{
		{
			in:   time.Date(2026, 5, 5, 12, 30, 45, 0, time.UTC),
			want: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			in:   time.Date(2026, 5, 31, 23, 59, 59, 0, time.UTC),
			want: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			in:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		got := MonthStart(tc.in)
		if !got.Equal(tc.want) {
			t.Fatalf("MonthStart(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if got := MonthStart(time.Time{}); !got.IsZero() {
		t.Fatalf("MonthStart(zero) = %v, want zero", got)
	}
}

func TestFormatMonth_RendersFirstOfMonth(t *testing.T) {
	got := FormatMonth(time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC))
	if got != "2026-05-01" {
		t.Fatalf("FormatMonth = %q, want %q", got, "2026-05-01")
	}
	if got := FormatMonth(time.Time{}); got != "" {
		t.Fatalf("FormatMonth(zero) = %q, want empty", got)
	}
}

func TestQuotaForMetric_ReturnsConfiguredCap(t *testing.T) {
	q := &Quota{Tenant: "acme", MaxObjects: 1000, MaxStorage: 5_000_000}
	if got := QuotaForMetric(q, MetricObjects); got != 1000 {
		t.Fatalf("MetricObjects cap = %d, want 1000", got)
	}
	if got := QuotaForMetric(q, MetricStorage); got != 5_000_000 {
		t.Fatalf("MetricStorage cap = %d, want 5_000_000", got)
	}
	if got := QuotaForMetric(q, MetricRequests); got != 0 {
		t.Fatalf("MetricRequests cap = %d, want 0 (unlimited)", got)
	}
	if got := QuotaForMetric(nil, MetricObjects); got != 0 {
		t.Fatalf("nil quota cap = %d, want 0", got)
	}
	if got := QuotaForMetric(q, "garbage"); got != 0 {
		t.Fatalf("unknown metric cap = %d, want 0", got)
	}
}

func TestComputePercent_Clamps(t *testing.T) {
	cases := []struct {
		amount, cap int64
		want        int
	}{
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{200, 100, 100}, // clamped
		{75, 0, 0},      // unlimited cap → 0
		{0, 0, 0},
		{-5, 100, 0}, // defensive
	}
	for _, tc := range cases {
		got := computePercent(tc.amount, tc.cap)
		if got != tc.want {
			t.Fatalf("computePercent(%d, %d) = %d, want %d", tc.amount, tc.cap, got, tc.want)
		}
	}
}

func TestMemoryUsageStore_AddAndGet(t *testing.T) {
	ctx := context.Background()
	quotas := NewMemoryStore()
	if err := quotas.CreateQuota(ctx, &Quota{Tenant: "acme", MaxObjects: 1000, MaxStorage: 5_000}); err != nil {
		t.Fatalf("CreateQuota: %v", err)
	}
	store := NewMemoryUsageStore(quotas)

	month := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	if err := store.AddUsage(ctx, "acme", month, MetricObjects, 250); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	if err := store.AddUsage(ctx, "acme", month, MetricObjects, 500); err != nil {
		t.Fatalf("AddUsage second: %v", err)
	}
	if err := store.AddUsage(ctx, "acme", month, MetricStorage, 4000); err != nil {
		t.Fatalf("AddUsage storage: %v", err)
	}

	rows, err := store.GetMonthlyUsage(ctx, "acme", month)
	if err != nil {
		t.Fatalf("GetMonthlyUsage: %v", err)
	}
	if len(rows) != len(ValidMetrics) {
		t.Fatalf("rows = %d, want %d", len(rows), len(ValidMetrics))
	}

	rowsByMetric := map[string]*MonthlyUsage{}
	for _, r := range rows {
		rowsByMetric[r.Metric] = r
	}

	if got := rowsByMetric[MetricObjects]; got.Amount != 750 || got.Cap != 1000 || got.Percent != 75 {
		t.Fatalf("objects row = %+v, want amount=750 cap=1000 percent=75", got)
	}
	if got := rowsByMetric[MetricStorage]; got.Amount != 4000 || got.Cap != 5000 || got.Percent != 80 {
		t.Fatalf("storage row = %+v, want amount=4000 cap=5000 percent=80", got)
	}
	if got := rowsByMetric[MetricRequests]; got.Amount != 0 || got.Cap != 0 || got.Percent != 0 {
		t.Fatalf("requests row = %+v, want zeros (no cap configured)", got)
	}
}

func TestMemoryUsageStore_AddUsage_ClampsAtZero(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUsageStore(nil)
	month := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// negative delta on an empty counter must clamp to 0 (matches the
	// SQL non-negative CHECK constraint).
	if err := store.AddUsage(ctx, "acme", month, MetricObjects, -100); err != nil {
		t.Fatalf("AddUsage neg: %v", err)
	}
	rows, _ := store.GetMonthlyUsage(ctx, "acme", month)
	for _, r := range rows {
		if r.Metric == MetricObjects && r.Amount != 0 {
			t.Fatalf("objects amount = %d, want 0 after clamped negative delta", r.Amount)
		}
	}
}

func TestMemoryUsageStore_AddUsage_RejectsUnknownMetric(t *testing.T) {
	store := NewMemoryUsageStore(nil)
	err := store.AddUsage(context.Background(), "acme", time.Now(), "garbage", 1)
	if err == nil {
		t.Fatal("expected error for unknown metric, got nil")
	}
	if !strings.Contains(err.Error(), "garbage") {
		t.Fatalf("error %q does not mention metric name", err.Error())
	}
}

func TestMemoryUsageStore_AddUsage_RejectsEmptyTenant(t *testing.T) {
	store := NewMemoryUsageStore(nil)
	err := store.AddUsage(context.Background(), "", time.Now(), MetricObjects, 1)
	if err == nil {
		t.Fatal("expected error for empty tenant, got nil")
	}
}

func TestMemoryUsageStore_ListMonthlyUsage_EmitsZeroRowsForConfiguredTenants(t *testing.T) {
	ctx := context.Background()
	quotas := NewMemoryStore()
	if err := quotas.CreateQuota(ctx, &Quota{Tenant: "acme", MaxObjects: 1000}); err != nil {
		t.Fatalf("CreateQuota: %v", err)
	}
	if err := quotas.CreateQuota(ctx, &Quota{Tenant: "globex", MaxStorage: 8_000_000}); err != nil {
		t.Fatalf("CreateQuota: %v", err)
	}
	store := NewMemoryUsageStore(quotas)

	month := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := store.AddUsage(ctx, "globex", month, MetricStorage, 6_400_000); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	rows, err := store.ListMonthlyUsage(ctx, month)
	if err != nil {
		t.Fatalf("ListMonthlyUsage: %v", err)
	}
	// 2 tenants × 3 metrics = 6 rows, and acme should appear before
	// globex so the API surface stays stable.
	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6", len(rows))
	}
	if rows[0].Tenant != "acme" || rows[3].Tenant != "globex" {
		t.Fatalf("rows order = %s,%s, want acme then globex", rows[0].Tenant, rows[3].Tenant)
	}
	for _, r := range rows {
		if r.Tenant == "globex" && r.Metric == MetricStorage {
			if r.Amount != 6_400_000 || r.Cap != 8_000_000 || r.Percent != 80 {
				t.Fatalf("globex storage row = %+v", r)
			}
		}
	}
}
