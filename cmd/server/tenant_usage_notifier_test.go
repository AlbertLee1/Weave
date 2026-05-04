package main

import (
	"context"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/tenants"
)

type fakeUsageDispatcher struct {
	mu       sync.Mutex
	calls    []dispatchCall
	hasChans bool
}

type dispatchCall struct {
	userID, title, body, link string
}

func (d *fakeUsageDispatcher) DispatchTo(_ context.Context, userID, title, body, link string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dispatchCall{userID, title, body, link})
	return nil
}

func (d *fakeUsageDispatcher) HasChannels() bool { return d.hasChans }

type capturingNotifier struct {
	mu     sync.Mutex
	alerts []tenants.Alert
}

func (n *capturingNotifier) NotifyUsageAlert(_ context.Context, a tenants.Alert) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.alerts = append(n.alerts, a)
	return nil
}

func TestDispatcherUsageNotifier_DispatchesPerRecipientAndLogs(t *testing.T) {
	dispatcher := &fakeUsageDispatcher{hasChans: true}
	fallback := &capturingNotifier{}
	n := newDispatcherUsageNotifier(dispatcher, []string{"u1", "u2"}, fallback)

	a := tenants.Alert{
		Tenant:    "acme",
		Month:     "2026-05-01",
		Metric:    tenants.MetricObjects,
		Threshold: tenants.AlertThresholdWarning,
		Amount:    80,
		Cap:       100,
		Percent:   80,
	}
	if err := n.NotifyUsageAlert(context.Background(), a); err != nil {
		t.Fatalf("NotifyUsageAlert: %v", err)
	}
	if len(dispatcher.calls) != 2 {
		t.Fatalf("dispatch calls = %d, want 2", len(dispatcher.calls))
	}
	if dispatcher.calls[0].userID != "u1" || dispatcher.calls[1].userID != "u2" {
		t.Fatalf("unexpected recipients: %+v", dispatcher.calls)
	}
	if dispatcher.calls[0].title == "" || dispatcher.calls[0].body == "" {
		t.Fatalf("title/body should be non-empty: %+v", dispatcher.calls[0])
	}
	// Fallback log path should also have fired.
	if len(fallback.alerts) != 1 {
		t.Fatalf("fallback alerts = %d, want 1", len(fallback.alerts))
	}
}

func TestDispatcherUsageNotifier_FallsBackWhenNoChannels(t *testing.T) {
	dispatcher := &fakeUsageDispatcher{hasChans: false}
	fallback := &capturingNotifier{}
	n := newDispatcherUsageNotifier(dispatcher, []string{"u1"}, fallback)

	a := tenants.Alert{Tenant: "acme", Month: "2026-05-01", Metric: tenants.MetricObjects, Threshold: 100}
	if err := n.NotifyUsageAlert(context.Background(), a); err != nil {
		t.Fatalf("NotifyUsageAlert: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("no channels: dispatcher should be skipped, got %d calls", len(dispatcher.calls))
	}
	if len(fallback.alerts) != 1 {
		t.Fatalf("fallback should still fire, got %d", len(fallback.alerts))
	}
}

func TestDispatcherUsageNotifier_FallsBackWhenNoRecipients(t *testing.T) {
	dispatcher := &fakeUsageDispatcher{hasChans: true}
	fallback := &capturingNotifier{}
	n := newDispatcherUsageNotifier(dispatcher, nil, fallback)

	a := tenants.Alert{Tenant: "acme", Month: "2026-05-01", Metric: tenants.MetricStorage, Threshold: 80}
	if err := n.NotifyUsageAlert(context.Background(), a); err != nil {
		t.Fatalf("NotifyUsageAlert: %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("no recipients: dispatcher should be skipped, got %d calls", len(dispatcher.calls))
	}
	if len(fallback.alerts) != 1 {
		t.Fatalf("fallback should fire when no recipients, got %d", len(fallback.alerts))
	}
}

func TestSplitCSV_TrimsAndSkipsEmpty(t *testing.T) {
	got := splitCSV(" u1, ,u2,  u3 ,")
	want := []string{"u1", "u2", "u3"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if splitCSV("") != nil {
		t.Fatal("splitCSV(empty) should be nil")
	}
}

func TestBuildTenantAlertMessage_SeverityDispatch(t *testing.T) {
	warning := tenants.Alert{Tenant: "acme", Month: "2026-05-01", Metric: tenants.MetricObjects, Threshold: 80, Percent: 85}
	limit := tenants.Alert{Tenant: "acme", Month: "2026-05-01", Metric: tenants.MetricObjects, Threshold: 100, Percent: 100}

	wTitle, _ := buildTenantAlertMessage(warning)
	if wTitle == "" || !contains(wTitle, "Warning") {
		t.Fatalf("warning title = %q, want includes Warning", wTitle)
	}
	lTitle, _ := buildTenantAlertMessage(limit)
	if lTitle == "" || !contains(lTitle, "Limit reached") {
		t.Fatalf("limit title = %q, want includes Limit reached", lTitle)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
