package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/liyang/weave/pkg/materialize"
)

// runMaterialize dispatches the `weave materialize <subcommand>` family.
// US-406 introduces `rebuild`, which replays parquet files written by the
// funnel materializer (US-405) into a deduped per-objectType snapshot.
// Future stories layer extra subcommands (compact, retention, etc.) on
// top of the same group without touching main.go.
func runMaterialize(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave materialize <rebuild> [flags]")
		return 2
	}
	switch args[0] {
	case "rebuild":
		return runMaterializeRebuild(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave materialize: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runMaterializeRebuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("materialize rebuild", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", defaultMaterializeDir(), "root directory containing materialized parquet files (defaults to $WEAVE_DATA_DIR/materialized or ./data/materialized)")
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	objectType := fs.String("object-type", "", "objectType apiName (required)")
	asOfRaw := fs.String("as-of", "", "RFC3339 cutoff timestamp (default: latest)")
	asJSON := fs.Bool("json", false, "emit raw JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ontology == "" {
		fmt.Fprintln(stderr, "weave: --ontology is required")
		return 2
	}
	if *objectType == "" {
		fmt.Fprintln(stderr, "weave: --object-type is required")
		return 2
	}

	asOf := time.Time{}
	if *asOfRaw != "" {
		t, err := time.Parse(time.RFC3339, *asOfRaw)
		if err != nil {
			fmt.Fprintf(stderr, "weave: --as-of must be RFC3339 (got %q): %v\n", *asOfRaw, err)
			return 2
		}
		asOf = t
	}

	m := materialize.NewMaterializer(*dataDir)
	rows, err := m.BuildSnapshot(context.Background(), *ontology, *objectType, asOf)
	if err != nil {
		fmt.Fprintf(stderr, "materialize rebuild: %v\n", err)
		return 1
	}

	if *asJSON {
		buf, _ := json.Marshal(map[string]interface{}{
			"ontology":   *ontology,
			"objectType": *objectType,
			"rows":       rows,
		})
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	printSnapshotSummary(stdout, *ontology, *objectType, asOf, rows)
	return 0
}

// defaultMaterializeDir mirrors cmd/server's WEAVE_DATA_DIR convention so
// the CLI works without flags when both processes share the same data
// directory layout.
func defaultMaterializeDir() string {
	root := os.Getenv("WEAVE_DATA_DIR")
	if root == "" {
		root = "data"
	}
	return filepath.Join(root, "materialized")
}

func printSnapshotSummary(w io.Writer, ontology, objectType string, asOf time.Time, rows []materialize.SnapshotRow) {
	cutoff := "(latest)"
	if !asOf.IsZero() {
		cutoff = asOf.UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(w, "Snapshot: ontology=%s objectType=%s asOf=%s rows=%d\n",
		ontology, objectType, cutoff, len(rows))
	if len(rows) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PRIMARY KEY\tPATCH OFFSET\tTIMESTAMP\tBATCH ID\tPROPERTIES")
	for _, r := range rows {
		ts := time.UnixMilli(r.TimestampMs).UTC().Format(time.RFC3339)
		props, _ := json.Marshal(r.Properties)
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n",
			r.PrimaryKey, r.PatchOffset, ts, r.BatchID, string(props))
	}
	tw.Flush()
}
