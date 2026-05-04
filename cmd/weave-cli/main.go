// Command weave is a small CLI for talking to a Weave ontology server.
//
// It uses the stdlib flag package — no Cobra — and reuses the
// internal/cliclient HTTP wrapper. Configuration is persisted to
// $WEAVE_CONFIG_DIR/config.toml (defaults to ~/.config/weave/config.toml).
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	exit := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	os.Exit(exit)
}

// run is the testable entry point. It returns a process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printRootUsage(stdout)
		return 2
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		printRootUsage(stdout)
		return 0
	case "ontology":
		return runOntology(rest, stdout, stderr)
	case "object":
		return runObject(rest, stdout, stderr)
	case "auth":
		return runAuth(rest, stdout, stderr)
	case "config":
		return runConfig(rest, stdout, stderr)
	case "admin":
		return runAdmin(rest, stdout, stderr)
	case "fixtures":
		return runFixtures(rest, stdout, stderr)
	case "pitr":
		return runPitr(rest, stdout, stderr)
	case "materialize":
		return runMaterialize(rest, stdout, stderr)
	case "repl":
		return runREPL(rest, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave: unknown command %q\n\n", cmd)
		printRootUsage(stderr)
		return 2
	}
}

func printRootUsage(w io.Writer) {
	fmt.Fprintln(w, `weave - command line client for the Weave ontology engine

Usage:
  weave <command> [subcommand] [flags]

Commands:
  ontology   Manage and inspect ontologies (list, get)
  object     Retrieve objects (list, get, search)
  auth       Authenticate against the server (login, logout, status)
  config     Read and write the local config file (~/.config/weave/config.toml)
  admin      Server administration (index rebuild, ...)
  fixtures   Generate synthetic test data for an ObjectType (generate)
  pitr         Point-in-time recovery (restore, history)
  materialize  Rebuild snapshots from materialized parquet (rebuild)
  repl         Interactive shell with tab-completion and history

Run "weave <command> --help" for command-specific help.`)
}
