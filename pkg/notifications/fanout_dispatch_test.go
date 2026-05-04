package notifications

import (
	"context"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/notifications/delivery"
)

// TestActivityFanout_DispatcherFiresForEachWatcher verifies the
// US-429 dispatcher integration: when a Fanout is wired with a
// Dispatcher, every non-actor watcher receives one driver-side
// delivery in addition to the in-app notification. Failures in one
// channel are scoped to that channel — peers still fire.
func TestActivityFanout_DispatcherFiresForEachWatcher(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{}
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	webhookDriver := &fakeDriver{channel: delivery.ChannelWebhook}
	registry := delivery.NewRegistry()
	registry.Register(slackDriver)
	registry.Register(webhookDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "user:alice", Channel: "slack", Enabled: true, Target: "https://hooks/alice"})
	_ = store.Upsert(ctx, &Preference{UserID: "user:bob", Channel: "webhook", Enabled: true, Target: "https://webhook/bob"})

	dispatcher := NewDispatcher(registry, store, nil)
	f := New(wl, nc).WithDispatcher(dispatcher)

	if err := f.HandleActivity(ctx, Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
		ActorID:    "user:dave",
	}); err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}

	if len(slackDriver.sends) != 1 || slackDriver.sends[0].UserID != "user:alice" {
		t.Errorf("slack should fire once for alice, got %v", slackDriver.sends)
	}
	if len(webhookDriver.sends) != 1 || webhookDriver.sends[0].UserID != "user:bob" {
		t.Errorf("webhook should fire once for bob, got %v", webhookDriver.sends)
	}

	// In-app notifications still fire for both
	if len(nc.calls) != 2 {
		t.Errorf("in-app should still fire for both watchers, got %d", len(nc.calls))
	}
}

// TestActivityFanout_DispatcherSkipsActor verifies the actor is
// excluded from dispatcher routing — same invariant as the legacy
// in-app/email path.
func TestActivityFanout_DispatcherSkipsActor(t *testing.T) {
	wl := &fakeWatcherLister{
		out: map[string][]string{
			"ri.phonograph2-objects.main.object.42": {"user:alice", "user:bob"},
		},
	}
	nc := &fakeNotificationCreator{}
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	registry := delivery.NewRegistry()
	registry.Register(slackDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "user:alice", Channel: "slack", Enabled: true, Target: "x"})
	_ = store.Upsert(ctx, &Preference{UserID: "user:bob", Channel: "slack", Enabled: true, Target: "y"})

	dispatcher := NewDispatcher(registry, store, nil)
	f := New(wl, nc).WithDispatcher(dispatcher)

	if err := f.HandleActivity(ctx, Activity{
		ObjectType: "Employee",
		PrimaryKey: "42",
		EditType:   "MODIFY",
		ActorID:    "user:alice",
	}); err != nil {
		t.Fatalf("HandleActivity: %v", err)
	}

	users := []string{}
	for _, s := range slackDriver.sends {
		users = append(users, s.UserID)
	}
	sort.Strings(users)
	if len(users) != 1 || users[0] != "user:bob" {
		t.Errorf("alice should be skipped as actor, got %v", users)
	}
}
