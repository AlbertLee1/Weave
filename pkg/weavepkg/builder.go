package weavepkg

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
)

// BuildInput aggregates everything required to produce a .weavepkg archive.
type BuildInput struct {
	Manifest   Manifest
	Ontology   json.RawMessage
	Actions    []ActionEntry
	Functions  []FunctionEntry
	Migrations []MigrationEntry
}

// ActionEntry is one ActionType serialised to JSON. APIName is the file stem
// inside actions/; Body is written verbatim.
type ActionEntry struct {
	APIName string
	Body    json.RawMessage
}

// FunctionEntry is one Function source body. Name maps to functions/<Name>.js
// when unique within the input; duplicates fall back to <Name>@<Version>.js
// so versioned co-existence (US-217) round-trips through export.
type FunctionEntry struct {
	Name       string
	Version    string
	SourceCode string
}

// MigrationEntry is one migration file. Filename must be a plain basename;
// Build rejects path-traversing names like "../escape.sql".
type MigrationEntry struct {
	Filename string
	Content  []byte
}

// Build writes the .weavepkg archive to w. The archive is a deflate-compressed
// ZIP with a fixed top-level layout (see package doc).
//
// Returns an error when required fields are missing, when the input contains
// duplicate action API names, or when a migration filename would escape the
// archive root.
func Build(w io.Writer, in BuildInput) error {
	if err := validate(in); err != nil {
		return err
	}

	manifest := in.Manifest
	if manifest.GeneratedAt.IsZero() {
		manifest.GeneratedAt = time.Now().UTC()
	} else {
		manifest.GeneratedAt = manifest.GeneratedAt.UTC()
	}
	manifest.Contents = Contents{Ontology: OntologyFilename}

	zw := zip.NewWriter(w)

	if err := writeJSONEntry(zw, OntologyFilename, in.Ontology); err != nil {
		return err
	}

	actionPaths, err := writeActions(zw, in.Actions)
	if err != nil {
		return err
	}
	manifest.Contents.Actions = actionPaths

	fnPaths, err := writeFunctions(zw, in.Functions)
	if err != nil {
		return err
	}
	manifest.Contents.Functions = fnPaths

	migrationPaths, err := writeMigrations(zw, in.Migrations)
	if err != nil {
		return err
	}
	manifest.Contents.Migrations = migrationPaths

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeRawEntry(zw, ManifestFilename, manifestBytes); err != nil {
		return err
	}

	return zw.Close()
}

func validate(in BuildInput) error {
	if strings.TrimSpace(in.Manifest.Name) == "" {
		return fmt.Errorf("weavepkg: manifest.name is required")
	}
	if strings.TrimSpace(in.Manifest.Version) == "" {
		return fmt.Errorf("weavepkg: manifest.version is required")
	}
	if len(in.Ontology) == 0 {
		return fmt.Errorf("weavepkg: ontology body is required")
	}
	seen := make(map[string]struct{}, len(in.Actions))
	for _, a := range in.Actions {
		name := strings.TrimSpace(a.APIName)
		if name == "" {
			return fmt.Errorf("weavepkg: action with empty apiName")
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("weavepkg: duplicate action apiName %q", name)
		}
		seen[name] = struct{}{}
	}
	for _, m := range in.Migrations {
		if err := validateMigrationFilename(m.Filename); err != nil {
			return err
		}
	}
	return nil
}

// validateMigrationFilename rejects anything that wouldn't be a plain
// basename — empty, absolute paths, "..", or any embedded path separator.
// The installer (US-412) extracts these into a target directory and a
// crafted name like "../etc/weave.conf" must never escape it.
func validateMigrationFilename(name string) error {
	if name == "" {
		return fmt.Errorf("weavepkg: migration filename is empty")
	}
	clean := path.Clean(name)
	if clean != name || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("weavepkg: migration filename %q must be a plain basename", name)
	}
	return nil
}

func writeJSONEntry(zw *zip.Writer, name string, body json.RawMessage) error {
	// Compact + indent for human readability. Prefer the wire shape; fall
	// back to literal bytes when the input isn't valid JSON (avoids surprising
	// callers who feed pre-formatted blobs).
	var pretty []byte
	if json.Valid(body) {
		var v interface{}
		if err := json.Unmarshal(body, &v); err == nil {
			if b, err := json.MarshalIndent(v, "", "  "); err == nil {
				pretty = b
			}
		}
	}
	if pretty == nil {
		pretty = []byte(body)
	}
	return writeRawEntry(zw, name, pretty)
}

func writeRawEntry(zw *zip.Writer, name string, body []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", name, err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write zip entry %s: %w", name, err)
	}
	return nil
}

func writeActions(zw *zip.Writer, actions []ActionEntry) ([]string, error) {
	if len(actions) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(actions))
	for _, a := range actions {
		entryPath := path.Join(ActionsDir, a.APIName+".json")
		if err := writeJSONEntry(zw, entryPath, a.Body); err != nil {
			return nil, err
		}
		paths = append(paths, entryPath)
	}
	sort.Strings(paths)
	return paths, nil
}

func writeFunctions(zw *zip.Writer, fns []FunctionEntry) ([]string, error) {
	if len(fns) == 0 {
		return nil, nil
	}
	used := make(map[string]struct{}, len(fns))
	paths := make([]string, 0, len(fns))
	for _, fn := range fns {
		stem := fn.Name
		if stem == "" {
			return nil, fmt.Errorf("weavepkg: function with empty name")
		}
		entry := path.Join(FunctionsDir, stem+".js")
		if _, ok := used[entry]; ok {
			// Disambiguate by version suffix to keep semver-coexisting
			// versions of the same function name addressable.
			if fn.Version == "" {
				return nil, fmt.Errorf("weavepkg: duplicate function name %q with empty version", fn.Name)
			}
			entry = path.Join(FunctionsDir, stem+"@"+fn.Version+".js")
			if _, ok := used[entry]; ok {
				return nil, fmt.Errorf("weavepkg: duplicate function entry %q", entry)
			}
		}
		if err := writeRawEntry(zw, entry, []byte(fn.SourceCode)); err != nil {
			return nil, err
		}
		used[entry] = struct{}{}
		paths = append(paths, entry)
	}
	sort.Strings(paths)
	return paths, nil
}

func writeMigrations(zw *zip.Writer, mig []MigrationEntry) ([]string, error) {
	if len(mig) == 0 {
		return nil, nil
	}
	used := make(map[string]struct{}, len(mig))
	paths := make([]string, 0, len(mig))
	for _, m := range mig {
		entry := path.Join(MigrationsDir, m.Filename)
		if _, dup := used[entry]; dup {
			return nil, fmt.Errorf("weavepkg: duplicate migration filename %q", m.Filename)
		}
		if err := writeRawEntry(zw, entry, m.Content); err != nil {
			return nil, err
		}
		used[entry] = struct{}{}
		paths = append(paths, entry)
	}
	sort.Strings(paths)
	return paths, nil
}
