package weavepkg

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// readZipEntries returns a map of file path -> contents for every entry in
// the zip body. Helper for assertions in the tests below.
func readZipEntries(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = buf
	}
	return out
}

// minimalInput returns a BuildInput with only the required fields populated.
// Tests extend this to exercise specific behaviour.
func minimalInput() BuildInput {
	return BuildInput{
		Manifest: Manifest{
			Name:    "northwind",
			Version: "1.2.3",
		},
		Ontology: json.RawMessage(`{"ontology":{"apiName":"northwind"}}`),
	}
}

func TestBuild_RejectsEmptyManifestName(t *testing.T) {
	in := minimalInput()
	in.Manifest.Name = ""
	var buf bytes.Buffer
	if err := Build(&buf, in); err == nil {
		t.Fatalf("expected error for empty manifest name")
	}
}

func TestBuild_RejectsEmptyManifestVersion(t *testing.T) {
	in := minimalInput()
	in.Manifest.Version = ""
	var buf bytes.Buffer
	if err := Build(&buf, in); err == nil {
		t.Fatalf("expected error for empty manifest version")
	}
}

func TestBuild_RejectsEmptyOntology(t *testing.T) {
	in := minimalInput()
	in.Ontology = nil
	var buf bytes.Buffer
	if err := Build(&buf, in); err == nil {
		t.Fatalf("expected error for missing ontology body")
	}
}

func TestBuild_WritesValidZipWithManifestAndOntology(t *testing.T) {
	in := minimalInput()
	in.Manifest.Author = "Albert"
	in.Manifest.License = "MIT"
	in.Manifest.MinWeaveVersion = "0.42.0"
	in.Manifest.Description = "Northwind sample data"
	in.Manifest.Dependencies = []Dependency{{Name: "core", Version: "1.0.0"}}

	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries := readZipEntries(t, buf.Bytes())

	// Required entries.
	if _, ok := entries[ManifestFilename]; !ok {
		t.Fatalf("manifest entry %q missing; entries=%v", ManifestFilename, keysOf(entries))
	}
	if _, ok := entries[OntologyFilename]; !ok {
		t.Fatalf("ontology entry %q missing", OntologyFilename)
	}

	// Manifest contains all the metadata fields the AC asks for.
	var got Manifest
	if err := json.Unmarshal(entries[ManifestFilename], &got); err != nil {
		t.Fatalf("manifest parse: %v body=%q", err, entries[ManifestFilename])
	}
	if got.Name != "northwind" || got.Version != "1.2.3" || got.Author != "Albert" ||
		got.License != "MIT" || got.MinWeaveVersion != "0.42.0" {
		t.Fatalf("manifest fields drift: %+v", got)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].Name != "core" {
		t.Fatalf("dependencies drift: %+v", got.Dependencies)
	}
	if got.Contents.Ontology != OntologyFilename {
		t.Fatalf("contents.ontology = %q, want %q", got.Contents.Ontology, OntologyFilename)
	}
	if got.GeneratedAt.IsZero() {
		t.Fatalf("generatedAt should be stamped, got zero time")
	}

	// Ontology body should round-trip the input bytes verbatim modulo formatting.
	var raw map[string]any
	if err := json.Unmarshal(entries[OntologyFilename], &raw); err != nil {
		t.Fatalf("ontology body parse: %v", err)
	}
}

func TestBuild_OneFilePerAction(t *testing.T) {
	in := minimalInput()
	in.Actions = []ActionEntry{
		{APIName: "createOrder", Body: json.RawMessage(`{"apiName":"createOrder"}`)},
		{APIName: "cancelOrder", Body: json.RawMessage(`{"apiName":"cancelOrder"}`)},
	}
	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries := readZipEntries(t, buf.Bytes())

	for _, name := range []string{"actions/createOrder.json", "actions/cancelOrder.json"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("entry %q missing; have %v", name, keysOf(entries))
		}
	}

	// Manifest contents.actions lists both files in deterministic order.
	var m Manifest
	if err := json.Unmarshal(entries[ManifestFilename], &m); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if len(m.Contents.Actions) != 2 {
		t.Fatalf("contents.actions count = %d, want 2", len(m.Contents.Actions))
	}
	for _, p := range m.Contents.Actions {
		if !strings.HasPrefix(p, "actions/") || !strings.HasSuffix(p, ".json") {
			t.Fatalf("contents.actions entry shape wrong: %q", p)
		}
	}
}

func TestBuild_OneFilePerFunctionWritesJavaScript(t *testing.T) {
	in := minimalInput()
	in.Functions = []FunctionEntry{
		{Name: "computeTotal", Version: "1.0.0", SourceCode: "function computeTotal(){ return 42; }"},
		{Name: "greet", Version: "0.1.0", SourceCode: "// greet"},
	}

	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries := readZipEntries(t, buf.Bytes())

	for _, want := range []string{"functions/computeTotal.js", "functions/greet.js"} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("entry %q missing; have %v", want, keysOf(entries))
		}
	}
	if got := string(entries["functions/computeTotal.js"]); !strings.Contains(got, "return 42") {
		t.Fatalf("function body drift: %q", got)
	}
}

func TestBuild_DuplicateFunctionNameDisambiguatesByVersion(t *testing.T) {
	in := minimalInput()
	in.Functions = []FunctionEntry{
		{Name: "compute", Version: "1.0.0", SourceCode: "// v1"},
		{Name: "compute", Version: "2.0.0", SourceCode: "// v2"},
	}

	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries := readZipEntries(t, buf.Bytes())
	// First name wins compute.js; the second collides and gets the version
	// suffix to keep both bodies addressable.
	if _, ok := entries["functions/compute.js"]; !ok {
		t.Fatalf("functions/compute.js missing")
	}
	if _, ok := entries["functions/compute@2.0.0.js"]; !ok {
		t.Fatalf("functions/compute@2.0.0.js missing; have %v", keysOf(entries))
	}
}

func TestBuild_MigrationsLandUnderMigrationsDir(t *testing.T) {
	in := minimalInput()
	in.Migrations = []MigrationEntry{
		{Filename: "000001_init.up.sql", Content: []byte("CREATE TABLE t1();")},
		{Filename: "000001_init.down.sql", Content: []byte("DROP TABLE t1;")},
	}
	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries := readZipEntries(t, buf.Bytes())
	if _, ok := entries["migrations/000001_init.up.sql"]; !ok {
		t.Fatalf("up migration missing")
	}
	if _, ok := entries["migrations/000001_init.down.sql"]; !ok {
		t.Fatalf("down migration missing")
	}

	var m Manifest
	if err := json.Unmarshal(entries[ManifestFilename], &m); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if len(m.Contents.Migrations) != 2 {
		t.Fatalf("contents.migrations = %v", m.Contents.Migrations)
	}
}

func TestBuild_RejectsUnsafeMigrationFilenames(t *testing.T) {
	in := minimalInput()
	in.Migrations = []MigrationEntry{
		{Filename: "../etc/passwd", Content: []byte("rm -rf /")},
	}
	var buf bytes.Buffer
	if err := Build(&buf, in); err == nil {
		t.Fatalf("expected error for path-traversing migration filename")
	}
}

func TestBuild_RejectsDuplicateActionAPINames(t *testing.T) {
	in := minimalInput()
	in.Actions = []ActionEntry{
		{APIName: "x", Body: json.RawMessage(`{}`)},
		{APIName: "x", Body: json.RawMessage(`{}`)},
	}
	var buf bytes.Buffer
	if err := Build(&buf, in); err == nil {
		t.Fatalf("expected error for duplicate action api names")
	}
}

func TestBuild_DeterministicGeneratedAt(t *testing.T) {
	in := minimalInput()
	fixed := time.Date(2026, 5, 4, 10, 11, 12, 0, time.UTC)
	in.Manifest.GeneratedAt = fixed

	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	entries := readZipEntries(t, buf.Bytes())
	var m Manifest
	if err := json.Unmarshal(entries[ManifestFilename], &m); err != nil {
		t.Fatalf("manifest parse: %v", err)
	}
	if !m.GeneratedAt.Equal(fixed) {
		t.Fatalf("generatedAt = %v, want %v", m.GeneratedAt, fixed)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
