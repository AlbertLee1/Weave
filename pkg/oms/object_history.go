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

// Edit source discriminators used by the funnel consumer to resolve
// user/ingest write conflicts (see US-019 / US-021). Rows written by the
// action executor default to SourceUser; pipeline ingestion paths must set
// SourceIngest so that user edits always win a race with stale ingest data.
const (
	EditSourceUser   = "user"
	EditSourceIngest = "ingest"
)

// ObjectHistory records a single revision of an object's state. One row is
// inserted by the funnel consumer for each CREATE / MODIFY / DELETE applied
// to a (objectTypeRID, primaryKey) tuple. Version is a monotonically
// increasing per-PK counter; prev_state is the snapshot before the edit
// (nil for CREATE) and new_state is the snapshot after (nil for DELETE).
// Source tags who produced the edit so downstream conflict resolution can
// prefer user writes over ingest writes (schema column default: 'user').
type ObjectHistory struct {
	ID            string          `json:"id"`
	ObjectTypeRID string          `json:"objectTypeRid"`
	PrimaryKey    string          `json:"primaryKey"`
	Version       int64           `json:"version"`
	PrevState     json.RawMessage `json:"prevState,omitempty"`
	NewState      json.RawMessage `json:"newState,omitempty"`
	EditType      string          `json:"editType"` // CREATE | MODIFY | DELETE
	Source        string          `json:"source,omitempty"`
	ActionLogRID  string          `json:"actionLogRid,omitempty"`
	UserID        string          `json:"userId,omitempty"`
	RecordedAt    time.Time       `json:"recordedAt"`
}
