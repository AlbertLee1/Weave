package main

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/metrics"
)

// DefaultCostPollerInterval is the cadence at which the per-ontology PG row
// poller refreshes the weave_cost_pg_rows gauge. The default of one minute
// keeps the cost dashboard responsive while staying well under the budget
// of two SELECT COUNT(*) GROUP BY queries against tables that hold the
// per-batch transaction chain and per-edit history rows.
const DefaultCostPollerInterval = time.Minute

// runOntologyCostPoller refreshes weave_cost_pg_rows{ontology, table} at the
// supplied interval. It is intentionally single-purpose: the poller owns
// the gauge surface that operators want for "which ontology is the heaviest
// PG row consumer". Writing once per tick (Set, not Add) means a vanished
// ontology drops back to zero without a reset call.
//
// The poller is best-effort: any query failure is logged and the gauge is
// left at its previous value so a transient PG hiccup doesn't blank the
// dashboard. Callers wire this from main.go on the same long-lived ctx as
// other periodic loops; cancelling ctx stops the poller.
//
// Empty / zero arguments are treated as "use defaults": nil pool returns
// immediately (no PG = nothing to poll), zero interval falls back to
// DefaultCostPollerInterval.
func runOntologyCostPoller(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if pool == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultCostPollerInterval
	}

	// Initial sample so the dashboard has data before the first tick fires
	// — operators who restart the server expect the cost panels to render
	// inside a few seconds, not a minute.
	pollOntologyCostOnce(ctx, pool)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollOntologyCostOnce(ctx, pool)
		}
	}
}

// pollOntologyCostOnce executes the two per-ontology row-count queries and
// updates the gauge. The lowercase identifier is shared between the
// long-lived ticker loop and one-shot test invocations.
func pollOntologyCostOnce(ctx context.Context, pool *pgxpool.Pool) {
	if pool == nil {
		return
	}

	// dataset_transactions has ontology_api_name as a denormalised column
	// (US-379), so the per-ontology aggregate is a single GROUP BY.
	const txQuery = `
		SELECT ontology_api_name, COUNT(*)::BIGINT
		FROM dataset_transactions
		GROUP BY ontology_api_name
	`
	scanCostRows(ctx, pool, "dataset_transactions", txQuery, metrics.CostPGTableDatasetTransactions)

	// object_history needs the join chain object_history → object_types →
	// ontologies to surface the ontology api_name. The aggregate stays
	// inside PG so the wire only carries the per-ontology summary.
	const historyQuery = `
		SELECT o.api_name, COUNT(h.id)::BIGINT
		FROM object_history h
		JOIN object_types ot ON h.object_type_rid = ot.rid
		JOIN ontologies o    ON ot.ontology_rid = o.rid
		GROUP BY o.api_name
	`
	scanCostRows(ctx, pool, "object_history", historyQuery, metrics.CostPGTableObjectHistory)
}

// scanCostRows runs the supplied (ontology, count) projection and routes
// each row into the shared gauge under the given table label. Errors at
// any phase log and return without touching the gauge so a transient
// failure leaves the dashboard's prior reading in place.
func scanCostRows(ctx context.Context, pool *pgxpool.Pool, opLabel, query, tableLabel string) {
	rows, err := pool.Query(ctx, query)
	if err != nil {
		log.Printf("[COST-POLLER] %s query: %v", opLabel, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ontology string
		var count int64
		if err := rows.Scan(&ontology, &count); err != nil {
			log.Printf("[COST-POLLER] %s scan: %v", opLabel, err)
			continue
		}
		metrics.SetOntologyPGRows(ontology, tableLabel, float64(count))
	}
	if err := rows.Err(); err != nil {
		log.Printf("[COST-POLLER] %s iter: %v", opLabel, err)
	}
}
