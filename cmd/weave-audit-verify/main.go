// Command weave-audit-verify walks the audit_events hash chain and
// verifies tamper-proofness end-to-end.
//
// It performs two checks:
//
//  1. Chain integrity. Loads every row ORDERED BY chain_seq ASC and
//     confirms (a) chain_seq is contiguous, (b) each row's prev_hash
//     matches the previous row's entry_hash, and (c) each row's
//     entry_hash re-computes correctly from the canonical envelope.
//  2. Root-file alignment (optional, -root-file). For every anchored
//     `YYYY-MM-DD\t<hex>` line in the append-only root-hash file,
//     recompute the root over the DB rows for that UTC day and compare.
//     A mismatch is tamper evidence.
//
// Exit codes:
//   - 0: both checks passed.
//   - 1: chain or root-file verification failed (tamper evidence).
//   - 2: flag / connect / IO error (operator problem, not tamper).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/pkg/audit"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point. Exit code semantics documented in the
// file comment above.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("weave-audit-verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dsn := fs.String("dsn", os.Getenv("PG_DSN"), "Postgres DSN (defaults to $PG_DSN)")
	rootFile := fs.String("root-file", "", "Optional append-only root-hash file to cross-check")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *dsn == "" {
		fmt.Fprintln(stderr, "weave-audit-verify: -dsn is required (or set PG_DSN)")
		return 2
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, *dsn)
	if err != nil {
		fmt.Fprintf(stderr, "weave-audit-verify: connect to PG: %v\n", err)
		return 2
	}
	defer pool.Close()

	store := audit.NewPGStore(pool)
	chain, err := store.ListChain(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "weave-audit-verify: list chain: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "loaded %d audit events from %s\n", len(chain), *dsn)

	if err := audit.VerifyChain(chain); err != nil {
		fmt.Fprintf(stdout, "FAIL: chain verification failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "PASS: chain integrity verified (%d events)\n", len(chain))

	if *rootFile != "" {
		f, err := os.Open(*rootFile)
		if err != nil {
			fmt.Fprintf(stderr, "weave-audit-verify: open root file: %v\n", err)
			return 2
		}
		defer f.Close()
		entries, err := audit.ParseRootFile(f)
		if err != nil {
			fmt.Fprintf(stderr, "weave-audit-verify: parse root file: %v\n", err)
			return 2
		}
		grouped := audit.GroupEventsByUTCDay(chain)
		if err := audit.VerifyRootFile(entries, grouped); err != nil {
			fmt.Fprintf(stdout, "FAIL: root-file verification failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "PASS: root-file cross-check verified (%d anchors)\n", len(entries))
	}

	return 0
}
