package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/liyang/weave/internal/cliclient"
	"github.com/liyang/weave/pkg/oms"
)

// runPitr dispatches the `weave pitr <subcommand>` family. PITR (Point-In-
// Time Recovery) wraps the US-388 dataset rollback API: the user picks a
// historical transaction id, the server marks every newer tx as
// rolled-back, replays the per-PK snapshot at the target into the live
// Bleve indexes, and stamps a fresh bookkeeping tx as the chain head.
func runPitr(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: weave pitr <restore|history> [flags]")
		return 2
	}
	switch args[0] {
	case "restore":
		return runPitrRestore(args[1:], stdout, stderr)
	case "history":
		return runPitrHistory(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "weave pitr: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runPitrRestore(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pitr restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataset := fs.String("dataset", "", "dataset rid or ontology api name (required)")
	toTx := fs.String("to-tx", "", "rollback target transaction id, e.g. tx-... (required)")
	asJSON := fs.Bool("json", false, "emit raw JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dataset == "" {
		fmt.Fprintln(stderr, "weave: --dataset is required")
		return 2
	}
	if *toTx == "" {
		fmt.Fprintln(stderr, "weave: --to-tx is required")
		return 2
	}
	if !strings.HasPrefix(*toTx, oms.DatasetTransactionIDPrefix) {
		fmt.Fprintf(stderr, "weave: --to-tx must start with %q (got %q)\n",
			oms.DatasetTransactionIDPrefix, *toTx)
		return 2
	}

	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	resp, err := c.RollbackDataset(context.Background(), *dataset, *toTx)
	if err != nil {
		fmt.Fprintf(stderr, "pitr restore: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(resp)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	printRollbackSummary(stdout, *toTx, resp)
	return 0
}

func runPitrHistory(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pitr history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataset := fs.String("dataset", "", "dataset rid or ontology api name (required)")
	asJSON := fs.Bool("json", false, "emit raw JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dataset == "" {
		fmt.Fprintln(stderr, "weave: --dataset is required")
		return 2
	}
	c, code := newCLIClient(stderr)
	if c == nil {
		return code
	}
	hist, err := c.DatasetHistory(context.Background(), *dataset)
	if err != nil {
		fmt.Fprintf(stderr, "pitr history: %v\n", err)
		return 1
	}
	if *asJSON {
		buf, _ := json.Marshal(hist)
		fmt.Fprintln(stdout, string(buf))
		return 0
	}
	if len(hist.Transactions) == 0 {
		fmt.Fprintln(stdout, "(no transactions)")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TX ID\tPARENT\tCOMMITTED AT\tEDITS\tROLLED BACK")
	for _, tx := range hist.Transactions {
		rolled := ""
		if !tx.RolledBackAt.IsZero() {
			rolled = "→ " + tx.RolledBackToTxID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			tx.TxID, tx.ParentTxID, tx.CommittedAt.Format("2006-01-02T15:04:05Z07:00"),
			tx.EditsCount, rolled)
	}
	tw.Flush()
	return 0
}

func printRollbackSummary(w io.Writer, target string, resp *cliclient.PITRRollbackResponse) {
	fmt.Fprintf(w, "Rollback to %s: %d objects restored, %d objects deleted\n",
		target, resp.RestoredObjects, resp.DeletedObjects)
	if len(resp.RolledBackTxIDs) == 0 {
		fmt.Fprintln(w, "(no newer transactions to roll back)")
	} else {
		fmt.Fprintf(w, "Rolled back transactions: %s\n", strings.Join(resp.RolledBackTxIDs, ", "))
	}
	if resp.NewTransaction != nil {
		fmt.Fprintf(w, "New chain head: %s (parent %s)\n",
			resp.NewTransaction.TxID, resp.NewTransaction.ParentTxID)
	}
}
