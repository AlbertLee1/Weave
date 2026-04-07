package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/liyang/weave/internal/cliclient"
)

func runOntology(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave ontology <list|get> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return ontologyList(args[1:], stdout, stderr)
	case "get":
		return ontologyGet(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave ontology: unknown subcommand %q\n", args[0])
		return 2
	}
}

func newCLIClient(stderr io.Writer) (*cliclient.Client, int) {
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return nil, 1
	}
	if cfg.BaseURL == "" {
		fmt.Fprintln(stderr, "weave: no base_url configured. Run `weave config set base_url <url>` first.")
		return nil, 1
	}
	return cliclient.NewClient(cfg.BaseURL, cfg.Token()), 0
}

func ontologyList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ontology list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit raw JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	list, err := c.ListOntologies(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "list ontologies: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(list)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "API NAME\tDISPLAY NAME\tVERSION\tRID")
	for _, o := range list {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", o.APIName, o.DisplayName, o.CurrentVersion, o.RID)
	}
	tw.Flush()
	return 0
}

func ontologyGet(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ontology get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fmt.Fprintln(stderr, "usage: weave ontology get <apiName>")
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	o, err := c.GetOntology(context.Background(), rest[0])
	if err != nil {
		fmt.Fprintf(stderr, "get ontology: %v\n", err)
		return 1
	}
	buf, _ := json.MarshalIndent(o, "", "  ")
	fmt.Fprintln(stdout, string(buf))
	return 0
}
