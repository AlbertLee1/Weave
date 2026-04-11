// Command seed-northwind is the thin CLI wrapper that test/fixtures/
// e2e_seed.sh executes. It opens a Postgres connection, delegates to
// seed_northwind.Seed() to wipe-and-reseed the ontology, and then walks
// the returned object type list calling POST /api/admin/indexes/rebuild
// on the live server so Bleve picks up the freshly-written
// object_history rows.
//
// Connection parameters come from environment variables so the script
// can be called unchanged from e2e-setup.sh, CI, or a human shell:
//
//	PG_DSN     postgres DSN (defaults to the weave dev compose stack)
//	WEAVE_URL  Weave API base URL (defaults to http://localhost:9117)
//
// A zero-exit status means the stack is ready for Playwright. Any error
// (PG unreachable, rebuild HTTP 5xx, schema mismatch) propagates via
// non-zero exit so e2e-setup.sh can surface it.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	seed "github.com/liyang/weave/test/fixtures/seed_northwind"
)

func main() {
	pgDSN := flag.String("pg-dsn", getenvDefault("PG_DSN", "postgres://weave:weave@localhost:5432/weave?sslmode=disable"), "Postgres DSN")
	weaveURL := flag.String("weave-url", getenvDefault("WEAVE_URL", "http://localhost:9117"), "Weave API base URL")
	skipRebuild := flag.Bool("skip-rebuild", false, "skip the POST /api/admin/indexes/rebuild step (PG-only seed)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-HTTP-request timeout for index rebuild calls")
	flag.Parse()

	logger := log.New(os.Stderr, "[seed] ", log.LstdFlags)
	logger.Printf("connecting to %s", *pgDSN)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, *pgDSN)
	if err != nil {
		logger.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	opts := seed.DefaultOptions()
	opts.Logger = logger

	start := time.Now()
	res, err := seed.Seed(ctx, pool, opts)
	if err != nil {
		logger.Fatalf("seed failed: %v", err)
	}
	logger.Printf("seeded ontology %q with object types %v (%s)", res.OntologyAPIName, res.ObjectTypes, time.Since(start).Round(time.Millisecond))

	if *skipRebuild {
		logger.Printf("skipping rebuild as requested")
		return
	}

	client := &http.Client{Timeout: *timeout}
	for _, ot := range res.ObjectTypes {
		rebuildStart := time.Now()
		count, err := rebuildIndex(ctx, client, *weaveURL, res.OntologyAPIName, ot)
		if err != nil {
			logger.Fatalf("rebuild %s.%s: %v", res.OntologyAPIName, ot, err)
		}
		logger.Printf("rebuilt %s.%s (%d docs, %s)", res.OntologyAPIName, ot, count, time.Since(rebuildStart).Round(time.Millisecond))
	}
	logger.Printf("seed complete in %s", time.Since(start).Round(time.Millisecond))
}

// rebuildIndex posts a single rebuild request to the Weave admin
// endpoint. In dev auth mode (AUTH_MODE=dev, the default) the request
// goes through unauthenticated because the middleware injects an admin
// context automatically. In jwt mode the caller must also export a
// WEAVE_ADMIN_TOKEN so the Authorization header has a valid bearer.
func rebuildIndex(ctx context.Context, client *http.Client, baseURL, ontology, objectType string) (int, error) {
	body, err := json.Marshal(map[string]string{
		"ontology":   ontology,
		"objectType": objectType,
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/admin/indexes/rebuild", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := os.Getenv("WEAVE_ADMIN_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(string(raw), 512))
	}
	var decoded struct {
		IndexedCount int `json:"indexedCount"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	return decoded.IndexedCount, nil
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
