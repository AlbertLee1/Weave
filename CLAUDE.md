# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Weave is a single-machine Ontology Layer engine inspired by Palantir Foundry OSv2, written in Go with a React frontend. It provides ontology metadata management, object retrieval, full-text search, aggregation, link resolution, action execution, plus the v2 Deep Parity surfaces (`withProperties` derived properties, row + column + Marking security via a single `policy_engine`, Goja embedded JS runtime for Function-backed Actions / `executeQuery`, ontology branching + RID `@vN` versioned reads, SIEM-pipelined audit with `RootHashPublisher` tamper-proof anchoring, MCP `completion/complete`, and a Python SDK with `ObjectSetBuilder` / `AggregationAPI` / `TimeSeriesAPI` / `AttachmentsAPI` / criteria builders / typed exceptions). See [`docs/CHANGES-v2.md`](docs/CHANGES-v2.md) for the full v2 release notes and [`docs/PRD-Weave-OSv2-深度复刻-V2.md`](docs/PRD-Weave-OSv2-深度复刻-V2.md) for the Gap-* tracker + US-048~US-081 backlog status.

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
| `WEAVE_PORT` | `9117` | Server port |
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
    │                    Models: Ontology, ObjectType, Property, LinkType,
    │                    ActionType, Interface, SharedPropertyType,
    │                    TypeGroup, ValueType, QueryType
    │                    ontology_branches table + branch_scope.go for
    │                    ?branch=/X-Weave-Branch read-time pinning
    │
    ├── pkg/oss/         OSS (Object Set Service)
    │   ├── objectset/   15 composable variants: base / filter / union /
    │   │                intersect / subtract / searchAround / static /
    │   │                reference / nearestNeighbors / asType /
    │   │                asBaseObjectTypes / interfaceBase / withProperties /
    │   │                interfaceLinkSearchAround / methodInput.
    │   │                executeWithProperties does real cross-link
    │   │                count/sum/avg/min/max (Gap-Q2)
    │   ├── aggregation/ Bleve-facet engine: full metric family + 5 groupBy
    │   │                + multi-layer nesting + ACCURATE/APPROXIMATE badging
    │   ├── pagination/  Cursor-based; composite_cursor.go for Interface
    │   │                multi-type stable paging
    │   ├── where/       Query filter clause types and SQL conversion
    │   └── subscribe_sse.go  SSE ObjectSet subscription endpoint
    │
    ├── pkg/security/    Row + column + Marking policy engine
    │                    (policy_engine.go Evaluate / AllowedProperties /
    │                    SetMarkingsEnabled / AllowedForIngest) +
    │                    CEL DSL (cel_evaluator.go) + 2 caches
    │                    (policy_cache / decision_cache) — Gap-S1+S2+S3
    │
    ├── pkg/functions/   Embedded dop251/goja JS sandbox runtime + ontology
    │                    shim + cache + typed errors — Gap-A5 / Phase 8 W1
    ├── pkg/queryexec/   Goja-backed executeQuery path (US-067)
    ├── pkg/actions/     Action execution: criteria (always /
    │                    parameterMatch / parameterCompare / and_ / or_ /
    │                    not_) → rules → generate edits → publish to NATS.
    │                    goja_dispatcher.go routes function-backed actions
    │                    (US-066). effects.go: webhook side-effects with
    │                    exponential backoff + DLQ + W3C TraceContext
    │
    ├── pkg/index/       Bleve full-text search index manager; mapping_builder
    │                    drives TypeClass analyzers (not_analyzed / keyword
    │                    / english). RebuildWithOptions covers disaster
    │                    recovery (rehydrate BDD locks the contract)
    ├── pkg/links/       FK + M2M link resolution; multi-hop searchAround
    │                    (US-366) with ErrQueryTooLarge guard
    ├── pkg/funnel/      NATS JetStream publisher/consumer for EditBatch;
    │                    broadcast.go fans events to SSE + WebSocket
    ├── pkg/subscriptions/  WebSocket subscription handler with cursor +
    │                    replay buffer (US-418)
    ├── pkg/auth/        Auth middleware: dev / JWT modes; bootstrap admin;
    │                    sessions API (list / revoke / revoke-others —
    │                    rounds 101-102); API key (wvk_ prefix); typed
    │                    WeaveAuthError
    ├── pkg/audit/       audit_events + hash chain + RedactingStore +
    │                    RootHashPublisher (US-266 tamper-proof) +
    │                    pkg/audit/export/* SIEM pipeline (syslog / S3 /
    │                    batched / tee) — Gap-S4
    ├── pkg/tracing/     OTel: HTTPMiddleware + BaggageMiddleware +
    │                    PgxTracer + cross-NATS TraceContext injection
    ├── pkg/metrics/     Business metrics: objectset / actions / funnel
    │                    durations + funnel_lag; surfaced on /health/ready
    ├── pkg/rid/         Resource Identifier format including optional @vN
    │                    suffix (splitVersionSuffix, US-070)
    ├── pkg/types/       Base type system (21 types) + recursive Struct/
    │                    Array validation + ValueType constraints (Gap-T2/T3)
    ├── pkg/sqlqueries/  SQL Query sandbox (safety.go tokenizer + 30+
    │                    forbidden keywords + system-table guard;
    │                    engine.go pgx.ReadOnly + 5s timeout + 10K row cap)
    ├── pkg/mcp/         MCP HTTP /mcp + prompts.go + resources.go +
    │                    completion.go + completion_ontology_source.go
    │                    (Gap-D4)
    ├── pkg/buildinfo/   build-info / dependencies / features endpoints
    ├── pkg/serverinfo/  server-info / connections endpoints
    ├── pkg/permissionrequests/  request-access workflow + soft cancel
    ├── pkg/dashboards/  dashboard CRUD + duplicate (Foundry "Duplicate")
    ├── pkg/notifications/  notification center + fan-out
    ├── pkg/reactions/   ReactionBar API
    ├── pkg/timeseries/  PG / VictoriaMetrics stores + downsample pushdown
    │                    + timeseries_cagg_5min
    ├── pkg/geotemporal/ PG store + PostGIS spatial indexes (000208)
    ├── pkg/vertex/      graphsvc + scenarioruns + sideeffects gating
    ├── pkg/quiver/      TimeSeries dashboard workbench
    ├── pkg/aip/         AIP semantic search + MCP tooling + logic flows
    ├── pkg/apierror/    Standardized error responses including typed
    │                    NewNotImplemented (501) / NewGone (410) helpers
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

React 19 + TypeScript + Vite + TailwindCSS. State: TanStack React Query (server) + Zustand (local). Vite dev proxy forwards `/api/`, `/health/`, `/metrics`, `/swagger`, and `/mcp` to `:9117`.

Pages: Dashboard (`/`), Explorer (`/explorer/:ontology`), Browser (`/browser/:ontology/:objectType`), Admin (`/admin`), Actions (`/actions/:ontology`), Aggregation (`/aggregation/:ontology/:objectType`).

**Vite dep prebundling for lazy chunks**: any third-party ESM package that imports `react` and lives behind a `React.lazy` boundary must be pinned in `web/vite.config.ts` `optimizeDeps.include`. Otherwise Vite discovers it on first navigation, triggers a mid-flight re-optimize, and serves a chunk whose `react` reference differs from the one already mounted in the app shell — producing `Invalid hook call` / `useRef returning null` on the first visit while a refresh works fine. Pair with `resolve.dedupe: ['react', 'react-dom']` so nested `node_modules` can't ever resolve two React instances. Current pinned entries: `@react-sigma/core`, `sigma`, `graphology` (Vertex workspace).

**Web bundle budget guard**: `npm run build` runs `web/scripts/check-bundle-budget.mjs` after `vite build`; keep checked thresholds there and keep Vite's `chunkSizeWarningLimit` aligned so CI failures point to the versioned budget message. Vendor chunk ownership is centralized in `web/src/build/chunking.ts`; when adding heavy upper-layer dependencies, classify their package family there so payloads stay in named chunks instead of `vendor-misc` and Rollup does not emit circular manual-chunk warnings.

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

## Development Process: BDD (MANDATORY)

**TDD 保证"代码做对了"，BDD 保证"做的是对的功能"。两者并行，不可替代。**

每一次 commit，除非有足够的理由（见下文「豁免条件」）并在 commit message 中明确说明，否则必须附带 BDD 集成测试，从外部可观察的行为层面覆盖本次改动引入或修改的功能。

### BDD Workflow (Given → When → Then)

1. **先写 scenario** —— 用 Given/When/Then 描述用户或调用方可观察的行为，作为本次 commit 的"功能契约"。
2. **以集成测试形式落地** —— 不允许只用单元测试 + mock 来满足 BDD 要求。必须是端到端可观测的集成测试：
   - 后端：走 chi router 的 HTTP 集成测试（`test/e2e/` 或 `_integration_test.go`，使用 `testutil.StartPGContainer` 起真实 PG，必要时起真实 NATS），断言 HTTP 响应、DB 状态、NATS 事件等外部可见结果。
   - 前端：Vitest + React Testing Library + MSW 模拟 API，断言渲染结果与用户交互行为；或 Playwright 端到端用例（如已配置）。
3. **scenario 先红后绿** —— 集成测试必须先失败，再通过实现让它变绿；与 TDD 的 Red→Green→Refactor 保持一致。
4. **命名约定** —— BDD 测试函数以 `TestBDD_` 前缀或 `_bdd_test.go` 文件后缀命名（前端：`*.bdd.test.tsx`），便于检索与统计覆盖。

### Rules

- **NEVER commit a feature change without an accompanying BDD integration test in the same commit.**
- 单元测试 + mock **不能**替代 BDD 集成测试，二者职责不同：单元测试覆盖分支，BDD 集成测试覆盖功能契约。
- bug 修复：BDD 测试必须先复现该 bug 的用户可见症状（HTTP 4xx/5xx、错误 UI 状态、丢失的 NATS 事件等），再修复。
- 新功能：以 BDD scenario 为接口设计的起点，scenario 通过即视为功能完成。
- 重构：行为不变，已有 BDD 测试必须在重构前后保持通过；不允许借重构之名删除或弱化 BDD 断言。

### 豁免条件（必须在 commit message 中显式声明）

满足以下之一可不附带 BDD 集成测试，但必须在 commit message 里写明理由（例如 `BDD-exempt: docs-only`）：

- 纯文档、注释、CLAUDE.md / README 更新
- 纯依赖升级且无行为变化（`go.mod` / `package.json` lockfile only）
- 纯格式化 / lint 修复 / 重命名变量等无行为变化的改动
- migrations 仅含 schema 变更且尚无任何代码读写新字段（下一次引入用法的 commit 必须补上 BDD）
- 实验性脚本 / `scripts/` 下的一次性工具（且未被生产代码引用）

任何对 `cmd/`, `pkg/`, `internal/`, `web/src/` 下的功能性代码改动，默认**不**适用豁免。

### Test Commands (BDD)

```bash
# 运行所有 BDD 集成测试（后端）
go test -tags integration ./... -run '^TestBDD_'

# 运行某个 BDD 场景
go test -tags integration ./test/e2e/... -run TestBDD_CreateOntology -v

# 前端 BDD
npm --prefix web test -- --run bdd
```

## Testing Patterns

- **Unit tests**: alongside source files, use in-memory implementations and table-driven subtests
- **Integration tests**: `go test -tags integration`, use `internal/testutil.StartPGContainer(t)` for real PostgreSQL via testcontainers
- **E2E tests**: `test/e2e/` with in-memory OMS repo and chi test router
- **Test datasets**: `testdata/northwind/` (11 CSVs) and `testdata/chinook/` (8 CSVs)
- **Frontend tests**: Vitest + React Testing Library + MSW for API mocking

## Database

PostgreSQL 16. Migrations in `migrations/` managed by golang-migrate. 11 core tables: ontologies, object_types, properties, link_types, action_types, interfaces, object_type_interfaces, value_types, datasource_bindings, security_policies, action_logs.
