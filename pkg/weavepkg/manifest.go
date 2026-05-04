// Package weavepkg builds and reads .weavepkg ontology archives (US-411).
//
// A .weavepkg is a ZIP file with a fixed top-level layout:
//
//	manifest.json          identity, version, dependencies, generation metadata
//	ontology.json          full ontology export (objectTypes, linkTypes, ...)
//	actions/<api>.json     one file per ActionType, JSON-encoded
//	functions/<name>.js    one file per Function, raw JavaScript source
//	migrations/<n>.sql     SQL migration files (verbatim copies)
//
// Build is the single chokepoint that produces the archive; downstream
// packages and CLI surfaces reach for it instead of hand-rolling the layout
// so future format changes (signing, manifest schema bumps) stay localised.
package weavepkg

import "time"

// File names that callers can rely on when verifying a built package.
const (
	ManifestFilename = "manifest.json"
	OntologyFilename = "ontology.json"
	ActionsDir       = "actions"
	FunctionsDir     = "functions"
	MigrationsDir    = "migrations"
)

// Manifest is the top-level metadata stamped at the root of every .weavepkg
// archive. Name + Version are required at build time; the remaining fields
// are optional and ride through to the manifest.json verbatim.
type Manifest struct {
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Author          string       `json:"author,omitempty"`
	License         string       `json:"license,omitempty"`
	Description     string       `json:"description,omitempty"`
	MinWeaveVersion string       `json:"minWeaveVersion,omitempty"`
	Dependencies    []Dependency `json:"dependencies,omitempty"`
	// GeneratedAt is stamped at Build time when zero so callers don't need
	// to set it; tests pin it for byte-stable assertions.
	GeneratedAt time.Time `json:"generatedAt"`
	// Contents is the file inventory of the package, populated by Build.
	// It lets `weave pkg install` audit completeness without unpacking the
	// whole archive.
	Contents Contents `json:"contents"`
}

// Dependency names another .weavepkg this archive expects to be installed
// before activation. The version string is a semver expression; install-time
// resolution is the responsibility of the package installer (US-412), not
// the builder.
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Contents enumerates every file the builder wrote into the archive,
// keyed by category. Paths are relative to the archive root and use
// forward slashes regardless of host OS.
type Contents struct {
	Ontology   string   `json:"ontology"`
	Actions    []string `json:"actions,omitempty"`
	Functions  []string `json:"functions,omitempty"`
	Migrations []string `json:"migrations,omitempty"`
}
