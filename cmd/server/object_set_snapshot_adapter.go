package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// objectSetSnapshotAdapter satisfies pkg/oss/objectset.PersistedSnapshotStore
// by marshalling Definition to JSON before delegating to the OMS-side store
// (oms.ObjectSetSnapshotStore, served by the uncached *PGRepository) and
// unmarshalling on read. Keeping the bridge in cmd/server preserves the
// dependency direction — pkg/oms never imports pkg/oss/objectset, same shape
// as historySnapshotAdapter (US-223) and the polymorphic invoke dispatcher.
type objectSetSnapshotAdapter struct {
	store oms.ObjectSetSnapshotStore
}

func newObjectSetSnapshotAdapter(store oms.ObjectSetSnapshotStore) *objectSetSnapshotAdapter {
	return &objectSetSnapshotAdapter{store: store}
}

func (a *objectSetSnapshotAdapter) CreatePersistedSnapshot(ctx context.Context, snap *objectset.PersistedSnapshot) error {
	if a == nil || a.store == nil {
		return errors.New("object set snapshot adapter not configured")
	}
	defJSON, err := json.Marshal(snap.Definition)
	if err != nil {
		return fmt.Errorf("marshal definition: %w", err)
	}
	row := &oms.ObjectSetSnapshot{
		RID:             snap.RID,
		OntologyAPIName: snap.OntologyAPIName,
		ObjectType:      snap.ObjectType,
		Definition:      json.RawMessage(defJSON),
		PrimaryKeys:     snap.PrimaryKeys,
		Truncated:       snap.Truncated,
		CreatedBy:       snap.CreatedBy,
		CreatedAt:       snap.CreatedAt,
	}
	return a.store.CreateObjectSetSnapshot(ctx, row)
}

func (a *objectSetSnapshotAdapter) GetPersistedSnapshot(ctx context.Context, rid string) (*objectset.PersistedSnapshot, error) {
	if a == nil || a.store == nil {
		return nil, errors.New("object set snapshot adapter not configured")
	}
	row, err := a.store.GetObjectSetSnapshot(ctx, rid)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return nil, objectset.ErrSnapshotNotFound
		}
		return nil, err
	}
	def := &objectset.Definition{}
	if len(row.Definition) > 0 && string(row.Definition) != "null" {
		if err := json.Unmarshal(row.Definition, def); err != nil {
			return nil, fmt.Errorf("unmarshal definition for %q: %w", rid, err)
		}
	} else {
		def = nil
	}
	return &objectset.PersistedSnapshot{
		RID:             row.RID,
		OntologyAPIName: row.OntologyAPIName,
		ObjectType:      row.ObjectType,
		Definition:      def,
		PrimaryKeys:     row.PrimaryKeys,
		Truncated:       row.Truncated,
		CreatedBy:       row.CreatedBy,
		CreatedAt:       row.CreatedAt,
	}, nil
}
