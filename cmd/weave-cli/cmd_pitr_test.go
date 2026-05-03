package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingPitrServer captures the most recent matching request and returns
// the configured JSON body. Mirrors recordingStubServer but supports query
// strings in the path (the rollback URL carries `?to=tx-...`).
func recordingPitrServer(t *testing.T, method, requestURI, respBody string) (*httptest.Server, *string, *string) {
	t.Helper()
	var lastBody, lastURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method && r.URL.RequestURI() == requestURI {
			raw, _ := io.ReadAll(r.Body)
			lastBody = string(raw)
			lastURI = r.URL.RequestURI()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(respBody))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody, &lastURI
}

// TestPitrRestoreCLI_SendsRollbackAndPrintsCounts verifies the CLI POSTs
// to /api/v2/datasets/{rid}/rollback?to=tx-... and surfaces the
// rolled-back tx ids + restored / deleted counts back to stdout.
func TestPitrRestoreCLI_SendsRollbackAndPrintsCounts(t *testing.T) {
	resp := `{
		"rolledBackTxIds": ["tx-2", "tx-3"],
		"restoredObjects": 1,
		"deletedObjects": 1,
		"newTransaction": {"txId":"tx-bookkeeping","parentTxId":"tx-1","ontologyApiName":"shop","committedAt":"2026-05-03T00:00:00Z","editsCount":2,"rolledBackToTxId":"tx-1"},
		"targetTx": {"txId":"tx-1","ontologyApiName":"shop","committedAt":"2026-01-01T00:00:00Z","editsCount":1}
	}`
	srv, lastBody, lastURI := recordingPitrServer(t,
		http.MethodPost,
		"/api/v2/datasets/shop/rollback?to=tx-1",
		resp)

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config set base_url exit")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "admin-tok"); exit != 0 {
		t.Fatalf("config set token exit")
	}

	stdout, stderr, exit := runCLIWith(t, tmp,
		"pitr", "restore",
		"--dataset", "shop",
		"--to-tx", "tx-1")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "tx-2") || !strings.Contains(stdout, "tx-3") {
		t.Errorf("stdout missing rolled-back txids: %q", stdout)
	}
	if !strings.Contains(stdout, "restored") || !strings.Contains(stdout, "deleted") {
		t.Errorf("stdout missing restored/deleted summary: %q", stdout)
	}
	if !strings.Contains(stdout, "tx-bookkeeping") {
		t.Errorf("stdout missing bookkeeping tx id: %q", stdout)
	}
	if *lastBody != "" {
		t.Errorf("expected empty POST body, got %q", *lastBody)
	}
	if !strings.Contains(*lastURI, "to=tx-1") {
		t.Errorf("request URI missing rollback target: %q", *lastURI)
	}
}

// TestPitrRestoreCLI_JSONOutput emits the raw response when --json is set.
func TestPitrRestoreCLI_JSONOutput(t *testing.T) {
	srv, _, _ := recordingPitrServer(t,
		http.MethodPost,
		"/api/v2/datasets/shop/rollback?to=tx-7",
		`{"rolledBackTxIds":["tx-8"],"restoredObjects":3,"deletedObjects":0,"newTransaction":{"txId":"tx-x","ontologyApiName":"shop","committedAt":"2026-05-03T00:00:00Z","editsCount":3}}`)
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp,
		"pitr", "restore",
		"--dataset", "shop",
		"--to-tx", "tx-7",
		"--json")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, stdout)
	}
	if got["restoredObjects"].(float64) != 3 {
		t.Errorf("restoredObjects = %v, want 3", got["restoredObjects"])
	}
}

// TestPitrRestoreCLI_MissingFlagsReportsUsage rejects calls that omit the
// dataset identifier or the rollback target.
func TestPitrRestoreCLI_MissingFlagsReportsUsage(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pitr", "restore", "--dataset", "shop")
	if exit == 0 {
		t.Fatalf("expected non-zero exit; stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "to-tx") {
		t.Errorf("stderr should mention missing flag: %q", stderr)
	}

	_, stderr, exit = runCLIWith(t, tmp, "pitr", "restore", "--to-tx", "tx-1")
	if exit == 0 {
		t.Fatalf("expected non-zero exit; stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "dataset") {
		t.Errorf("stderr should mention missing flag: %q", stderr)
	}
}

// TestPitrRestoreCLI_RejectsTargetWithoutPrefix surfaces the "to-tx must
// start with tx-" guardrail client-side, mirroring the server's
// InvalidRollbackTarget envelope.
func TestPitrRestoreCLI_RejectsTargetWithoutPrefix(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp,
		"pitr", "restore",
		"--dataset", "shop",
		"--to-tx", "garbage")
	if exit == 0 {
		t.Fatalf("expected non-zero exit; stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "tx-") {
		t.Errorf("stderr should mention tx- prefix: %q", stderr)
	}
}

// TestPitrRestoreCLI_PropagatesServerError surfaces a 4xx envelope from the
// server through the CLI's exit code.
func TestPitrRestoreCLI_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorCode":"NOT_FOUND","errorName":"RollbackTargetNotFound","parameters":{"to":"tx-missing"}}`))
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	_, stderr, exit := runCLIWith(t, tmp,
		"pitr", "restore",
		"--dataset", "shop",
		"--to-tx", "tx-missing")
	if exit == 0 {
		t.Fatal("expected non-zero exit on 404")
	}
	if !strings.Contains(stderr, "RollbackTargetNotFound") {
		t.Errorf("stderr missing error name: %q", stderr)
	}
}

// TestPitrUnknownSubcommand mirrors the existing admin-subcommand discipline.
func TestPitrUnknownSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "pitr", "ghost")
	if exit == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown") {
		t.Errorf("stderr: %q", stderr)
	}
}

// TestPitrHistoryCLI_ListsTransactions verifies the helper that surfaces
// the dataset transaction chain — useful for picking a `--to-tx` target
// before invoking restore.
func TestPitrHistoryCLI_ListsTransactions(t *testing.T) {
	srv, _, _ := recordingPitrServer(t,
		http.MethodGet,
		"/api/v2/datasets/shop/history",
		`{"transactions":[
			{"txId":"tx-3","parentTxId":"tx-2","ontologyApiName":"shop","committedAt":"2026-05-03T00:02:00Z","editsCount":1},
			{"txId":"tx-2","parentTxId":"tx-1","ontologyApiName":"shop","committedAt":"2026-05-03T00:01:00Z","editsCount":2},
			{"txId":"tx-1","ontologyApiName":"shop","committedAt":"2026-05-03T00:00:00Z","editsCount":1}
		]}`)
	tmp := t.TempDir()
	_, _, _ = runCLIWith(t, tmp, "config", "set", "base_url", srv.URL)
	_, _, _ = runCLIWith(t, tmp, "config", "set", "access_token", "tok")

	stdout, stderr, exit := runCLIWith(t, tmp, "pitr", "history", "--dataset", "shop")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	for _, want := range []string{"tx-1", "tx-2", "tx-3"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %s: %q", want, stdout)
		}
	}
}
