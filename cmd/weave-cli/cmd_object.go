package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/liyang/weave/internal/cliclient"
)

func runObject(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave object <list|get|search> [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return objectList(args[1:], stdout, stderr)
	case "get":
		return objectGet(args[1:], stdout, stderr)
	case "search":
		return objectSearch(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave object: unknown subcommand %q\n", args[0])
		return 2
	}
}

type commonObjectFlags struct {
	ontology string
	objType  string
}

func registerCommon(fs *flag.FlagSet) *commonObjectFlags {
	c := &commonObjectFlags{}
	fs.StringVar(&c.ontology, "ontology", "", "ontology api name (required)")
	fs.StringVar(&c.objType, "type", "", "object type api name (required)")
	return c
}

func validateCommon(c *commonObjectFlags, stderr io.Writer) int {
	if c.ontology == "" {
		fmt.Fprintln(stderr, "weave: --ontology is required")
		return 2
	}
	if c.objType == "" {
		fmt.Fprintln(stderr, "weave: --type is required")
		return 2
	}
	return 0
}

func objectList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("object list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	common := registerCommon(fs)
	limit := fs.Int("limit", 50, "page size")
	pageToken := fs.String("page-token", "", "next page token from a previous call")
	orderBy := fs.String("order-by", "", "field to order by")
	asJSON := fs.Bool("json", false, "emit raw JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if code := validateCommon(common, stderr); code != 0 {
		return code
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	page, err := c.ListObjects(context.Background(), common.ontology, common.objType, cliclient.ListObjectsOptions{
		PageSize:  *limit,
		PageToken: *pageToken,
		OrderBy:   *orderBy,
	})
	if err != nil {
		fmt.Fprintf(stderr, "list objects: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(page)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	printWireObjects(stdout, page.Data)
	if page.NextPageToken != "" {
		fmt.Fprintf(stdout, "\nNext page token: %s\n", page.NextPageToken)
	}
	return 0
}

func objectGet(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("object get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	common := registerCommon(fs)
	pk := fs.String("pk", "", "primary key of the object (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if code := validateCommon(common, stderr); code != 0 {
		return code
	}
	if *pk == "" {
		fmt.Fprintln(stderr, "weave: --pk is required")
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	obj, err := c.GetObject(context.Background(), common.ontology, common.objType, *pk)
	if err != nil {
		fmt.Fprintf(stderr, "get object: %v\n", err)
		return 1
	}
	buf, _ := json.MarshalIndent(obj, "", "  ")
	fmt.Fprintln(stdout, string(buf))
	return 0
}

func objectSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("object search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	common := registerCommon(fs)
	whereJSON := fs.String("where", "", "JSON-encoded where clause (required)")
	selectCSV := fs.String("select", "", "Comma-separated property apiNames to return")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if code := validateCommon(common, stderr); code != 0 {
		return code
	}
	if *whereJSON == "" {
		fmt.Fprintln(stderr, "weave: --where is required (JSON)")
		return 2
	}
	var where map[string]any
	if err := json.Unmarshal([]byte(*whereJSON), &where); err != nil {
		fmt.Fprintf(stderr, "weave: --where is not valid JSON: %v\n", err)
		return 2
	}
	var selectProps []string
	if *selectCSV != "" {
		selectProps = strings.Split(*selectCSV, ",")
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	page, err := c.SearchObjects(context.Background(), common.ontology, common.objType, where, selectProps)
	if err != nil {
		fmt.Fprintf(stderr, "search objects: %v\n", err)
		return 1
	}
	printWireObjects(stdout, page.Data)
	return 0
}

// printWireObjects renders a slice of WireObject as a table. Columns are
// derived from the union of keys (excluding double-underscore meta keys), in
// stable lexical order. Long string values are truncated.
func printWireObjects(w io.Writer, rows []cliclient.WireObject) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no rows)")
		return
	}
	// Collect column set.
	cols := map[string]struct{}{}
	for _, r := range rows {
		for k := range r {
			if strings.HasPrefix(k, "__") {
				continue
			}
			cols[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(cols))
	for k := range cols {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "PK"
	for _, k := range keys {
		header += "\t" + strings.ToUpper(k)
	}
	fmt.Fprintln(tw, header)
	for _, r := range rows {
		pk := fmt.Sprintf("%v", r["__primaryKey"])
		line := pk
		for _, k := range keys {
			line += "\t" + truncate(fmt.Sprintf("%v", r[k]), 40)
		}
		fmt.Fprintln(tw, line)
	}
	tw.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
