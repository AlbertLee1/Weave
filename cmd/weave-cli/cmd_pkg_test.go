package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/weavepkg"
)

// pkgExportStubServer mounts the routes needed by the `weave pkg export`
// command. The export endpoint returns the OntologyExport envelope shape
// produced by pkg/oms.OMSHandler.ExportOntologyV2.
func pkgExportStubServer(t *testing.T, ontology string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v2/ontologies/"+ontology+"/export" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// readExportedPackage opens a .weavepkg file and returns a map of entry path
// -> body for assertions.
func readExportedPackage(t *testing.T, path string) (map[string][]byte, weavepkg.Manifest) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	entries := make(map[string][]byte, len(zr.File))
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
		entries[f.Name] = buf
	}
	var manifest weavepkg.Manifest
	if mf, ok := entries[weavepkg.ManifestFilename]; ok {
		if err := json.Unmarshal(mf, &manifest); err != nil {
			t.Fatalf("manifest parse: %v", err)
		}
	}
	return entries, manifest
}

func TestPkgExportRequiresOntology(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "export", "-o", filepath.Join(tmp, "out.weavepkg"))
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "--ontology is required") {
		t.Fatalf("stderr missing required flag note: %q", stderr)
	}
}

func TestPkgExportRequiresOutputPath(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "export", "--ontology", "northwind")
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "-o (output path) is required") {
		t.Fatalf("stderr missing -o note: %q", stderr)
	}
}

func TestPkgExportUnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "doesnotexist")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("stderr missing unknown subcommand: %q", stderr)
	}
}

func TestPkgExportNoSubcommandShowsUsage(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pkg")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "usage: weave pkg") {
		t.Fatalf("stderr missing usage: %q", stderr)
	}
}

func TestPkgExportProducesValidWeavepkg(t *testing.T) {
	exportBody := `{
		"ontology": {"rid": "ri.ontology.main.ontology.northwind", "apiName": "northwind", "displayName": "Northwind", "currentVersion": 3},
		"objectTypes": [{"apiName": "Customer", "displayName": "Customer", "primaryKey": "id"}],
		"linkTypes": [],
		"actionTypes": [
			{"apiName": "createCustomer", "displayName": "Create Customer", "rid": "ri.actiontype.main.x.1"},
			{"apiName": "deleteCustomer", "displayName": "Delete Customer", "rid": "ri.actiontype.main.x.2"}
		],
		"interfaces": [],
		"sharedProperties": [],
		"valueTypes": [],
		"typeGroups": [],
		"functions": [
			{"name": "computeTotal", "version": "1.0.0", "sourceCode": "function computeTotal(){ return 42; }", "rid": "ri.function.main.fn.1"}
		],
		"queryTypes": []
	}`
	srv := pkgExportStubServer(t, "northwind", exportBody)

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config set base_url failed")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "tok"); exit != 0 {
		t.Fatalf("config set access_token failed")
	}

	migDir := filepath.Join(tmp, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	if err := os.WriteFile(filepath.Join(migDir, "000001_init.up.sql"), []byte("CREATE TABLE t1();"), 0o644); err != nil {
		t.Fatalf("write up.sql: %v", err)
	}
	if err := os.WriteFile(filepath.Join(migDir, "000001_init.down.sql"), []byte("DROP TABLE t1;"), 0o644); err != nil {
		t.Fatalf("write down.sql: %v", err)
	}
	// A non-sql sibling is skipped.
	if err := os.WriteFile(filepath.Join(migDir, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	out := filepath.Join(tmp, "northwind.weavepkg")
	stdout, stderr, exit := runCLIWith(t, tmp,
		"pkg", "export",
		"--ontology", "northwind",
		"-o", out,
		"--version", "1.2.3",
		"--author", "Albert",
		"--license", "MIT",
		"--min-weave-version", "0.42.0",
		"--migrations-dir", migDir,
		"--dependencies", "core@1.0.0,extras@2.1.0",
	)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}
	if !strings.Contains(stdout, "Wrote "+out) {
		t.Fatalf("stdout missing summary: %q", stdout)
	}
	if !strings.Contains(stdout, "2 actions") || !strings.Contains(stdout, "1 functions") || !strings.Contains(stdout, "2 migrations") {
		t.Fatalf("stdout summary missing counts: %q", stdout)
	}

	entries, manifest := readExportedPackage(t, out)
	for _, want := range []string{
		weavepkg.ManifestFilename,
		weavepkg.OntologyFilename,
		"actions/createCustomer.json",
		"actions/deleteCustomer.json",
		"functions/computeTotal.js",
		"migrations/000001_init.up.sql",
		"migrations/000001_init.down.sql",
	} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("missing entry %q; have %v", want, keysOfBytes(entries))
		}
	}
	if string(entries["functions/computeTotal.js"]) != "function computeTotal(){ return 42; }" {
		t.Fatalf("function body drift: %q", entries["functions/computeTotal.js"])
	}

	// README.md is not a *.sql file → must NOT appear under migrations/.
	if _, ok := entries["migrations/README.md"]; ok {
		t.Fatalf("non-sql sibling leaked into migrations/")
	}

	if manifest.Name != "northwind" || manifest.Version != "1.2.3" || manifest.Author != "Albert" ||
		manifest.License != "MIT" || manifest.MinWeaveVersion != "0.42.0" {
		t.Fatalf("manifest fields drift: %+v", manifest)
	}
	if len(manifest.Dependencies) != 2 {
		t.Fatalf("manifest deps drift: %+v", manifest.Dependencies)
	}
	wantDeps := map[string]string{"core": "1.0.0", "extras": "2.1.0"}
	for _, d := range manifest.Dependencies {
		if v, ok := wantDeps[d.Name]; !ok || v != d.Version {
			t.Fatalf("unexpected dependency %+v", d)
		}
	}
	if len(manifest.Contents.Actions) != 2 || len(manifest.Contents.Functions) != 1 || len(manifest.Contents.Migrations) != 2 {
		t.Fatalf("manifest.contents drift: %+v", manifest.Contents)
	}

	// ontology.json must round-trip the entire envelope including objectTypes.
	var ontology map[string]any
	if err := json.Unmarshal(entries[weavepkg.OntologyFilename], &ontology); err != nil {
		t.Fatalf("ontology body parse: %v", err)
	}
	if _, ok := ontology["objectTypes"]; !ok {
		t.Fatalf("ontology body missing objectTypes")
	}
}

func TestPkgExportSkipsMigrationsWhenDirEmpty(t *testing.T) {
	exportBody := `{
		"ontology": {"apiName": "x"}, "objectTypes": [], "linkTypes": [], "actionTypes": [],
		"interfaces": [], "sharedProperties": [], "valueTypes": [], "typeGroups": [],
		"functions": [], "queryTypes": []
	}`
	srv := pkgExportStubServer(t, "x", exportBody)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	out := filepath.Join(tmp, "x.weavepkg")
	stdout, stderr, exit := runCLIWith(t, tmp,
		"pkg", "export",
		"--ontology", "x",
		"-o", out,
		"--migrations-dir", "", // explicit "no migrations"
	)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "0 migrations") {
		t.Fatalf("expected '0 migrations' summary: %q", stdout)
	}
	entries, _ := readExportedPackage(t, out)
	for k := range entries {
		if strings.HasPrefix(k, "migrations/") {
			t.Fatalf("unexpected migration entry %q", k)
		}
	}
}

func TestPkgExportRejectsBadDependencySpec(t *testing.T) {
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", "http://localhost:0")
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	out := filepath.Join(tmp, "x.weavepkg")
	_, stderr, exit := runCLIWith(t, tmp,
		"pkg", "export",
		"--ontology", "x", "-o", out,
		"--dependencies", "no-version-here",
	)
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "name@version form") {
		t.Fatalf("stderr missing parse hint: %q", stderr)
	}
}

func TestPkgExportPropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorCode":"INTERNAL","errorName":"BoomFailed"}`))
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	out := filepath.Join(tmp, "x.weavepkg")
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "export", "--ontology", "boom", "-o", out)
	if exit != 1 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stderr, "fetch export") {
		t.Fatalf("stderr should mention fetch step: %q", stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatalf("output file should not exist on error")
	}
}

func TestPkgExportDefaultsPkgNameToOntology(t *testing.T) {
	exportBody := `{
		"ontology": {"apiName": "shoebox"}, "objectTypes": [], "linkTypes": [], "actionTypes": [],
		"interfaces": [], "sharedProperties": [], "valueTypes": [], "typeGroups": [],
		"functions": [], "queryTypes": []
	}`
	srv := pkgExportStubServer(t, "shoebox", exportBody)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	out := filepath.Join(tmp, "shoebox.weavepkg")
	if _, _, exit := runCLIWith(t, tmp, "pkg", "export", "--ontology", "shoebox", "-o", out); exit != 0 {
		t.Fatalf("exit non-zero")
	}
	_, manifest := readExportedPackage(t, out)
	if manifest.Name != "shoebox" {
		t.Fatalf("manifest.name = %q, want %q", manifest.Name, "shoebox")
	}
}

func keysOfBytes(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
