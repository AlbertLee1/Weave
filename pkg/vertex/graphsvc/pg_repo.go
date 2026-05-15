package graphsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/rid"
)

// PGRepo is the PostgreSQL-backed Repo built on the system_graphs +
// system_graph_versions tables from migration 000200. Each Create that has
// versioned=true also writes a v=1 row to history so GetVersion(rid, 1)
// works without a special-case live-row fallback.
type PGRepo struct {
	pool *pgxpool.Pool
}

// NewPGRepo wires a PGRepo over an existing pgx pool. The pool is borrowed,
// not owned.
func NewPGRepo(pool *pgxpool.Pool) *PGRepo {
	return &PGRepo{pool: pool}
}

// Create inserts a new system_graphs row at version=1 and, when
// versioned=true, also seeds an initial system_graph_versions row.
//
// The trigger on system_graphs only fires for UPDATE, so the v=1 seeding
// must happen here explicitly — otherwise GetVersion(rid, 1) would fail for
// freshly-created graphs.
func (r *PGRepo) Create(ctx context.Context, ontologyRID, name, createdBy string, payload json.RawMessage, versioned bool) (*Graph, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{"layers":[],"edges":[]}`)
	}
	g := &Graph{
		RID:         rid.New("vertex", "main", "graph"),
		OntologyRID: ontologyRID,
		Name:        name,
		Version:     1,
		Versioned:   versioned,
		Payload:     payload,
		CreatedBy:   createdBy,
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		`INSERT INTO system_graphs (rid, ontology_rid, name, version, versioned, payload, created_by)
		 VALUES ($1, $2, $3, 1, $4, $5::jsonb, $6)
		 RETURNING created_at, updated_at`,
		g.RID, g.OntologyRID, g.Name, g.Versioned, []byte(g.Payload), g.CreatedBy,
	).Scan(&g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert graph: %w", err)
	}

	if g.Versioned {
		if _, err := tx.Exec(ctx,
			`INSERT INTO system_graph_versions (graph_rid, version, payload, created_at)
			 VALUES ($1, 1, $2::jsonb, $3)`,
			g.RID, []byte(g.Payload), g.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("seed v1 history: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create: %w", err)
	}
	return g, nil
}

// Get reads a single graph by RID, mapping pgx.ErrNoRows to ErrGraphNotFound.
func (r *PGRepo) Get(ctx context.Context, ridStr string) (*Graph, error) {
	g := &Graph{}
	var raw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, name, version, versioned, payload, created_by, created_at, updated_at
		 FROM system_graphs WHERE rid = $1`, ridStr,
	).Scan(&g.RID, &g.OntologyRID, &g.Name, &g.Version, &g.Versioned, &raw,
		&g.CreatedBy, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGraphNotFound
		}
		return nil, fmt.Errorf("get graph: %w", err)
	}
	g.Payload = json.RawMessage(raw)
	return g, nil
}

// Update is a full save: bumps version, rewrites payload, sets updated_at.
// The system_graphs trigger writes a history row when versioned=true. Returns
// the new state.
func (r *PGRepo) Update(ctx context.Context, ridStr string, payload json.RawMessage) (*Graph, error) {
	g := &Graph{}
	var raw []byte
	err := r.pool.QueryRow(ctx,
		`UPDATE system_graphs
		 SET payload = $1::jsonb, version = version + 1, updated_at = NOW()
		 WHERE rid = $2
		 RETURNING rid, ontology_rid, name, version, versioned, payload, created_by, created_at, updated_at`,
		[]byte(payload), ridStr,
	).Scan(&g.RID, &g.OntologyRID, &g.Name, &g.Version, &g.Versioned, &raw,
		&g.CreatedBy, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGraphNotFound
		}
		return nil, fmt.Errorf("update graph: %w", err)
	}
	g.Payload = json.RawMessage(raw)
	return g, nil
}

// UpdateLayout merges per-node positions into payload.positions without
// bumping version. Uses JSONB's `||` operator on the positions sub-object so
// keys NOT mentioned in the patch are preserved — drag of one node must not
// clobber sibling positions (VTX-024 BDD acceptance). When payload.positions
// is absent we seed it to {} via coalesce before the merge so the first
// PATCH after Create works. The trigger doesn't fire because version is
// unchanged.
func (r *PGRepo) UpdateLayout(ctx context.Context, ridStr string, positions json.RawMessage) error {
	if len(positions) == 0 {
		return fmt.Errorf("positions must not be empty")
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE system_graphs
		 SET payload = jsonb_set(
		     payload,
		     '{positions}',
		     COALESCE(payload->'positions', '{}'::jsonb) || $1::jsonb,
		     true),
		     updated_at = NOW()
		 WHERE rid = $2`,
		[]byte(positions), ridStr,
	)
	if err != nil {
		return fmt.Errorf("update layout: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGraphNotFound
	}
	return nil
}

// Duplicate copies an existing graph into a new RID, version=1, with an
// independent payload (deep copy via the JSONB column's natural value
// semantics). Versioned flag is preserved.
func (r *PGRepo) Duplicate(ctx context.Context, sourceRID string) (*Graph, error) {
	src, err := r.Get(ctx, sourceRID)
	if err != nil {
		return nil, err
	}
	// Re-marshal/unmarshal to ensure the caller-visible payload bytes are
	// distinct from src.Payload (defense-in-depth deep copy, since pgx may
	// share the underlying byte slice).
	clone := make(json.RawMessage, len(src.Payload))
	copy(clone, src.Payload)

	return r.Create(ctx, src.OntologyRID, src.Name+" (copy)", src.CreatedBy, clone, src.Versioned)
}

// GetVersion returns the historical snapshot at the given version. The shape
// matches Get's return value but Version/Payload reflect the historical row
// and Versioned is always true (only versioned graphs have history).
func (r *PGRepo) GetVersion(ctx context.Context, ridStr string, version int) (*Graph, error) {
	g := &Graph{}
	var raw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT g.rid, g.ontology_rid, g.name, v.version, g.versioned, v.payload,
		        g.created_by, g.created_at, v.created_at
		 FROM system_graph_versions v
		 JOIN system_graphs g ON g.rid = v.graph_rid
		 WHERE v.graph_rid = $1 AND v.version = $2`,
		ridStr, version,
	).Scan(&g.RID, &g.OntologyRID, &g.Name, &g.Version, &g.Versioned, &raw,
		&g.CreatedBy, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVersionNotFound
		}
		return nil, fmt.Errorf("get version: %w", err)
	}
	g.Payload = json.RawMessage(raw)
	return g, nil
}

// ListVersions returns all history rows for a graph ordered by version ASC.
// Empty slice (not nil) when no history exists, so callers can range freely.
func (r *PGRepo) ListVersions(ctx context.Context, ridStr string) ([]GraphVersion, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT graph_rid, version, payload, created_at
		 FROM system_graph_versions WHERE graph_rid = $1 ORDER BY version ASC`,
		ridStr)
	if err != nil {
		return nil, fmt.Errorf("query versions: %w", err)
	}
	defer rows.Close()

	out := make([]GraphVersion, 0)
	for rows.Next() {
		var v GraphVersion
		var raw []byte
		if err := rows.Scan(&v.GraphRID, &v.Version, &raw, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		v.Payload = json.RawMessage(raw)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Compile-time assertion: PGRepo satisfies the Repo interface.
var _ Repo = (*PGRepo)(nil)
