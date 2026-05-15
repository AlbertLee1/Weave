// VTX-116 — Scenario archive policy.
//
// After RetentionWindow elapses on a scenario whose status is "applied"
// or "failed", the periodic cron sweep gzips the edits + overrides
// payload, inserts a row into scenarios_archive, and deletes the
// original scenario row. Reads come back through LoadArchivedPayload
// which transparently decompresses the BYTEA.
//
// The cron wiring itself (boot-time scheduler) is intentionally not in
// this file — it depends on the project's cron scheduler choice, which
// is owned at the cmd/server seam. The two functions exported here
// give that wiring everything it needs.

package scenarios

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// RetentionWindow is the age (since CreatedAt) past which an applied or
// failed scenario qualifies for archival. PRD VTX-116 names 90 days.
const RetentionWindow = 90 * 24 * time.Hour

// archivableStatus returns true iff status is one the cron sweep
// archives. Draft and frozen scenarios are kept hot indefinitely.
func archivableStatus(status string) bool {
	return status == "applied" || status == "failed"
}

// ShouldArchive returns true when a scenario qualifies for archival at
// now. It is pure so the cron sweep can decide per row without a DB
// round-trip and the test suite can table-drive every branch.
func ShouldArchive(s Scenario, now time.Time) bool {
	if !archivableStatus(s.Status) {
		return false
	}
	if s.CreatedAt.IsZero() {
		return false
	}
	return now.Sub(s.CreatedAt) >= RetentionWindow
}

// ArchivePayload is the JSON shape gzipped into scenarios_archive.
// Edits + Overrides are the full state required to rehydrate the
// scenario for a transparent read.
type ArchivePayload struct {
	Edits     []ScenarioEdit     `json:"edits"`
	Overrides []ScenarioOverride `json:"overrides"`
}

// CompressArchivePayload marshals + gzips an ArchivePayload. Exported so
// callers can write the bytes via any DB driver — the cron sweep wires
// it through *sql.DB but a future Snowflake exporter could use the
// same function unchanged.
func CompressArchivePayload(p ArchivePayload) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal archive payload: %w", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// DecompressArchivePayload is the inverse of CompressArchivePayload —
// used by the read path to surface archived scenarios transparently.
func DecompressArchivePayload(b []byte) (ArchivePayload, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return ArchivePayload{}, fmt.Errorf("gunzip: %w", err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		return ArchivePayload{}, fmt.Errorf("read gzip: %w", err)
	}
	var p ArchivePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return ArchivePayload{}, fmt.Errorf("unmarshal archive payload: %w", err)
	}
	return p, nil
}

// ErrArchived signals that a scenario lookup resolved to an archived
// row. The caller can decide whether to transparently rehydrate (read
// path) or surface "archived" status (admin path).
type ErrArchived struct {
	ScenarioRID string
}

func (e ErrArchived) Error() string {
	return fmt.Sprintf("scenario %s is archived", e.ScenarioRID)
}

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
