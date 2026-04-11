// Command foundry-parity is the Weave / Foundry parity runner for US-031.
//
// It walks every JSON fixture under test/foundry_parity/ (recursively) and,
// for any fixture that declares an executable `request` block, fires an
// HTTP call against a running Weave server and compares the response body
// against the fixture's `expected` block using deep-equal on canonicalised
// JSON. Failures are reported as unified-style line diffs.
//
// Documentary fixtures (which describe expected response shapes without a
// concrete method/path to execute) are recognised, counted separately, and
// skipped so this runner can coexist with the doc-style fixtures shipped
// before the Phase 6 Gate.
//
// Usage:
//
//	go run ./test/foundry_parity -base http://localhost:9117 [-dir test/foundry_parity]
//
// Exit code is non-zero iff at least one executable fixture fails.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Fixture is the common envelope for both executable and documentary
// parity fixtures. Only the Request + Expected fields are consumed by the
// runner; other fields (story, title, notes, ...) are carried for error
// messages and are ignored during comparison.
type Fixture struct {
	Name     string           `json:"name,omitempty"`
	Story    string           `json:"story,omitempty"`
	Title    string           `json:"title,omitempty"`
	Request  *FixtureRequest  `json:"request,omitempty"`
	Expected *FixtureExpected `json:"expected,omitempty"`
	Notes    any              `json:"notes,omitempty"`
}

// FixtureRequest describes the HTTP call to fire.
type FixtureRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// FixtureExpected declares the minimum conditions the runner asserts
// against the response. Status defaults to 200 when zero. Body is compared
// on canonicalised JSON — absent body means "do not check body".
type FixtureExpected struct {
	Status int             `json:"status,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// loadedFixture pairs a Fixture with the on-disk path it came from so the
// runner can emit deterministic, source-attributed error output.
type loadedFixture struct {
	Path    string
	Fixture Fixture
}

// isExecutable reports whether a fixture has enough information to fire
// an HTTP call. Documentary fixtures are skipped by the runner.
func isExecutable(f Fixture) bool {
	return f.Request != nil && f.Request.Path != ""
}

// loadFixtures walks dir recursively, parses every *.json file into a
// Fixture envelope, and returns them sorted by file path. Parse errors on
// a single file are surfaced as a hard failure so that a malformed fixture
// cannot silently mask a regression.
func loadFixtures(dir string) ([]loadedFixture, error) {
	var out []loadedFixture
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var fx Fixture
		if err := json.Unmarshal(data, &fx); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		out = append(out, loadedFixture{Path: path, Fixture: fx})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// canonicalJSON re-marshals raw with deterministic key ordering and
// stable indentation. encoding/json already sorts map keys alphabetically
// when marshalling a map[string]any, so the round-trip is enough.
func canonicalJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

// diffLines returns a unified-style diff of two line slices, or empty
// string when the slices are equal. Implemented via a small LCS
// (Longest Common Subsequence) table — overkill for single-line bodies
// but keeps the output meaningful when fixtures grow.
func diffLines(want, got []string) string {
	if len(want) == len(got) {
		equal := true
		for i := range want {
			if want[i] != got[i] {
				equal = false
				break
			}
		}
		if equal {
			return ""
		}
	}
	n, m := len(want), len(got)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if want[i] == got[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var b strings.Builder
	b.WriteString("--- expected\n+++ actual\n")
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case want[i] == got[j]:
			fmt.Fprintf(&b, " %s\n", want[i])
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			fmt.Fprintf(&b, "-%s\n", want[i])
			i++
		default:
			fmt.Fprintf(&b, "+%s\n", got[j])
			j++
		}
	}
	for ; i < n; i++ {
		fmt.Fprintf(&b, "-%s\n", want[i])
	}
	for ; j < m; j++ {
		fmt.Fprintf(&b, "+%s\n", got[j])
	}
	return b.String()
}

// diffJSON normalises two JSON payloads to canonical form and returns the
// line-based diff (empty when semantically equal). Non-JSON bytes are
// compared as raw strings so that text/plain bodies still produce useful
// output.
func diffJSON(want, got []byte) (string, error) {
	cw, errW := canonicalJSON(want)
	cg, errG := canonicalJSON(got)
	if errW != nil || errG != nil {
		return diffLines(strings.Split(string(want), "\n"), strings.Split(string(got), "\n")), nil
	}
	if bytes.Equal(cw, cg) {
		return "", nil
	}
	return diffLines(strings.Split(string(cw), "\n"), strings.Split(string(cg), "\n")), nil
}

// runFixture fires a single executable fixture against baseURL and
// returns a non-empty failure message when status or body do not match.
// Transport-level errors are returned as the second return value.
func runFixture(client *http.Client, baseURL string, f Fixture) (string, error) {
	if !isExecutable(f) {
		return "", fmt.Errorf("fixture is not executable")
	}
	method := f.Request.Method
	if method == "" {
		method = http.MethodGet
	}
	url := strings.TrimRight(baseURL, "/") + f.Request.Path

	var body io.Reader
	if len(f.Request.Body) > 0 {
		body = bytes.NewReader(f.Request.Body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if len(f.Request.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range f.Request.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	wantStatus := 200
	if f.Expected != nil && f.Expected.Status != 0 {
		wantStatus = f.Expected.Status
	}
	var failures []string
	if resp.StatusCode != wantStatus {
		failures = append(failures, fmt.Sprintf("status: want %d, got %d", wantStatus, resp.StatusCode))
	}
	if f.Expected != nil && len(f.Expected.Body) > 0 {
		diff, err := diffJSON(f.Expected.Body, respBody)
		if err != nil {
			failures = append(failures, fmt.Sprintf("diff error: %v", err))
		} else if diff != "" {
			failures = append(failures, "body mismatch:\n"+diff)
		}
	}
	if len(failures) == 0 {
		return "", nil
	}
	return strings.Join(failures, "\n"), nil
}

func main() {
	baseURL := flag.String("base", envDefault("WEAVE_BASE_URL", "http://localhost:9117"), "base URL of the running Weave server")
	dir := flag.String("dir", envDefault("WEAVE_PARITY_DIR", "test/foundry_parity"), "directory to scan for fixtures")
	verbose := flag.Bool("v", false, "print per-fixture status lines")
	flag.Parse()

	fixtures, err := loadFixtures(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fixtures: %v\n", err)
		os.Exit(2)
	}
	if len(fixtures) == 0 {
		fmt.Fprintf(os.Stderr, "no fixtures found under %s\n", *dir)
		os.Exit(2)
	}

	client := &http.Client{Timeout: 45 * time.Second}
	var (
		passed  int
		failed  int
		skipped int
	)
	for _, lf := range fixtures {
		name := lf.Fixture.Name
		if name == "" {
			name = lf.Fixture.Story
		}
		if name == "" {
			name = filepath.Base(lf.Path)
		}

		if !isExecutable(lf.Fixture) {
			skipped++
			if *verbose {
				fmt.Printf("SKIP  %s (%s) — documentary\n", name, lf.Path)
			}
			continue
		}
		failure, err := runFixture(client, *baseURL, lf.Fixture)
		if err != nil {
			failed++
			fmt.Printf("ERROR %s (%s): %v\n", name, lf.Path, err)
			continue
		}
		if failure != "" {
			failed++
			fmt.Printf("FAIL  %s (%s)\n%s\n", name, lf.Path, failure)
			continue
		}
		passed++
		if *verbose {
			fmt.Printf("PASS  %s (%s)\n", name, lf.Path)
		}
	}

	total := passed + failed
	fmt.Printf("\nparity: %d passed / %d executed, %d skipped (documentary)\n", passed, total, skipped)
	if failed > 0 {
		os.Exit(1)
	}
}

func envDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
