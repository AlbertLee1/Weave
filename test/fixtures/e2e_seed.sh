#!/usr/bin/env bash
# test/fixtures/e2e_seed.sh — Playwright baseline seeder (US-030).
#
# Wipes any prior Northwind ontology + test users from Postgres and
# recreates them via test/fixtures/seed_northwind. After the PG writes
# commit, each object type's index is rebuilt against the live Weave
# server at $WEAVE_URL so Bleve reflects the seeded rows.
#
# Environment variables:
#   PG_DSN     Postgres DSN (default: weave dev compose stack)
#   WEAVE_URL  Weave API base URL (default: http://localhost:9117)
#
# Exit codes propagate from the Go binary — any non-zero means the
# Playwright stack is NOT ready and callers (e.g. scripts/e2e-setup.sh)
# should abort the bring-up.
#
# Safe to run repeatedly: the seeder library is wipe-and-reseed so a
# rerun converges on the same final state.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

PG_DSN="${PG_DSN:-postgres://weave:weave@localhost:5432/weave?sslmode=disable}"
WEAVE_URL="${WEAVE_URL:-http://localhost:9117}"

log() { printf '[e2e-seed] %s\n' "$*"; }

log "pg_dsn=$PG_DSN weave_url=$WEAVE_URL"
log "building test/fixtures/seed_northwind/cmd"
go build -o bin/seed-northwind ./test/fixtures/seed_northwind/cmd

log "running seeder"
PG_DSN="$PG_DSN" WEAVE_URL="$WEAVE_URL" ./bin/seed-northwind "$@"

log "seed complete"
