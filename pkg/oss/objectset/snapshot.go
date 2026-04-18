package objectset

import (
	"context"
	"time"
)

// HistorySnapshotProvider returns the per-PK snapshot of an ObjectType at
// the requested past timestamp. Implementations scan object_history for the
// row whose [valid_from, valid_to) interval covers asOf and decode its
// post-edit state, omitting any PK whose covering row was a DELETE
// tombstone. The empty result means "no objects of this type existed at
// asOf". US-223.
//
// The handler passes both ontologyAPIName and objectTypeAPIName so
// implementations can resolve the ObjectType row in a multi-ontology
// deployment without re-walking chi context.
type HistorySnapshotProvider interface {
	SnapshotObjectsAt(ctx context.Context, ontologyAPIName, objectTypeAPIName string, asOf time.Time) ([]ObjectSnapshot, error)
}

// ObjectSnapshot is one row of a HistorySnapshotProvider response. The
// Properties map is the decoded object_history.new_state at asOf — the same
// shape Bleve hits expose for live reads, so downstream WireObject
// formatting can ignore the time-travel branch.
type ObjectSnapshot struct {
	PrimaryKey string
	Properties map[string]interface{}
}
