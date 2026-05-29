# Weave v2 Changes

> Release notes for the Weave OSv2 deep-parity v2 cycle. Companion document to
> `docs/PRD-Weave-OSv2-深度复刻-V2.md` — this file records *what shipped*; the
> PRD records *what the targets were*. The Phase 6 → Phase 8 + ongoing
> Phase 9 polish work below brings Weave from "REST API surface aligned" to
> "MUST/SHOULD semantic depth aligned to single-machine OSv2 reference
> implementation". Reading order: §1 highlights → §2 by area → §3 breaking
> changes → §4 upgrade notes.

Last updated: round 158 (after PR #57 merge).

---

## 1. Highlights

* **Foundry derived-property MUST line landed in full**: `withProperties` now
  *actually computes* across links (Gap-Q2). `executor.executeWithProperties`
  is a 101-line implementation covering count / sum / avg / min / max plus
  reverse-link aggregations, formula expressions (FullName / arithmetic),
  multi-hop searchAround, and M2M traversal — all behind an `ErrQueryTooLarge`
  guard. 12 + 5 + N subtest coverage.

* **Phase 8 W1 — Goja embedded ECMAScript runtime complete (Gap-A5)**:
  `pkg/functions/goja_runtime.go` embeds `dop251/goja` with a sandboxed
  runtime (no fs / net), `pkg/actions/goja_dispatcher.go` routes
  function-backed actions (US-066), and `pkg/queryexec/goja.go` drives
  `executeQuery` through the same runtime (US-067). Ontology client shim,
  caching, typed errors, US-218 sandbox boundary tests, and US-476 hardening
  all on the box.

* **Row + column + Marking security collapsed into one engine (Gap-S1 +
  S2 + S3)**: `pkg/security/policy_engine.go::Evaluate` returns a Bleve query
  the OSS pipeline ANDs into the user where-clause; `AllowedProperties`
  filters serialization; `SetMarkingsEnabled` / `AllowedForIngest` merge
  marking-based decisions from `auth.User.Attributes[MarkingsAttributeKey]`.
  Backed by CEL DSL, decision + policy caches, six integration tests, BDD,
  and `rls_bench_test.go`.

* **Ontology branching + RID `@vN` (Gap-T4)**: `ontology_branches` table
  (migrations `000024` + `000091`), RID parser supports `@vN` suffix
  (`splitVersionSuffix`), `?branch=` / `X-Weave-Branch` header dispatch
  (`pkg/oms/branch_scope.go`), and eight Get endpoints typed-reject
  versioned reads with `501 VersionedLookupNotSupported`.

* **Multi-groupBy + accuracy badge end-to-end (Gap-Q3)**: 105-doc fixture
  in `test/foundry_parity/us015_multi_groupby.json` drives
  `aggregation_multigroupby_test.go::TestMultiGroupBy_NorthwindOrders`
  across `country × freight × orderDate` (ExactValue × FixedWidth ×
  Duration). `accuracy_test.go::TestAggregationAccuracyMarker` asserts
  ACCURATE vs APPROXIMATE across six scenarios.

* **Aggregation, audit, CI all hardened together**: SIEM pipeline (Gap-S4)
  with batched-retry + syslog + S3 + TeeStore fan-out + `RootHashPublisher`
  for US-266 tamper-proof anchoring; rehydrate disaster-recovery BDD
  (Gap-R3 — wipe Bleve dir → fresh manager → rebuild from source → query
  equivalent); golangci-lint config corrected for PRs > 300 files (use
  `--new-from-merge-base=origin/main` instead of the GH PR diff API that
  406s); vulncheck cleared (otlptracehttp v1.41 → v1.43 for
  GO-2026-4985, x/crypto v0.51 → v0.52).

## 2. Changes by area

### OMS (Ontology Metadata Service)

* RID parser supports `@vN` suffix; same ID with different versions are not
  equal. (`pkg/rid/rid.go::Version` + `splitVersionSuffix`, commit P91)
* `?branch=<name>` query + `X-Weave-Branch: <name>` header for read-time
  branch pinning; query wins on conflict. (`pkg/oms/branch_scope.go`)
* `ontology_branches` + `ontology_proposals` + `ontology_branches_parent_tx`
  + `aip_messages_branch` migrations land the branch metadata.

### OSS (Object Set Service)

* `executor.executeWithProperties` does real cross-link aggregation; counts
  show up in `Result.DerivedValues`, downstream aggregation routes through
  `handler_aggregate_derived.go::aggregationNeedsDerivedPath`.
* `nearestNeighbors` accepts `PropertyIdentifiers` (multi-vector column) +
  `fusionStrategy: min | rrf` (RRF k = 60) for rank fusion.
* `composite_cursor.go` makes 3-type Northwind HasOwner interface paging
  stable; `us463_interface_cursor_stability_test.go` locks the wire shape.
* `nearestNeighbors` now enforces the Foundry-documented limits K ≤ 100 and
  vector dimension ≤ 2048 (syntax ref L115): `numNeighbors` > 100 or a raw
  query vector longer than 2048 is rejected at `Definition.Validate` with a
  400 `InvalidObjectSet` instead of being handed to the vector store. See
  `nn_caps_test.go::TestBDD_NearestNeighbors_OverLimitK_Rejected`.
* Chained `searchAround` now enforces the Foundry-documented 3-hop ceiling
  ("最多 3 层链式 SearchAround", syntax ref L97/L226): a `Path` of more than
  `MaxSearchAroundHops` (3) is rejected at `Definition.Validate` with a 400
  `InvalidObjectSet` instead of executing the over-deep chain (previously
  bounded only by the runtime `SearchAroundIntermediateCap`). See
  `searcharound_hoplimit_test.go::TestBDD_SearchAround_FourHopPath_Rejected`.
* Aggregation `groupBy` duration now accepts the `P3M` (byQuarter) and `PT1H`
  (byHours) ISO 8601 shortcuts in addition to `P1D`/`P1W`/`P1M`/`P1Y`,
  matching the Foundry OntologyAggregation grammar (`.byQuarter()` /
  `.byHours()`); previously callers had to fall back to the verbose
  `DurationValue {unit, value}` form. See `parseDuration` +
  `duration_iso8601_test.go` and the HTTP-level
  `TestBDD_Aggregate_GroupByDuration_Quarter` / `_Hour`.

### Actions

* `parameterCompare` cross-field criteria (`gt` / `gte` / `lt` / `lte` /
  `eq` / `neq`) joined the `parameterMatch` / `always` primitives.
* `and` / `or` / `not` composite groups round out the boolean algebra.
* Admin save validates the criteria tree structurally (`P2-135`); SDK ships
  typed `WeaveValidationError` for the resulting `400 InvalidParameter:
  submissionCriteria`.
* Webhook side-effects retry with exponential backoff, per-effect outcomes
  persist to `action_logs.side_effect_status` JSONB, failures DLQ into
  `action_log_side_effect_dlq`, admin replay endpoint, full
  W3C TraceContext injection.
* Function-backed action dispatches through `pkg/actions/goja_dispatcher.go`
  + Goja runtime; HTTP dispatcher still available as a fallback.

### Security

* Single `Engine` in `pkg/security/policy_engine.go` carries row + column +
  marking semantics, fronted by a CEL DSL evaluator and decision / policy
  caches. Wired into `pkg/oss/service_impl.go` so the gates are on the hot
  path, with integration / aggregate / BDD / bench tests.
* Row-level policy is now pushed down on the direct
  `/objects/{objectType}/aggregate` endpoint too: `handlers_aggregate.go`
  previously hit Bleve with `MatchAll`, so `count`/`sum`/`avg` leaked the
  existence and values of rows the caller's row policy forbids (only the
  column-level `PropertyFilterProvider` gate was applied). It now compiles
  the caller's policy via a `RowPolicyQueryProvider` (the same
  `policyQueryAdapter` the ObjectSet aggregate path uses) and feeds it to
  `aggregation.Engine.AggregateWithQuery` as the base query. See
  `handlers_aggregate_row_policy_test.go::TestBDD_Aggregation_RowPolicyScopesCount`.
  Strict marking-subset and row-CEL enforcement on the facet path (vs the
  loose marking-overlap clause already baked into the compiled query) remains
  a SHOULD-layer follow-up that needs an engine-level doc-ID prefilter.
* SQL Query sandbox: 30+ forbidden keywords, system-table guard, full
  tokenizer; `pgx.ReadOnly` + 5 s timeout + 10 K row cap with
  `ErrQueryTimeout` typed mapping.

### Realtime

* SSE `/api/v2/ontologies/{ont}/objectSets/{rid}/subscribe` with
  `Last-Event-ID` / `since` replay and per-user connection guard; WebSocket
  `/api/v2/ontologies/{ont}/subscriptions/ws`; frontend
  `useObjectSetSubscription` hook + Browser realtime mode +
  `ObjectSetLivePage`.
* `pkg/oss/stream_ingest.go` dedicated streaming-ingest path with
  `ValidateConstraints` on the write side.

### Indexing

* `mapping_builder.go` reads `property.typeclass` (analyzer.not_analyzed /
  keyword / english) to choose Bleve `FieldMapping`.
* `RebuildWithOptions` covers drop + recreate + estimate + batch + complete
  with progress callbacks, and round 146 added the disaster-recovery BDD
  (`rehydrate_disaster_recovery_bdd_test.go`) for the
  "wipe-dataDir → new manager → rebuild from source → queries equivalent"
  contract.

### Observability

* Business metrics: `weave_objectset_execute_duration_seconds` /
  `_load_duration` / `weave_actions_apply_duration_seconds` /
  `_applied_total` / `weave_funnel_lag_messages` (also surfaced on
  `/health/ready`).
* OTel tracing: HTTPMiddleware + BaggageMiddleware + PgxTracer +
  `pkg/funnel/tracing.go` (NATS header carrier) + outbound HTTP /
  webhook side-effect TraceContext injection.
* Audit: `pkg/audit/*` core + SIEM pipeline (`export/{exporter,syslog,s3,
  batched,tee}.go`) + `RootHashPublisher` US-266.

### Developer experience

* Python SDK: `ObjectSetBuilder` for composable ObjectSet construction;
  `AggregationAPI` / `TimeSeriesAPI` / `AttachmentsAPI`;
  `WeaveValidationError` / `WeaveVersionedLookupError` typed exceptions;
  sync + async client mirrors across the new surfaces.
* CLI: `weave action apply` / `weave aggregate` /
  `weave objectset load | create-temporary`; `docs/cli.md` got
  `### weave action apply` (L141) / `### weave aggregate` (L177) /
  `### weave objectset` (L220) sections with body templates + Northwind
  examples, guarded by `cli_docs_bdd_test.go`.
* MCP: `completion/complete` joined the `prompts/*` + `resources/*`
  surfaces (`pkg/mcp/completion.go` + `completion_ontology_source.go`).

### Testing

* `prd_v2_gap_currency_bdd_test.go` staleness CI test grew from 12 to 29
  marker cases over rounds 137 – 147; each `Gap-*` paragraph now has a
  pinned implementation token.
* `rehydrate_disaster_recovery_bdd_test.go` adds disaster-recovery e2e
  coverage.
* `pkg/apierror` regained 100% coverage with `NewNotImplemented` /
  `NewGone` / `NewAutomationRuleCycle` tests.

## 3. Breaking changes

None observable on the wire. The Phase 6 → Phase 8 work all sits behind
existing surfaces and either replaces internal stubs or adds new behavior
that defaults off:

* `?branch=` / `X-Weave-Branch` is opt-in; the absence of either header
  routes through the existing HEAD path.
* `@vN` suffix on RID is opt-in; bare RIDs continue to mean HEAD.
* `policy_engine` evaluation is no-op when no policies are configured for
  the ObjectType.
* `Goja` runtime engages only when ActionType `implementation: function` is
  set on the metadata; HTTP-dispatcher ActionTypes keep working unchanged.

## 4. Upgrade notes

* Run migrations through `000220+` (final round 137 added migrations
  `000208_geotemporal_spatial_indexes` and the Phase 6 / 7 / 8 batch from
  `000196` onward).
* Bleve indexes can be hot-rebuilt via `RebuildWithOptions`; for full
  disaster recovery `os.RemoveAll(WEAVE_DATA_DIR)` then restart the server
  — the round-146 BDD locks that path.
* SIEM destinations: configure `AuditExportConfig` in your deploy spec to
  enable syslog and / or S3 export; `RootHashPublisher` starts
  automatically when `audit.export.rootHashPath` is set.
* CI hardening (round 154): if you fork this repo and your PR exceeds 300
  changed files, `.github/workflows/ci.yml` already uses
  `--new-from-merge-base=origin/main` so `golangci-lint` survives the GH
  PR-diff REST 406. No action needed.

## 5. References

* `docs/PRD-Weave-OSv2-深度复刻-V2.md` — PRD with §4 Gap-* analysis,
  §2.1 module-completion matrix, §2.3 Palantir baseline mapping, and §6
  US-048~US-081 backlog overview.
* `docs/单机复刻 Palantir OSv2 本体层 — 完整技术架构.md` — technical
  architecture blueprint.
* `docs/Palantir Foundry Ontology Layer 完整技术蓝图.md` — Foundry
  baseline reference.
* `docs/Palantir ObjectSet & OntologyAggregation 完整语法参考.md` —
  ObjectSet / aggregation grammar reference.
