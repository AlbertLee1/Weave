// Package reactions implements per-(user, target_rid, emoji) toggle
// persistence (US-342). One row records that a user has reacted with a
// particular emoji to a target_rid; the unique index on
// (user_id, target_rid, emoji) keeps Toggle idempotent. The aggregate
// view returned by /api/v2/reactions sums counts per emoji and lists the
// emojis the caller has personally toggled on so the SPA can render
// pressed state without a second round-trip.
package reactions

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liyang/weave/pkg/rid"
)

// Reaction is the wire + DB shape for a single reaction toggle. The id
// is server-assigned. Removal is keyed on (target, emoji) — more
// ergonomic than juggling the id from the create response.
type Reaction struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	TargetRID string    `json:"targetRid"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"createdAt"`
}

// EmojiCount is one bucket of the aggregate view served by GET
// /api/v2/reactions?targetRid=… — emoji + total count + whether the
// caller has personally toggled it on.
type EmojiCount struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine"`
}

// Summary is the wire shape of GET /api/v2/reactions?targetRid=… —
// emojis is sorted by descending count and ascending emoji for
// deterministic order. Always emits a non-nil Emojis slice so consumers
// don't need a JSON-null branch.
type Summary struct {
	TargetRID string       `json:"targetRid"`
	Emojis    []EmojiCount `json:"emojis"`
}

// MaxEmojiByteLength bounds the per-row emoji string. 32 bytes is enough
// for any single grapheme cluster (a flag-on-skin-tone modifier sequence
// fits) without enabling pathological dumps. Pairs with the matching
// CHECK constraint in migration 000080.
const MaxEmojiByteLength = 32

// MaxEmojiRuneLength is a belt-and-braces guard so a single multi-byte
// emoji that fits the byte cap can't blow past a sensible visual width.
const MaxEmojiRuneLength = 16

// ValidateTargetRID enforces the canonical RID prefix on the target so
// the reactions table never accumulates rows pointing at malformed
// identifiers. Same shape as comments.ValidateTargetRID and
// watches.ValidateTargetRID.
func ValidateTargetRID(targetRID string) error {
	if strings.TrimSpace(targetRID) == "" {
		return errors.New("targetRid must not be empty")
	}
	if !rid.IsRID(targetRID) {
		return errors.New("targetRid must be a Resource Identifier (ri.<service>.<realm>.<type>.<id>)")
	}
	return nil
}

// ValidateEmoji ensures the emoji is non-empty after trim, bounded in
// both bytes and runes, and contains no control characters or
// whitespace. The handler trims the input before calling this so leading
// or trailing whitespace surfaces a "must not be empty" error rather
// than slipping through and corrupting the unique-index key.
func ValidateEmoji(emoji string) error {
	if emoji == "" {
		return errors.New("emoji must not be empty")
	}
	if strings.TrimSpace(emoji) != emoji {
		return errors.New("emoji must not have leading or trailing whitespace")
	}
	if len(emoji) > MaxEmojiByteLength {
		return errors.New("emoji exceeds maximum length")
	}
	if utf8.RuneCountInString(emoji) > MaxEmojiRuneLength {
		return errors.New("emoji exceeds maximum length")
	}
	for _, r := range emoji {
		if r < 0x20 || r == 0x7F {
			return errors.New("emoji must not contain control characters")
		}
	}
	return nil
}
