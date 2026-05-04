package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/pkg/oms"
)

// pgPackageMigrationRunner satisfies oms.PackageMigrationRunner for the pkg
// install flow (US-412). It writes the supplied SQL files atomically to
// {dataDir}/installed_packages/{name}/migrations/ and runs `golang-migrate
// up` against that directory using the same pgx5 driver the server uses for
// its core migration tree.
//
// Reinstalls are idempotent: golang-migrate persists per-source state in the
// schema_migrations table (one per source URL), so re-running against the
// same per-package directory after a successful first run is a no-op.
type pgPackageMigrationRunner struct {
	dataDir string
	pgDSN   string
}

func newPGPackageMigrationRunner(dataDir, pgDSN string) *pgPackageMigrationRunner {
	return &pgPackageMigrationRunner{dataDir: dataDir, pgDSN: pgDSN}
}

// RunPackageMigrations writes the files to disk under
// {dataDir}/installed_packages/{name}/migrations/ and runs them through
// golang-migrate. The returned int is the number of files written; it is
// always equal to len(files) on success.
func (r *pgPackageMigrationRunner) RunPackageMigrations(ctx context.Context, name string, files []oms.PackageMigrationEntry) (int, error) {
	if strings.TrimSpace(name) == "" {
		return 0, errors.New("package migration runner: empty package name")
	}
	if len(files) == 0 {
		return 0, nil
	}
	dir := filepath.Join(r.dataDir, "installed_packages", name, "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("create migrations dir %s: %w", dir, err)
	}
	for _, f := range files {
		if err := validatePackageMigrationFilename(f.Filename); err != nil {
			return 0, err
		}
		out := filepath.Join(dir, f.Filename)
		tmp := out + ".tmp"
		if err := os.WriteFile(tmp, f.Content, 0o644); err != nil {
			return 0, fmt.Errorf("write migration %s: %w", f.Filename, err)
		}
		if err := os.Rename(tmp, out); err != nil {
			_ = os.Remove(tmp)
			return 0, fmt.Errorf("rename migration %s: %w", f.Filename, err)
		}
	}

	// Skip the actual migrate.New call when the DSN is empty — degraded
	// boot doesn't have PG wired but we still want the disk persistence
	// step to land so a future operator-side run picks up the SQL.
	if strings.TrimSpace(r.pgDSN) == "" {
		return len(files), nil
	}

	src := "file://" + dir
	m, err := migrate.New(src, pgxMigrationDSN(r.pgDSN))
	if err != nil {
		return 0, fmt.Errorf("create package migrator for %s: %w", name, err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("run package migrations for %s: %w", name, err)
	}
	return len(files), nil
}

// validatePackageMigrationFilename mirrors weavepkg.validateMigrationFilename
// so the runner stays defence-in-depth even if a CLI client somehow produced
// a path-traversing filename. A flat basename is required.
func validatePackageMigrationFilename(name string) error {
	if name == "" {
		return errors.New("package migration runner: migration filename is empty")
	}
	clean := filepath.Base(name)
	if clean != name || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("package migration runner: %q must be a plain basename", name)
	}
	return nil
}

// pgxMigrationDSN reuses internal/database's URL-rewriter so the package
// migrator hits the same pgx5 driver as the core migration tree. The
// internal helper isn't exported, so this is the same logic transplanted —
// a future US could promote it to a shared helper.
func pgxMigrationDSN(dsn string) string {
	switch {
	case strings.HasPrefix(dsn, "postgres://"):
		return "pgx5://" + strings.TrimPrefix(dsn, "postgres://")
	case strings.HasPrefix(dsn, "postgresql://"):
		return "pgx5://" + strings.TrimPrefix(dsn, "postgresql://")
	}
	return dsn
}

// silence unused imports in degraded builds — internal/database is pulled
// in for the side-effect driver registration above.
var _ = database.RunMigrationsUp
