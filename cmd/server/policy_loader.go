package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/security"
)

// loadPoliciesFromDB reads all rows from the security_policies table and
// loads them into the in-memory security.Engine. This is called once at
// startup so that policies seeded via the e2e fixture (or any future admin
// CRUD) are active from the first request onward.
//
// The function is best-effort: a decode error for a single row's rules
// JSONB logs a warning and skips that row rather than blocking startup.
func loadPoliciesFromDB(ctx context.Context, pool *pgxpool.Pool, engine *security.Engine) error {
	if pool == nil || engine == nil {
		return nil
	}

	rows, err := pool.Query(ctx,
		`SELECT rid, object_type_rid, policy_type, rules FROM security_policies`)
	if err != nil {
		return fmt.Errorf("load policies: query: %w", err)
	}
	defer rows.Close()

	// Group policies by ObjectType RID so we can call SetPolicies once
	// per ObjectType (the Engine replaces the full list on each call).
	byOT := make(map[string][]security.Policy)
	var loaded, skipped int

	for rows.Next() {
		var (
			rid       string
			otRID     string
			pType     string
			rulesJSON json.RawMessage
		)
		if err := rows.Scan(&rid, &otRID, &pType, &rulesJSON); err != nil {
			return fmt.Errorf("load policies: scan: %w", err)
		}

		var rules []security.Rule
		if err := json.Unmarshal(rulesJSON, &rules); err != nil {
			log.Printf("[POLICY] warning: skipping policy %s — invalid rules JSON: %v", rid, err)
			skipped++
			continue
		}

		byOT[otRID] = append(byOT[otRID], security.Policy{
			RID:           rid,
			ObjectTypeRID: otRID,
			PolicyType:    security.PolicyType(pType),
			Rules:         rules,
		})
		loaded++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load policies: rows iteration: %w", err)
	}

	for otRID, policies := range byOT {
		engine.SetPolicies(otRID, policies)
	}

	if loaded > 0 || skipped > 0 {
		log.Printf("[POLICY] loaded %d policies (%d skipped) for %d object types from DB", loaded, skipped, len(byOT))
	}
	return nil
}
