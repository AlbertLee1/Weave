package main

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/notifications"
	"github.com/liyang/weave/pkg/tenants"
)

// pgTenantUsageStore is the Postgres-backed tenants.UsageStore wired in
// cmd/server. Mirrors the dep-direction trick used by pgTenantQuotaStore:
// pkg/tenants stays free of pgx imports.
type pgTenantUsageStore struct {
	pool *pgxpool.Pool
}

func newPGTenantUsageStore(pool *pgxpool.Pool) *pgTenantUsageStore {
	if pool == nil {
		return nil
	}
	return &pgTenantUsageStore{pool: pool}
}

// AddUsage atomically increments the (tenant, month, metric) counter by
// delta. The GREATEST(..., 0) clamp on the INSERT handles a fresh row
// with a negative delta; the UPDATE clamp keeps an existing row from
// dropping below zero (matches the SQL non-negative CHECK constraint).
func (s *pgTenantUsageStore) AddUsage(ctx context.Context, tenant string, month time.Time, metric string, delta int64) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if !tenants.IsValidMetric(metric) {
		return errInvalidUsageMetric{metric: metric}
	}
	if tenant == "" {
		return errEmptyTenant{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tenant_monthly_usage (tenant, month, metric, amount)
		 VALUES ($1, $2, $3, GREATEST($4::BIGINT, 0))
		 ON CONFLICT (tenant, month, metric) DO UPDATE
		   SET amount     = GREATEST(tenant_monthly_usage.amount + $4::BIGINT, 0),
		       updated_at = NOW()`,
		tenant, tenants.MonthStart(month), metric, delta,
	)
	return err
}

func (s *pgTenantUsageStore) GetMonthlyUsage(ctx context.Context, tenant string, month time.Time) ([]*tenants.MonthlyUsage, error) {
	if s == nil || s.pool == nil || tenant == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT tenant, month, metric, amount, cap, percent
		   FROM tenant_usage_monthly
		  WHERE tenant = $1
		    AND month  = $2
		  ORDER BY metric`,
		tenant, tenants.MonthStart(month),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out, err := scanUsageRows(rows)
	if err != nil {
		return nil, err
	}
	// The view emits one row per (configured tenant × metric). When the
	// tenant has no quota configured at all the view returns empty;
	// surface a synthetic zero-row slice so callers can iterate without
	// nil guards.
	if len(out) == 0 {
		monthKey := tenants.FormatMonth(month)
		for _, metric := range tenants.ValidMetrics {
			out = append(out, &tenants.MonthlyUsage{
				Tenant: tenant,
				Month:  monthKey,
				Metric: metric,
			})
		}
	}
	return out, nil
}

func (s *pgTenantUsageStore) ListMonthlyUsage(ctx context.Context, month time.Time) ([]*tenants.MonthlyUsage, error) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT tenant, month, metric, amount, cap, percent
		   FROM tenant_usage_monthly
		  WHERE month = $1
		  ORDER BY tenant, metric`,
		tenants.MonthStart(month),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsageRows(rows)
}

func scanUsageRows(rows pgx.Rows) ([]*tenants.MonthlyUsage, error) {
	var out []*tenants.MonthlyUsage
	for rows.Next() {
		var (
			r         tenants.MonthlyUsage
			monthScan time.Time
		)
		if err := rows.Scan(&r.Tenant, &monthScan, &r.Metric, &r.Amount, &r.Cap, &r.Percent); err != nil {
			return nil, err
		}
		r.Month = tenants.FormatMonth(monthScan)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// pgTenantAlertStore implements tenants.AlertStore against the
// tenant_quota_alerts dedup table. RecordAlert relies on the table's
// composite primary key (tenant, month, metric, threshold) for the
// at-most-once guarantee per calendar month.
type pgTenantAlertStore struct {
	pool *pgxpool.Pool
}

func newPGTenantAlertStore(pool *pgxpool.Pool) *pgTenantAlertStore {
	if pool == nil {
		return nil
	}
	return &pgTenantAlertStore{pool: pool}
}

func (s *pgTenantAlertStore) RecordAlert(ctx context.Context, a tenants.Alert) (bool, error) {
	if s == nil || s.pool == nil {
		return false, nil
	}
	if !tenants.IsValidMetric(a.Metric) {
		return false, errInvalidUsageMetric{metric: a.Metric}
	}
	if a.Threshold != tenants.AlertThresholdWarning && a.Threshold != tenants.AlertThresholdLimit {
		return false, errUnsupportedThreshold{threshold: a.Threshold}
	}
	if a.Tenant == "" {
		return false, errEmptyTenant{}
	}
	month, err := time.Parse("2006-01-02", a.Month)
	if err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO tenant_quota_alerts (tenant, month, metric, threshold, sent_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (tenant, month, metric, threshold) DO NOTHING`,
		a.Tenant, month, a.Metric, a.Threshold,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *pgTenantAlertStore) ListAlerts(ctx context.Context, month time.Time) ([]*tenants.Alert, error) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	var (
		rows pgx.Rows
		err  error
	)
	if month.IsZero() {
		rows, err = s.pool.Query(ctx,
			`SELECT tenant, month, metric, threshold, sent_at
			   FROM tenant_quota_alerts
			  ORDER BY tenant, metric, threshold`)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT tenant, month, metric, threshold, sent_at
			   FROM tenant_quota_alerts
			  WHERE month = $1
			  ORDER BY tenant, metric, threshold`,
			tenants.MonthStart(month),
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*tenants.Alert
	for rows.Next() {
		var (
			a         tenants.Alert
			monthScan time.Time
		)
		if err := rows.Scan(&a.Tenant, &monthScan, &a.Metric, &a.Threshold, &a.RecordedAt); err != nil {
			return nil, err
		}
		a.Month = tenants.FormatMonth(monthScan)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// ----------------------------------------------------------------------
// dispatcherUsageNotifier — routes usage alerts through the US-429
// notifications.Dispatcher to a configured operator recipient list.
// ----------------------------------------------------------------------

// usageDispatcher is the narrow surface dispatcherUsageNotifier needs.
// Satisfied by *notifications.Dispatcher; carved out so tests can inject
// a fake without spinning up a full preference store.
type usageDispatcher interface {
	DispatchTo(ctx context.Context, userID, title, body, link string) error
	HasChannels() bool
}

// dispatcherUsageNotifier publishes one notification per recipient via
// the multi-channel dispatcher. Recipients is read once at construction
// time so config changes require a server restart, matching how
// WEAVE_OPENAI_API_KEY and friends behave.
type dispatcherUsageNotifier struct {
	dispatcher usageDispatcher
	recipients []string
	fallback   tenants.UsageNotifier
}

func newDispatcherUsageNotifier(d usageDispatcher, recipients []string, fallback tenants.UsageNotifier) *dispatcherUsageNotifier {
	if fallback == nil {
		fallback = tenants.LogUsageNotifier{}
	}
	cleaned := make([]string, 0, len(recipients))
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r != "" {
			cleaned = append(cleaned, r)
		}
	}
	return &dispatcherUsageNotifier{
		dispatcher: d,
		recipients: cleaned,
		fallback:   fallback,
	}
}

// NotifyUsageAlert builds a human-readable title + body and dispatches
// it to every configured recipient. Per-recipient failures are
// swallowed inside the dispatcher; missing dispatcher OR empty
// recipient list both fall through to the LogUsageNotifier so the
// alert is at minimum recorded in the server log.
func (n *dispatcherUsageNotifier) NotifyUsageAlert(ctx context.Context, a tenants.Alert) error {
	useFallback := n == nil || n.dispatcher == nil || !n.dispatcher.HasChannels() || len(n.recipients) == 0
	if useFallback {
		if n != nil && n.fallback != nil {
			return n.fallback.NotifyUsageAlert(ctx, a)
		}
		return tenants.LogUsageNotifier{}.NotifyUsageAlert(ctx, a)
	}
	title, body := buildTenantAlertMessage(a)
	for _, userID := range n.recipients {
		_ = n.dispatcher.DispatchTo(ctx, userID, title, body, "/admin/tenant-usage")
	}
	// Always log too — preserves the alert when notification_preferences
	// are empty for the target users (the dispatcher silently no-ops in
	// that case).
	if n.fallback != nil {
		_ = n.fallback.NotifyUsageAlert(ctx, a)
	}
	return nil
}

// buildTenantUsageNotifier constructs the production tenants.UsageNotifier.
// When a multi-channel registry has at least one driver wired AND the
// recipientCSV resolves to ≥1 non-empty user IDs, alerts are dispatched
// via the US-429 Dispatcher to those operators. Otherwise the notifier
// degrades to LogUsageNotifier so alerts always at minimum land in the
// server log.
//
// userRepo may be nil — recipient lookups are best-effort, and an
// unresolvable user ID still gets a dispatch attempt (the dispatcher
// silently no-ops on missing notification_preferences rows).
func buildTenantUsageNotifier(pool *pgxpool.Pool, userRepo auth.UserRepository, recipientCSV string) tenants.UsageNotifier {
	registry := buildDeliveryRegistry()
	if registry == nil {
		return tenants.LogUsageNotifier{}
	}
	prefStore := newPGNotificationPreferenceStore(pool)
	resolver := newUserEmailResolverAdapter(userRepo)
	dispatcher := notifications.NewDispatcher(registry, prefStore, resolver)
	if !dispatcher.HasChannels() {
		return tenants.LogUsageNotifier{}
	}
	recipients := splitCSV(recipientCSV)
	return newDispatcherUsageNotifier(dispatcher, recipients, tenants.LogUsageNotifier{})
}

// splitCSV trims and drops empty entries from a comma-separated env var.
func splitCSV(in string) []string {
	if in == "" {
		return nil
	}
	parts := strings.Split(in, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func buildTenantAlertMessage(a tenants.Alert) (title, body string) {
	severity := "Warning"
	if a.Threshold >= tenants.AlertThresholdLimit {
		severity = "Limit reached"
	}
	title = severity + ": tenant " + a.Tenant + " " + a.Metric + " usage at " + itoa(a.Percent) + "%"
	body = "Tenant=" + a.Tenant + " month=" + a.Month +
		" metric=" + a.Metric +
		" amount=" + itoa64(a.Amount) +
		" cap=" + itoa64(a.Cap) +
		" percent=" + itoa(a.Percent) + "%" +
		" threshold=" + itoa(a.Threshold) + "%"
	return title, body
}

// errInvalidUsageMetric / errUnsupportedThreshold / errEmptyTenant —
// typed errors surfaced from the PG store paths.
type errInvalidUsageMetric struct{ metric string }

func (e errInvalidUsageMetric) Error() string { return "tenants: unknown metric " + e.metric }

type errUnsupportedThreshold struct{ threshold int }

func (e errUnsupportedThreshold) Error() string {
	return "tenants: unsupported threshold " + itoa(e.threshold)
}

type errEmptyTenant struct{}

func (errEmptyTenant) Error() string { return "tenants: tenant required" }

// Tiny stdlib-free integer formatters used in the alert message
// builder; one alert per metric per month per tenant is the steady
// state, so this hardly matters for performance — it's just a way to
// keep this file from pulling fmt for two formatter calls.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
