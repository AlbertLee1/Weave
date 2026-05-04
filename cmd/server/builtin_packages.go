package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	examplepkgs "github.com/liyang/weave/examples/packages"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/weavepkg"
)

// builtinPackageProvider satisfies oms.BuiltinPackageProvider by serving
// the embedded examples/packages/ tree. It loads the catalog once at
// construction time so the per-request List call is a slice copy.
type builtinPackageProvider struct {
	metadata []oms.BuiltinPackageMetadata
	bySlug   map[string]builtinEntry
}

type builtinEntry struct {
	manifest oms.PackageManifest
	pkg      weavepkg.BuiltinPackage
}

// newBuiltinPackageProviderFromFS loads the embedded catalog from fsys
// and returns the provider. Returns an error if any package directory is
// malformed — a build-time bug that should fail boot rather than silently
// hiding the broken entry.
func newBuiltinPackageProviderFromFS(fsys fs.FS) (*builtinPackageProvider, error) {
	pkgs, err := weavepkg.LoadBuiltinPackages(fsys)
	if err != nil {
		return nil, fmt.Errorf("load builtin packages: %w", err)
	}

	bySlug := make(map[string]builtinEntry, len(pkgs))
	metadata := make([]oms.BuiltinPackageMetadata, 0, len(pkgs))
	for _, p := range pkgs {
		ontologyAPIName, otCount, ltCount, atCount, fnCount := summariseOntology(p.Ontology)
		// Function count combines exported helpers in the ontology body
		// AND any standalone functions/<name>.js shipped under the package
		// directory.
		fnCount += len(p.Functions)

		manifest := oms.PackageManifest{
			Name:            p.Manifest.Name,
			Version:         p.Manifest.Version,
			Author:          p.Manifest.Author,
			License:         p.Manifest.License,
			Description:     p.Manifest.Description,
			MinWeaveVersion: p.Manifest.MinWeaveVersion,
			Dependencies:    convertDependencies(p.Manifest.Dependencies),
		}

		md := oms.BuiltinPackageMetadata{
			Slug:            p.Slug,
			Name:            manifest.Name,
			Version:         manifest.Version,
			OntologyAPIName: ontologyAPIName,
			Author:          manifest.Author,
			License:         manifest.License,
			Description:     manifest.Description,
			MinWeaveVersion: manifest.MinWeaveVersion,
			Dependencies:    manifest.Dependencies,
			ObjectTypeCount: otCount,
			LinkTypeCount:   ltCount,
			ActionTypeCount: atCount + len(p.Actions),
			FunctionCount:   fnCount,
			MigrationCount:  len(p.Migrations),
		}
		metadata = append(metadata, md)
		bySlug[p.Slug] = builtinEntry{manifest: manifest, pkg: p}
	}

	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Slug < metadata[j].Slug })

	return &builtinPackageProvider{metadata: metadata, bySlug: bySlug}, nil
}

// newBuiltinPackageProvider loads the catalog from the embedded
// examples/packages FS shipped with the server binary.
func newBuiltinPackageProvider() (*builtinPackageProvider, error) {
	return newBuiltinPackageProviderFromFS(examplepkgs.FS)
}

func (p *builtinPackageProvider) List(_ context.Context) []oms.BuiltinPackageMetadata {
	out := make([]oms.BuiltinPackageMetadata, len(p.metadata))
	copy(out, p.metadata)
	return out
}

func (p *builtinPackageProvider) Get(_ context.Context, slug string) (*oms.PackageInstallRequest, *oms.BuiltinPackageMetadata, bool) {
	entry, ok := p.bySlug[slug]
	if !ok {
		return nil, nil, false
	}
	migrations := make([]oms.PackageMigrationEntry, 0, len(entry.pkg.Migrations))
	for _, m := range entry.pkg.Migrations {
		migrations = append(migrations, oms.PackageMigrationEntry{
			Filename: m.Filename,
			Content:  m.Content,
		})
	}
	req := &oms.PackageInstallRequest{
		Manifest:   entry.manifest,
		Ontology:   json.RawMessage(append([]byte(nil), entry.pkg.Ontology...)),
		Migrations: migrations,
	}
	for i, md := range p.metadata {
		if md.Slug == slug {
			return req, &p.metadata[i], true
		}
	}
	return req, nil, true
}

func convertDependencies(in []weavepkg.Dependency) []oms.PackageDependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]oms.PackageDependency, len(in))
	for i, d := range in {
		out[i] = oms.PackageDependency{Name: d.Name, Version: d.Version}
	}
	return out
}

// summariseOntology extracts catalog-display counts from the OntologyExport
// body without fully decoding into the rich oms.OntologyExport struct
// (which would require importing the full set of strongly-typed shapes
// here just to count them).
func summariseOntology(raw json.RawMessage) (apiName string, ot, lt, at, fn int) {
	if len(raw) == 0 {
		return
	}
	var env struct {
		Ontology    map[string]any `json:"ontology"`
		ObjectTypes []any          `json:"objectTypes"`
		LinkTypes   []any          `json:"linkTypes"`
		ActionTypes []any          `json:"actionTypes"`
		Functions   []any          `json:"functions"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}
	if v, ok := env.Ontology["apiName"].(string); ok {
		apiName = strings.TrimSpace(v)
	}
	return apiName, len(env.ObjectTypes), len(env.LinkTypes), len(env.ActionTypes), len(env.Functions)
}
