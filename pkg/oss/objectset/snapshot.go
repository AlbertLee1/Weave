package objectset

import (
	"context"
	"errors"
	"time"
)

// ErrTransactionNotFound is the sentinel a TransactionResolver returns
// when the requested tx_id has no matching row in dataset_transactions.
// The handler maps it to a TransactionNotFound 400 envelope; any other
// error surfaces as TimeTravelFailed so configuration mistakes stay
// visible.
var ErrTransactionNotFound = errors.New("dataset transaction not found")

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

// TransactionResolver translates a US-379 dataset_transactions tx_id into
// the concrete commit timestamp that ?asOf= should target. Implementations
// query the dataset_transactions table; cmd/server wires a thin adapter
// over oms.DatasetTransactionStore. ErrTransactionNotFound (or any error
// the implementation returns) surfaces as a TransactionNotFound 400.
//
// Decoupled from HistorySnapshotProvider so a deployment can wire one
// without the other (e.g. degraded-mode test routers without PG keep the
// timestamp-based asOf path live and just refuse tx-id lookups).
type TransactionResolver interface {
	ResolveTransaction(ctx context.Context, txID string) (time.Time, error)
}

// ErrBranchNotFound is the sentinel a BranchScopeProvider returns when the
// caller asked for a branch with no matching row. The handler maps it to a
// BranchNotFound 400 envelope; any other error surfaces as
// BranchScopeFailed so configuration mistakes stay visible.
var ErrBranchNotFound = errors.New("ontology branch not found")

// BranchScopeProvider rewrites a live ObjectSet result for a non-default
// branch (US-381). The handler short-circuits and never calls the provider
// for branch="main"; for any other branch the provider is consulted with
// the executor's live PrimaryKeys and is expected to return the
// authoritative replacement set visible on that branch. Implementations
// may add branch-only PKs (objects written on the branch but absent from
// main), drop branch-deleted PKs, or return a branch-scoped subset.
//
// Returning ErrBranchNotFound surfaces as BranchNotFound 400; any other
// error becomes BranchScopeFailed.
//
// Kept as a narrow interface so pkg/oss/objectset does not depend on
// pkg/oms for the OntologyBranch shape — cmd/server wires a thin adapter
// when the durable branch overlay (US-383+) lands.
type BranchScopeProvider interface {
	ScopeObjectSet(ctx context.Context, branch, ontologyAPIName, objectTypeAPIName string, livePKs []string) ([]string, error)
}
