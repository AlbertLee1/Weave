package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/liyang/weave/internal/backup"
)

// runBackup implements `weave backup -o <bundle.tar.gz>` (US-448). The
// command shells out to pg_dump for the database and walks the data
// directory for Bleve indexes + Parquet materialised tier + media. The
// shell-out is dependency-injected via dumpFn so tests can exercise the
// CLI without a live Postgres.
func runBackup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "output bundle path (required, e.g. backup.tar.gz)")
	dataDir := fs.String("data-dir", defaultBackupDataDir(), "Weave data directory (defaults to $WEAVE_DATA_DIR or ./data)")
	dsn := fs.String("pg-dsn", defaultBackupDSN(), "Postgres DSN (defaults to $PG_DSN)")
	asJSON := fs.Bool("json", false, "emit raw JSON manifest")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*output) == "" {
		fmt.Fprintln(stderr, "weave: -o (output path) is required")
		return 2
	}
	if strings.TrimSpace(*dsn) == "" {
		fmt.Fprintln(stderr, "weave: --pg-dsn is required (or set PG_DSN)")
		return 2
	}

	b := &backup.Bundle{
		DataDir:  *dataDir,
		PGDumpFn: defaultPGDumpFn,
	}
	manifest, err := b.Backup(context.Background(), *dsn, *output)
	if err != nil {
		fmt.Fprintf(stderr, "backup: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(manifest)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	printBackupSummary(stdout, *output, manifest)
	return 0
}

// runRestore implements `weave restore -i <bundle.tar.gz>` (US-448).
func runRestore(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("i", "", "input bundle path (required)")
	dataDir := fs.String("data-dir", defaultBackupDataDir(), "target data directory (defaults to $WEAVE_DATA_DIR or ./data)")
	dsn := fs.String("pg-dsn", defaultBackupDSN(), "Postgres DSN (defaults to $PG_DSN)")
	asJSON := fs.Bool("json", false, "emit raw JSON manifest")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*input) == "" {
		fmt.Fprintln(stderr, "weave: -i (input path) is required")
		return 2
	}
	if strings.TrimSpace(*dsn) == "" {
		fmt.Fprintln(stderr, "weave: --pg-dsn is required (or set PG_DSN)")
		return 2
	}

	b := &backup.Bundle{
		DataDir:     *dataDir,
		PGRestoreFn: defaultPGRestoreFn,
	}
	manifest, err := b.Restore(context.Background(), *dsn, *input)
	if err != nil {
		fmt.Fprintf(stderr, "restore: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(manifest)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	printRestoreSummary(stdout, *input, *dataDir, manifest)
	return 0
}

func defaultBackupDataDir() string {
	if v := os.Getenv("WEAVE_DATA_DIR"); v != "" {
		return v
	}
	return "data"
}

func defaultBackupDSN() string {
	return os.Getenv("PG_DSN")
}

// defaultPGDumpFn is the production dumper — shells out to pg_dump in
// custom format. Tests inject a stub via Bundle.PGDumpFn directly.
var defaultPGDumpFn backup.PGDumpFn = func(ctx context.Context, dsn string, w io.Writer) error {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return fmt.Errorf("pg_dump not in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "pg_dump",
		"--dbname="+dsn,
		"--format=custom",
		"--no-owner",
		"--no-privileges",
	)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// defaultPGRestoreFn is the production restorer — shells out to pg_restore.
var defaultPGRestoreFn backup.PGRestoreFn = func(ctx context.Context, dsn string, r io.Reader) error {
	if _, err := exec.LookPath("pg_restore"); err != nil {
		return fmt.Errorf("pg_restore not in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--dbname="+dsn,
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"--exit-on-error",
	)
	cmd.Stdin = r
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func printBackupSummary(w io.Writer, path string, m backup.Manifest) {
	fmt.Fprintf(w, "Backup written: %s\n", path)
	fmt.Fprintf(w, "  bundle version: %d\n", m.Version)
	fmt.Fprintf(w, "  timestamp:      %s\n", m.Timestamp)
	if c, ok := m.Components["db.dump"]; ok {
		fmt.Fprintf(w, "  db.dump:        %d bytes (sha256 %s)\n", c.Size, shortSHA(c.SHA256))
	}
	if c, ok := m.Components["data"]; ok {
		fmt.Fprintf(w, "  data files:     %d (%d bytes)\n", c.FileCount, c.Size)
	}
}

func printRestoreSummary(w io.Writer, path, dataDir string, m backup.Manifest) {
	fmt.Fprintf(w, "Restore complete: %s → %s\n", path, dataDir)
	fmt.Fprintf(w, "  bundle version: %d\n", m.Version)
	fmt.Fprintf(w, "  timestamp:      %s\n", m.Timestamp)
	if c, ok := m.Components["db.dump"]; ok {
		fmt.Fprintf(w, "  db.dump:        %d bytes (sha256 verified)\n", c.Size)
	}
	if c, ok := m.Components["data"]; ok {
		fmt.Fprintf(w, "  data files:     %d (%d bytes)\n", c.FileCount, c.Size)
	}
}

func shortSHA(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}
