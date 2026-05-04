// Top-level `weave index` command surface (US-408). Distinct from
// `weave admin index rebuild` (which is a thin admin POST without progress
// reporting), this command renders an estimated-row count + spinner /
// progress line so operators can see the rebuild make forward progress on
// large ObjectTypes. The HTTP rebuild call itself is synchronous on the
// server side, so the CLI's progress UI is a two-stage render: pre-call
// estimate, then post-call indexed count.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/liyang/weave/internal/cliclient"
)

// runIndex dispatches the `weave index <subcommand>` family. Today the
// only verb is `rebuild`; future stories may layer status / list / drop on
// top of the same group without touching main.go.
func runIndex(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave index <rebuild> [flags]")
		return 2
	}
	switch args[0] {
	case "rebuild":
		return runIndexRebuild(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave index: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runIndexRebuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("index rebuild", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ontology := fs.String("ontology", "", "ontology apiName (required)")
	objectType := fs.String("object-type", "", "objectType apiName (required)")
	asJSON := fs.Bool("json", false, "emit final result as JSON instead of a human summary")
	noEstimate := fs.Bool("no-estimate", false, "skip the pre-rebuild row-count estimate query")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ontology == "" || *objectType == "" {
		fmt.Fprintln(stderr, "usage: weave index rebuild --ontology X --object-type Y")
		return 2
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}

	return executeIndexRebuild(c, *ontology, *objectType, *asJSON, *noEstimate, stdout, stderr)
}

// executeIndexRebuild is the testable seam: it accepts the cliclient
// directly so unit tests can stub the HTTP boundary with the existing
// test server fixtures rather than wiring config + token plumbing.
func executeIndexRebuild(c *cliclient.Client, ontology, objectType string, asJSON, noEstimate bool, stdout, stderr io.Writer) int {
	ctx := context.Background()

	// Pre-rebuild estimate. The count endpoint queries the (possibly
	// stale) hot index, but the order of magnitude is what an operator
	// needs to decide whether to wait. We never gate the rebuild on
	// estimate failure — if the server is in a degraded state where
	// /count returns 5xx, the rebuild itself may still be the fix.
	var estimate int
	estimateOK := false
	if !asJSON && !noEstimate {
		if n, err := c.CountObjects(ctx, ontology, objectType); err == nil {
			estimate = n
			estimateOK = true
			fmt.Fprintf(stdout, "Rebuilding index ontology=%s objectType=%s (estimated %d rows)...\n",
				ontology, objectType, estimate)
		} else {
			fmt.Fprintf(stdout, "Rebuilding index ontology=%s objectType=%s (estimate unavailable: %v)...\n",
				ontology, objectType, err)
		}
	}

	start := time.Now()
	resp, err := c.RebuildIndex(ctx, ontology, objectType)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(stderr, "rebuild: %v\n", err)
		return 1
	}

	if asJSON {
		buf, _ := json.Marshal(resp)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}

	printRebuildSummary(stdout, resp, elapsed, estimate, estimateOK)
	return 0
}

func printRebuildSummary(w io.Writer, resp *cliclient.RebuildIndexResponse, elapsed time.Duration, estimate int, estimateOK bool) {
	fmt.Fprintf(w, "Rebuilt index %s in %s\n", resp.ScopedKey, formatDuration(elapsed))
	fmt.Fprintf(w, "  Indexed rows: %d\n", resp.IndexedCount)
	if estimateOK {
		delta := resp.IndexedCount - estimate
		switch {
		case delta == 0:
			fmt.Fprintln(w, "  Matches pre-rebuild estimate.")
		case delta > 0:
			fmt.Fprintf(w, "  Pre-rebuild estimate: %d (+%d new)\n", estimate, delta)
		default:
			fmt.Fprintf(w, "  Pre-rebuild estimate: %d (%d removed)\n", estimate, -delta)
		}
	}
}

// formatDuration is a tiny helper that strips the trailing zero
// nanoseconds Time.String emits on whole-second durations and caps
// resolution at milliseconds for human-readable output.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return d.String()
	}
	return d.Round(time.Millisecond).String()
}
