# Weave

An open-source, single-machine **Ontology Layer engine** inspired by [Palantir Foundry OSv2](https://www.palantir.com/docs/foundry/ontology/overview/), written in Go.

Weave lets you define rich data models (ontologies) — object types, properties, links, and actions — then query, search, aggregate, and mutate data through a composable API, all backed by PostgreSQL, Bleve full-text search, and NATS JetStream event streaming.

## Features

- **Ontology Metadata Service (OMS)** — define and manage ontologies, object types, properties, link types, action types, and interfaces
- **Object Set Service (OSS)** — retrieve, search, filter, and paginate objects with composable ObjectSet queries (union, intersect, subtract, searchAround)
- **Full-Text Search** — per-object-type Bleve indexes with automatic indexing
- **Aggregation Engine** — groupBy queries with metrics (sum, count, min, max, avg, cardinality, etc.)
- **Link Resolution** — traverse relationships between objects via foreign key configuration
- **Action Execution** — define parameterized actions with validation rules, execute to generate edits, publish via NATS JetStream
- **Event Streaming** — NATS JetStream consumer applies edits to search indexes in real-time
- **Web UI** — React dashboard for exploring schemas, browsing objects, running aggregations, and managing ontology definitions

## Tech Stack

| Layer | Technology |
|---|---|
| HTTP Router | [chi](https://github.com/go-chi/chi) v5 |
| Database | PostgreSQL 16 + [pgx](https://github.com/jackc/pgx) v5 |
| Migrations | [golang-migrate](https://github.com/golang-migrate/migrate) v4 |
| Full-Text Search | [Bleve](https://github.com/blevesearch/bleve) v2 |
| Event Streaming | [NATS](https://nats.io/) 2.10 + JetStream |
| Frontend | React 19, TypeScript, TailwindCSS 4, Vite 6 |
| State Management | TanStack React Query + Zustand |
| Testing | `go test`, testcontainers-go, Vitest, React Testing Library, MSW |

## Prerequisites

- **Go** 1.25+
- **Node.js** 20+ & npm
- **Docker** & Docker Compose

## Quick Start

```bash
# Clone the repository
git clone https://github.com/AlbertLee1/Weave.git
cd Weave

# Start everything with one command
make dev
```

This will:
1. Start PostgreSQL and NATS via Docker Compose
2. Install frontend dependencies (if needed)
3. Build and start the Go server on `:9117`
4. Start the Vite dev server on `:5173` with hot reload

```
╔══════════════════════════════════════════════╗
║  Weave Dev Environment Ready                 ║
╠══════════════════════════════════════════════╣
║  WebUI:   http://localhost:5173              ║
║  API:     http://localhost:9117              ║
║  PG:      localhost:5432                     ║
║  NATS:    localhost:4222                     ║
╚══════════════════════════════════════════════╝
```

Press `Ctrl+C` to stop all services.

## Manual Setup

```bash
# Start infrastructure
make docker-up

# Build and run the Go server
make build
PG_DSN="postgres://weave:weave@localhost:5432/weave?sslmode=disable" \
NATS_URL="nats://localhost:4222" \
./bin/weave

# In another terminal, start the frontend dev server
make web-dev
```

## Available Commands

```bash
# Development
make dev                # Start all services (Docker + Go API + Vite HMR)
make docker-up          # Start PostgreSQL + NATS
make docker-down        # Stop Docker services

# Backend
make build              # Build Go binary to bin/weave
make run                # Build and run
make test               # Run unit tests
make test-integration   # Run integration tests (requires Docker)
make lint               # Run golangci-lint

# Frontend
make web-install        # Install npm dependencies
make web-dev            # Start Vite dev server (:5173)
make web-build          # Build production frontend
make web-test           # Run Vitest

# Production build
make build-with-ui      # Build frontend + embed into Go binary
```

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `WEAVE_PORT` | `9117` | HTTP server port |
| `PG_DSN` | `postgres://weave:weave@localhost:5432/weave?sslmode=disable` | PostgreSQL connection string |
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `WEAVE_DATA_DIR` | `data` | Directory for Bleve search indexes |
| `WEAVE_LOG_LEVEL` | `info` | Log level |
| `AUTH_MODE` | `dev` | `dev` (no auth) or `token` (Bearer token) |

## Architecture

```
cmd/server/            Server entry point, routing, SPA handler
internal/
  config/              Environment-based configuration
  database/            PostgreSQL connection pool + migrations
  testutil/            Test containers for integration tests
pkg/
  oms/                 Ontology Metadata Service (models, repository, handlers)
  oss/                 Object Set Service
    objectset/         Composable ObjectSet definitions & execution
    aggregation/       Aggregation engine (Bleve-backed)
    pagination/        Cursor-based pagination
    where/             Query filter clause system
  index/               Bleve search index manager
  links/               Link resolution (foreign key traversal)
  actions/             Action execution with rules & validation
  funnel/              NATS JetStream publisher/consumer
  auth/                Authentication middleware
  rid/                 Resource Identifier (ri.service.realm.type.uuid)
  types/               Base type system (21 types) with coercion
  apierror/            Standardized API error responses
  httputil/            JSON encoding utilities
migrations/            PostgreSQL schema migrations
web/                   React frontend (TypeScript + TailwindCSS + Vite)
test/                  E2E and integration tests
testdata/              Northwind & Chinook CSV datasets
```

## API Overview

### Ontology Metadata (OMS)

```
GET    /api/v2/ontologies
GET    /api/v2/ontologies/{ontology}
GET    /api/v2/ontologies/{ontology}/objectTypes
GET    /api/v2/ontologies/{ontology}/objectTypes/{objectType}
GET    /api/v2/ontologies/{ontology}/objectTypes/{objectType}/outgoingLinkTypes
GET    /api/v2/ontologies/{ontology}/actionTypes
GET    /api/v2/ontologies/{ontology}/actionTypes/{actionTypeRid}
```

### Admin

```
POST   /api/admin/ontologies
POST   /api/admin/ontologies/{ontology}/objectTypes
PUT    /api/admin/objectTypes/{objectTypeRid}
DELETE /api/admin/objectTypes/{objectTypeRid}
POST   /api/admin/objectTypes/{objectTypeRid}/properties
DELETE /api/admin/properties/{propertyRid}
POST   /api/admin/ontologies/{ontology}/linkTypes
POST   /api/admin/ontologies/{ontology}/actionTypes
PUT    /api/admin/actionTypes/{actionTypeRid}
```

### Object Set Service (OSS)

```
GET    /api/v2/ontologies/{ontology}/objects/{objectType}/{primaryKey}
POST   /api/v2/ontologies/{ontology}/objects/{objectType}/list
POST   /api/v2/ontologies/{ontology}/objects/{objectType}/search
POST   /api/v2/ontologies/{ontology}/objects/{objectType}/linkedObjects
POST   /api/v2/ontologies/{ontology}/objects/{objectType}/aggregate
```

### ObjectSets & Actions

```
POST   /api/v2/ontologies/{ontology}/objectSets/loadObjects
POST   /api/v2/ontologies/{ontology}/objectSets/aggregate
POST   /api/v2/ontologies/{ontology}/objectSets/createTemporary
POST   /api/v2/ontologies/{ontology}/actions/{action}/apply
POST   /api/v2/ontologies/{ontology}/actions/{action}/applyBatch
```

### Health Check

```
GET    /health
```

## Web UI

The frontend provides six main views:

| Page | Path | Description |
|---|---|---|
| Dashboard | `/` | Overview of all ontologies with stats |
| Explorer | `/explorer/:ontology` | Schema explorer with type tree and relationship graph |
| Browser | `/browser/:ontology/:objectType` | Object data browser with search, filters, and linked objects |
| Admin | `/admin` | Create and manage ontology definitions |
| Actions | `/actions/:ontology` | Action execution console |
| Aggregation | `/aggregation/:ontology/:objectType` | Aggregation query builder with charts |

## Testing

```bash
# Go unit tests
make test

# Go integration tests (requires Docker)
make test-integration

# Run a specific Go test
go test ./pkg/oms/... -run TestGetOntology -v

# Frontend tests
make web-test

# Frontend tests in watch mode
cd web && npm run test:watch
```

Integration tests use [testcontainers-go](https://github.com/testcontainers/testcontainers-go) to spin up real PostgreSQL instances. Test datasets (Northwind, Chinook) are located in `testdata/`.

## License

MIT
