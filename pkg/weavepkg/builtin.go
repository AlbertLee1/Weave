package weavepkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// BuiltinPackage is one example .weavepkg loaded from a source tree of
// `manifest.json` + `ontology.json` (and optional actions/, functions/,
// migrations/) directories. It is the in-memory shape consumed by the
// Marketplace UI's "Built-in" tab (US-414): the server lists the available
// packages, and on install pipes the embedded data through the existing
// pkg install handler without touching the filesystem.
type BuiltinPackage struct {
	// Slug is the directory name (e.g. "northwind"). Used in URLs and as
	// the canonical lookup key — falls back to manifest.Name when it is
	// non-empty so callers don't have to dual-key.
	Slug string
	// Manifest is the parsed manifest.json from the package directory.
	Manifest Manifest
	// Ontology is the verbatim ontology.json bytes. The bundle's import
	// path expects an OntologyExport envelope.
	Ontology json.RawMessage
	// Actions / Functions / Migrations mirror BuildInput so a built-in
	// package can carry the same payload shape as a real .weavepkg.
	// Optional — these directories may not exist for simple packages.
	Actions    []ActionEntry
	Functions  []FunctionEntry
	Migrations []MigrationEntry
}

// LoadBuiltinPackages walks fsys looking for first-level directories that
// each contain manifest.json + ontology.json. Each directory becomes one
// BuiltinPackage. The returned slice is sorted by slug for deterministic
// catalog rendering.
//
// Returns an error if any candidate package is malformed (missing required
// files, manifest schema violations, invalid migration filename). Callers
// in cmd/server treat that as a fatal boot error — a malformed embedded
// catalog is a build-time bug, not a runtime degradation.
func LoadBuiltinPackages(fsys fs.FS) ([]BuiltinPackage, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("weavepkg: read builtin root: %w", err)
	}
	out := make([]BuiltinPackage, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkg, err := loadBuiltinPackageDir(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("weavepkg: load builtin %q: %w", e.Name(), err)
		}
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func loadBuiltinPackageDir(fsys fs.FS, dir string) (BuiltinPackage, error) {
	manifestBytes, err := fs.ReadFile(fsys, path.Join(dir, ManifestFilename))
	if err != nil {
		return BuiltinPackage{}, fmt.Errorf("read %s: %w", ManifestFilename, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return BuiltinPackage{}, fmt.Errorf("parse %s: %w", ManifestFilename, err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return BuiltinPackage{}, errors.New("manifest.name is required")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return BuiltinPackage{}, errors.New("manifest.version is required")
	}

	ontologyBytes, err := fs.ReadFile(fsys, path.Join(dir, OntologyFilename))
	if err != nil {
		return BuiltinPackage{}, fmt.Errorf("read %s: %w", OntologyFilename, err)
	}
	if !json.Valid(ontologyBytes) {
		return BuiltinPackage{}, fmt.Errorf("%s is not valid JSON", OntologyFilename)
	}

	actions, err := loadBuiltinSubdir(fsys, path.Join(dir, ActionsDir), ".json", buildActionEntry)
	if err != nil {
		return BuiltinPackage{}, err
	}
	functions, err := loadBuiltinSubdir(fsys, path.Join(dir, FunctionsDir), ".js", buildFunctionEntry)
	if err != nil {
		return BuiltinPackage{}, err
	}
	migrations, err := loadBuiltinSubdir(fsys, path.Join(dir, MigrationsDir), ".sql", buildMigrationEntry)
	if err != nil {
		return BuiltinPackage{}, err
	}

	return BuiltinPackage{
		Slug:       dir,
		Manifest:   manifest,
		Ontology:   json.RawMessage(ontologyBytes),
		Actions:    actions,
		Functions:  functions,
		Migrations: migrations,
	}, nil
}

func loadBuiltinSubdir[T any](fsys fs.FS, dir, suffix string, build func(name string, body []byte) (T, error)) ([]T, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", dir, name, err)
		}
		entry, err := build(strings.TrimSuffix(name, suffix), body)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, name, err)
		}
		out = append(out, entry)
	}
	return out, nil
}

func buildActionEntry(stem string, body []byte) (ActionEntry, error) {
	if !json.Valid(body) {
		return ActionEntry{}, fmt.Errorf("action %q body is not valid JSON", stem)
	}
	return ActionEntry{APIName: stem, Body: json.RawMessage(body)}, nil
}

func buildFunctionEntry(stem string, body []byte) (FunctionEntry, error) {
	name, version := splitNameVersion(stem)
	return FunctionEntry{Name: name, Version: version, SourceCode: string(body)}, nil
}

func buildMigrationEntry(stem string, body []byte) (MigrationEntry, error) {
	filename := stem + ".sql"
	if err := validateMigrationFilename(filename); err != nil {
		return MigrationEntry{}, err
	}
	return MigrationEntry{Filename: filename, Content: body}, nil
}
