package comments

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
)

// mentionRegex captures `@<email>` tokens. The leading `@` distinguishes
// an explicit mention from an email that merely appears in the body
// (which the SPA can render as a clickable link without sending a
// notification). Email shape mirrors the conservative pattern used by
// pkg/auth — letters/digits/`._+-%` in the local part, dotted hostnames
// with at least one dot and a 2+ char TLD. Case is preserved as written;
// the caller normalises before lookup.
var mentionRegex = regexp.MustCompile(`@([A-Za-z0-9._+%\-]+@[A-Za-z0-9](?:[A-Za-z0-9\-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9\-]*[A-Za-z0-9])?)+)`)

// ExtractMentions returns the unique `@<email>` mentions found in body.
// Order is preserved for the first occurrence; subsequent duplicates
// (case-insensitive) are dropped. Returns nil when the body contains no
// mentions so callers can skip the resolver hop entirely.
func ExtractMentions(body string) []string {
	if body == "" {
		return nil
	}
	matches := mentionRegex.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		raw := strings.TrimSpace(m[1])
		if raw == "" {
			continue
		}
		key := strings.ToLower(raw)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MentionUser is the directory's view of a user — just enough fields to
// drive the autocomplete UI and notification fan-out. Stays decoupled
// from auth.UserRecord so the comments package keeps a narrow surface.
type MentionUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// ErrMentionUserNotFound is the sentinel a directory returns when the
// requested email is unknown. Callers swallow it and skip the user.
var ErrMentionUserNotFound = errors.New("comments: mention user not found")

// MentionUserDirectory is the narrow lookup surface backing both the
// `/api/v2/mentions/search` autocomplete endpoint and the post-write
// mention resolution path. Implementations may be backed by a database
// (PG users table), an LDAP directory, or any other identity store.
type MentionUserDirectory interface {
	// LookupUserByEmail returns the user whose email matches (case
	// insensitive). Returns ErrMentionUserNotFound when no row matches.
	LookupUserByEmail(ctx context.Context, email string) (MentionUser, error)
	// SearchMentionUsers returns up to limit users matching the query
	// substring (case-insensitive against email + display name).
	// Implementations should clamp limit at a reasonable upper bound
	// (e.g. 25) to keep response sizes predictable.
	SearchMentionUsers(ctx context.Context, query string, limit int) ([]MentionUser, error)
}

// MentionEvent is the payload handed to the notifier when a mention is
// resolved. The notifier is responsible for whatever delivery channel(s)
// the deployment supports — at minimum the in-app notification center;
// future extensions could fan out to email or chat.
type MentionEvent struct {
	RecipientID string
	AuthorID    string
	CommentID   string
	TargetRID   string
	Snippet     string
}

// MentionNotifier dispatches a mention event. Implementations should
// return a non-nil error only for transient/configuration faults; nil is
// the normal "delivered" outcome. Errors are logged + swallowed by the
// comments handler so notification flakes never abort the comment write.
type MentionNotifier interface {
	NotifyMention(ctx context.Context, ev MentionEvent) error
}

// processMentions resolves every mention in c.Body via dir, then fans
// them out to notifier. Self-mentions (recipient == author) are skipped
// — authoring a comment is its own signal of attention. Unknown emails
// drop silently; transient lookup or notify errors are logged but never
// surface to the caller.
//
// Both collaborators are allowed to be nil so degraded-mode wiring (no
// PG, no notifier wired) keeps the comment-write path on the same code.
func processMentions(ctx context.Context, dir MentionUserDirectory, notifier MentionNotifier, c *Comment) {
	if c == nil || c.Body == "" || dir == nil || notifier == nil {
		return
	}
	emails := ExtractMentions(c.Body)
	if len(emails) == 0 {
		return
	}
	seenRecipients := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		user, err := dir.LookupUserByEmail(ctx, email)
		if err != nil {
			if !errors.Is(err, ErrMentionUserNotFound) {
				log.Printf("[comments] mention lookup failed email=%s err=%v", email, err)
			}
			continue
		}
		if user.ID == "" || user.ID == c.Author {
			continue
		}
		if _, dup := seenRecipients[user.ID]; dup {
			continue
		}
		seenRecipients[user.ID] = struct{}{}
		ev := MentionEvent{
			RecipientID: user.ID,
			AuthorID:    c.Author,
			CommentID:   c.ID,
			TargetRID:   c.TargetRID,
			Snippet:     mentionSnippet(c.Body),
		}
		if err := notifier.NotifyMention(ctx, ev); err != nil {
			log.Printf("[comments] mention notify failed recipient=%s err=%v", user.ID, err)
		}
	}
}

// mentionSnippet trims the comment body to a preview suitable for the
// notification.Body field. ~140 chars matches the existing notification
// shape and keeps long comments from blowing out the notification list
// width.
func mentionSnippet(body string) string {
	const max = 140
	body = strings.TrimSpace(body)
	if len(body) <= max {
		return body
	}
	return body[:max] + "…"
}
