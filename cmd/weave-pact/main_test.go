package main

import (
	"bytes"
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

type capture struct {
	mu   sync.Mutex
	reqs []*http.Request
	body map[string][]byte
}

func newCapture() (*capture, *httptest.Server) {
	c := &capture{body: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.reqs = append(c.reqs, r)
		switch r.Method {
		case http.MethodPut:
			c.body[r.URL.Path] = body
		}
		c.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"_links":{}}`))
		case http.MethodGet:
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) == 4 && parts[0] == "pacts" && parts[3] == "latest" {
				c.mu.Lock()
				keys := make([]string, 0, len(c.body))
				for k := range c.body {
					keys = append(keys, k)
				}
				c.mu.Unlock()
				refs := []map[string]any{}
				for _, k := range keys {
					if !strings.HasPrefix(k, "/pacts/provider/"+parts[2]+"/") {
						continue
					}
					refs = append(refs, map[string]any{"href": "http://" + r.Host + k})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"_links": map[string]any{"pacts": refs}})
				return
			}
			c.mu.Lock()
			body, ok := c.body[r.URL.Path]
			c.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		}
	}))
	return c, srv
}

func TestPublish_PostsEveryPactInDir(t *testing.T) {
	cap, srv := newCapture()
	defer srv.Close()

	dir := t.TempDir()
	writePactFile(t, filepath.Join(dir, "py-sdk.pact.json"), "weave-py-sdk")
	writePactFile(t, filepath.Join(dir, "ts-sdk.pact.json"), "weave-ts-sdk")
	writePactFile(t, filepath.Join(dir, "ignored.txt"), "irrelevant")

	var stdout, stderr bytes.Buffer
	err := run([]string{"publish", "-broker", srv.URL, "-dir", dir, "-version", "1.2.3"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("publish: %v\nstderr: %s", err, stderr.String())
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.reqs) != 2 {
		t.Fatalf("expected 2 PUT requests, got %d", len(cap.reqs))
	}
	for _, r := range cap.reqs {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/version/1.2.3") {
			t.Errorf("path %q missing version segment", r.URL.Path)
		}
	}
	if !strings.Contains(stdout.String(), "published py-sdk.pact.json") {
		t.Errorf("stdout missing publish line: %q", stdout.String())
	}
}

func TestPublish_RequiresBroker(t *testing.T) {
	t.Setenv("WEAVE_PACT_BROKER_URL", "")
	var stdout, stderr bytes.Buffer
	err := run([]string{"publish", "-dir", t.TempDir()}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "-broker is required") {
		t.Fatalf("expected missing-broker error, got %v", err)
	}
}

func TestPublish_DefaultsVersionFromEnv(t *testing.T) {
	cap, srv := newCapture()
	defer srv.Close()
	dir := t.TempDir()
	writePactFile(t, filepath.Join(dir, "py-sdk.pact.json"), "weave-py-sdk")

	t.Setenv("WEAVE_PACT_VERSION", "from-env-7")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"publish", "-broker", srv.URL, "-dir", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("publish: %v\nstderr: %s", err, stderr.String())
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.reqs) != 1 {
		t.Fatalf("expected 1 PUT, got %d", len(cap.reqs))
	}
	if !strings.Contains(cap.reqs[0].URL.Path, "/version/from-env-7") {
		t.Errorf("expected version from env, got path %q", cap.reqs[0].URL.Path)
	}
}

func TestList_PrintsConsumersForProvider(t *testing.T) {
	cap, srv := newCapture()
	defer srv.Close()
	dir := t.TempDir()
	writePactFile(t, filepath.Join(dir, "py-sdk.pact.json"), "weave-py-sdk")
	writePactFile(t, filepath.Join(dir, "ts-sdk.pact.json"), "weave-ts-sdk")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"publish", "-broker", srv.URL, "-dir", dir, "-version", "1"}, &stdout, &stderr); err != nil {
		t.Fatalf("publish: %v", err)
	}
	stdout.Reset()
	if err := run([]string{"list", "-broker", srv.URL, "-provider", "weave-server"}, &stdout, &stderr); err != nil {
		t.Fatalf("list: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"weave-py-sdk", "weave-ts-sdk"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q\noutput:\n%s", want, out)
		}
	}
	_ = cap
}

func TestRun_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"foo"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}

func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "subcommand required") {
		t.Fatalf("expected subcommand-required error, got %v", err)
	}
}

func writePactFile(t *testing.T, path, consumer string) {
	t.Helper()
	body := `{
		"consumer": {"name": "` + consumer + `"},
		"provider": {"name": "weave-server"},
		"interactions": [
			{
				"description": "GET /health",
				"request": {"method": "GET", "path": "/health"},
				"response": {"status": 200, "body": {"status":"alive"}}
			}
		]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
