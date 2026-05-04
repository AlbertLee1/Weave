package weavepkg

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// buildSampleArchive returns the zipped bytes of a deterministic .weavepkg
// covering every content axis the reader is expected to round-trip.
func buildSampleArchive(t *testing.T) []byte {
	t.Helper()
	in := BuildInput{
		Manifest: Manifest{
			Name:            "northwind",
			Version:         "1.2.3",
			Author:          "Albert",
			License:         "MIT",
			MinWeaveVersion: "0.42.0",
			Dependencies:    []Dependency{{Name: "core", Version: "1.0.0"}},
		},
		Ontology: json.RawMessage(`{"ontology":{"apiName":"northwind"},"objectTypes":[{"apiName":"Customer"}]}`),
		Actions: []ActionEntry{
			{APIName: "createCustomer", Body: json.RawMessage(`{"apiName":"createCustomer"}`)},
			{APIName: "deleteCustomer", Body: json.RawMessage(`{"apiName":"deleteCustomer"}`)},
		},
		Functions: []FunctionEntry{
			{Name: "compute", Version: "1.0.0", SourceCode: "// v1"},
			{Name: "compute", Version: "2.0.0", SourceCode: "// v2"},
		},
		Migrations: []MigrationEntry{
			{Filename: "000001_init.up.sql", Content: []byte("CREATE TABLE t1();")},
			{Filename: "000001_init.down.sql", Content: []byte("DROP TABLE t1;")},
		},
	}
	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return buf.Bytes()
}

func TestReadBytes_RoundTripsSampleArchive(t *testing.T) {
	body := buildSampleArchive(t)
	pkg, err := ReadBytes(body)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	if pkg.Manifest.Name != "northwind" || pkg.Manifest.Version != "1.2.3" {
		t.Fatalf("manifest drift: %+v", pkg.Manifest)
	}
	if pkg.Manifest.MinWeaveVersion != "0.42.0" {
		t.Fatalf("minWeaveVersion drift: %q", pkg.Manifest.MinWeaveVersion)
	}
	if len(pkg.Manifest.Dependencies) != 1 || pkg.Manifest.Dependencies[0].Name != "core" {
		t.Fatalf("dependencies drift: %+v", pkg.Manifest.Dependencies)
	}
	if len(pkg.Actions) != 2 {
		t.Fatalf("actions count = %d, want 2", len(pkg.Actions))
	}
	if pkg.Actions[0].APIName != "createCustomer" || pkg.Actions[1].APIName != "deleteCustomer" {
		t.Fatalf("actions order drift: %+v", pkg.Actions)
	}
	// Functions: "compute" + "compute@2.0.0" — version-suffixed entry comes
	// back with Name="compute", Version="2.0.0".
	if len(pkg.Functions) != 2 {
		t.Fatalf("functions count = %d, want 2", len(pkg.Functions))
	}
	for _, fn := range pkg.Functions {
		if fn.Name != "compute" {
			t.Fatalf("function name drift: %q", fn.Name)
		}
	}
	// One of the entries lost its version (the un-suffixed compute.js); the
	// other carries 2.0.0.
	versions := []string{pkg.Functions[0].Version, pkg.Functions[1].Version}
	if versions[0] != "" || versions[1] != "2.0.0" {
		t.Fatalf("function versions drift: %v", versions)
	}
	if len(pkg.Migrations) != 2 {
		t.Fatalf("migrations count = %d, want 2", len(pkg.Migrations))
	}
}

func TestReadBytes_RejectsArchiveWithoutManifest(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(OntologyFilename)
	_, _ = w.Write([]byte(`{}`))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := ReadBytes(buf.Bytes()); err == nil {
		t.Fatalf("expected error for missing manifest")
	} else if !strings.Contains(err.Error(), ManifestFilename) {
		t.Fatalf("error should mention manifest: %v", err)
	}
}

func TestReadBytes_RejectsArchiveWithoutOntology(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(ManifestFilename)
	_, _ = w.Write([]byte(`{"name":"x","version":"1.0.0","contents":{"ontology":"ontology.json"}}`))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := ReadBytes(buf.Bytes()); err == nil {
		t.Fatalf("expected error for missing ontology")
	} else if !strings.Contains(err.Error(), OntologyFilename) {
		t.Fatalf("error should mention ontology: %v", err)
	}
}

func TestReadBytes_RejectsManifestMissingName(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(ManifestFilename)
	_, _ = w.Write([]byte(`{"version":"1.0.0","contents":{"ontology":"ontology.json"}}`))
	w2, _ := zw.Create(OntologyFilename)
	_, _ = w2.Write([]byte(`{}`))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := ReadBytes(buf.Bytes()); err == nil {
		t.Fatalf("expected error for empty manifest.name")
	}
}

func TestReadBytes_RejectsMalformedZip(t *testing.T) {
	if _, err := ReadBytes([]byte("not a zip")); err == nil {
		t.Fatalf("expected error for malformed zip")
	}
}

func TestReadBytes_RejectsActionWithInvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(ManifestFilename)
	_, _ = w.Write([]byte(`{"name":"x","version":"1.0.0","contents":{"ontology":"ontology.json"}}`))
	w2, _ := zw.Create(OntologyFilename)
	_, _ = w2.Write([]byte(`{}`))
	w3, _ := zw.Create("actions/bad.json")
	_, _ = w3.Write([]byte(`not json`))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := ReadBytes(buf.Bytes()); err == nil {
		t.Fatalf("expected error for invalid action JSON")
	}
}

func TestReadBytes_RejectsMigrationWithPathTraversal(t *testing.T) {
	// Build manually because the writer would also reject this — we simulate
	// a malicious archive that bypassed the writer.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(ManifestFilename)
	_, _ = w.Write([]byte(`{"name":"x","version":"1.0.0","contents":{"ontology":"ontology.json"}}`))
	w2, _ := zw.Create(OntologyFilename)
	_, _ = w2.Write([]byte(`{}`))
	// archive/zip silently rejects "../" prefixes in some Go versions; route
	// through a nested path to simulate the escape.
	w3, _ := zw.Create("migrations/../etc/passwd")
	_, _ = w3.Write([]byte("rm -rf /"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	pkg, err := ReadBytes(buf.Bytes())
	if err == nil && len(pkg.Migrations) > 0 {
		// Either the entry was rejected at archive-create time (no migration
		// in the parse) OR the reader rejected it. The contract is "never
		// surface a path-traversing migration to the caller".
		for _, m := range pkg.Migrations {
			if strings.Contains(m.Filename, "..") || strings.Contains(m.Filename, "/") {
				t.Fatalf("path-traversing migration leaked: %q", m.Filename)
			}
		}
	}
}

func TestReadBytes_PreservesOntologyBodyVerbatim(t *testing.T) {
	body := buildSampleArchive(t)
	pkg, err := ReadBytes(body)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(pkg.Ontology, &raw); err != nil {
		t.Fatalf("ontology body parse: %v", err)
	}
	if _, ok := raw["objectTypes"]; !ok {
		t.Fatalf("ontology body missing objectTypes; got %v", raw)
	}
}

func TestSplitNameVersion(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantVer  string
	}{
		{"compute", "compute", ""},
		{"compute@1.0.0", "compute", "1.0.0"},
		{"@bad", "@bad", ""},
		{"trail@", "trail@", ""},
		{"a@b@c", "a@b", "c"},
	}
	for _, tc := range cases {
		gotName, gotVer := splitNameVersion(tc.in)
		if gotName != tc.wantName || gotVer != tc.wantVer {
			t.Errorf("splitNameVersion(%q) = (%q, %q), want (%q, %q)", tc.in, gotName, gotVer, tc.wantName, tc.wantVer)
		}
	}
}
