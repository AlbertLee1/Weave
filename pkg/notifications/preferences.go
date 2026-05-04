package notifications

import (
	"context"
	"errors"
	"time"
)

// ErrPreferenceNotFound is returned by PreferenceStore implementations
// when the requested (userID, channel) row does not exist. Callers can
// errors.Is against it to distinguish "no preference set" from a real
// lookup failure.
var ErrPreferenceNotFound = errors.New("notifications: preference not found")

// Preference is a single user-per-channel delivery preference. The
// table is opt-in: absence of any row for (UserID, Channel) means the
// channel is not active for that user. Target is channel-specific:
//   - email channel: optional override email address; empty means
//     "use the address resolved from auth.UserRepository".
//   - slack channel: full Slack incoming-webhook URL (required).
//   - webhook channel: full HTTP endpoint URL (required).
type Preference struct {
	UserID    string
	Channel   string
	Enabled   bool
	Target    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PreferenceStore is the abstract repository for notification_preferences
// rows. The dispatcher consults ListByUser on every fan-out — a typical
// row is small (≤3 entries per user) so this is cheap; future
// optimisations could cache the rows behind an LRU keyed on userID.
type PreferenceStore interface {
	ListByUser(ctx context.Context, userID string) ([]Preference, error)
	Upsert(ctx context.Context, p *Preference) error
	Delete(ctx context.Context, userID, channel string) error
}
