package weavepkg

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Package is the parsed contents of a .weavepkg archive. It is the symmetric
// counterpart of BuildInput: Read rebuilds it from a ZIP body so callers
// (US-412 install, future marketplace UI) can validate manifest fields, fan
// out per-action / per-function entries, and apply the bundled migrations.
type Package struct {
	Manifest   Manifest
	Ontology   json.RawMessage
	Actions    []ActionEntry
	Functions  []FunctionEntry
	Migrations []MigrationEntry
}

// ReadBytes is a convenience wrapper around Read for callers that already
// have the archive in memory.
func ReadBytes(body []byte) (*Package, error) {
	return Read(bytes.NewReader(body), int64(len(body)))
}

// Read parses a .weavepkg archive from r. size is the total byte count of the
// archive (zip readers are random-access — they need the length up front).
//
// The returned Package mirrors BuildInput exactly so a Read followed by Build
// produces a byte-equivalent archive (modulo the GeneratedAt stamp). Read
// validates the same path-traversal invariants as Build's writer so a
// malicious archive cannot escape the migrations directory at install time.
func Read(r io.ReaderAt, size int64) (*Package, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("weavepkg: open archive: %w", err)
	}

	manifestRaw, err := readZipFile(zr, ManifestFilename)
	if err != nil {
		return nil, err
	}
	if manifestRaw == nil {
		return nil, fmt.Errorf("weavepkg: %s missing", ManifestFilename)
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, fmt.Errorf("weavepkg: parse %s: %w", ManifestFilename, err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return nil, fmt.Errorf("weavepkg: manifest.name is required")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return nil, fmt.Errorf("weavepkg: manifest.version is required")
	}

	ontologyBody, err := readZipFile(zr, OntologyFilename)
	if err != nil {
		return nil, err
	}
	if len(ontologyBody) == 0 {
		return nil, fmt.Errorf("weavepkg: %s missing", OntologyFilename)
	}

	pkg := &Package{
		Manifest: manifest,
		Ontology: json.RawMessage(ontologyBody),
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		switch {
		case f.Name == ManifestFilename || f.Name == OntologyFilename:
			continue
		case strings.HasPrefix(f.Name, ActionsDir+"/"):
			entry, err := readActionEntry(f)
			if err != nil {
				return nil, err
			}
			pkg.Actions = append(pkg.Actions, entry)
		case strings.HasPrefix(f.Name, FunctionsDir+"/"):
			entry, err := readFunctionEntry(f)
			if err != nil {
				return nil, err
			}
			pkg.Functions = append(pkg.Functions, entry)
		case strings.HasPrefix(f.Name, MigrationsDir+"/"):
			entry, err := readMigrationEntry(f)
			if err != nil {
				return nil, err
			}
			pkg.Migrations = append(pkg.Migrations, entry)
		}
	}

	sort.Slice(pkg.Actions, func(i, j int) bool { return pkg.Actions[i].APIName < pkg.Actions[j].APIName })
	sort.Slice(pkg.Functions, func(i, j int) bool {
		if pkg.Functions[i].Name != pkg.Functions[j].Name {
			return pkg.Functions[i].Name < pkg.Functions[j].Name
		}
		return pkg.Functions[i].Version < pkg.Functions[j].Version
	})
	sort.Slice(pkg.Migrations, func(i, j int) bool {
		return pkg.Migrations[i].Filename < pkg.Migrations[j].Filename
	})

	return pkg, nil
}

func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("weavepkg: open %s: %w", name, err)
			}
			defer rc.Close()
			body, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("weavepkg: read %s: %w", name, err)
			}
			return body, nil
		}
	}
	return nil, nil
}

func readActionEntry(f *zip.File) (ActionEntry, error) {
	stem, ok := stripPrefixSuffix(f.Name, ActionsDir+"/", ".json")
	if !ok {
		return ActionEntry{}, fmt.Errorf("weavepkg: action entry %q must be %s/<api>.json", f.Name, ActionsDir)
	}
	if stem == "" {
		return ActionEntry{}, fmt.Errorf("weavepkg: action entry %q has empty apiName", f.Name)
	}
	body, err := readEntryBody(f)
	if err != nil {
		return ActionEntry{}, err
	}
	if !json.Valid(body) {
		return ActionEntry{}, fmt.Errorf("weavepkg: action %q body is not valid JSON", stem)
	}
	return ActionEntry{APIName: stem, Body: json.RawMessage(body)}, nil
}

func readFunctionEntry(f *zip.File) (FunctionEntry, error) {
	stem, ok := stripPrefixSuffix(f.Name, FunctionsDir+"/", ".js")
	if !ok {
		return FunctionEntry{}, fmt.Errorf("weavepkg: function entry %q must be %s/<name>.js", f.Name, FunctionsDir)
	}
	if stem == "" {
		return FunctionEntry{}, fmt.Errorf("weavepkg: function entry %q has empty name", f.Name)
	}
	body, err := readEntryBody(f)
	if err != nil {
		return FunctionEntry{}, err
	}
	name, version := splitNameVersion(stem)
	return FunctionEntry{Name: name, Version: version, SourceCode: string(body)}, nil
}

func readMigrationEntry(f *zip.File) (MigrationEntry, error) {
	rel := strings.TrimPrefix(f.Name, MigrationsDir+"/")
	if rel == "" || strings.Contains(rel, "/") {
		return MigrationEntry{}, fmt.Errorf("weavepkg: migration %q must be a flat basename under %s/", f.Name, MigrationsDir)
	}
	if err := validateMigrationFilename(rel); err != nil {
		return MigrationEntry{}, err
	}
	body, err := readEntryBody(f)
	if err != nil {
		return MigrationEntry{}, err
	}
	return MigrationEntry{Filename: rel, Content: body}, nil
}

func readEntryBody(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("weavepkg: open %s: %w", f.Name, err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("weavepkg: read %s: %w", f.Name, err)
	}
	return body, nil
}

// stripPrefixSuffix returns the stem of name when it is sandwiched between
// prefix and suffix, with no embedded path separators in between. The boolean
// reports whether the shape matched at all.
func stripPrefixSuffix(name, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	stem := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if stem == "" || strings.Contains(stem, "/") {
		return "", false
	}
	if path.Clean(stem) != stem || strings.Contains(stem, "..") {
		return "", false
	}
	return stem, true
}

// splitNameVersion reverses Build's <name>@<version> disambiguation suffix.
// "compute" -> ("compute", ""), "compute@2.0.0" -> ("compute", "2.0.0").
func splitNameVersion(stem string) (string, string) {
	at := strings.LastIndex(stem, "@")
	if at <= 0 || at == len(stem)-1 {
		return stem, ""
	}
	return stem[:at], stem[at+1:]
}
