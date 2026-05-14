// Package graphsvc persists Vertex SystemGraphs: serialized graph workspaces
// composed of layers, edges, saved selections, time settings, and positions.
//
// A Graph is the unit of editing in the Vertex UI. The repo distinguishes two
// kinds of update:
//   - full save (Update): bumps version and writes a row to
//     system_graph_versions (when versioned=true).
//   - layout patch (UpdateLayout): rewrites payload.positions in-place, leaves
//     version alone, and writes no history.
//
// The version-bump rule is enforced both in this Go code AND by the
// AFTER UPDATE trigger installed in migrations/000200_system_graphs.up.sql, so
// raw SQL writers can't accidentally skip history.
package graphsvc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrGraphNotFound is returned when a graph RID does not exist.
var ErrGraphNotFound = errors.New("graph not found")

// ErrVersionNotFound is returned when GetVersion targets a (rid, version)
// pair that has no history row.
var ErrVersionNotFound = errors.New("graph version not found")

// Graph is the canonical Vertex graph row. Payload carries the serialized
// workspace (layers, edges, savedSelections, timeSettings, positions) as
// opaque JSONB — VTX-011 will layer schema validation on top.
type Graph struct {
	RID         string
	OntologyRID string
	Name        string
	Version     int
	Versioned   bool
	Payload     json.RawMessage
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GraphVersion is a historical snapshot of a graph payload at a given version.
type GraphVersion struct {
	GraphRID  string
	Version   int
	Payload   json.RawMessage
	CreatedAt time.Time
}

// Repo is the persistence boundary for SystemGraphs. Mirrors the style of
// pkg/oms.Repository and pkg/scenarios.Repo: small, ctx-first, returns
// sentinel errors that callers can errors.Is against.
type Repo interface {
	Create(ctx context.Context, ontologyRID, name, createdBy string, payload json.RawMessage, versioned bool) (*Graph, error)
	Get(ctx context.Context, rid string) (*Graph, error)

	// Update is a full save: bumps version and writes a history row when
	// versioned=true. Returns the new state.
	Update(ctx context.Context, rid string, payload json.RawMessage) (*Graph, error)

	// UpdateLayout rewrites payload.positions in-place without bumping
	// version or writing history (incremental save for UI drag/zoom).
	UpdateLayout(ctx context.Context, rid string, positions json.RawMessage) error

	// Duplicate creates a new graph with a fresh RID and a deep-copied
	// payload (version reset to 1, history independent of the source).
	Duplicate(ctx context.Context, rid string) (*Graph, error)

	// GetVersion returns the historical snapshot at the given version.
	// Returns ErrVersionNotFound if the (rid, version) pair has no row.
	GetVersion(ctx context.Context, rid string, version int) (*Graph, error)

	// ListVersions returns all history rows for a graph ordered by version
	// ASC. Empty slice (not nil) when no history exists.
	ListVersions(ctx context.Context, rid string) ([]GraphVersion, error)
}
