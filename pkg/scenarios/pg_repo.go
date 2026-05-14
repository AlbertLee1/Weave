package scenarios

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/rid"
)

// PGRepo is the PostgreSQL-backed implementation of Repo, built on the same
// migrations (000105_vertex_scenarios) as VTX-001. All RIDs minted here use
// the `vertex` service + `main` realm to keep Vertex resources visually
// distinct from `ri.ontology.*` while sharing the same DB schema.
type PGRepo struct {
	pool *pgxpool.Pool
}

// NewPGRepo wires a PGRepo over an existing pgx pool. The pool is borrowed,
// not owned — callers are responsible for shutdown.
func NewPGRepo(pool *pgxpool.Pool) *PGRepo {
	return &PGRepo{pool: pool}
}

// CreateCaseStudy inserts a new case_studies row and returns the populated
// struct. The RID is minted server-side so callers don't accidentally collide
// on UUID generation.
func (r *PGRepo) CreateCaseStudy(ctx context.Context, name, ontologyRID, createdBy string) (*CaseStudy, error) {
	cs := &CaseStudy{
		RID:         rid.New("vertex", "main", "case-study"),
		Name:        name,
		OntologyRID: ontologyRID,
		CreatedBy:   createdBy,
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO case_studies (rid, name, ontology_rid, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING created_at`,
		cs.RID, cs.Name, cs.OntologyRID, cs.CreatedBy,
	).Scan(&cs.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create case study: %w", err)
	}
	return cs, nil
}

// GetCaseStudy returns ErrScenarioNotFound when the RID does not match a row
// (the sentinel is shared across scenario / case-study lookups for caller
// simplicity).
func (r *PGRepo) GetCaseStudy(ctx context.Context, ridStr string) (*CaseStudy, error) {
	cs := &CaseStudy{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, name, ontology_rid, created_by, created_at
		 FROM case_studies WHERE rid = $1`, ridStr,
	).Scan(&cs.RID, &cs.Name, &cs.OntologyRID, &cs.CreatedBy, &cs.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScenarioNotFound
		}
		return nil, err
	}
	return cs, nil
}

// CreateScenario inserts a draft scenario under the given case study. Status
// and immutable are forced to their initial values; downstream Freeze() is
// the only path to flip them.
func (r *PGRepo) CreateScenario(ctx context.Context, caseStudyRID, name, parentCommit, createdBy string) (*Scenario, error) {
	sc := &Scenario{
		RID:                  rid.New("vertex", "main", "scenario"),
		CaseStudyRID:         caseStudyRID,
		Name:                 name,
		ParentOntologyCommit: parentCommit,
		Status:               "draft",
		Immutable:            false,
		CreatedBy:            createdBy,
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO scenarios (rid, case_study_rid, name, parent_ontology_commit, status, immutable, created_by)
		 VALUES ($1, $2, $3, $4, 'draft', FALSE, $5)
		 RETURNING created_at`,
		sc.RID, sc.CaseStudyRID, sc.Name, sc.ParentOntologyCommit, sc.CreatedBy,
	).Scan(&sc.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create scenario: %w", err)
	}
	return sc, nil
}

// GetScenario reads a single scenario by RID, mapping pgx.ErrNoRows to
// ErrScenarioNotFound so callers can pattern-match on the sentinel.
func (r *PGRepo) GetScenario(ctx context.Context, ridStr string) (*Scenario, error) {
	sc := &Scenario{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, case_study_rid, name, parent_ontology_commit, status, immutable, created_by, created_at
		 FROM scenarios WHERE rid = $1`, ridStr,
	).Scan(&sc.RID, &sc.CaseStudyRID, &sc.Name, &sc.ParentOntologyCommit,
		&sc.Status, &sc.Immutable, &sc.CreatedBy, &sc.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScenarioNotFound
		}
		return nil, err
	}
	return sc, nil
}

// AppendEdit checks immutability and inserts the edit atomically inside a
// single transaction so a Freeze() racing with AppendEdit cannot append after
// the flag flips. Returns ErrScenarioImmutable if frozen, ErrScenarioNotFound
// if the scenario row is missing.
func (r *PGRepo) AppendEdit(ctx context.Context, scenarioRID string, edit ScenarioEdit) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var immutable bool
	err = tx.QueryRow(ctx,
		`SELECT immutable FROM scenarios WHERE rid = $1 FOR UPDATE`, scenarioRID,
	).Scan(&immutable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrScenarioNotFound
		}
		return fmt.Errorf("lock scenario: %w", err)
	}
	if immutable {
		return ErrScenarioImmutable
	}

	newValue := edit.NewValue
	if len(newValue) == 0 {
		// JSONB column allows NULL but pgx panics on a typed nil RawMessage
		// in some driver versions; pass nil interface explicitly.
		newValue = nil
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO scenario_edits
		   (scenario_rid, op, object_type, object_id, property, new_value, link_type, src_id, dst_id)
		 VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''))`,
		scenarioRID, edit.Op, edit.ObjectType, edit.ObjectID, edit.Property,
		newValue, edit.LinkType, edit.SrcID, edit.DstID,
	)
	if err != nil {
		return fmt.Errorf("insert edit: %w", err)
	}
	return tx.Commit(ctx)
}

// ListEdits returns all edits for a scenario ordered by seq ASC. Empty slice
// (not nil) is returned when no edits exist, so callers can range without a
// nil-check.
func (r *PGRepo) ListEdits(ctx context.Context, scenarioRID string) ([]ScenarioEdit, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT scenario_rid, seq, op,
		        COALESCE(object_type, ''), COALESCE(object_id, ''),
		        COALESCE(property, ''), new_value,
		        COALESCE(link_type, ''), COALESCE(src_id, ''), COALESCE(dst_id, ''),
		        created_at
		 FROM scenario_edits WHERE scenario_rid = $1 ORDER BY seq ASC`, scenarioRID)
	if err != nil {
		return nil, fmt.Errorf("query edits: %w", err)
	}
	defer rows.Close()

	out := make([]ScenarioEdit, 0)
	for rows.Next() {
		var e ScenarioEdit
		var raw []byte
		if err := rows.Scan(&e.ScenarioRID, &e.Seq, &e.Op,
			&e.ObjectType, &e.ObjectID, &e.Property, &raw,
			&e.LinkType, &e.SrcID, &e.DstID, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan edit: %w", err)
		}
		if len(raw) > 0 {
			e.NewValue = json.RawMessage(raw)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Freeze flips immutable=true and status='frozen'. Re-freezing is a no-op.
// Returns ErrScenarioNotFound if the scenario does not exist.
func (r *PGRepo) Freeze(ctx context.Context, scenarioRID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE scenarios SET immutable = TRUE, status = 'frozen' WHERE rid = $1`,
		scenarioRID)
	if err != nil {
		return fmt.Errorf("freeze: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrScenarioNotFound
	}
	return nil
}

// UpsertOverride writes or replaces a row keyed by
// (scenario_rid, model_rid, parameter, object_id). The DB PK guarantees
// at-most-one row per key; ON CONFLICT replaces value + applied_at.
func (r *PGRepo) UpsertOverride(ctx context.Context, ov ScenarioOverride) error {
	if len(ov.Value) == 0 {
		return fmt.Errorf("override value must not be empty")
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO scenario_overrides (scenario_rid, model_rid, parameter, object_id, value)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (scenario_rid, model_rid, parameter, object_id)
		 DO UPDATE SET value = EXCLUDED.value, applied_at = NOW()`,
		ov.ScenarioRID, ov.ModelRID, ov.Parameter, ov.ObjectID, []byte(ov.Value),
	)
	if err != nil {
		return fmt.Errorf("upsert override: %w", err)
	}
	return nil
}

// ListOverrides returns all overrides for a scenario, ordered by
// (model_rid, parameter, object_id) for stable iteration.
func (r *PGRepo) ListOverrides(ctx context.Context, scenarioRID string) ([]ScenarioOverride, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT scenario_rid, model_rid, parameter, object_id, value, applied_at
		 FROM scenario_overrides WHERE scenario_rid = $1
		 ORDER BY model_rid, parameter, object_id`, scenarioRID)
	if err != nil {
		return nil, fmt.Errorf("query overrides: %w", err)
	}
	defer rows.Close()

	out := make([]ScenarioOverride, 0)
	for rows.Next() {
		var ov ScenarioOverride
		var raw []byte
		if err := rows.Scan(&ov.ScenarioRID, &ov.ModelRID, &ov.Parameter,
			&ov.ObjectID, &raw, &ov.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan override: %w", err)
		}
		ov.Value = json.RawMessage(raw)
		out = append(out, ov)
	}
	return out, rows.Err()
}

// Compile-time assertion: PGRepo satisfies the Repo interface.
var _ Repo = (*PGRepo)(nil)
