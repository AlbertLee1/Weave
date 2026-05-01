package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runREPLBatch wraps runREPL with a non-TTY stdin so we exercise the batch
// branch in tests.
func runREPLBatchTest(t *testing.T, configDir, input string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	t.Setenv("WEAVE_CONFIG_DIR", configDir)
	var stdoutBuf, stderrBuf bytes.Buffer
	stdin := strings.NewReader(input)
	exit = runREPL(args, stdin, &stdoutBuf, &stderrBuf)
	return stdoutBuf.String(), stderrBuf.String(), exit
}

func TestREPLExitCommandReturnsZero(t *testing.T) {
	tmp := t.TempDir()
	_, _, exit := runREPLBatchTest(t, tmp, "exit\n")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
}

func TestREPLEOFReturnsZero(t *testing.T) {
	tmp := t.TempDir()
	_, _, exit := runREPLBatchTest(t, tmp, "")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 on EOF", exit)
	}
}

func TestREPLEmptyAndCommentLinesNoOp(t *testing.T) {
	tmp := t.TempDir()
	stdout, stderr, exit := runREPLBatchTest(t, tmp, "\n   \n# comment\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty for blank/comment lines: %q", stderr)
	}
	_ = stdout
}

func TestREPLHelpPrintsUsage(t *testing.T) {
	tmp := t.TempDir()
	stdout, _, exit := runREPLBatchTest(t, tmp, "help\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stdout, "ontology") || !strings.Contains(stdout, "object") {
		t.Fatalf("help output missing commands: %q", stdout)
	}
}

func TestREPLUnknownCommandReportsErrorAndContinues(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runREPLBatchTest(t, tmp, "garbage\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (error should not abort REPL)", exit)
	}
	if !strings.Contains(stderr, "unknown") {
		t.Fatalf("stderr should mention unknown command: %q", stderr)
	}
}

func TestREPLNestedREPLRejected(t *testing.T) {
	tmp := t.TempDir()
	_, stderr, exit := runREPLBatchTest(t, tmp, "repl\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stderr, "already in REPL") {
		t.Fatalf("stderr should reject nested repl: %q", stderr)
	}
}

func TestREPLDispatchesToConfigSubcommand(t *testing.T) {
	tmp := t.TempDir()
	_, _, exit := runREPLBatchTest(t, tmp, `config set base_url http://r.example`+"\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.BaseURL != "http://r.example" {
		t.Fatalf("base_url = %q, want http://r.example", cfg.BaseURL)
	}
}

func TestREPLDispatchesToOntologyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"rid":"ri.x","apiName":"northwind","displayName":"NW","currentVersion":1}]}`))
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "base_url", srv.URL); exit != 0 {
		t.Fatalf("set base_url failed")
	}
	if _, _, exit := runCLIWith(t, tmp, "config", "set", "access_token", "tok"); exit != 0 {
		t.Fatalf("set token failed")
	}

	stdout, stderr, exit := runREPLBatchTest(t, tmp, "ontology list\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "northwind") {
		t.Fatalf("expected northwind in output: %q", stdout)
	}
}

func TestREPLBatchAppendsHistoryFile(t *testing.T) {
	tmp := t.TempDir()
	_, _, exit := runREPLBatchTest(t, tmp, "help\n# skipped comment\n\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "history"))
	if err != nil {
		t.Fatalf("history file should exist: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "help") || !strings.Contains(body, "exit") {
		t.Fatalf("history missing entries: %q", body)
	}
	if strings.Contains(body, "# skipped") {
		t.Fatalf("history should skip comment lines: %q", body)
	}
}

func TestREPLBatchHistoryTrimsToMaxEntries(t *testing.T) {
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history")
	// Pre-seed N+10 history lines so the trim path actually fires.
	var b strings.Builder
	for i := 0; i < replMaxHistory+10; i++ {
		b.WriteString("seed\n")
	}
	if err := os.WriteFile(historyPath, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	_, _, exit := runREPLBatchTest(t, tmp, "help\nexit\n")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	data, _ := os.ReadFile(historyPath)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > replMaxHistory {
		t.Fatalf("history not trimmed: %d > %d", len(lines), replMaxHistory)
	}
}

func TestSplitREPLArgsBasic(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo bar baz", []string{"foo", "bar", "baz"}},
		{"  spaces  around  ", []string{"spaces", "around"}},
		{`config set base_url "http://x:9117"`, []string{"config", "set", "base_url", "http://x:9117"}},
		{`auth login --email "a b" --password 'p w'`, []string{"auth", "login", "--email", "a b", "--password", "p w"}},
		{`hello\ world`, []string{"hello world"}},
	}
	for _, tc := range tests {
		got, err := splitREPLArgs(tc.in)
		if err != nil {
			t.Fatalf("splitREPLArgs(%q) error = %v", tc.in, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("splitREPLArgs(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitREPLArgs(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestSplitREPLArgsRejectsUnclosedQuote(t *testing.T) {
	if _, err := splitREPLArgs(`foo "unterminated`); err == nil {
		t.Fatalf("expected error on unclosed quote")
	}
}

func TestAutoCompleteREPLTopLevel(t *testing.T) {
	// Unique prefix that matches exactly one command should auto-complete and add a trailing space.
	got, pos, ok := autoCompleteREPL("ont", 3)
	if !ok {
		t.Fatalf("expected completion for 'ont'")
	}
	if got != "ontology " || pos != len(got) {
		t.Fatalf("got %q pos %d, want 'ontology ' at end", got, pos)
	}
}

func TestAutoCompleteREPLAmbiguousNoExtensionReturnsFalse(t *testing.T) {
	// Both "object" and "ontology" start with "o" but their longest common
	// prefix is just "o" — there's nothing more to extend, so the callback
	// should signal "no change" (ok=false) so the terminal beeps instead of
	// blanking the line.
	got, pos, ok := autoCompleteREPL("o", 1)
	if ok || got != "" || pos != 0 {
		t.Fatalf("expected no-op for ambiguous prefix with no extension, got (%q, %d, %v)", got, pos, ok)
	}
}

func TestAutoCompleteREPLExtendsCommonPrefix(t *testing.T) {
	// Direct unit test on longestCommonPrefix so the extension path is
	// covered without needing a contrived candidate set.
	if got := longestCommonPrefix([]string{"object", "obscure"}); got != "ob" {
		t.Fatalf("longestCommonPrefix = %q, want 'ob'", got)
	}
	if got := longestCommonPrefix([]string{"a", "abc"}); got != "a" {
		t.Fatalf("longestCommonPrefix = %q, want 'a'", got)
	}
	if got := longestCommonPrefix([]string{"foo", "bar"}); got != "" {
		t.Fatalf("longestCommonPrefix = %q, want ''", got)
	}
}

func TestAutoCompleteREPLNoMatchReturnsFalse(t *testing.T) {
	if _, _, ok := autoCompleteREPL("zzz", 3); ok {
		t.Fatalf("expected no completion for 'zzz'")
	}
}

func TestAutoCompleteREPLSubcommand(t *testing.T) {
	got, _, ok := autoCompleteREPL("ontology l", 10)
	if !ok {
		t.Fatalf("expected completion for 'ontology l'")
	}
	if got != "ontology list " {
		t.Fatalf("got %q, want 'ontology list '", got)
	}
}

func TestAutoCompleteREPLSubcommandUnknownTopLevel(t *testing.T) {
	if _, _, ok := autoCompleteREPL("garbage l", 9); ok {
		t.Fatalf("expected no completion for unknown top-level command")
	}
}

func TestAutoCompleteREPLOnlyFiresOnTabKey(t *testing.T) {
	got, pos, ok := replAutoCompleteCallback("ont", 3, 'a')
	if ok || got != "" || pos != 0 {
		t.Fatalf("non-tab key should be a no-op, got (%q, %d, %v)", got, pos, ok)
	}
}

func TestREPLMainDispatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WEAVE_CONFIG_DIR", tmp)
	var stdoutBuf, stderrBuf bytes.Buffer
	exit := run([]string{"repl"}, strings.NewReader("exit\n"), &stdoutBuf, &stderrBuf)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderrBuf.String())
	}
}
