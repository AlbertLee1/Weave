// Package installedpkgpg ships the PostgreSQL implementation of
// oms.InstalledPackageStore. It lives in its own package (rather than
// inside cmd/server) so the pkg install registry can be exercised by
// tests outside the server binary — notably the godog BDD suite under
// test/bdd/, which talks to the same chi handler the server registers.
package installedpkgpg

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/oms"
)

// Store satisfies oms.InstalledPackageStore for the pkg install flow
// (US-412). Rows live in installed_packages (one per package name,
// name UNIQUE; see migration 000097).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a *Store backed by the supplied pgx pool. The pool
// must be connected against a schema that has migration 000097 applied.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// UpsertInstalledPackage inserts or updates the row keyed by Name and
// back-fills ID / InstalledAt / UpdatedAt onto the supplied pointer.
func (s *Store) UpsertInstalledPackage(ctx context.Context, pkg *oms.InstalledPackage) error {
	manifest := pkg.ManifestJSON
	if len(manifest) == 0 {
		manifest = json.RawMessage("{}")
	}
	migrations := pkg.Migrations
	if migrations == nil {
		migrations = []string{}
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO installed_packages
		   (name, version, ontology, manifest_json, migrations, enabled, installed_by)
		 VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
		 ON CONFLICT (name) DO UPDATE SET
		   version       = EXCLUDED.version,
		   ontology      = EXCLUDED.ontology,
		   manifest_json = EXCLUDED.manifest_json,
		   migrations    = EXCLUDED.migrations,
		   enabled       = EXCLUDED.enabled,
		   installed_by  = EXCLUDED.installed_by,
		   updated_at    = NOW()
		 RETURNING id, installed_at, updated_at`,
		pkg.Name, pkg.Version, pkg.Ontology,
		string(manifest), migrations, pkg.Enabled, pkg.InstalledBy,
	)
	return row.Scan(&pkg.ID, &pkg.InstalledAt, &pkg.UpdatedAt)
}

// GetInstalledPackage looks up by Name. Returns oms.ErrInstalledPackageNotFound
// when no such row exists.
func (s *Store) GetInstalledPackage(ctx context.Context, name string) (*oms.InstalledPackage, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, version, ontology,
		        COALESCE(manifest_json, '{}'::jsonb),
		        COALESCE(migrations, ARRAY[]::TEXT[]),
		        enabled, installed_by, installed_at, updated_at
		 FROM installed_packages WHERE name = $1`, name)
	pkg := &oms.InstalledPackage{}
	var manifest []byte
	var migrations []string
	if err := row.Scan(
		&pkg.ID, &pkg.Name, &pkg.Version, &pkg.Ontology,
		&manifest, &migrations, &pkg.Enabled, &pkg.InstalledBy,
		&pkg.InstalledAt, &pkg.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrInstalledPackageNotFound
		}
		return nil, err
	}
	pkg.ManifestJSON = manifest
	pkg.Migrations = migrations
	return pkg, nil
}

// ListInstalledPackages returns every installed package, newest first.
func (s *Store) ListInstalledPackages(ctx context.Context) ([]oms.InstalledPackage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, version, ontology,
		        COALESCE(manifest_json, '{}'::jsonb),
		        COALESCE(migrations, ARRAY[]::TEXT[]),
		        enabled, installed_by, installed_at, updated_at
		 FROM installed_packages
		 ORDER BY installed_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]oms.InstalledPackage, 0)
	for rows.Next() {
		var pkg oms.InstalledPackage
		var manifest []byte
		var migrations []string
		if err := rows.Scan(
			&pkg.ID, &pkg.Name, &pkg.Version, &pkg.Ontology,
			&manifest, &migrations, &pkg.Enabled, &pkg.InstalledBy,
			&pkg.InstalledAt, &pkg.UpdatedAt,
		); err != nil {
			return nil, err
		}
		pkg.ManifestJSON = manifest
		pkg.Migrations = migrations
		out = append(out, pkg)
	}
	return out, rows.Err()
}

// SetInstalledPackageEnabled toggles the enabled flag without rewriting
// the rest of the row. Returns oms.ErrInstalledPackageNotFound when no
// such row exists.
func (s *Store) SetInstalledPackageEnabled(ctx context.Context, name string, enabled bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE installed_packages SET enabled = $1, updated_at = NOW() WHERE name = $2`,
		enabled, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrInstalledPackageNotFound
	}
	return nil
}

// DeleteInstalledPackage removes the row by name. Returns
// oms.ErrInstalledPackageNotFound when no such row exists.
func (s *Store) DeleteInstalledPackage(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM installed_packages WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrInstalledPackageNotFound
	}
	return nil
}
