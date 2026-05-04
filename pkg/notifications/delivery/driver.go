// Package delivery is the multi-channel notification dispatch surface
// added in US-429. The Fanout (pkg/notifications) used to deliver only
// to in-app + SMTP; this package introduces a Driver abstraction that
// lets the same Activity reach a user via any combination of email,
// Slack, or generic JSON webhook depending on their preferences.
//
// Driver implementations live in this package (smtp.go / slack.go /
// webhook.go) so the Fanout's wiring stays declarative — it picks
// drivers off a Registry by channel name and dispatches without
// knowing the concrete transport.
package delivery

import "context"

// Channel constants are the canonical wire values stored in the
// notification_preferences table. Drivers self-report via Channel()
// so the Registry can build a name → Driver map at construction.
const (
	ChannelEmail   = "email"
	ChannelSlack   = "slack"
	ChannelWebhook = "webhook"
)

// Envelope is the per-recipient payload every Driver receives. The
// transport-agnostic shape matches the in-app Notification fields
// (title / body / link) so the same activity dispatched via three
// channels carries identical user-facing semantics regardless of where
// it lands.
//
// Recipient is the channel-specific delivery destination resolved by
// the dispatcher: email address for SMTP, webhook URL for Slack /
// Webhook. An empty Recipient is a soft-skip signal — the driver
// drops the dispatch without surfacing an error.
type Envelope struct {
	Channel    string
	UserID     string
	Title      string
	Body       string
	Link       string
	Recipient  string
	Properties map[string]interface{}
}

// Driver dispatches a single notification on its channel. The wire
// contract is fail-soft: per-recipient errors propagate to the
// dispatcher which logs and swallows them, so one broken webhook never
// poisons the rest of the fan-out. A Driver returning nil for an
// envelope it chose to skip (e.g. SMTP with empty Host) is correct
// behaviour, NOT a contract violation.
type Driver interface {
	Send(ctx context.Context, env Envelope) error
	Channel() string
}

// Registry maps channel names to Drivers. Constructed once at boot and
// passed to the Dispatcher; the empty registry is a no-op that lets
// degraded-mode boots skip the fan-out entirely without nil checks at
// every call site.
type Registry struct {
	drivers map[string]Driver
}

// NewRegistry returns an empty Registry. Use Register to add drivers.
func NewRegistry() *Registry {
	return &Registry{drivers: make(map[string]Driver)}
}

// Register adds a Driver, keyed on its Channel(). A second Register
// with the same channel REPLACES the existing driver — by design, so
// tests can stub a Driver without rebuilding the registry.
func (r *Registry) Register(d Driver) {
	if r == nil || d == nil {
		return
	}
	if r.drivers == nil {
		r.drivers = make(map[string]Driver)
	}
	r.drivers[d.Channel()] = d
}

// Get returns the Driver for the given channel, or nil when none is
// registered. Callers are expected to nil-check — the dispatcher's
// preferences loop skips channels with no driver wired (typical for
// degraded-mode deployments where Slack credentials are not present).
func (r *Registry) Get(channel string) Driver {
	if r == nil {
		return nil
	}
	return r.drivers[channel]
}

// Channels returns the registered channel names in insertion-stable
// order. Used by the dispatcher when a user has no preferences row to
// fall through "all enabled channels" — keeps the fanout deterministic
// across boots even when map iteration order would otherwise leak.
func (r *Registry) Channels() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.drivers))
	for _, c := range []string{ChannelEmail, ChannelSlack, ChannelWebhook} {
		if _, ok := r.drivers[c]; ok {
			out = append(out, c)
		}
	}
	return out
}
