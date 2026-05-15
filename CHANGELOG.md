# Changelog

All notable changes to Weave-Vertex are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The canonical release checklist with per-story status and known-issue mapping
lives at [`docs/RELEASE-vertex-v1.md`](docs/RELEASE-vertex-v1.md).

---

## [v1.0.0-vertex] — 2026-05-15

First Vertex GA release. Ships 92 of the 125 planned Vertex stories — see
[`docs/RELEASE-vertex-v1.md`](docs/RELEASE-vertex-v1.md) §3 for the deferred-scope
roster and R1–R9 risk mapping.

### Highlights

- **Scenario Read Overlay (Phase 1)** — `X-Scenario-Id` header on every Ontology
  read API rewrites object properties / aggregations through a per-user
  scenario fold, with zero impact on baseline reads.
- **System Graph (Phase 2)** — `graphs` / `graph_versions` tables, REST surface
  `/api/vertex/v1/graphs/*`, JSON-schema-validated SystemGraph payloads,
  templated graphs, Workshop embed widget, admin Control Panel.
- **TimescaleDB time series (Phase 4)** — `object_time_series` hypertable plus
  `cagg_5min` continuous aggregate, query API at
  `/api/v2/ontologies/.../timeseries/{property}`, TopBar time-range UI, node
  sparkline labels (uPlot), bottom series panel, unified events+series Timeline.
- **Scenario UX (Phase 5)** — full Scenario Pane with Add Action / Add I/O,
  scalar / object-property / time-series override cells.
- **Layers · Filters · Search Around (Phase 9)** — fillColor by property and by
  time-series value, saved selections, histogram filter, time-range Shift+drag,
  right-click Search Around expansion, custom Search Around Function, layers
  panel, URL-param deep links, Graph Template instantiation, threshold colouring,
  compare-window mode, missing-data warnings.
- **Events / History (Phase 10)** — events overview API, coloured event bars,
  graph history sidebar with diff view, duplicate graph, quick-share links,
  versioned graphs with optimistic locking.
- **Apply Scenario (Phase 11)** — commits a scenario fold back to baseline with
  audit trail, webhook gating inside scenario forks, follow-up Action triggers,
  Live Mode badge.
- **Observability + E2E (Phase 12)** — Scenario / SystemGraph benchmarks,
  Prometheus metrics, retry visualization, failed-scenario debug drawer, full
  airline-ops E2E suite.
- **Integration · SDK · Cookbook (Phase 13)** — Layers drag-drop, Workshop-embed
  VertexGraphWidget, AIP Logic ↔ Scenario integration, Map App ↔ Vertex
  interop, VertexClient SDK in TypeScript / Python / Go, end-to-end cookbook,
  MCP server integration, AIP-style LLM Scenario Copilot.
- **Scope guards (Phase 14)** — diagramming stub, SSE reconnect, Scenario data
  retention policy, RID naming spec, cross-ontology graph guard, snapshot diff
  API.
- **Release polish (Phase 17 / Stream C)** — keyboard shortcuts + help modal,
  zh-CN / en-US i18n at 100% Vertex coverage, hard 80% backend / 75% frontend
  coverage gates, scenario authz audit with ≥ 8 negative tests, Docker Compose
  one-shot stack (Postgres + NATS + TimescaleDB + function-runtime + Weave),
  and this release checklist.

### Added

- `migrations/000106_vertex_object_time_series.{up,down}.sql` — TimescaleDB
  hypertable + `cagg_5min` continuous aggregate.
- `pkg/scenarios/` — ScenarioRepo (PG + in-memory), Edit Fold engine, archive
  store, owner / admin authorization (`authz.go`).
- `pkg/timeseries/` — VertexService query API, aggregation helpers.
- `pkg/vertex/graphsvc/` — SystemGraph service, repo, handlers, JSON-schema
  payload validator.
- `pkg/links/` — Vertex-specific link type classes.
- `pkg/auth/` — Marking-aware authorization shared with scenarios (R4
  mitigation).
- `cmd/weave-function-runtime/` — in-stack Tier 3.2 dispatcher stub
  (`POST /functions/{rid}` → empty edits) used by docker-compose.
- `web/src/vertex/` — VertexGraphWidget, DiagrammingStubPage, LayersDragPanel,
  MapOpenInVertexButton, ScenarioCopilotButtons, ScenarioDebugDrawer,
  ScenarioPane, ScenarioRetryPane.
- `web/src/i18n/resources/{en,zh-CN}.ts` — `vertex.*` namespace, 25 keys × 8
  sub-groups, parity-checked by `extract-i18n.mjs`.
- `web/src/hotkeys/` — global registry, `?` help modal, Cmd+S save graph,
  Cmd+Enter run scenario.
- `coverage/thresholds.json` — backend per-package coverage floors with
  `excludeFiles` for integration-only files.
- `docker-compose.yml` — `timescaledb` (host :5433) and `function-runtime`
  (host :9000) services with curl / pg_isready healthchecks.
- `docs/RELEASE-vertex-v1.md` — canonical Vertex v1 release checklist + known
  issues mapping.
- `CHANGELOG.md` — this file.

### Changed

- `cmd/server/main.go` — Vertex service wiring, scenario authz middleware,
  graph routes, timeseries routes.
- `scripts/dev.sh` — boot banner now lists PostgreSQL + NATS + TimescaleDB +
  Function Runtime + Vertex UI surface.
- `cmd/covercheck/main.go` — `Config.ExcludeFiles` filters cover-profile lines
  by path suffix before per-package aggregation; PG-only files (`pg_repo.go`,
  `archive_db.go`, `vertex_service_pg.go`, …) no longer drag unit-test floors.
- `web/vite.config.ts` — Vitest per-glob coverage threshold for
  `src/vertex/**/*.{ts,tsx}` @ 75% lines/branches/functions/statements; global
  `testTimeout` raised to 30 s for coverage transform headroom.

### Security

- `pkg/scenarios/authz.go` — owner-only scenario reads/writes with admin bypass
  (`auth.PermUserManage`); diff payloads run through `MaskEditsForUser` to
  redact properties when the caller's markings don't cover an object's marking
  set. 18 negative tests in `pkg/scenarios/authz_test.go` (well above the
  PRD ≥ 8 floor).

### Known issues

See [`docs/RELEASE-vertex-v1.md`](docs/RELEASE-vertex-v1.md) §3 for the full
R1–R9 + Foundry-baseline-traps table and the 33 deferred stories (Phase 3
advanced graph, Phase 6 scenario run/diff, Phase 7 functions/models,
Phase 8 extended labels). Quick-glance items:

- `TestContract_AllRoutesDocumented` is RED on `main` — the VTX-028 timeseries
  route was not added to `api/openapi.yaml`. Fix lands in v1.1 via
  `undocumentedRouteAllowList` or openapi.yaml update.
- `pkg/xlsxstream` `-race` test times out at 10 min; isolated run finishes
  in ~27 s. Unrelated to Vertex; investigating in v1.1.
- `pkg/timeseries` integration tests fail on first run against a fresh
  Postgres due to a duplicate `down.sql` migration. Workaround: `migrate down`
  then `migrate up` once, then re-run `go test -tags integration`.
- `cmd/weave-function-runtime` is a permissive no-op stub for local dev. Any
  Vertex deployment that needs Function-backed Actions must replace it with a
  real runtime that implements the `pkg/actions/function_dispatcher.go`
  `POST /functions/{rid}` contract.
- Browser-level verification for Stream C stories (VTX-120 ~ VTX-125) was
  deferred — the dev-browser MCP wasn't available; manual walkthrough is
  documented in `docs/RELEASE-vertex-v1.md` §2.

### Migration notes

- New deployments: run `docker compose up` for the full stack
  (Postgres + NATS + TimescaleDB + function-runtime + Weave + monitoring).
  `WEAVE_FUNCTIONS_BASE_URL=http://function-runtime:9000/functions` is wired
  by default; Functions still need `WEAVE_FUNCTIONS_ENABLED=true` to dispatch.
- Existing v0.x deployments: run `migrate up` to apply
  `000106_vertex_object_time_series` plus the scenario / graph tables. The
  migration is conditional on `pg_available_extensions` so non-Timescale
  Postgres images still apply the plain-table fallback.

---

[v1.0.0-vertex]: https://github.com/liyang/weave/releases/tag/v1.0.0-vertex
