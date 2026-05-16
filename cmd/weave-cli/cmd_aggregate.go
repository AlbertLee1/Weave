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
)

// runAggregate dispatches `weave aggregate` (OSV2-304). The wire shape is
// "POST /api/v2/ontologies/{o}/objects/{ot}/aggregate" with a JSON body
// authored by the caller — the CLI forwards it verbatim and pretty-prints
// the response either as JSON (default) or as a flat key/value table.
func runAggregate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("aggregate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology api name (required)")
	objType := fs.String("type", "", "object type api name (required)")
	bodyRef := fs.String("body", "", "JSON aggregation body: literal '{...}' or '@/path/to/file.json' (required)")
	output := fs.String("output", "json", "output format: json | table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ontology) == "" {
		fmt.Fprintln(stderr, "weave aggregate: --ontology is required")
		return 2
	}
	if strings.TrimSpace(*objType) == "" {
		fmt.Fprintln(stderr, "weave aggregate: --type is required")
		return 2
	}
	if strings.TrimSpace(*bodyRef) == "" {
		fmt.Fprintln(stderr, "weave aggregate: --body is required (literal JSON or @file)")
		return 2
	}
	raw, err := readJSONBlobRef(*bodyRef)
	if err != nil {
		fmt.Fprintf(stderr, "weave aggregate: --body: %v\n", err)
		return 2
	}
	if !json.Valid(raw) {
		fmt.Fprintln(stderr, "weave aggregate: --body is not valid JSON")
		return 2
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	resp, err := c.AggregateObjects(context.Background(), *ontology, *objType, raw)
	if err != nil {
		printAPIError(stderr, "aggregate", err)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(*output)) {
	case "table":
		printAggregateTable(stdout, resp)
	default:
		fmt.Fprintln(stdout, string(jsonIndentOrRaw(resp)))
	}
	return 0
}

// jsonIndentOrRaw pretty-prints a JSON blob; on parse failure it returns the
// original bytes so the user still sees the response body.
func jsonIndentOrRaw(in json.RawMessage) []byte {
	dec := json.NewDecoder(strings.NewReader(string(in)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return []byte(in)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte(in)
	}
	return out
}

// printAggregateTable best-effort flattens the {data: {<metric>: [<buckets>]}}
// envelope into a key/value table. Two layouts are recognised:
//
//	{"data":{"<metric>":[{"key":"...","value":<n>}, ...]}}
//	{"data":{"<metric>":[{"value":<n>}]}}
//
// Anything else falls back to a JSON dump of the response with a header line
// noting the format mismatch, so callers still see *something*.
func printAggregateTable(w io.Writer, raw json.RawMessage) {
	var envelope struct {
		Data map[string][]struct {
			Key   any `json:"key"`
			Value any `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Data == nil {
		fmt.Fprintln(w, "# aggregate result (table output unsupported for this shape)")
		fmt.Fprintln(w, string(raw))
		return
	}

	metrics := make([]string, 0, len(envelope.Data))
	for m := range envelope.Data {
		metrics = append(metrics, m)
	}
	sort.Strings(metrics)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "METRIC\tKEY\tVALUE")
	for _, m := range metrics {
		for _, b := range envelope.Data[m] {
			fmt.Fprintf(tw, "%s\t%v\t%v\n", m, b.Key, b.Value)
		}
	}
	tw.Flush()
}
