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
└── phase9/                     (added Phase 9)
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

Specs under US-028 create their own ontology + ObjectType fixtures via the
admin API in `beforeAll` and only exercise front-end rendering contracts
(no indexed documents required). US-030 will add
`test/fixtures/e2e_seed.sh` to reset the stack to a known Northwind +
Chinook + test-user baseline before each gate run.

## Writing new specs

- Put Phase N specs under `web/e2e/phaseN/`.
- Reuse helpers in `helpers.ts` for ontology / object-type creation.
- Prefer `data-testid` selectors over class-based selectors (see SearchBar
  and FilterBuilder for the canonical `search-input` / `toggle-filters`
  test ids).
- Use `uniqueName(prefix)` to isolate concurrent runs.
