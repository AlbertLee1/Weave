package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/weavepkg"
)

// writeSampleArchive builds a minimal .weavepkg under dir/<name>.weavepkg
// and returns the absolute path. The archive carries one ontology, one
// action, one function, and one migration so the install summary line has
// non-zero counts.
func writeSampleArchive(t *testing.T, dir, name string) string {
	t.Helper()
	in := weavepkg.BuildInput{
		Manifest: weavepkg.Manifest{
			Name:            name,
			Version:         "1.0.0",
			MinWeaveVersion: "0.42.0",
		},
		Ontology: json.RawMessage(`{"ontology":{"apiName":"` + name + `","displayName":"X"},"objectTypes":[{"apiName":"Foo","displayName":"Foo","primaryKey":"id"}],"linkTypes":[],"actionTypes":[{"apiName":"createFoo","displayName":"Create Foo","status":"ACTIVE"}],"interfaces":[],"sharedProperties":[],"valueTypes":[],"typeGroups":[],"functions":[{"name":"compute","version":"1.0.0","sourceCode":"// fn"}],"queryTypes":[]}`),
		Actions: []weavepkg.ActionEntry{
			{APIName: "createFoo", Body: json.RawMessage(`{"apiName":"createFoo"}`)},
		},
		Functions: []weavepkg.FunctionEntry{
			{Name: "compute", Version: "1.0.0", SourceCode: "// fn"},
		},
		Migrations: []weavepkg.MigrationEntry{
			{Filename: "000001_init.up.sql", Content: []byte("SELECT 1;")},
		},
	}
	path := filepath.Join(dir, name+".weavepkg")
	var buf bytes.Buffer
	if err := weavepkg.Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return path
}

// pkgInstallStubServer mounts a single /api/v2/pkg/install handler that
// returns the supplied status + JSON body. The captured request body is
// stashed on the returned struct for assertion.
type pkgInstallStub struct {
	srv      *httptest.Server
	captured map[string]any
}

func newPkgInstallStub(t *testing.T, status int, body string) *pkgInstallStub {
	t.Helper()
	stub := &pkgInstallStub{}
	stub.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/pkg/install" {
			http.NotFound(w, r)
			return
		}
		var captured map[string]any
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "decode: "+err.Error(), 400)
			return
		}
		stub.captured = captured
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.srv.Close)
	return stub
}

func TestPkgInstallRequiresArchivePath(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "install")
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "exactly one archive") {
		t.Fatalf("stderr should mention required archive path: %q", stderr)
	}
}

func TestPkgInstallReportsMissingFile(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "install", filepath.Join(tmp, "nope.weavepkg"))
	if exit != 1 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stderr, "read") {
		t.Fatalf("stderr should mention read failure: %q", stderr)
	}
}

func TestPkgInstallRejectsBadOnConflict(t *testing.T) {
	tmp := t.TempDir()
	archive := writeSampleArchive(t, tmp, "x")
	_, stderr, exit := runCLIWith(t, tmp, "pkg", "install", "--on-conflict", "explode", archive)
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "fail|overwrite|skip") {
		t.Fatalf("stderr should list valid values: %q", stderr)
	}
}

func TestPkgInstallSendsParsedManifestAndMigrations(t *testing.T) {
	tmp := t.TempDir()
	archive := writeSampleArchive(t, tmp, "northwind")

	stub := newPkgInstallStub(t, 201, `{
		"name": "northwind",
		"version": "1.0.0",
		"ontology": "northwind",
		"imported": {"objectTypes": 1, "actionTypes": 1, "functions": 1},
		"migrationsRan": 1,
		"migrationsTotal": 1,
		"message": "package installed"
	}`)

	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", stub.srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp, "pkg", "install", archive)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}
	if !strings.Contains(stdout, "Installed northwind@1.0.0") {
		t.Fatalf("stdout should report install summary: %q", stdout)
	}
	if !strings.Contains(stdout, "objectTypes=1") || !strings.Contains(stdout, "migrations=1/1") {
		t.Fatalf("stdout should report counts: %q", stdout)
	}

	// The captured body must contain manifest, ontology, migrations, and a
	// `fail` default for onConflict.
	if stub.captured == nil {
		t.Fatalf("server received no body")
	}
	manifest, ok := stub.captured["manifest"].(map[string]any)
	if !ok || manifest["name"] != "northwind" {
		t.Fatalf("manifest drift: %+v", stub.captured["manifest"])
	}
	if stub.captured["onConflict"] != "fail" {
		t.Fatalf("default onConflict drift: %v", stub.captured["onConflict"])
	}
	migrations, ok := stub.captured["migrations"].([]any)
	if !ok || len(migrations) != 1 {
		t.Fatalf("migrations drift: %v", stub.captured["migrations"])
	}
	first, _ := migrations[0].(map[string]any)
	if first["filename"] != "000001_init.up.sql" {
		t.Fatalf("migration filename drift: %v", first)
	}
}

func TestPkgInstallForwardsOnConflictFlag(t *testing.T) {
	tmp := t.TempDir()
	archive := writeSampleArchive(t, tmp, "northwind")
	stub := newPkgInstallStub(t, 201, `{"name":"northwind","version":"1.0.0","ontology":"northwind","imported":{},"migrationsRan":0,"migrationsTotal":0,"message":"ok"}`)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", stub.srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	_, stderr, exit := runCLIWith(t, tmp, "pkg", "install", "--on-conflict", "overwrite", archive)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if stub.captured["onConflict"] != "overwrite" {
		t.Fatalf("on-conflict not forwarded: %v", stub.captured["onConflict"])
	}
}

func TestPkgInstall_Conflict409RendersConflictList(t *testing.T) {
	tmp := t.TempDir()
	archive := writeSampleArchive(t, tmp, "northwind")
	stub := newPkgInstallStub(t, 409, `{
		"errorCode": "CONFLICT",
		"errorName": "PackageConflict",
		"errorInstanceId": "abc",
		"parameters": {
			"package": "northwind",
			"version": "1.0.0",
			"conflicts": "[{\"kind\":\"objectType\",\"apiName\":\"Foo\"},{\"kind\":\"actionType\",\"apiName\":\"createFoo\"}]",
			"hint": "rerun with --on-conflict=overwrite"
		}
	}`)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", stub.srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	_, stderr, exit := runCLIWith(t, tmp, "pkg", "install", archive)
	if exit != 1 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stderr, "objectType/Foo") || !strings.Contains(stderr, "actionType/createFoo") {
		t.Fatalf("stderr should list conflict tuples: %q", stderr)
	}
	if !strings.Contains(stderr, "overwrite") {
		t.Fatalf("stderr should surface server hint: %q", stderr)
	}
}

func TestPkgInstall_MinWeaveVersion400RendersFriendlyError(t *testing.T) {
	tmp := t.TempDir()
	archive := writeSampleArchive(t, tmp, "northwind")
	stub := newPkgInstallStub(t, 400, `{
		"errorCode": "INVALID_ARGUMENT",
		"errorName": "PackageMinWeaveVersionUnsatisfied",
		"errorInstanceId": "abc",
		"parameters": {
			"required": "99.0.0",
			"server":   "0.99.0",
			"reason":   "server version 0.99.0 is older than required 99.0.0"
		}
	}`)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", stub.srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	_, stderr, exit := runCLIWith(t, tmp, "pkg", "install", archive)
	if exit != 1 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stderr, "0.99.0") || !strings.Contains(stderr, "99.0.0") {
		t.Fatalf("stderr should mention both versions: %q", stderr)
	}
}

func TestPkgInstall_JSONFlagDumpsRawResponse(t *testing.T) {
	tmp := t.TempDir()
	archive := writeSampleArchive(t, tmp, "x")
	stub := newPkgInstallStub(t, 201, `{"name":"x","version":"1.0.0","ontology":"x","imported":{},"migrationsRan":0,"migrationsTotal":0,"message":"ok"}`)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", stub.srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp, "pkg", "install", "--json", archive)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json output not valid JSON: %v stdout=%q", err, stdout)
	}
	if got["name"] != "x" {
		t.Fatalf("--json drift: %+v", got)
	}
}
