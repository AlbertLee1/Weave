package notifications

import (
	"context"
	"errors"
	"log"

	"github.com/liyang/weave/pkg/notifications/delivery"
)

// Dispatcher is the per-recipient routing surface introduced in
// US-429. It looks up a user's notification_preferences rows, then
// dispatches one Envelope per enabled (channel, target) tuple via the
// matching Driver in its Registry.
//
// A nil Dispatcher is a no-op so the Fanout's wiring stays branch-
// free at the call site — `dispatcher.DispatchTo(...)` does the right
// thing whether or not multi-channel delivery is wired.
type Dispatcher struct {
	registry    *delivery.Registry
	preferences PreferenceStore

	// emailFallback resolves a UserID to an email address when the
	// user has an enabled `email` preference with empty Target. Mirrors
	// the EmailResolver used by the legacy fan-out.
	emailFallback EmailResolver
}

// NewDispatcher constructs a Dispatcher. registry, preferences, and
// emailFallback may all be nil — the dispatcher gracefully degrades:
// missing registry → DispatchTo returns nil, missing preferences → no
// rows means no deliveries, missing emailFallback → email channel
// without an explicit Target is silently skipped.
func NewDispatcher(registry *delivery.Registry, preferences PreferenceStore, emailFallback EmailResolver) *Dispatcher {
	return &Dispatcher{
		registry:      registry,
		preferences:   preferences,
		emailFallback: emailFallback,
	}
}

// HasChannels reports whether at least one Driver is registered. The
// Fanout calls this before invoking DispatchTo so it can skip the
// preference lookup on degraded-mode boots that registered zero
// channels.
func (d *Dispatcher) HasChannels() bool {
	if d == nil || d.registry == nil {
		return false
	}
	return len(d.registry.Channels()) > 0
}

// DispatchTo runs the fan-out for one recipient. Per-channel failures
// are logged and swallowed so one broken webhook never poisons the
// rest of the fan-out — same shape every other "best effort" hook in
// this codebase follows.
//
// Returns an error only when the PreferenceStore lookup fails — the
// caller-side fan-out logs and continues.
func (d *Dispatcher) DispatchTo(ctx context.Context, userID, title, body, link string) error {
	if d == nil || d.registry == nil || userID == "" {
		return nil
	}
	prefs, err := d.preferencesFor(ctx, userID)
	if err != nil {
		return err
	}
	for _, p := range prefs {
		if !p.Enabled {
			continue
		}
		driver := d.registry.Get(p.Channel)
		if driver == nil {
			continue
		}
		recipient, ok := d.resolveRecipient(ctx, userID, p)
		if !ok {
			continue
		}
		env := delivery.Envelope{
			Channel:   p.Channel,
			UserID:    userID,
			Title:     title,
			Body:      body,
			Link:      link,
			Recipient: recipient,
		}
		if sendErr := driver.Send(ctx, env); sendErr != nil {
			log.Printf("notifications: dispatch %s to user=%s failed: %v", p.Channel, userID, sendErr)
		}
	}
	return nil
}

// preferencesFor returns the user's enabled preferences. The empty
// store / empty user case yields nil rows (no deliveries); the
// dispatcher does NOT fall back to "all channels" because explicit
// opt-in is the contract — a user without any preference row has not
// asked for any notifications via the multi-channel surface.
func (d *Dispatcher) preferencesFor(ctx context.Context, userID string) ([]Preference, error) {
	if d.preferences == nil {
		return nil, nil
	}
	prefs, err := d.preferences.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return prefs, nil
}

// resolveRecipient returns the channel-specific delivery destination
// for one preference row. The boolean return distinguishes "skip this
// row, no error" (false) from "ready to dispatch" (true).
//
// Rules:
//   - email + non-empty Target → use Target
//   - email + empty Target → resolve via emailFallback; ErrEmailNotFound
//     and unset resolver → skip
//   - slack/webhook + non-empty Target → use Target
//   - slack/webhook + empty Target → skip (driver will treat as soft-
//     skip anyway, but skipping early avoids a wasted log line)
func (d *Dispatcher) resolveRecipient(ctx context.Context, userID string, p Preference) (string, bool) {
	if p.Target != "" {
		return p.Target, true
	}
	if p.Channel != delivery.ChannelEmail {
		return "", false
	}
	if d.emailFallback == nil {
		return "", false
	}
	addr, err := d.emailFallback.ResolveEmail(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrEmailNotFound) {
			log.Printf("notifications: email lookup failed user=%s err=%v", userID, err)
		}
		return "", false
	}
	if addr == "" {
		return "", false
	}
	return addr, true
}
