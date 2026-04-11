package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalJSON_SortsKeysDeterministically(t *testing.T) {
	a := []byte(`{"b":1,"a":2,"c":{"z":3,"y":4}}`)
	b := []byte(`{"a":2,"c":{"y":4,"z":3},"b":1}`)

	ca, err := canonicalJSON(a)
	if err != nil {
		t.Fatalf("canonicalJSON(a): %v", err)
	}
	cb, err := canonicalJSON(b)
	if err != nil {
		t.Fatalf("canonicalJSON(b): %v", err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("canonical forms differ:\nA: %s\nB: %s", ca, cb)
	}
}

func TestCanonicalJSON_HandlesArraysAndScalars(t *testing.T) {
	cases := [][]byte{
		[]byte(`null`),
		[]byte(`42`),
		[]byte(`"hello"`),
		[]byte(`[3,2,1]`),
		[]byte(`{"x":[1,{"a":1,"b":2}]}`),
	}
	for _, raw := range cases {
		got, err := canonicalJSON(raw)
		if err != nil {
			t.Fatalf("canonicalJSON(%s): %v", raw, err)
		}
		if len(got) == 0 {
			t.Fatalf("empty canonical form for %s", raw)
		}
	}
}

func TestIsExecutable(t *testing.T) {
	doc := Fixture{Story: "US-005", Title: "desc only"}
	if isExecutable(doc) {
		t.Errorf("documentary fixture should not be executable")
	}
	exe := Fixture{
		Request: &FixtureRequest{Method: "GET", Path: "/health"},
	}
	if !isExecutable(exe) {
		t.Errorf("fixture with request.path should be executable")
	}
	empty := Fixture{Request: &FixtureRequest{Method: "GET"}}
	if isExecutable(empty) {
		t.Errorf("fixture with empty path should not be executable")
	}
}

func TestDiffLines_Equal(t *testing.T) {
	d := diffLines([]string{"a", "b", "c"}, []string{"a", "b", "c"})
	if d != "" {
		t.Errorf("expected empty diff, got %q", d)
	}
}

func TestDiffLines_Different(t *testing.T) {
	d := diffLines([]string{"a", "b", "c"}, []string{"a", "x", "c"})
	if d == "" {
		t.Fatalf("expected non-empty diff")
	}
	if !strings.Contains(d, "-b") {
		t.Errorf("diff missing - marker for removed line 'b':\n%s", d)
	}
	if !strings.Contains(d, "+x") {
		t.Errorf("diff missing + marker for added line 'x':\n%s", d)
	}
}

func TestLoadFixtures_MixedDocAndExecutable(t *testing.T) {
	dir := t.TempDir()
	// documentary fixture — lives at top level, should be skipped by runner
	doc := `{"story":"US-005","title":"doc only","expected":{"sortKey":["x"]}}`
	if err := os.WriteFile(filepath.Join(dir, "us005_doc.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	// executable fixture in a subdirectory
	runnerDir := filepath.Join(dir, "runner")
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := `{"name":"us005","story":"US-005","request":{"method":"GET","path":"/health"},"expected":{"status":200,"body":{"status":"alive"}}}`
	if err := os.WriteFile(filepath.Join(runnerDir, "us005.json"), []byte(exe), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadFixtures(dir)
	if err != nil {
		t.Fatalf("loadFixtures: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded fixtures, got %d", len(loaded))
	}
	// sorted by path — "runner/us005.json" sorts before "us005_doc.json"
	// because 'r' < 'u' at the first differing byte
	if !strings.HasSuffix(loaded[0].Path, filepath.Join("runner", "us005.json")) {
		t.Errorf("unexpected first fixture: %s", loaded[0].Path)
	}
	if !strings.HasSuffix(loaded[1].Path, "us005_doc.json") {
		t.Errorf("unexpected second fixture: %s", loaded[1].Path)
	}
	exeCount := 0
	docCount := 0
	for _, lf := range loaded {
		if isExecutable(lf.Fixture) {
			exeCount++
		} else {
			docCount++
		}
	}
	if exeCount != 1 || docCount != 1 {
		t.Errorf("expected 1 exec + 1 doc, got exec=%d doc=%d", exeCount, docCount)
	}
}

func TestRunFixture_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	}))
	defer srv.Close()

	fx := Fixture{
		Name: "health_ok",
		Request: &FixtureRequest{
			Method: "GET",
			Path:   "/health",
		},
		Expected: &FixtureExpected{
			Status: 200,
			Body:   json.RawMessage(`{"status":"alive"}`),
		},
	}
	failure, err := runFixture(srv.Client(), srv.URL, fx)
	if err != nil {
		t.Fatalf("runFixture: %v", err)
	}
	if failure != "" {
		t.Fatalf("unexpected failure: %s", failure)
	}
}

func TestRunFixture_StatusMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	fx := Fixture{
		Name:     "boom",
		Request:  &FixtureRequest{Method: "GET", Path: "/anything"},
		Expected: &FixtureExpected{Status: 200},
	}
	failure, err := runFixture(srv.Client(), srv.URL, fx)
	if err != nil {
		t.Fatalf("runFixture: %v", err)
	}
	if failure == "" {
		t.Fatal("expected failure message, got empty")
	}
	if !strings.Contains(failure, "status") {
		t.Errorf("failure should mention status, got: %s", failure)
	}
}

func TestRunFixture_BodyMismatchProducesDiff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"dead"}`))
	}))
	defer srv.Close()

	fx := Fixture{
		Name:    "drift",
		Request: &FixtureRequest{Method: "GET", Path: "/health"},
		Expected: &FixtureExpected{
			Status: 200,
			Body:   json.RawMessage(`{"status":"alive"}`),
		},
	}
	failure, err := runFixture(srv.Client(), srv.URL, fx)
	if err != nil {
		t.Fatalf("runFixture: %v", err)
	}
	if failure == "" {
		t.Fatal("expected body-mismatch failure")
	}
	if !strings.Contains(failure, "-") || !strings.Contains(failure, "+") {
		t.Errorf("body-diff failure missing unified-diff markers:\n%s", failure)
	}
}

func TestRunFixture_PostWithBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	fx := Fixture{
		Name: "echo",
		Request: &FixtureRequest{
			Method: "POST",
			Path:   "/echo",
			Body:   json.RawMessage(`{"a":1}`),
		},
		Expected: &FixtureExpected{Status: 200, Body: json.RawMessage(`{"ok":true}`)},
	}
	failure, err := runFixture(srv.Client(), srv.URL, fx)
	if err != nil {
		t.Fatalf("runFixture: %v", err)
	}
	if failure != "" {
		t.Fatalf("unexpected failure: %s", failure)
	}
	if gotBody["a"].(float64) != 1 {
		t.Errorf("server did not see posted body: %v", gotBody)
	}
}

func TestSeedFixturesLoadable(t *testing.T) {
	// The 5 seed runner fixtures required by US-031 must exist and parse.
	seeds := []string{"us005.json", "us008.json", "us012.json", "us015.json", "us022.json"}
	for _, name := range seeds {
		path := filepath.Join("runner", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("missing seed fixture %s: %v", path, err)
			continue
		}
		var fx Fixture
		if err := json.Unmarshal(data, &fx); err != nil {
			t.Errorf("%s: invalid JSON: %v", path, err)
			continue
		}
		if !isExecutable(fx) {
			t.Errorf("%s: seed fixture must be executable (request.path)", path)
		}
		if fx.Expected == nil || fx.Expected.Status == 0 {
			t.Errorf("%s: seed fixture must declare expected.status", path)
		}
	}
}
