# Weave Playwright E2E

End-to-end tests for the Weave OSv2 v2 frontend, organized per Phase so each
Integration Gate can run its own scoped suite.

## Layout

```
web/e2e/
├── helpers.ts                  Shared helpers: API fixtures + page navigation
├── search-and-filter.spec.ts   Browser page SearchBar / FilterBuilder baseline
├── phase6/                     (added US-038 onwards) Phase 6 gate specs
├── phase7/                     (added Phase 7)
├── phase8/                     (added Phase 8)
├── phase9/                     (added Phase 9)
└── us444/                      20 core-flow specs (US-444 — see us444/README.md)
```

Historical note: in v1 (archived under `archive/2026-04-11-foundry-osv2-api-alignment/`)
this directory housed ten admin-console CRUD specs. v1 US-006 deleted the admin
UI routes, which stranded those specs. Phase 6 gate US-028 removed them and
re-scoped `search-and-filter.spec.ts` to the v2 Browser page
(`/browser/:ontology/:objectType`).

## Running

```bash
cd web

# Single spec
npm run test:e2e -- search-and-filter

# Full suite (requires backend + dev server running — see below)
npm run test:e2e
```

## Backend prerequisites

Playwright hits the Vite dev server on :5173 and expects the Weave API on
:9117. Phase 6 gate US-029 introduced `scripts/e2e-setup.sh` /
`scripts/e2e-teardown.sh` (wired into `make e2e-up` / `make e2e-down`);
this is the supported entry point:

```bash
make e2e-up            # docker compose + bin/weave + vite (idempotent)
cd web && npm run test:e2e
make e2e-down          # tears everything back down
```

The setup script is idempotent: rerunning while the stack is already
healthy is a no-op, and stale PID files under `.e2e-pids/` are reaped on
the next run. Process output is tailed into `.e2e-logs/{weave,vite}.log`.
Both directories are gitignored.

## Test data

`scripts/e2e-setup.sh` automatically runs `test/fixtures/e2e_seed.sh`
after the API server is healthy, so a fresh `make e2e-up` always leaves
the stack on the same deterministic baseline:

- **Ontology**: `northwind` (3 ObjectTypes — `customer`, `order`,
  `product` — with 2 link types and ~15 seed rows driven into Bleve via
  `/api/admin/indexes/rebuild`).
- **Users**: `admin@test` / `manager@test` / `peer@test`, password
  `test1234`, with global `admin` / `editor` / `viewer` roles
  respectively. Use these for JWT-mode login specs once Phase 7 wires
  the full auth flow.

The seeder is wipe-and-reseed (idempotent) and runs in under a few
seconds on a local box. Reseed mid-session with:

```bash
make e2e-seed
```

To point the seeder at a non-default backend override `PG_DSN` and
`WEAVE_URL`:

```bash
PG_DSN=postgres://... WEAVE_URL=http://host:9117 make e2e-seed
```

The seeder library + CLI live under `test/fixtures/seed_northwind/`
(`seed.go`, `schemas.go`, `cmd/main.go`). The library function
`seed_northwind.Seed(ctx, pool, opts)` is reusable from Go tests; see
`test/fixtures/seed_northwind/seed_test.go` for the integration-tagged
end-to-end coverage.

Individual specs that need extra fixtures on top of the baseline may
still create ad-hoc ontology / ObjectType rows via helpers in
`helpers.ts`.

## Writing new specs

- Put Phase N specs under `web/e2e/phaseN/`.
- Reuse helpers in `helpers.ts` for ontology / object-type creation.
- Prefer `data-testid` selectors over class-based selectors (see SearchBar
  and FilterBuilder for the canonical `search-input` / `toggle-filters`
  test ids).
- Use `uniqueName(prefix)` to isolate concurrent runs.
