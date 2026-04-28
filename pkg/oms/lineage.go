package oms

import (
	"context"
	"fmt"
	"time"
)

// LineageEdge records a single upstream→downstream provenance relation. One
// row is appended for every CREATE/MODIFY/DELETE applied to an object so
// the platform can answer "where did this object come from?". Append-only:
// existing rows are NEVER updated (a re-write mints a fresh row).
type LineageEdge struct {
	ID            int64     `json:"id"`
	UpstreamRID   string    `json:"upstreamRid"`
	DownstreamRID string    `json:"downstreamRid"`
	Operation     string    `json:"operation"`
	Timestamp     time.Time `json:"timestamp"`
}

// LineageStore is the narrow interface every write path uses to persist
// lineage edges. Implementations are expected to be safe for concurrent use.
// Defined here (rather than as an extension of Repository) so degraded-mode /
// test routers can leave it unwired without dragging the cascade-stub problem
// across ~15 mock implementations of oms.Repository.
type LineageStore interface {
	// InsertLineageEdge appends one edge and back-fills edge.ID + a non-zero
	// edge.Timestamp on success. Implementations should accept a zero-valued
	// edge.Timestamp and substitute the wall-clock time so callers can stay
	// time-source agnostic.
	InsertLineageEdge(ctx context.Context, edge *LineageEdge) error
	// ListUpstreamLineage returns the most recent up-to limit edges whose
	// downstream_rid matches. Newest-first ordering. limit <= 0 falls back
	// to a sensible default.
	ListUpstreamLineage(ctx context.Context, downstreamRID string, limit int) ([]LineageEdge, error)
	// ListDownstreamLineage is the inverse — given an upstream RID, return
	// the most recent up-to limit downstream edges. Used by callers that
	// want to fan out from a single source (e.g. "what objects did this
	// pipeline run produce?").
	ListDownstreamLineage(ctx context.Context, upstreamRID string, limit int) ([]LineageEdge, error)
}

// ActionLogLineageRID builds the canonical upstream RID for an action-log
// row identified by its auto-incremented BIGSERIAL id. Returns "" for
// non-positive ids so the caller can detect an unstamped log row without
// guessing.
func ActionLogLineageRID(actionLogID int64) string {
	if actionLogID <= 0 {
		return ""
	}
	return fmt.Sprintf("ri.actions.main.action-log.%d", actionLogID)
}

// ObjectLineageRID builds the canonical downstream RID for an object. The
// shape mirrors oss.FormatObject's wire format with an optional objectType
// segment so call sites that only have a primary key (legacy paths) and
// call sites that have both (modern paths) emit consistent RIDs. Returns
// "" when primaryKey is empty so the caller can short-circuit before
// inserting an unusable edge.
func ObjectLineageRID(objectType, primaryKey string) string {
	if primaryKey == "" {
		return ""
	}
	if objectType == "" {
		return fmt.Sprintf("ri.phonograph2-objects.main.object.%s", primaryKey)
	}
	return fmt.Sprintf("ri.phonograph2-objects.main.object.%s.%s", objectType, primaryKey)
}
