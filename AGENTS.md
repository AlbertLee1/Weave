# Repository Guidelines

## Project Structure & Module Organization

Weave is a Go service with a React/TypeScript frontend. The server entry point and route wiring live in `cmd/server/`. Core backend packages are under `pkg/` by domain (`pkg/oms`, `pkg/oss`, `pkg/actions`, `pkg/funnel`, plus the v2 packages `pkg/security` for the row + column + Marking policy engine, `pkg/functions` for the embedded Goja JS runtime, `pkg/queryexec` for Goja-backed `executeQuery`, `pkg/subscriptions` for WebSocket subscription, `pkg/audit` + `pkg/audit/export` for the SIEM pipeline + `RootHashPublisher`, `pkg/tracing` + `pkg/metrics` for observability, `pkg/sqlqueries` for the SQL Query sandbox, `pkg/mcp` for the MCP HTTP + prompts/resources/completion surface, `pkg/timeseries` + `pkg/geotemporal` for special data types, and the upper-layer `pkg/vertex` / `pkg/quiver` / `pkg/aip` workspaces); shared internal helpers live in `internal/`. Database migrations are in `migrations/`, API specs in `api/`, and generated/embedded web assets are copied to `cmd/server/web/dist/`. Frontend code lives in `web/src/`, with components, features, and colocated tests. Broader integration and E2E tests live under `test/`; reusable datasets are in `testdata/`. The canonical Backend-Layers tree lives in [`CLAUDE.md`](CLAUDE.md), and the v2 Deep Parity feature catalogue + Gap-* tracker live in [`docs/CHANGES-v2.md`](docs/CHANGES-v2.md) and [`docs/PRD-Weave-OSv2-深度复刻-V2.md`](docs/PRD-Weave-OSv2-深度复刻-V2.md).

## Build, Test, and Development Commands

- `make dev` starts Docker services, the Go API, and Vite HMR.
- `make docker-up` / `make docker-down` manage PostgreSQL and NATS.
- `make build` builds `bin/weave`; `make run` builds and runs it.
- `make test` runs backend unit tests through `scripts/ci/go-packages.sh`.
- `make test-integration` runs Go tests with the `integration` tag.
- `make web-install`, `make web-dev`, `make web-build`, and `make web-test` cover frontend install, dev server, production build, and Vitest.
- `make build-with-ui` builds the frontend and embeds it in the Go server.

## Coding Style & Naming Conventions

Format Go with `gofmt`; run `make lint` for `golangci-lint`. Keep Go package names short, lowercase, and domain-oriented. Name Go tests `*_test.go`, with table-driven tests preferred where practical. Frontend files use TypeScript modules; React components are PascalCase (`ObjectTable.tsx`), utilities are camelCase, and tests use `*.test.ts` or `*.test.tsx`. Run `cd web && npm run lint` or the Make targets before submitting UI changes.

## Testing Guidelines

This project expects TDD for behavior changes: write the failing test first, then implementation. Feature and bug-fix commits should also include BDD coverage unless the change is docs-only, dependency-only, formatting-only, or otherwise explicitly exempt. Backend BDD tests use `TestBDD_` names or `_bdd_test.go`; frontend BDD tests use `*.bdd.test.tsx`. Use `make test-cover-check` for coverage-gated backend changes and `make web-test-cover` for frontend coverage.

## Commit & Pull Request Guidelines

Recent commits use `type: [ticket] - summary`, for example `feat: [P105-object-check] - GET ...` or `test: [SDK94-batch-contract] - Lock ...`. Keep summaries imperative and specific. Pull requests should describe the change, link the ticket, list verification commands, and include screenshots for UI changes. Note migrations, API spec updates, and whether `make sync-openapi` was run when `api/openapi.yaml` changes.

