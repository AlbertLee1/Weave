package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// runFn dispatches the `weave fn <subcommand>` family. US-415 introduces
// `pull` (fetch the live source code into a local file) and `push` (commit
// a local file's contents back to the per-Function bare git repo).
func runFn(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave fn <pull|push|log> [flags]")
		return 2
	}
	switch args[0] {
	case "pull":
		return runFnPull(args[1:], stdout, stderr)
	case "push":
		return runFnPush(args[1:], stdout, stderr)
	case "log":
		return runFnLog(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave fn: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runFnPull(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fn pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	ref := fs.String("ref", "", "function rid, name, or name@version (required)")
	output := fs.String("o", "", "output file path (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ontology) == "" || strings.TrimSpace(*ref) == "" {
		fmt.Fprintln(stderr, "weave fn pull: --ontology and --ref are required")
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	fn, err := c.GetFunction(context.Background(), *ontology, *ref)
	if err != nil {
		fmt.Fprintf(stderr, "fn pull: %v\n", err)
		return 1
	}
	if *output == "" {
		fmt.Fprint(stdout, fn.SourceCode)
		return 0
	}
	if err := os.WriteFile(*output, []byte(fn.SourceCode), 0o644); err != nil {
		fmt.Fprintf(stderr, "fn pull: write %s: %v\n", *output, err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s (%d bytes)\n", *output, len(fn.SourceCode))
	return 0
}

func runFnPush(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fn push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	ref := fs.String("ref", "", "function rid, name, or name@version (required)")
	source := fs.String("f", "", "path to a file containing the new source code (required)")
	message := fs.String("m", "", "commit message (required)")
	author := fs.String("author", "", "commit author name (default: server identity)")
	email := fs.String("email", "", "commit author email (default: server identity)")
	asJSON := fs.Bool("json", false, "emit raw JSON commit response")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ontology) == "" || strings.TrimSpace(*ref) == "" {
		fmt.Fprintln(stderr, "weave fn push: --ontology and --ref are required")
		return 2
	}
	if strings.TrimSpace(*source) == "" {
		fmt.Fprintln(stderr, "weave fn push: -f (source file path) is required")
		return 2
	}
	if strings.TrimSpace(*message) == "" {
		fmt.Fprintln(stderr, "weave fn push: -m (commit message) is required")
		return 2
	}
	bytes, err := os.ReadFile(*source)
	if err != nil {
		fmt.Fprintf(stderr, "fn push: read %s: %v\n", *source, err)
		return 1
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	body := map[string]any{
		"message":    *message,
		"sourceCode": string(bytes),
	}
	if *author != "" {
		body["author"] = *author
	}
	if *email != "" {
		body["email"] = *email
	}
	commit, err := c.CreateFunctionRepoCommit(context.Background(), *ontology, *ref, body)
	if err != nil {
		fmt.Fprintf(stderr, "fn push: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(commit)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	fmt.Fprintf(stdout, "committed %s (%s) by %s <%s> at %s\n",
		commit.Hash, commit.Message, commit.Author, commit.Email,
		commit.AuthorDate.Format(time.RFC3339))
	return 0
}

func runFnLog(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fn log", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	ref := fs.String("ref", "", "function rid, name, or name@version (required)")
	limit := fs.Int("limit", 0, "max commits to show (0 = server default)")
	asJSON := fs.Bool("json", false, "emit raw JSON commit list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ontology) == "" || strings.TrimSpace(*ref) == "" {
		fmt.Fprintln(stderr, "weave fn log: --ontology and --ref are required")
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	commits, err := c.ListFunctionRepoCommits(context.Background(), *ontology, *ref, *limit)
	if err != nil {
		fmt.Fprintf(stderr, "fn log: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(commits)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HASH\tWHEN\tAUTHOR\tMESSAGE")
	for _, c := range commits {
		hash := c.Hash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			hash, c.AuthorDate.Format(time.RFC3339), c.Author,
			strings.TrimSpace(c.Message))
	}
	tw.Flush()
	return 0
}
