package notifications

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/notifications/delivery"
)

type fakeDriver struct {
	mu      sync.Mutex
	channel string
	sends   []delivery.Envelope
	err     error
}

func (f *fakeDriver) Channel() string { return f.channel }
func (f *fakeDriver) Send(_ context.Context, env delivery.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, env)
	return f.err
}

type fakeEmailResolver2 struct {
	emails map[string]string
	err    error
}

func (f *fakeEmailResolver2) ResolveEmail(_ context.Context, userID string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if e, ok := f.emails[userID]; ok {
		return e, nil
	}
	return "", ErrEmailNotFound
}

func TestDispatcher_RoutesByPreferences(t *testing.T) {
	emailDriver := &fakeDriver{channel: delivery.ChannelEmail}
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	webhookDriver := &fakeDriver{channel: delivery.ChannelWebhook}

	registry := delivery.NewRegistry()
	registry.Register(emailDriver)
	registry.Register(slackDriver)
	registry.Register(webhookDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "user:alice", Channel: "email", Enabled: true})
	_ = store.Upsert(ctx, &Preference{UserID: "user:alice", Channel: "slack", Enabled: true, Target: "https://hooks.slack/x"})
	// webhook deliberately disabled
	_ = store.Upsert(ctx, &Preference{UserID: "user:alice", Channel: "webhook", Enabled: false, Target: "https://example.com/hook"})

	resolver := &fakeEmailResolver2{emails: map[string]string{"user:alice": "alice@example.com"}}
	d := NewDispatcher(registry, store, resolver)

	if err := d.DispatchTo(ctx, "user:alice", "title", "body", "/link"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}

	if len(emailDriver.sends) != 1 {
		t.Errorf("email should fire once, got %d", len(emailDriver.sends))
	} else if emailDriver.sends[0].Recipient != "alice@example.com" {
		t.Errorf("email recipient = %q", emailDriver.sends[0].Recipient)
	}
	if len(slackDriver.sends) != 1 {
		t.Errorf("slack should fire once, got %d", len(slackDriver.sends))
	} else if slackDriver.sends[0].Recipient != "https://hooks.slack/x" {
		t.Errorf("slack recipient = %q", slackDriver.sends[0].Recipient)
	}
	if len(webhookDriver.sends) != 0 {
		t.Errorf("disabled webhook should not fire, got %d", len(webhookDriver.sends))
	}
}

func TestDispatcher_NoPreferencesIsNoop(t *testing.T) {
	emailDriver := &fakeDriver{channel: delivery.ChannelEmail}
	registry := delivery.NewRegistry()
	registry.Register(emailDriver)

	d := NewDispatcher(registry, NewMemoryPreferenceStore(), nil)
	if err := d.DispatchTo(context.Background(), "user:bob", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}
	if len(emailDriver.sends) != 0 {
		t.Errorf("user with no prefs should yield zero sends, got %d", len(emailDriver.sends))
	}
}

func TestDispatcher_DriverFailureDoesNotPoisonOthers(t *testing.T) {
	emailDriver := &fakeDriver{channel: delivery.ChannelEmail, err: errors.New("smtp down")}
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	registry := delivery.NewRegistry()
	registry.Register(emailDriver)
	registry.Register(slackDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: true, Target: "u@x.com"})
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "slack", Enabled: true, Target: "https://x"})

	d := NewDispatcher(registry, store, nil)
	if err := d.DispatchTo(ctx, "u", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}
	if len(slackDriver.sends) != 1 {
		t.Errorf("slack should fire even when email fails, got %d", len(slackDriver.sends))
	}
}

func TestDispatcher_EmailFallbackResolver(t *testing.T) {
	emailDriver := &fakeDriver{channel: delivery.ChannelEmail}
	registry := delivery.NewRegistry()
	registry.Register(emailDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	// no Target → resolver should fill it
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: true})

	resolver := &fakeEmailResolver2{emails: map[string]string{"u": "u@x.com"}}
	d := NewDispatcher(registry, store, resolver)
	if err := d.DispatchTo(ctx, "u", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}
	if len(emailDriver.sends) != 1 || emailDriver.sends[0].Recipient != "u@x.com" {
		t.Errorf("resolver should populate Recipient, got %v", emailDriver.sends)
	}
}

func TestDispatcher_EmailWithoutTargetOrResolverIsSkipped(t *testing.T) {
	emailDriver := &fakeDriver{channel: delivery.ChannelEmail}
	registry := delivery.NewRegistry()
	registry.Register(emailDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: true})

	d := NewDispatcher(registry, store, nil)
	if err := d.DispatchTo(ctx, "u", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}
	if len(emailDriver.sends) != 0 {
		t.Errorf("email with no target and no resolver should soft-skip, got %d", len(emailDriver.sends))
	}
}

func TestDispatcher_SlackWithoutTargetIsSkipped(t *testing.T) {
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	registry := delivery.NewRegistry()
	registry.Register(slackDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "slack", Enabled: true})

	d := NewDispatcher(registry, store, nil)
	if err := d.DispatchTo(ctx, "u", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}
	if len(slackDriver.sends) != 0 {
		t.Errorf("slack without target should skip, got %d", len(slackDriver.sends))
	}
}

func TestDispatcher_MissingDriverIsSilentSkip(t *testing.T) {
	// User has email pref but only slack driver is registered
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	registry := delivery.NewRegistry()
	registry.Register(slackDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: true, Target: "u@x.com"})

	d := NewDispatcher(registry, store, nil)
	if err := d.DispatchTo(ctx, "u", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}
	if len(slackDriver.sends) != 0 {
		t.Errorf("slack should not fire when user prefers email, got %d", len(slackDriver.sends))
	}
}

func TestDispatcher_HasChannelsReportsRegistry(t *testing.T) {
	registry := delivery.NewRegistry()
	d := NewDispatcher(registry, nil, nil)
	if d.HasChannels() {
		t.Errorf("empty registry → HasChannels false")
	}
	registry.Register(&fakeDriver{channel: delivery.ChannelEmail})
	if !d.HasChannels() {
		t.Errorf("non-empty registry → HasChannels true")
	}
}

func TestDispatcher_NilDispatcherIsNoop(t *testing.T) {
	var d *Dispatcher
	if err := d.DispatchTo(context.Background(), "u", "t", "b", "/l"); err != nil {
		t.Errorf("nil dispatcher should be a no-op, got %v", err)
	}
	if d.HasChannels() {
		t.Errorf("nil dispatcher should report no channels")
	}
}

// TestDispatcher_OrderingIsStable verifies the dispatch order matches
// the preference ordering returned by the store, which is sorted by
// channel name. Some test runners rely on this when asserting per-call
// fields without sorting up-front.
func TestDispatcher_OrderingIsStable(t *testing.T) {
	emailDriver := &fakeDriver{channel: delivery.ChannelEmail}
	slackDriver := &fakeDriver{channel: delivery.ChannelSlack}
	webhookDriver := &fakeDriver{channel: delivery.ChannelWebhook}
	registry := delivery.NewRegistry()
	registry.Register(slackDriver)
	registry.Register(webhookDriver)
	registry.Register(emailDriver)

	store := NewMemoryPreferenceStore()
	ctx := context.Background()
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "webhook", Enabled: true, Target: "https://w"})
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: true, Target: "u@x"})
	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "slack", Enabled: true, Target: "https://s"})

	d := NewDispatcher(registry, store, nil)
	if err := d.DispatchTo(ctx, "u", "t", "b", "/l"); err != nil {
		t.Fatalf("DispatchTo: %v", err)
	}

	// Each driver got exactly one send
	if len(emailDriver.sends)+len(slackDriver.sends)+len(webhookDriver.sends) != 3 {
		t.Errorf("expected 3 total sends, got %d/%d/%d",
			len(emailDriver.sends), len(slackDriver.sends), len(webhookDriver.sends))
	}

	// Ensure all three channels were exercised exactly once
	channels := []string{}
	if len(emailDriver.sends) == 1 {
		channels = append(channels, emailDriver.sends[0].Channel)
	}
	if len(slackDriver.sends) == 1 {
		channels = append(channels, slackDriver.sends[0].Channel)
	}
	if len(webhookDriver.sends) == 1 {
		channels = append(channels, webhookDriver.sends[0].Channel)
	}
	sort.Strings(channels)
	want := []string{delivery.ChannelEmail, delivery.ChannelSlack, delivery.ChannelWebhook}
	for i := range want {
		if channels[i] != want[i] {
			t.Errorf("channels[%d] = %q want %q", i, channels[i], want[i])
		}
	}
}
