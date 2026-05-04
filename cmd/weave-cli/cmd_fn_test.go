package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fnStubServer mounts the routes used by the `weave fn` subcommands. The
// recorded request bodies are exposed via the returned struct so tests can
// assert what the CLI sent.
type fnStubServer struct {
	*httptest.Server
	mu             sync.Mutex
	lastCommitBody map[string]any
}

func newFnStubServer(t *testing.T) *fnStubServer {
	t.Helper()
	srv := &fnStubServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/functions/hello"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"rid":"ri.ontology.main.function.f1","name":"hello","sourceCode":"function hello() { return 1; }"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/commits"):
			body, _ := io.ReadAll(r.Body)
			srv.mu.Lock()
			_ = json.Unmarshal(body, &srv.lastCommitBody)
			srv.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"hash":"abcdef0123456789","message":"unit","author":"alice","email":"alice@example.com","authorDate":"2026-05-04T10:00:00Z"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/log"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"hash":"abcdef0123456789","message":"first","author":"alice","email":"alice@example.com","authorDate":"2026-05-04T10:00:00Z"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Server.Close)
	return srv
}

func TestFn_Pull_WritesSourceToFile(t *testing.T) {
	tmp := t.TempDir()
	srv := newFnStubServer(t)

	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config set: exit %d", exit)
	}
	out := filepath.Join(tmp, "hello.js")
	stdout, stderr, exit := runCLIWith(t, tmp, "fn", "pull",
		"--ontology", "northwind", "--ref", "hello", "-o", out)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "wrote ") {
		t.Fatalf("stdout missing wrote line: %q", stdout)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !strings.Contains(string(body), "function hello()") {
		t.Fatalf("source mismatch: %q", string(body))
	}
}

func TestFn_Pull_ToStdout(t *testing.T) {
	tmp := t.TempDir()
	srv := newFnStubServer(t)
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config: %d", exit)
	}
	stdout, stderr, exit := runCLIWith(t, tmp, "fn", "pull",
		"--ontology", "northwind", "--ref", "hello")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "function hello()") {
		t.Fatalf("stdout missing source: %q", stdout)
	}
}

func TestFn_Push_SendsSourceCode(t *testing.T) {
	tmp := t.TempDir()
	srv := newFnStubServer(t)
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config: %d", exit)
	}

	src := filepath.Join(tmp, "new.js")
	if err := os.WriteFile(src, []byte("function bumped() { return 99; }"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, stderr, exit := runCLIWith(t, tmp, "fn", "push",
		"--ontology", "northwind", "--ref", "hello",
		"-f", src, "-m", "bump", "--author", "alice", "--email", "alice@example.com")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "committed abcdef0123456789") {
		t.Fatalf("stdout missing hash: %q", stdout)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.lastCommitBody["sourceCode"] != "function bumped() { return 99; }" {
		t.Fatalf("body sourceCode missing: %v", srv.lastCommitBody)
	}
	if srv.lastCommitBody["message"] != "bump" {
		t.Fatalf("body message missing: %v", srv.lastCommitBody)
	}
	if srv.lastCommitBody["author"] != "alice" {
		t.Fatalf("body author missing: %v", srv.lastCommitBody)
	}
}

func TestFn_Push_RequiresFlags(t *testing.T) {
	tmp := t.TempDir()
	srv := newFnStubServer(t)
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config: %d", exit)
	}
	cases := []struct {
		args []string
		wantSubstr string
	}{
		{[]string{"fn", "push"}, "--ontology and --ref are required"},
		{[]string{"fn", "push", "--ontology", "n", "--ref", "r"}, "-f"},
		{[]string{"fn", "push", "--ontology", "n", "--ref", "r", "-f", "x"}, "-m"},
	}
	for _, c := range cases {
		_, stderr, exit := runCLIWith(t, tmp, c.args...)
		if exit == 0 {
			t.Fatalf("args %v expected failure, stderr=%q", c.args, stderr)
		}
		if !strings.Contains(stderr, c.wantSubstr) {
			t.Fatalf("stderr missing %q: %q", c.wantSubstr, stderr)
		}
	}
}

func TestFn_Log_RendersTable(t *testing.T) {
	tmp := t.TempDir()
	srv := newFnStubServer(t)
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("config: %d", exit)
	}
	stdout, stderr, exit := runCLIWith(t, tmp, "fn", "log",
		"--ontology", "northwind", "--ref", "hello")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "HASH") || !strings.Contains(stdout, "abcdef012345") {
		t.Fatalf("stdout missing table: %q", stdout)
	}
}

func TestFn_UnknownSubcommandFails(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runCLIWith(t, tmp, "fn", "rebase")
	if exit == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("stderr missing message: %q", stderr)
	}
}
