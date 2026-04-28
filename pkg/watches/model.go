// Package watches implements per-user "follow this RID" persistence
// (US-337). One row records that a user wants future change events for
// the given target_rid (object, action log, or any future watchable
// resource). The row is unique on (user_id, target_rid) so toggling is
// idempotent.
package watches

import (
	"errors"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/rid"
)

// Watch is the wire + DB shape for a single follow relationship. The id
// is server-assigned so callers don't need to construct one to subscribe;
// unwatch is keyed on target_rid (more ergonomic than juggling the id
// from the create response).
type Watch struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	TargetRID string    `json:"targetRid"`
	CreatedAt time.Time `json:"createdAt"`
}

// ValidateTargetRID enforces the canonical RID prefix on the target so
// the watches table never accumulates rows pointing at malformed
// identifiers. Same shape as comments.ValidateTargetRID.
func ValidateTargetRID(targetRID string) error {
	if strings.TrimSpace(targetRID) == "" {
		return errors.New("targetRid must not be empty")
	}
	if !rid.IsRID(targetRID) {
		return errors.New("targetRid must be a Resource Identifier (ri.<service>.<realm>.<type>.<id>)")
	}
	return nil
}
