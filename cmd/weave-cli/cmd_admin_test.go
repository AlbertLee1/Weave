package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingStubServer captures the body of the most recent request and
// returns a fixed JSON payload — we need to verify the CLI is POSTing the
// {ontology, objectType} envelope the server expects.
func recordingStubServer(t *testing.T, method, path, respBody string) (*httptest.Server, *string) {
	t.Helper()
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method && r.URL.Path == path {
			raw, _ := io.ReadAll(r.Body)
			lastBody = string(raw)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(respBody))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody
}

func TestAdminIndexRebuildCLI_SendsRequestAndPrintsCount(t *testing.T) {
	srv, lastBody := recordingStubServer(t,
		http.MethodPost,
		"/api/admin/indexes/rebuild",
		`{"scopedKey":"northwind__Customer","indexedCount":42}`)

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config set base_url exit")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "admin-tok"); exit != 0 {
		t.Fatalf("config set token exit")
	}

	stdout, stderr, exit := runCLIWith(t, tmp,
		"admin", "index", "rebuild",
		"--ontology", "northwind",
		"--object-type", "Customer")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "northwind__Customer") {
		t.Errorf("stdout missing scopedKey: %q", stdout)
	}
	if !strings.Contains(stdout, "42") {
		t.Errorf("stdout missing indexedCount: %q", stdout)
	}
	if !strings.Contains(*lastBody, `"ontology":"northwind"`) {
		t.Errorf("request body missing ontology: %q", *lastBody)
	}
	if !strings.Contains(*lastBody, `"objectType":"Customer"`) {
		t.Errorf("request body missing objectType: %q", *lastBody)
	}
}

func TestAdminIndexRebuildCLI_MissingFlagsReportsUsage(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "admin", "index", "rebuild", "--ontology", "northwind")
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "object-type") {
		t.Errorf("stderr should mention missing flag: %q", stderr)
	}
}

func TestAdminUnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "admin", "ghost")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown") {
		t.Errorf("stderr: %q", stderr)
	}
}

func TestAdminIndexUnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "admin", "index", "destroy")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown") {
		t.Errorf("stderr: %q", stderr)
	}
}
