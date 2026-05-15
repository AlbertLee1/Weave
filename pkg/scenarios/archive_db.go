// VTX-116 — Scenario archive DB glue.
//
// The PG-bound load/store half of pkg/scenarios/archive.go. Kept in its
// own file so the unit-test coverage gate (covercheck excludeFiles) can
// drop it alongside the other integration-only files (pg_repo.go,
// pg_store.go, ...). The pure compression / status helpers stay in
// archive.go and are unit-tested without a database.

package scenarios

import (
	"context"
	"database/sql"
	"fmt"
)

// LoadArchivedPayload fetches and decompresses a scenarios_archive row
// by RID. Returns sql.ErrNoRows when the RID is not archived (callers
// should fall back to a regular scenarios.Repo lookup).
func LoadArchivedPayload(ctx context.Context, db *sql.DB, scenarioRID string) (ArchivePayload, error) {
	const q = `SELECT compressed_payload FROM scenarios_archive WHERE scenario_rid = $1`
	var blob []byte
	if err := db.QueryRowContext(ctx, q, scenarioRID).Scan(&blob); err != nil {
		return ArchivePayload{}, err
	}
	return DecompressArchivePayload(blob)
}

// ArchiveOne writes one scenario into scenarios_archive (compressing
// its payload) and removes it from scenarios + scenario_edits +
// scenario_overrides via FK cascade. The whole thing runs in one
// transaction so an interrupted run leaves no half-archived state.
func ArchiveOne(ctx context.Context, db *sql.DB, s Scenario, payload ArchivePayload) error {
	blob, err := CompressArchivePayload(payload)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scenarios_archive
		    (scenario_rid, case_study_rid, name, parent_ontology_commit,
		     status, created_by, created_at, compressed_payload)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (scenario_rid) DO NOTHING`,
		s.RID, s.CaseStudyRID, s.Name, s.ParentOntologyCommit,
		s.Status, s.CreatedBy, s.CreatedAt, blob,
	); err != nil {
		return fmt.Errorf("insert scenarios_archive: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM scenarios WHERE rid = $1`, s.RID); err != nil {
		return fmt.Errorf("delete scenarios: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
