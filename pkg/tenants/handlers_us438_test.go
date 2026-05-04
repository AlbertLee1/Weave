package tenants

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// newAlertingHandlerRouter wires a Handler against a manager that has
// usage + alert + notifier hooks. Returns the router so HTTP-level
// tests can exercise the full chain.
func newAlertingHandlerRouter(t *testing.T, now time.Time) (*Handler, *chi.Mux, *fakeNotifier, Store) {
	t.Helper()
	quotas := NewMemoryStore()
	usage := NewMemoryUsageStore(quotas)
	alerts := NewMemoryAlertStore()
	notifier := &fakeNotifier{}
	mgr := NewManager(quotas).
		WithUsageStore(usage).
		WithAlertStore(alerts).
		WithNotifier(notifier)
	mgr.SetClock(fixedClock(now))
	h := NewHandler(quotas, mgr)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return h, r, notifier, quotas
}

func TestHandler_AddUsage_BumpsCounterAndFiresAlerts(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	_, r, notifier, quotas := newAlertingHandlerRouter(t, now)

	// Configure a quota so the threshold check fires.
	if err := quotas.CreateQuota(context.Background(), &Quota{
		Tenant:     "acme",
		MaxObjects: 100,
	}); err != nil {
		t.Fatalf("CreateQuota: %v", err)
	}

	// Bump usage to 85 (crosses 80% but not 100%).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-usage/acme/objects", map[string]interface{}{
		"delta": 85,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("AddUsage: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	var resp addUsageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if len(resp.Fired) != 1 {
		t.Fatalf("fired alerts = %d, want 1 (warning)", len(resp.Fired))
	}
	if resp.Fired[0].Threshold != AlertThresholdWarning {
		t.Fatalf("fired threshold = %d, want %d", resp.Fired[0].Threshold, AlertThresholdWarning)
	}
	// 3 metrics in the payload — objects with amount=85, others=0.
	if len(resp.Usage) != len(ValidMetrics) {
		t.Fatalf("usage rows = %d, want %d", len(resp.Usage), len(ValidMetrics))
	}
	if got := notifier.seen(); len(got) != 1 {
		t.Fatalf("notifier dispatches = %d, want 1", len(got))
	}
}

func TestHandler_AddUsage_RejectsInvalidMetric(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	_, r, _, _ := newAlertingHandlerRouter(t, now)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-usage/acme/garbage", map[string]interface{}{
		"delta": 10,
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid metric: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_AddUsage_RejectsInvalidTenant(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	_, r, _, _ := newAlertingHandlerRouter(t, now)

	w := httptest.NewRecorder()
	// "!!" fails the tenant character check.
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-usage/!!/objects", map[string]interface{}{
		"delta": 10,
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid tenant: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandler_GetUsage_ReturnsEmptyArrayBeforeFirstWrite(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	_, r, _, quotas := newAlertingHandlerRouter(t, now)

	if err := quotas.CreateQuota(context.Background(), &Quota{
		Tenant:     "acme",
		MaxObjects: 100,
	}); err != nil {
		t.Fatalf("CreateQuota: %v", err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodGet, "/api/admin/tenant-usage/acme", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GetUsage: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp usageListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Usage) != len(ValidMetrics) {
		t.Fatalf("usage rows = %d, want %d (one per metric, all zeros)", len(resp.Usage), len(ValidMetrics))
	}
	for _, r := range resp.Usage {
		if r.Amount != 0 {
			t.Fatalf("expected zero amount before any write, got metric=%s amount=%d", r.Metric, r.Amount)
		}
	}
}

func TestHandler_ListUsage_AcrossTenants(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	_, r, _, quotas := newAlertingHandlerRouter(t, now)

	for _, q := range []*Quota{
		{Tenant: "acme", MaxObjects: 100},
		{Tenant: "globex", MaxStorage: 10000},
	} {
		if err := quotas.CreateQuota(context.Background(), q); err != nil {
			t.Fatalf("CreateQuota: %v", err)
		}
	}
	// Bump globex storage to 9000 (90%, fires warning).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-usage/globex/storage", map[string]interface{}{
		"delta": 9000,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("AddUsage globex: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodGet, "/api/admin/tenant-usage", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListUsage: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp usageListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 2 tenants × 3 metrics = 6 rows.
	if len(resp.Usage) != 6 {
		t.Fatalf("usage rows = %d, want 6", len(resp.Usage))
	}
}

func TestHandler_GetUsage_RequiresAuth(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	_, r, _, _ := newAlertingHandlerRouter(t, now)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/tenant-usage/acme", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anon GET: want 401, got %d", w.Code)
	}
}

func TestHandler_AddUsage_WithoutUsageStore_503(t *testing.T) {
	// Build a Handler against a manager that has no UsageStore — emulates
	// degraded-mode (PG not wired) operation.
	store := NewMemoryStore()
	mgr := NewManager(store) // no WithUsageStore
	h := NewHandler(store, mgr)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authedReq(http.MethodPost, "/api/admin/tenant-usage/acme/objects", map[string]interface{}{
		"delta": 1,
	}))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing usage store: want 500, got %d (%s)", w.Code, w.Body.String())
	}
}
