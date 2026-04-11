package oms

import (
	"encoding/json"
	"time"
)

// LatestObjectState is the post-edit snapshot of a single primary key, used
// when rebuilding a Bleve index from object_history. DELETE tombstones are
// filtered out upstream; consumers only see rows where NewState is non-nil.
type LatestObjectState struct {
	PrimaryKey string
	NewState   json.RawMessage
}

// ObjectHistory records a single revision of an object's state. One row is
// inserted by the funnel consumer for each CREATE / MODIFY / DELETE applied
// to a (objectTypeRID, primaryKey) tuple. Version is a monotonically
// increasing per-PK counter; prev_state is the snapshot before the edit
// (nil for CREATE) and new_state is the snapshot after (nil for DELETE).
type ObjectHistory struct {
	ID            string          `json:"id"`
	ObjectTypeRID string          `json:"objectTypeRid"`
	PrimaryKey    string          `json:"primaryKey"`
	Version       int64           `json:"version"`
	PrevState     json.RawMessage `json:"prevState,omitempty"`
	NewState      json.RawMessage `json:"newState,omitempty"`
	EditType      string          `json:"editType"` // CREATE | MODIFY | DELETE
	ActionLogRID  string          `json:"actionLogRid,omitempty"`
	UserID        string          `json:"userId,omitempty"`
	RecordedAt    time.Time       `json:"recordedAt"`
}
