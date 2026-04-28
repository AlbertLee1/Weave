// Package notifications implements the US-338 activity fan-out: when an
// object edit is applied, every watcher (US-337) receives a platform
// notification (notification_center row) and, when an SMTP mailer is
// configured, a parallel email. The package is wired by the funnel
// consumer's SetOnChange callback so each applied edit dispatches one
// HandleActivity call. Failures inside the fan-out are logged but never
// abort the underlying edit — notifications are best-effort enrichment.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
)

// ErrEmailNotFound is the sentinel an EmailResolver returns when the
// requested user ID is unknown. Treated as a "skip email, keep in-app"
// signal by the fan-out so a missing email never aborts notification
// delivery.
var ErrEmailNotFound = errors.New("notifications: email not found")

// Activity is the per-edit input the fan-out dispatches on. Mirrors the
// fields the funnel consumer already exposes via ChangeEvent +
// EditBatch.UserID, repackaged so this package never imports pkg/funnel
// (which would form a tangled dep graph since funnel publishes events
// and notifications consumes them).
//
// EditType ∈ {"CREATE", "MODIFY", "DELETE"} — link / unknown types are
// silently dropped at the top of HandleActivity so callers can dispatch
// every change without pre-filtering.
type Activity struct {
	OntologyAPIName string
	ObjectType      string
	PrimaryKey      string
	EditType        string
	ActorID         string
	Properties      map[string]interface{}
}

// WatcherLister is the narrow surface this package needs from
// pkg/watches. Implemented by *watches.MemoryStore and the PG-backed
// adapter in cmd/server.
type WatcherLister interface {
	WatchersFor(ctx context.Context, targetRIDs []string) (map[string][]string, error)
}

// NotificationCreator persists a single in-app notification row. Same
// shape as automate.NotificationCreator — both interfaces are satisfied
// by *oms.OMSHandler so the same notification_center table backs every
// platform-generated notification (mention, watch, automation).
type NotificationCreator interface {
	CreateNotificationForUser(ctx context.Context, userID, title, body, nType, link string) error
}

// Mailer dispatches a single email. Production wiring uses *SMTPMailer;
// tests inject a fake. A nil Mailer disables email delivery — the
// in-app notification still fires.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// EmailResolver looks up the email address for a given userID. Used by
// the fan-out to translate watcher userIDs into envelope recipients.
// Returning ErrEmailNotFound means "skip email, keep in-app" — every
// other error is logged and treated the same way.
type EmailResolver interface {
	ResolveEmail(ctx context.Context, userID string) (string, error)
}

// Fanout dispatches one Activity into N notifications + emails.
type Fanout struct {
	watchers WatcherLister
	creator  NotificationCreator
	mailer   Mailer
	emails   EmailResolver
}

// New constructs a Fanout with the minimum required dependencies (an
// in-app notification creator + a watcher lister). Email delivery is
// opt-in via WithMailer.
func New(watchers WatcherLister, creator NotificationCreator) *Fanout {
	return &Fanout{watchers: watchers, creator: creator}
}

// WithMailer enables the parallel email delivery path. Both arguments
// must be non-nil to take effect — passing a nil mailer or resolver
// leaves email delivery disabled. Returns f for chaining.
func (f *Fanout) WithMailer(m Mailer, r EmailResolver) *Fanout {
	if f == nil {
		return nil
	}
	if m == nil || r == nil {
		return f
	}
	f.mailer = m
	f.emails = r
	return f
}

// HandleActivity runs the fan-out for a single edit. Returns an error
// only when the WatcherLister fails — per-recipient failures are logged
// and swallowed so one bad row never poisons the whole batch.
//
// Semantics:
//   - LINK_CREATE / LINK_DELETE / unknown editType → silent no-op (no
//     watcher lookup), since these are graph operations the user-facing
//     watch experience does not surface.
//   - The actor (a.ActorID) is excluded from the recipient set — a user
//     editing their own watched object should not get notified.
//   - Every non-actor watcher receives one in-app notification; when an
//     SMTPMailer is wired, they also receive a plain-text email at the
//     address returned by the EmailResolver.
func (f *Fanout) HandleActivity(ctx context.Context, a Activity) error {
	if f == nil || f.watchers == nil || f.creator == nil {
		return nil
	}
	if !isObjectEdit(a.EditType) {
		return nil
	}
	target := ComputeTargetRID(a.ObjectType, a.PrimaryKey)
	if target == "" {
		return nil
	}
	watchers, err := f.watchers.WatchersFor(ctx, []string{target})
	if err != nil {
		return fmt.Errorf("notifications: watchers lookup: %w", err)
	}
	recipients := watchers[target]
	if len(recipients) == 0 {
		return nil
	}
	title := buildTitle(a)
	body := buildBody(a)
	link := buildLink(target)

	for _, userID := range recipients {
		if userID == "" || userID == a.ActorID {
			continue
		}
		if err := f.creator.CreateNotificationForUser(ctx, userID, title, body, "watch", link); err != nil {
			log.Printf("notifications: in-app create failed user=%s target=%s err=%v", userID, target, err)
		}
		f.dispatchEmail(ctx, userID, title, body)
	}
	return nil
}

// dispatchEmail is the SMTP side of the fan-out. No-ops cleanly when no
// mailer is wired or the address cannot be resolved; non-recoverable
// SMTP errors are logged and dropped so the in-app delivery is never
// rolled back on a transport hiccup.
func (f *Fanout) dispatchEmail(ctx context.Context, userID, title, body string) {
	if f.mailer == nil || f.emails == nil {
		return
	}
	addr, err := f.emails.ResolveEmail(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrEmailNotFound) {
			log.Printf("notifications: email lookup failed user=%s err=%v", userID, err)
		}
		return
	}
	if addr == "" {
		return
	}
	if err := f.mailer.Send(ctx, addr, title, body); err != nil {
		log.Printf("notifications: email send failed user=%s err=%v", userID, err)
	}
}

// ComputeTargetRID returns the deterministic object RID the SPA pins
// to a WatchButton. Mirrors oss.FormatObject so every changed object
// is matchable against the watches table even though Bleve indexes
// only carry (objectType, primaryKey).
func ComputeTargetRID(objectType, primaryKey string) string {
	if primaryKey == "" {
		return ""
	}
	// Object RIDs use a fixed service ("phonograph2-objects") +
	// realm ("main") shape; the primary key tail is the unique part.
	// Keep this in lockstep with pkg/oss.FormatObject — change one,
	// change the other.
	return "ri.phonograph2-objects.main.object." + primaryKey
}

func isObjectEdit(t string) bool {
	switch t {
	case "CREATE", "MODIFY", "DELETE":
		return true
	default:
		return false
	}
}

func buildTitle(a Activity) string {
	switch a.EditType {
	case "CREATE":
		return fmt.Sprintf("New %s created", a.ObjectType)
	case "DELETE":
		return fmt.Sprintf("%s deleted", a.ObjectType)
	default:
		return fmt.Sprintf("%s updated", a.ObjectType)
	}
}

func buildBody(a Activity) string {
	verb := "updated"
	switch a.EditType {
	case "CREATE":
		verb = "created"
	case "DELETE":
		verb = "deleted"
	}
	actor := a.ActorID
	if actor == "" {
		actor = "Someone"
	}
	return fmt.Sprintf("%s %s %s %s", actor, verb, a.ObjectType, a.PrimaryKey)
}

// buildLink encodes the deep-link the notification dropdown follows.
// The SPA reads `?rid=<targetRid>` and routes to the object detail
// view. Mirrors comments_mentions.buildMentionLink.
func buildLink(targetRID string) string {
	if targetRID == "" {
		return ""
	}
	q := url.Values{}
	q.Set("rid", targetRID)
	return "/watches?" + q.Encode()
}
