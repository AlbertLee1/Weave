package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// historySnapshotRepo is the narrow oms.Repository subset the US-223
// time-travel adapter needs. Same shape as policyProviderRepo so future
// admin/repo lookups can reuse it; declared local to cmd/server to keep
// the consumer-specific subset out of oms.Repository proper.
type historySnapshotRepo interface {
	GetOntology(ctx context.Context, ridOrApiName string) (*oms.Ontology, error)
	GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error)
}

// historySnapshotStore is the *PGRepository contract the adapter forwards
// to. Lives as a separate interface so contract tests / degraded-mode
// routers can plug a fake. Returns the raw oms.LatestObjectState slice;
// the adapter handles JSON decode + handler-side wire mapping.
type historySnapshotStore interface {
	SnapshotObjectsAt(ctx context.Context, objectTypeRID string, asOf time.Time) ([]oms.LatestObjectState, error)
}

// historySnapshotAdapter satisfies pkg/oss/objectset.HistorySnapshotProvider
// by resolving (ontologyAPIName, objectTypeAPIName) to a concrete
// ObjectType RID via the OMS repository and forwarding to the PG
// snapshot store. Decodes object_history.new_state JSONB into the
// map[string]interface{} shape the handler's WireObject formatter expects.
type historySnapshotAdapter struct {
	repo  historySnapshotRepo
	store historySnapshotStore
}

func newHistorySnapshotAdapter(repo historySnapshotRepo, store historySnapshotStore) *historySnapshotAdapter {
	return &historySnapshotAdapter{repo: repo, store: store}
}

// SnapshotObjectsAt resolves the ObjectType, fetches the per-PK snapshot
// from the PG store, and decodes each new_state JSONB into a property map.
// Errors fail loud — the handler surfaces them as TimeTravelFailed 400 so
// configuration mistakes are visible rather than silently returning empty.
func (a *historySnapshotAdapter) SnapshotObjectsAt(ctx context.Context, ontologyAPIName, objectTypeAPIName string, asOf time.Time) ([]objectset.ObjectSnapshot, error) {
	if a == nil || a.repo == nil || a.store == nil {
		return nil, errors.New("history snapshot adapter not configured")
	}
	if ontologyAPIName == "" {
		return nil, errors.New("history snapshot: ontology api name required")
	}
	ont, err := a.repo.GetOntology(ctx, ontologyAPIName)
	if err != nil {
		return nil, fmt.Errorf("history snapshot: lookup ontology %q: %w", ontologyAPIName, err)
	}
	if ont == nil {
		return nil, fmt.Errorf("history snapshot: ontology %q not found", ontologyAPIName)
	}
	ot, err := a.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectTypeAPIName)
	if err != nil {
		return nil, fmt.Errorf("history snapshot: lookup object type %q: %w", objectTypeAPIName, err)
	}
	if ot == nil {
		return nil, fmt.Errorf("history snapshot: object type %q not found", objectTypeAPIName)
	}

	rows, err := a.store.SnapshotObjectsAt(ctx, ot.RID, asOf)
	if err != nil {
		return nil, fmt.Errorf("history snapshot: scan object_history: %w", err)
	}

	out := make([]objectset.ObjectSnapshot, 0, len(rows))
	for _, row := range rows {
		var props map[string]interface{}
		if len(row.NewState) > 0 {
			if err := json.Unmarshal(row.NewState, &props); err != nil {
				return nil, fmt.Errorf("history snapshot: decode new_state for pk=%q: %w", row.PrimaryKey, err)
			}
		}
		out = append(out, objectset.ObjectSnapshot{
			PrimaryKey: row.PrimaryKey,
			Properties: props,
		})
	}
	return out, nil
}
