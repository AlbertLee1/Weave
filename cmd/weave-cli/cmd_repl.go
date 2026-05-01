package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"
)

// REPL constants — exposed for tests.
const (
	replPrompt      = "weave> "
	replHistoryName = "history"
	replMaxHistory  = 1000
)

// replCommands lists top-level command names recognised inside the REPL.
// Mirrors the dispatch in main.run plus the REPL-only "exit"/"help".
var replCommands = []string{
	"admin", "auth", "config", "exit", "help", "object", "ontology", "quit",
}

// replSubcommands enumerates known second-token completions per top-level
// command. Source of truth is the matching `case` block in each cmd_*.go file.
var replSubcommands = map[string][]string{
	"admin":    {"index"},
	"auth":     {"login", "logout", "status"},
	"config":   {"get", "set"},
	"object":   {"get", "list", "search"},
	"ontology": {"get", "list"},
}

// runREPL is the entry point for `weave repl`.
//
// Two execution modes:
//   - interactive: stdin is a TTY → use golang.org/x/term for line editing,
//     tab completion and an in-session history ring buffer.
//   - batch: stdin is anything else → read commands one per line via bufio.
//
// Both modes append accepted lines to a flat-text history file under
// configDir() so future sessions (and operators auditing CLI use) can grep it.
func runREPL(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "weave repl: takes no arguments (got %q)\n", args[0])
		return 2
	}

	historyPath := filepath.Join(configDir(), replHistoryName)

	stdinFile, _ := stdin.(*os.File)
	if stdinFile != nil && term.IsTerminal(int(stdinFile.Fd())) {
		return runREPLInteractive(stdinFile, stdout, stderr, historyPath)
	}
	return runREPLBatch(stdin, stdout, stderr, historyPath)
}

// runREPLBatch reads one command per line, dispatches it, and appends to the
// history file. Used for piped input and tests; an exit/quit short-circuits.
func runREPLBatch(stdin io.Reader, stdout, stderr io.Writer, historyPath string) int {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		appendHistoryIfMeaningful(historyPath, line)
		if cont, code := dispatchREPLLine(line, stdout, stderr); !cont {
			return code
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(stderr, "repl: %v\n", err)
		return 1
	}
	return 0
}

// runREPLInteractive enters raw-mode and uses term.Terminal for line editing
// + tab completion. Each accepted line is also appended to the history file.
func runREPLInteractive(stdinFile *os.File, stdout, stderr io.Writer, historyPath string) int {
	oldState, err := term.MakeRaw(int(stdinFile.Fd()))
	if err != nil {
		fmt.Fprintf(stderr, "repl: enter raw mode: %v\n", err)
		// Best-effort fallback so a degraded TTY (e.g. CI) still works.
		return runREPLBatch(stdinFile, stdout, stderr, historyPath)
	}
	defer func() { _ = term.Restore(int(stdinFile.Fd()), oldState) }()

	rw := struct {
		io.Reader
		io.Writer
	}{Reader: stdinFile, Writer: stdout}
	t := term.NewTerminal(rw, replPrompt)
	t.AutoCompleteCallback = replAutoCompleteCallback

	fmt.Fprintf(t, "Weave REPL — type \"help\" for commands, \"exit\" to quit.\n")

	for {
		line, err := t.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0
			}
			fmt.Fprintf(stderr, "repl: %v\n", err)
			return 1
		}
		appendHistoryIfMeaningful(historyPath, line)
		if cont, code := dispatchREPLLine(line, t, stderr); !cont {
			return code
		}
	}
}

// dispatchREPLLine parses one line and routes it to the appropriate runX
// helper. cont=false signals the REPL loop should exit with the returned code.
func dispatchREPLLine(line string, stdout, stderr io.Writer) (cont bool, exitCode int) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return true, 0
	}
	args, err := splitREPLArgs(trimmed)
	if err != nil {
		fmt.Fprintf(stderr, "repl: %v\n", err)
		return true, 0
	}
	if len(args) == 0 {
		return true, 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "exit", "quit":
		return false, 0
	case "help", "?":
		printRootUsage(stdout)
	case "ontology":
		runOntology(rest, stdout, stderr)
	case "object":
		runObject(rest, stdout, stderr)
	case "auth":
		runAuth(rest, stdout, stderr)
	case "config":
		runConfig(rest, stdout, stderr)
	case "admin":
		runAdmin(rest, stdout, stderr)
	case "repl":
		fmt.Fprintln(stderr, "repl: already in REPL")
	default:
		fmt.Fprintf(stderr, "repl: unknown command %q (try \"help\")\n", cmd)
	}
	return true, 0
}

// splitREPLArgs is a small shell-like tokenizer: whitespace-separated tokens
// with single- and double-quoted spans, plus backslash escaping outside quotes.
// Returns an error for unterminated quotes so the caller can show a clean
// diagnostic instead of misparsing the line.
func splitREPLArgs(line string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inToken := false
	quote := byte(0)
	escape := false

	flush := func() {
		if inToken {
			out = append(out, cur.String())
			cur.Reset()
			inToken = false
		}
	}

	for i := 0; i < len(line); i++ {
		c := line[i]
		if escape {
			cur.WriteByte(c)
			inToken = true
			escape = false
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
			continue
		}
		switch c {
		case '\\':
			escape = true
		case '"', '\'':
			quote = c
			inToken = true
		case ' ', '\t':
			flush()
		default:
			cur.WriteByte(c)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if escape {
		return nil, errors.New("trailing backslash")
	}
	flush()
	return out, nil
}

// replAutoCompleteCallback is the term.Terminal hook. Only fires on Tab; all
// other keys delegate to the default editor by returning ok=false.
func replAutoCompleteCallback(line string, pos int, key rune) (string, int, bool) {
	if key != '\t' {
		return "", 0, false
	}
	return autoCompleteREPL(line, pos)
}

// autoCompleteREPL implements the completion logic as a pure function so it
// can be tested without a terminal. Behaviour:
//   - empty line: no completion (don't dump every command on first Tab)
//   - first token: complete from replCommands; unique match adds trailing space,
//     multi-match extends to longest common prefix without a trailing space
//   - second token: complete from replSubcommands[firstToken] under the same rules
//   - third+ token or unknown context: no completion (let the user type)
func autoCompleteREPL(line string, pos int) (string, int, bool) {
	prefix := line[:pos]
	suffix := line[pos:]

	// Tokenise prefix only — completion is for what's been typed so far.
	tokens := strings.Fields(prefix)
	endsWithSpace := pos > 0 && (prefix[pos-1] == ' ' || prefix[pos-1] == '\t')

	var (
		candidates []string
		partial    string
		head       string
	)
	switch {
	case len(tokens) == 0:
		return "", 0, false
	case len(tokens) == 1 && !endsWithSpace:
		candidates = replCommands
		partial = tokens[0]
		head = ""
	case len(tokens) == 1 && endsWithSpace:
		subs, ok := replSubcommands[tokens[0]]
		if !ok {
			return "", 0, false
		}
		candidates = subs
		partial = ""
		head = tokens[0] + " "
	case len(tokens) == 2 && !endsWithSpace:
		subs, ok := replSubcommands[tokens[0]]
		if !ok {
			return "", 0, false
		}
		candidates = subs
		partial = tokens[1]
		head = tokens[0] + " "
	default:
		return "", 0, false
	}

	matches := matchPrefix(candidates, partial)
	if len(matches) == 0 {
		return "", 0, false
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		newPrefix := head + matches[0] + " "
		newLine := newPrefix + suffix
		return newLine, len(newPrefix), true
	}
	common := longestCommonPrefix(matches)
	if common == "" || common == partial {
		// Nothing more to extend; signal "no change" so the editor doesn't
		// blank the line. Returning ok=false lets term.Terminal beep instead.
		return "", 0, false
	}
	newPrefix := head + common
	newLine := newPrefix + suffix
	return newLine, len(newPrefix), true
}

func matchPrefix(candidates []string, prefix string) []string {
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		max := len(prefix)
		if len(s) < max {
			max = len(s)
		}
		i := 0
		for i < max && prefix[i] == s[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			break
		}
	}
	return prefix
}

// appendHistoryIfMeaningful appends the line to the history file unless it is
// blank or a comment. Trims the file to the most recent replMaxHistory lines
// after each append so it doesn't grow unbounded.
func appendHistoryIfMeaningful(path, line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
	trimHistoryFile(path)
}

// trimHistoryFile keeps the file at most replMaxHistory lines by rewriting
// it with the tail. Best-effort: any I/O error is silently ignored — losing
// a few lines of REPL history is not worth bubbling up to the prompt.
func trimHistoryFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= replMaxHistory {
		return
	}
	tail := lines[len(lines)-replMaxHistory:]
	_ = os.WriteFile(path, []byte(strings.Join(tail, "\n")+"\n"), 0o600)
}
