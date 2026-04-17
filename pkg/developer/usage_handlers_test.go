package developer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/metrics"
)

// newUsageHarness builds an ApplicationHandler harness with a seeded
// application (owned by ownerID) and a UsageHandler wrapping a fresh
// sample store.
func newUsageHarness(t *testing.T, ownerID, clientID string) (*UsageHandler, *fakeApplicationRepo, *Application, *metrics.UsageSampleStore) {
	t.Helper()
	repo := newFakeApplicationRepo()
	app := &Application{
		Name:             "owned-app",
		ClientID:         clientID,
		ClientSecretHash: HashClientSecret("wsec_" + clientID),
		RedirectURIs:     []string{},
		Scopes:           []string{},
		CreatedBy:        ownerID,
	}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("create: %v", err)
	}
	store := metrics.NewUsageSampleStore(30*24*time.Hour, 100)
	h := NewUsageHandler(repo, store)
	// Pin the clock so tests assert deterministic window boundaries.
	h.now = func() time.Time { return time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC) }
	return h, repo, app, store
}

func usageReq(ownerID, id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/developer/applications/"+id+"/usage", nil)
	return req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: ownerID}))
}

func TestUsageHandler_ReturnsThreeWindows(t *testing.T) {
	h, _, app, store := newUsageHarness(t, "user:alice", "wapp_alice")

	anchor := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	// 2 in last 24h, +1 older than 24h but within 7d, +1 older than 7d but
	// within 30d. The 24h window sees 2, 7d sees 3, 30d sees 4.
	store.Record(app.ClientID, metrics.UsageSample{Timestamp: anchor.Add(-10 * time.Minute), Endpoint: "/a", Method: "GET", Status: 200, Duration: 10 * time.Millisecond})
	store.Record(app.ClientID, metrics.UsageSample{Timestamp: anchor.Add(-1 * time.Hour), Endpoint: "/a", Method: "GET", Status: 500, Duration: 20 * time.Millisecond})
	store.Record(app.ClientID, metrics.UsageSample{Timestamp: anchor.Add(-3 * 24 * time.Hour), Endpoint: "/b", Method: "POST", Status: 201, Duration: 15 * time.Millisecond})
	store.Record(app.ClientID, metrics.UsageSample{Timestamp: anchor.Add(-10 * 24 * time.Hour), Endpoint: "/c", Method: "GET", Status: 200, Duration: 5 * time.Millisecond})

	rec := httptest.NewRecorder()
	h.GetFor(rec, usageReq("user:alice", app.ID), app.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp UsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ApplicationID != app.ID {
		t.Errorf("applicationId: got %q", resp.ApplicationID)
	}
	if resp.ClientID != app.ClientID {
		t.Errorf("clientId: got %q", resp.ClientID)
	}
	if len(resp.Windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(resp.Windows))
	}

	want := map[string]int{"24h": 2, "7d": 3, "30d": 4}
	for _, w := range resp.Windows {
		if got, ok := want[w.Window]; ok && w.Total != got {
			t.Errorf("%s window: expected total=%d, got %d", w.Window, got, w.Total)
		}
	}
	// 24h window: 1 error (500), 1 ok.
	h24 := findWindow(resp.Windows, "24h")
	if h24.Errors != 1 {
		t.Errorf("24h window errors: expected 1, got %d", h24.Errors)
	}
}

func TestUsageHandler_UnauthorizedWithoutUser(t *testing.T) {
	h, _, app, _ := newUsageHarness(t, "user:alice", "wapp_alice")
	req := httptest.NewRequest(http.MethodGet, "/api/v2/developer/applications/"+app.ID+"/usage", nil)
	rec := httptest.NewRecorder()
	h.GetFor(rec, req, app.ID)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestUsageHandler_ForbiddenForNonOwner(t *testing.T) {
	h, _, app, _ := newUsageHarness(t, "user:alice", "wapp_alice")
	rec := httptest.NewRecorder()
	h.GetFor(rec, usageReq("user:bob", app.ID), app.ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUsageHandler_NotFound(t *testing.T) {
	h, _, _, _ := newUsageHarness(t, "user:alice", "wapp_alice")
	rec := httptest.NewRecorder()
	h.GetFor(rec, usageReq("user:alice", "missing-id"), "missing-id")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUsageHandler_MissingID(t *testing.T) {
	h, _, _, _ := newUsageHarness(t, "user:alice", "wapp_alice")
	rec := httptest.NewRecorder()
	h.GetFor(rec, usageReq("user:alice", ""), "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUsageHandler_EmptyWindowsWhenStoreNil(t *testing.T) {
	// A nil sample store is a supported degraded mode — the handler should
	// still return three windows, each with zero counts.
	repo := newFakeApplicationRepo()
	app := &Application{
		Name:         "app",
		ClientID:     "wapp_x",
		RedirectURIs: []string{},
		Scopes:       []string{},
		CreatedBy:    "user:alice",
	}
	_ = repo.Create(context.Background(), app)
	h := NewUsageHandler(repo, nil)
	h.now = func() time.Time { return time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC) }

	rec := httptest.NewRecorder()
	h.GetFor(rec, usageReq("user:alice", app.ID), app.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp UsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Windows) != 3 {
		t.Fatalf("windows: expected 3, got %d", len(resp.Windows))
	}
	for _, w := range resp.Windows {
		if w.Total != 0 {
			t.Errorf("%s total: expected 0, got %d", w.Window, w.Total)
		}
	}
}

func findWindow(windows []metrics.UsageSummary, name string) metrics.UsageSummary {
	for _, w := range windows {
		if w.Window == name {
			return w
		}
	}
	return metrics.UsageSummary{}
}
