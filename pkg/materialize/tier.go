package materialize

import (
	"context"
	"errors"
	"time"
)

// TierRouter is the cold-tier read adapter that surfaces materialised
// Parquet rows older than the supplied wall-clock cutoff. It satisfies the
// `objectset.TierRouter` shape (US-407) via Go's structural typing — the
// dependency direction stays one-way (consumer → producer) without an
// explicit import on `pkg/oss/objectset`.
//
// One process-wide TierRouter wrapping the same `*Materializer` that
// Consumer.SetEditMaterializer wires is the canonical setup. The reader
// is read-only against the directory tree the writer maintains, so the
// two surfaces share state without needing extra synchronisation.
type TierRouter struct {
	mat *Materializer
}

// NewTierRouter constructs a TierRouter that reads from the supplied
// Materializer's root directory. A nil Materializer yields a router that
// returns empty results — useful in degraded-mode boots where the cold
// tier is intentionally absent so the executor can still wire the hook
// without conditional plumbing.
func NewTierRouter(m *Materializer) *TierRouter {
	return &TierRouter{mat: m}
}

// ColdPrimaryKeys returns the primary keys of materialised rows whose
// timestamp_ms is at or before `before`. The cold view dedupes per PK
// internally (max __patch_offset wins, deletes drop the row) before
// projecting to the PK list, so the caller never sees stale or
// soft-deleted PKs.
//
// A blank ontologyAPIName or objectType is rejected at the Materializer
// boundary; missing on-disk directories are not errors and yield an
// empty slice — a freshly-deployed system has no cold tier yet, and the
// PRD's "cold query merges into hot" semantics must remain correct in
// that state.
func (r *TierRouter) ColdPrimaryKeys(ctx context.Context, ontologyAPIName, objectType string, before time.Time) ([]string, error) {
	if r == nil || r.mat == nil {
		return nil, nil
	}
	if ontologyAPIName == "" || objectType == "" {
		return nil, errors.New("materialize: ontologyAPIName and objectType required for cold-tier read")
	}
	rows, err := r.mat.BuildSnapshot(ctx, ontologyAPIName, objectType, before)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	pks := make([]string, 0, len(rows))
	for _, row := range rows {
		pks = append(pks, row.PrimaryKey)
	}
	return pks, nil
}
