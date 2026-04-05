# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Weave is a single-machine Ontology Layer engine inspired by Palantir Foundry OSv2, written in Go with a React frontend. It provides ontology metadata management, object retrieval, full-text search, aggregation, link resolution, and action execution.

## Common Commands

```bash
# Full dev environment (starts Docker, builds Go, starts Vite)
make dev

# Docker services (PostgreSQL 16 + NATS 2.10)
make docker-up          # start
make docker-down        # stop

# Backend
make build              # go build -o bin/weave ./cmd/server
make run                # build + run
make test               # go test ./... (unit tests only)
make test-integration   # go test -tags integration ./...
make lint               # golangci-lint run ./...

# Run a single Go test
go test ./pkg/oms/... -run TestGetOntology -v

# Frontend
make web-install        # npm install
make web-dev            # Vite dev server on :5173
make web-build          # tsc + vite build, copies to cmd/server/web/dist/
make web-test           # vitest

# Full build with embedded frontend
make build-with-ui
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `WEAVE_PORT` | `8080` | Server port |
| `PG_DSN` | `postgres://weave:weave@localhost:5432/weave?sslmode=disable` | PostgreSQL DSN |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `WEAVE_DATA_DIR` | `data` | Directory for Bleve indexes |
| `AUTH_MODE` | `dev` | Auth mode: `dev` (no auth) or `token` |

## Architecture

### Backend Layers

```
cmd/server/        Entry point, route registration, SPA handler
    │
    ├── pkg/oms/         OMS (Ontology Metadata Service)
    │                    Repository interface → PostgreSQL implementation
    │                    Models: Ontology, ObjectType, Property, LinkType, ActionType, Interface
    │
    ├── pkg/oss/         OSS (Object Set Service)
    │   ├── objectset/   Composable ObjectSet definitions (base/filter/union/intersect/subtract/searchAround)
    │   │                Lazy evaluation via Executor, in-memory Store with TTL
    │   ├── aggregation/ Aggregation engine backed by Bleve indexes
    │   ├── pagination/  Cursor-based pagination
    │   └── where/       Query filter clause types and SQL conversion
    │
    ├── pkg/index/       Bleve full-text search index manager (per-ObjectType indexes)
    ├── pkg/links/       Link resolution via foreign key config in OMS metadata
    ├── pkg/actions/     Action execution: validate params → apply rules → generate edits → publish to NATS
    ├── pkg/funnel/      NATS JetStream publisher/consumer for EditBatch events
    ├── pkg/auth/        Auth middleware (dev mode: no-op; token mode: Bearer validation)
    ├── pkg/rid/         Resource Identifier format: ri.{service}.{realm}.{resourceType}.{uuid}
    ├── pkg/types/       Base type system (21 types) with coercion and validation
    ├── pkg/apierror/    Standardized error responses
    └── pkg/httputil/    JSON encoding utilities

internal/
    ├── config/          Environment-based configuration loading
    ├── database/        PostgreSQL connection pool + migration runner
    └── testutil/        testcontainers-go based PostgreSQL container for tests
```

### Server Initialization Order (cmd/server/main.go)

Config → PostgreSQL + migrations → Bleve IndexManager → LinkResolver → OSSService → AggregationEngine → ObjectSetStore (1h TTL) → NATS JetStream Consumer → ActionExecutor → chi Router → SPA handler → HTTP server with graceful shutdown.

### API Route Groups

- `GET /health` — health check
- `/api/v2/ontologies/...` — read-only OMS endpoints (ontologies, objectTypes, linkTypes, actionTypes)
- `/api/admin/...` — admin CRUD for ontology definitions
- `/api/v2/ontologies/{ontology}/objects/...` — OSS endpoints (search, list, get, linkedObjects)
- `/api/v2/ontologies/{ontology}/objectSets/...` — ObjectSet load, aggregate, createTemporary
- `/api/v2/ontologies/{ontology}/actions/...` — apply, applyBatch
- `/api/v2/ontologies/{ontology}/objects/{objectType}/aggregate` — aggregation

### Action Execution Flow

Lookup ActionType → validate parameters → execute rules → generate Edit list (CREATE/MODIFY/DELETE) → collapse redundant edits → publish EditBatch to NATS JetStream → Consumer updates Bleve indexes.

### Frontend (web/)

React 19 + TypeScript + Vite + TailwindCSS. State: TanStack React Query (server) + Zustand (local). Vite dev proxy forwards `/api/` and `/health/` to `:8080`.

Pages: Dashboard (`/`), Explorer (`/explorer/:ontology`), Browser (`/browser/:ontology/:objectType`), Admin (`/admin`), Actions (`/actions/:ontology`), Aggregation (`/aggregation/:ontology/:objectType`).

## Development Process: TDD (MANDATORY)

**This project follows strict Test-Driven Development. This is a hard constraint, not a suggestion.**

### TDD Workflow (Red → Green → Refactor)

1. **Write tests FIRST** — Before writing any implementation code, write failing tests that define the expected behavior.
2. **Verify tests fail** — Run the tests and confirm they fail for the right reason (not due to syntax errors or missing imports).
3. **Write minimal implementation** — Write just enough code to make the tests pass. No more.
4. **Verify tests pass** — Run the tests and confirm they all pass.
5. **Refactor** — Clean up the code while keeping tests green.

### Rules

- **NEVER write implementation code without a corresponding test written first.**
- **NEVER skip the "verify it fails" step.** If a test passes immediately, it's not testing new behavior.
- For bug fixes: write a test that reproduces the bug FIRST, then fix it.
- For new features: write acceptance tests defining the API/behavior FIRST, then implement.
- For refactors: ensure existing tests pass before AND after changes.

### Test Commands

```bash
# Run a specific test
go test ./pkg/oms/... -run TestMyNewFeature -v

# Run all tests
make test

# Frontend tests
make web-test
```

## Testing Patterns

- **Unit tests**: alongside source files, use in-memory implementations and table-driven subtests
- **Integration tests**: `go test -tags integration`, use `internal/testutil.StartPGContainer(t)` for real PostgreSQL via testcontainers
- **E2E tests**: `test/e2e/` with in-memory OMS repo and chi test router
- **Test datasets**: `testdata/northwind/` (11 CSVs) and `testdata/chinook/` (8 CSVs)
- **Frontend tests**: Vitest + React Testing Library + MSW for API mocking

## Database

PostgreSQL 16. Migrations in `migrations/` managed by golang-migrate. 11 core tables: ontologies, object_types, properties, link_types, action_types, interfaces, object_type_interfaces, value_types, datasource_bindings, security_policies, action_logs.
