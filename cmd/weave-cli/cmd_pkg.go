package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/liyang/weave/internal/cliclient"
	"github.com/liyang/weave/pkg/weavepkg"
)

// runPkg dispatches the `weave pkg <subcommand>` family. US-411 introduced
// `export`; US-412 adds `install`. Future stories layer publish / lint on
// top without touching main.go.
func runPkg(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave pkg <export|install> [flags]")
		return 2
	}
	switch args[0] {
	case "export":
		return runPkgExport(args[1:], stdout, stderr)
	case "install":
		return runPkgInstall(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave pkg: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runPkgExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pkg export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	output := fs.String("o", "", "output .weavepkg path (required)")
	pkgVersion := fs.String("version", "0.0.0", "package version (semver)")
	pkgName := fs.String("name", "", "package name (defaults to --ontology)")
	author := fs.String("author", "", "package author")
	license := fs.String("license", "", "package license")
	description := fs.String("description", "", "package description")
	minWeaveVersion := fs.String("min-weave-version", "", "minimum Weave server version required")
	migrationsDir := fs.String("migrations-dir", "migrations", "directory containing SQL migration files (set to empty to skip migrations)")
	dependenciesRaw := fs.String("dependencies", "", "comma-separated 'name@version' dependency list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ontology) == "" {
		fmt.Fprintln(stderr, "weave: --ontology is required")
		return 2
	}
	if strings.TrimSpace(*output) == "" {
		fmt.Fprintln(stderr, "weave: -o (output path) is required")
		return 2
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}

	deps, err := parseDependencies(*dependenciesRaw)
	if err != nil {
		fmt.Fprintf(stderr, "weave pkg export: %v\n", err)
		return 2
	}

	manifest := weavepkg.Manifest{
		Name:            firstNonEmpty(*pkgName, *ontology),
		Version:         *pkgVersion,
		Author:          *author,
		License:         *license,
		Description:     *description,
		MinWeaveVersion: *minWeaveVersion,
		Dependencies:    deps,
	}

	migrations, err := loadMigrations(*migrationsDir)
	if err != nil {
		fmt.Fprintf(stderr, "weave pkg export: %v\n", err)
		return 1
	}

	in, err := buildExportInput(context.Background(), c, *ontology, manifest, migrations)
	if err != nil {
		fmt.Fprintf(stderr, "weave pkg export: %v\n", err)
		return 1
	}

	if err := writePackageFile(*output, in); err != nil {
		fmt.Fprintf(stderr, "weave pkg export: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Wrote %s (%d actions, %d functions, %d migrations)\n",
		*output,
		len(in.Actions),
		len(in.Functions),
		len(in.Migrations),
	)
	return 0
}

// buildExportInput is the testable seam between flag parsing and the
// HTTP / filesystem dependencies. It pulls the ontology export envelope
// from the server and slices out the per-action / per-function pieces the
// builder expects.
func buildExportInput(ctx context.Context, c *cliclient.Client, ontology string, manifest weavepkg.Manifest, migrations []weavepkg.MigrationEntry) (weavepkg.BuildInput, error) {
	export, err := c.ExportOntology(ctx, ontology)
	if err != nil {
		return weavepkg.BuildInput{}, fmt.Errorf("fetch export: %w", err)
	}

	ontologyJSON, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return weavepkg.BuildInput{}, fmt.Errorf("marshal ontology export: %w", err)
	}

	actions, err := extractActions(export["actionTypes"])
	if err != nil {
		return weavepkg.BuildInput{}, err
	}

	functions, err := extractFunctions(export["functions"])
	if err != nil {
		return weavepkg.BuildInput{}, err
	}

	if manifest.GeneratedAt.IsZero() {
		manifest.GeneratedAt = time.Now().UTC()
	}

	return weavepkg.BuildInput{
		Manifest:   manifest,
		Ontology:   ontologyJSON,
		Actions:    actions,
		Functions:  functions,
		Migrations: migrations,
	}, nil
}

func extractActions(raw any) ([]weavepkg.ActionEntry, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("actionTypes: unexpected type %T", raw)
	}
	out := make([]weavepkg.ActionEntry, 0, len(list))
	for _, entry := range list {
		obj, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("actionTypes: element is not an object")
		}
		apiName, _ := obj["apiName"].(string)
		if strings.TrimSpace(apiName) == "" {
			continue
		}
		body, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal action %q: %w", apiName, err)
		}
		out = append(out, weavepkg.ActionEntry{APIName: apiName, Body: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].APIName < out[j].APIName })
	return out, nil
}

func extractFunctions(raw any) ([]weavepkg.FunctionEntry, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("functions: unexpected type %T", raw)
	}
	out := make([]weavepkg.FunctionEntry, 0, len(list))
	for _, entry := range list {
		obj, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("functions: element is not an object")
		}
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		version, _ := obj["version"].(string)
		source, _ := obj["sourceCode"].(string)
		out = append(out, weavepkg.FunctionEntry{
			Name:       name,
			Version:    version,
			SourceCode: source,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

// loadMigrations reads every *.sql file under dir (non-recursive) and returns
// them in lexical order. dir == "" disables migration packaging entirely so
// repos without an authoritative migrations checkout can still produce a
// .weavepkg from server state.
func loadMigrations(dir string) ([]weavepkg.MigrationEntry, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Default flag value points at "./migrations"; tolerate a
			// missing directory there silently so the CLI works from
			// non-repo working directories. Explicit non-default values
			// fall through to the same nil-on-missing path because the
			// CLI's contract is "no migrations available".
			return nil, nil
		}
		return nil, fmt.Errorf("stat migrations dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("migrations path %q is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	out := make([]weavepkg.MigrationEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		out = append(out, weavepkg.MigrationEntry{Filename: name, Content: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out, nil
}

func writePackageFile(path string, in weavepkg.BuildInput) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return fmt.Errorf("ensure output dir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if err := weavepkg.Build(f, in); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

func parseDependencies(raw string) ([]weavepkg.Dependency, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	deps := make([]weavepkg.Dependency, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		at := strings.LastIndex(p, "@")
		if at <= 0 || at == len(p)-1 {
			return nil, fmt.Errorf("dependency %q must be in name@version form", p)
		}
		deps = append(deps, weavepkg.Dependency{
			Name:    p[:at],
			Version: p[at+1:],
		})
	}
	return deps, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
