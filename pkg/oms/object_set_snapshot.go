package oms

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ObjectSetSnapshot is the OMS-side row for the object_set_snapshots table
// (US-224). It freezes a Definition together with the materialised PrimaryKeys
// list and the ObjectType those PKs resolve under so a future GET can return
// the same membership without re-running the original query.
//
// US-365 added DefinitionHash, SnapshotAt, IsImmutable. They are populated
// from migration 000082's columns and let the caller chain follow-up reads
// against the snapshot transaction id and verify the saved definition has
// not changed under it.
type ObjectSetSnapshot struct {
	RID             string          `json:"rid"`
	OntologyAPIName string          `json:"ontologyApiName"`
	ObjectType      string          `json:"objectType"`
	Definition      json.RawMessage `json:"definition"`
	PrimaryKeys     []string        `json:"primaryKeys"`
	Truncated       bool            `json:"truncated,omitempty"`
	CreatedBy       string          `json:"createdBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	DefinitionHash  string          `json:"definitionHash,omitempty"`
	SnapshotAt      int64           `json:"snapshotAt,omitempty"`
	IsImmutable     bool            `json:"isImmutable"`
}

// Validate rejects rows the read path could not interpret. Called by repo
// writes before INSERT so configuration mistakes fail fast at the boundary
// rather than producing rows that 500 on every subsequent read.
func (s ObjectSetSnapshot) Validate() error {
	if s.RID == "" {
		return fmt.Errorf("object set snapshot requires rid")
	}
	if s.OntologyAPIName == "" {
		return fmt.Errorf("object set snapshot requires ontologyApiName")
	}
	if s.ObjectType == "" {
		return fmt.Errorf("object set snapshot requires objectType")
	}
	if len(s.Definition) == 0 {
		return fmt.Errorf("object set snapshot requires definition")
	}
	return nil
}

// ObjectSetSnapshotStore is the narrow read/write surface the snapshot
// handler depends on. Kept outside Repository for the same reason as
// ComputedPropertyStore (US-202) and MediaAssetStore (US-204): every mock
// repo in the test tree would otherwise need stub methods for a row type it
// does not exercise.
type ObjectSetSnapshotStore interface {
	CreateObjectSetSnapshot(ctx context.Context, snap *ObjectSetSnapshot) error
	GetObjectSetSnapshot(ctx context.Context, rid string) (*ObjectSetSnapshot, error)
}
