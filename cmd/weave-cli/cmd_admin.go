package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
)

// runAdmin dispatches the `weave admin <subcommand>` family. The only
// subcommand today is `index rebuild`; future admin operations can plug in
// without touching main.go. The auth.MiddlewareWithAPIKeys stack on the
// server enforces the admin role — this CLI is a thin POST client.
func runAdmin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave admin <index> [args]")
		return 2
	}
	switch args[0] {
	case "index":
		return runAdminIndex(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave admin: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runAdminIndex(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave admin index <rebuild> [flags]")
		return 2
	}
	switch args[0] {
	case "rebuild":
		return runAdminIndexRebuild(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave admin index: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runAdminIndexRebuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("admin index rebuild", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	objectType := fs.String("object-type", "", "objectType apiName (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ontology == "" || *objectType == "" {
		fmt.Fprintln(stderr, "usage: weave admin index rebuild --ontology X --object-type Y")
		return 2
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}

	resp, err := c.RebuildIndex(context.Background(), *ontology, *objectType)
	if err != nil {
		fmt.Fprintf(stderr, "rebuild: %v\n", err)
		return 1
	}
	out, _ := json.Marshal(resp)
	fmt.Fprintln(stdout, string(out))
	return 0
}
