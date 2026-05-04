package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// indexRebuildStubServer fakes both the count endpoint (for the
// pre-rebuild estimate) and the admin rebuild endpoint, recording the
// last rebuild request body so callers can assert the wire envelope.
func indexRebuildStubServer(t *testing.T, count int, rebuildResp string) (*httptest.Server, *string, *int) {
	t.Helper()
	var lastBody string
	var rebuildHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/count"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"count":` + itoa(count) + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/admin/indexes/rebuild":
			rebuildHits++
			raw, _ := io.ReadAll(r.Body)
			lastBody = string(raw)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(rebuildResp))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody, &rebuildHits
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestIndexRebuildCLI_PrintsEstimateAndSummary walks the happy path:
// CLI fetches the count for an estimate, calls the admin rebuild
// endpoint, and prints both the scoped key + indexed count.
func TestIndexRebuildCLI_PrintsEstimateAndSummary(t *testing.T) {
	srv, lastBody, hits := indexRebuildStubServer(t, 7,
		`{"scopedKey":"northwind__Customer","indexedCount":7}`)

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config set base_url exit")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "admin-tok"); exit != 0 {
		t.Fatalf("config set token exit")
	}

	stdout, stderr, exit := runCLIWith(t, tmp,
		"index", "rebuild",
		"--ontology", "northwind",
		"--object-type", "Customer")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if *hits != 1 {
		t.Errorf("rebuild endpoint hit %d times, want 1", *hits)
	}
	if !strings.Contains(stdout, "estimated 7 rows") {
		t.Errorf("stdout missing estimate: %q", stdout)
	}
	if !strings.Contains(stdout, "northwind__Customer") {
		t.Errorf("stdout missing scopedKey: %q", stdout)
	}
	if !strings.Contains(stdout, "Indexed rows: 7") {
		t.Errorf("stdout missing indexed count line: %q", stdout)
	}
	if !strings.Contains(stdout, "Matches pre-rebuild estimate") {
		t.Errorf("stdout missing estimate match line: %q", stdout)
	}
	if !strings.Contains(*lastBody, `"ontology":"northwind"`) {
		t.Errorf("request body missing ontology: %q", *lastBody)
	}
	if !strings.Contains(*lastBody, `"objectType":"Customer"`) {
		t.Errorf("request body missing objectType: %q", *lastBody)
	}
}

// TestIndexRebuildCLI_NoEstimateFlagSkipsCount confirms --no-estimate
// short-circuits the count probe so an offline / migrated server can
// still rebuild without the pre-call tripping a 5xx.
func TestIndexRebuildCLI_NoEstimateFlagSkipsCount(t *testing.T) {
	var sawCount bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count") {
			sawCount = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scopedKey":"x__Y","indexedCount":1,"count":0}`))
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp,
		"index", "rebuild", "--ontology", "x", "--object-type", "Y", "--no-estimate")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if sawCount {
		t.Error("--no-estimate should skip the count endpoint")
	}
	if !strings.Contains(stdout, "x__Y") {
		t.Errorf("stdout missing scopedKey: %q", stdout)
	}
}

// TestIndexRebuildCLI_JSONFlagSkipsHumanOutput confirms --json emits the
// raw response envelope and bypasses the human progress lines so
// downstream tooling can parse the result without regex.
func TestIndexRebuildCLI_JSONFlagSkipsHumanOutput(t *testing.T) {
	srv, _, _ := indexRebuildStubServer(t, 3,
		`{"scopedKey":"o__T","indexedCount":3}`)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, _, exit := runCLIWith(t, tmp,
		"index", "rebuild", "--ontology", "o", "--object-type", "T", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if strings.Contains(stdout, "estimated") || strings.Contains(stdout, "Indexed rows:") {
		t.Errorf("--json should suppress human lines: %q", stdout)
	}
	if !strings.Contains(stdout, `"scopedKey":"o__T"`) {
		t.Errorf("stdout missing JSON envelope: %q", stdout)
	}
}

// TestIndexRebuildCLI_EstimateFailureFallsBackToUnavailable asserts that
// an erroring count endpoint is logged inline ("estimate unavailable")
// but does NOT abort the rebuild itself — the rebuild may be the fix
// for whatever broke /count.
func TestIndexRebuildCLI_EstimateFailureFallsBackToUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count") {
			http.Error(w, `{"errorCode":"INTERNAL"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scopedKey":"o__T","indexedCount":2}`))
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp,
		"index", "rebuild", "--ontology", "o", "--object-type", "T")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "estimate unavailable") {
		t.Errorf("stdout missing estimate-unavailable line: %q", stdout)
	}
	if !strings.Contains(stdout, "Indexed rows: 2") {
		t.Errorf("stdout missing indexed count: %q", stdout)
	}
}

// TestIndexRebuildCLI_DeltaReporting confirms the post-rebuild summary
// renders +N (rows added) when indexedCount > estimate.
func TestIndexRebuildCLI_DeltaReporting(t *testing.T) {
	srv, _, _ := indexRebuildStubServer(t, 5,
		`{"scopedKey":"o__T","indexedCount":8}`)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, _, exit := runCLIWith(t, tmp,
		"index", "rebuild", "--ontology", "o", "--object-type", "T")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout, "+3 new") {
		t.Errorf("stdout missing positive delta: %q", stdout)
	}
}

// TestIndexRebuildCLI_MissingFlags reports usage instead of panicking
// when --ontology / --object-type are absent.
func TestIndexRebuildCLI_MissingFlags(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "index", "rebuild", "--ontology", "x")
	if exit == 0 {
		t.Fatalf("expected non-zero exit, stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "object-type") {
		t.Errorf("stderr should mention missing flag: %q", stderr)
	}
}

// TestIndexRebuildCLI_UnknownSubcommand keeps the CLI surface predictable
// when operators typo `weave index rebild`.
func TestIndexRebuildCLI_UnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "index", "destroy")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown") {
		t.Errorf("stderr: %q", stderr)
	}
}

// TestIndexRebuildCLI_NoArgs prints usage rather than panicking.
func TestIndexRebuildCLI_NoArgs(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "index")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "rebuild") {
		t.Errorf("stderr should mention rebuild: %q", stderr)
	}
}
