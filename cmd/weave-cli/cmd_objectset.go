package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runObjectSet dispatches `weave objectset <load|create-temporary>` (OSV2-304).
// Both subcommands forward a JSON body verbatim to the matching server route.
func runObjectSet(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave objectset <load|create-temporary> [flags]")
		return 2
	}
	switch args[0] {
	case "load":
		return objectSetLoad(args[1:], stdout, stderr)
	case "create-temporary", "createTemporary":
		return objectSetCreateTemporary(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave objectset: unknown subcommand %q\n", args[0])
		return 2
	}
}

func objectSetLoad(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("objectset load", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology api name (required)")
	bodyRef := fs.String("body", "", "JSON request body: literal or '@/path/to/file.json' (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, code := loadObjectSetBody(stderr, *ontology, *bodyRef)
	if code != 0 {
		return code
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	resp, err := c.LoadObjectSet(context.Background(), *ontology, raw)
	if err != nil {
		printAPIError(stderr, "objectset load", err)
		return 1
	}
	fmt.Fprintln(stdout, string(jsonIndentOrRaw(resp)))
	return 0
}

func objectSetCreateTemporary(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("objectset create-temporary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology api name (required)")
	bodyRef := fs.String("body", "", "JSON ObjectSet definition body: literal or '@/path/to/file.json' (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, code := loadObjectSetBody(stderr, *ontology, *bodyRef)
	if code != 0 {
		return code
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	resp, err := c.CreateTemporaryObjectSetRaw(context.Background(), *ontology, raw)
	if err != nil {
		printAPIError(stderr, "objectset create-temporary", err)
		return 1
	}
	fmt.Fprintln(stdout, string(jsonIndentOrRaw(resp)))
	return 0
}

// loadObjectSetBody validates --ontology and --body and returns the raw JSON
// bytes ready for forwarding. Exit code 2 = usage error.
func loadObjectSetBody(stderr io.Writer, ontology, bodyRef string) (json.RawMessage, int) {
	if strings.TrimSpace(ontology) == "" {
		fmt.Fprintln(stderr, "weave objectset: --ontology is required")
		return nil, 2
	}
	if strings.TrimSpace(bodyRef) == "" {
		fmt.Fprintln(stderr, "weave objectset: --body is required (literal JSON or @file)")
		return nil, 2
	}
	raw, err := readJSONBlobRef(bodyRef)
	if err != nil {
		fmt.Fprintf(stderr, "weave objectset: --body: %v\n", err)
		return nil, 2
	}
	if !json.Valid(raw) {
		fmt.Fprintln(stderr, "weave objectset: --body is not valid JSON")
		return nil, 2
	}
	return raw, 0
}
