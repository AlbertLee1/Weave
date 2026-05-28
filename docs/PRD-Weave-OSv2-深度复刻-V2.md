# Weave v2 产品需求文档 (PRD)

**副标题**：从"API 形状对齐"走向"语义深度对齐"——本地完整复刻 Palantir Foundry OSv2

| 字段 | 值 |
|---|---|
| 文档版本 | v2.0 |
| 生成日期 | 2026-04-11 |
| 作者 | 项目组（综合多 agent 调研） |
| 当前分支 | `ralph/foundry-osv2-api-alignment` |
| 前序文档 | `prd.json`（US-001~047，v1）、`docs/单机复刻 Palantir OSv2 本体层 — 完整技术架构.md` |
| 状态 | Draft — 等待评审 |
| 目标读者 | Weave 研发团队、架构评审、潜在贡献者 |

---

## 0. 执行摘要

**一句话结论**：Weave 已完成了 OSv2 的 **REST API 表面形状对齐**（68/68 路由，47 个 US 全量落地），但**语义深度仍有系统性差距**——多处存在"端点已开通、底层是内存/MVP、语义未完整"的情形。本 PRD 的核心任务是把项目从 "API 已就位" 推到 "可作为单机 OSv2 参考实现部署并产出正确结果"。

**三个关键判断**：

1. **API 表面真的是 100%** — 67 条路由、MCP/Python SDK/CLI、OpenAPI、前端页面基本齐全。这是过去 30+ 天 Ralph 模式下快速推进的成果，不应被低估。
2. **语义深度约为 65~75%** — 真实能用的是 OMS/OSS/Actions/Funnel 主链路。行列级安全、Edit 冲突策略、Derived Property、Type Class、Interface 多态分页、Ontology 分支版本、GeoTemporal/TimeSeries 深度、客户端实时订阅等**都还缺"最后一公里"**，在公开 Foundry 行为参照下容易被发现差异。
3. **最大的风险不是"还有什么没做"，而是"已经做了的能否经受住相同输入下与 Foundry 语义等价"** — 因此 v2 的核心不是堆新端点，而是**加深已开通端点的语义**，并补齐 MUST/SHOULD 层的遗漏。

**关键数字**（真实 vs 声称）：

| 维度 | 声称（prd.json / progress.txt） | 真实（代码审计） | 差值 |
|---|---|---|---|
| Foundry REST 端点 | 68/68 (100%) | ≈ 68/68 (路由就位) | 0 |
| ObjectSet 定义变体 | 15/15 | 15/15 路由，但 `interfaceLinkSearchAround`/`asBaseObjectTypes` 深度未验证 | 语义差距 |
| 聚合算子 | 全覆盖 | 12 种 metric + 5 种 groupBy | 精度/近似分位数待校对 |
| 安全模型 | RBAC Phase 1 完成 | 仅 Ontology-scoped RBAC，**无行级 / 列级 / Marking 评估链路** | 语义缺 |
| 实时订阅 | WebSocket/SSE 订阅列入路线 | 已有 WebSocket `/api/v2/ontologies/{ontologyApiName}/subscriptions/ws` + SSE `/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe`；Funnel broadcast 已暴露到客户端 | 深度完善 |
| TimeSeries | 7 端点 + PG/VM 存储 | PG 存储、VictoriaMetrics 存储、transform/resample 下推、Timescale CAGG 与 Vertex window aggregation 已有；深度缺口在 calendar-aware bucket、retention policy、multi-resolution materialization | 深度完善 |
| GeoTemporal | 2 端点 + 存储 | PG `geotemporal_values` 持久化，缺聚合/订阅深度 | 深度缺 |
| Function backed action | Tier 3.2 声称完成 | HTTP dispatcher 存在，**无内嵌运行时**（Goja/Wasm） | 深度缺 |
| Type classes | 语法支持 | **不驱动 Bleve 索引映射**（analyzer.not_analyzed 等未生效） | 行为缺 |
| Ontology 分支/版本 | Snapshot 存在 | **无分支 / 无语义版本链路 / RID 不含 version** | 结构缺 |
| Edit 冲突解决 | — | **无策略**（隐式 last-write-wins） | 策略缺 |

---

## 1. 背景与定位

### 1.1 项目定位

Weave 是一个**单机开源的 Palantir OSv2 服务与上层体验复刻**，目标读者有三类：

- **学习者**：想在本地跑一个 "小号 Foundry" 来理解 Ontology、ObjectSet、Action、Funnel 这些概念。
- **评估者**：想用 Foundry 同形状的 API 来做架构决策、前后端隔离实验，不希望被 SaaS 绑架。
- **小规模生产**：在单机/单团队场景下，以 Weave 作为 Ontology 层运行真实业务。

**非目标**：替代企业级多租户、大集群、完整 AIP Logic、Workshop/Slate 全套产品面；Vertex、Quiver、Dashboard、通知/协作等上层体验已经作为本地 OSv2 工作流的一部分落地，但仍按 MVP 深度管理。

### 1.2 技术栈（已锁定）

- **后端**：Go 1.22+ / chi router / pgx / JetStream / Bleve / DuckDB（按需）
- **存储**：PostgreSQL 16 为元数据、pgvector 为向量、Parquet 为冷存档（规划中）
- **前端**：React 19 / Vite / Tailwind / TanStack Query / Zustand
- **SDK**：Python (httpx + pydantic)；CLI (Go)；MCP stdio/HTTP
- **认证**：dev / token / JWT (RS256) + API Key (`wvk_*`)

### 1.3 v1 成果快照（US-001 ~ US-047）

过去迭代以 "Rip-and-Replace" 的强手段把项目的 API 表面从 **29% (20/68)** 拉到 **100% (68/68)**：

| Phase | US 范围 | 核心产出 | 交付日 |
|---|---|---|---|
| Phase 1 | US-001 ~ US-024 | 54→37 端点就位 / Action options / 删除 54 个 admin 路由 / OpenAPI 重写 / 生产硬化 | 2026-04-11 |
| Phase 2 | US-025 ~ US-031 | Interface metadata/data 端点 / ObjectSet 6 变体 / applyWithOverrides | 2026-04-11 |
| Phase 3 | US-032 ~ US-036 | Attachment / MediaReference 全链路 | 2026-04-11 |
| Phase 4 | US-037 ~ US-043 | TimeSeries / GeoTemporal / Cipher / Transaction / SQL Query → 68/68 | 2026-04-11 |
| Phase 5 | US-044 ~ US-047 | 多本体隔离 / OMS 缓存 / nearestNeighbors / AI MCP tools | 2026-04-11 |

**必须承认的事实**：以 US 为单位的节奏让"可演示的路由"先完成，这是正确的选择；但也意味着**每个 US 的验收标准更多是"这条路线能返回 200 且结构正确"，而不是"在 Foundry 同输入下语义等价"**。v2 PRD 需要补齐这一半。

---

## 2. 项目现状分析

### 2.1 真实的模块完成度矩阵

> 下表综合了代码 grep、测试 grep、文件列表、迁移表、三份 agent 报告。绿色=端到端可用；黄色=有骨架但语义不完整；红色=缺失。

| 层 | 模块 | API 表面 | 存储/持久 | 语义深度 | 综合 | 备注 |
|---|---|:---:|:---:|:---:|:---:|---|
| OMS | 元数据 CRUD | 🟢 | 🟢 PG + 缓存 | 🟢 | **95%** | Ontology/ObjectType/LinkType/ActionType/Interface/ValueType/QueryType 全 CRUD |
| OMS | Snapshot / 版本 | 🟢 | 🟢 | 🟢 | **90%** | `ontology_branches` 表（migration 000024 + 000091 parent_tx）+ RID `@vN` suffix（`pkg/rid` splitVersionSuffix）+ `?branch=` / `X-Weave-Branch` header（`pkg/oms/branch_scope.go`）+ 8 Get 端点 typed `501 VersionedLookupNotSupported`；Gap-T4 全部 4 块 done |
| OMS | SharedProperty / TypeGroup | 🟢 | 🟢 | 🟢 | **90%** | 表与 CRUD 在；round 54 DeleteSharedProperty 在使用中拒绝 (409 SharedPropertyInUse + usageCount)；round 55 CreateProperty 新增 `sharedPropertyTypeApiName` + baseType/isArray 强校验（400 SharedPropertyTypeMismatch 含双向 diff）；round 58 DeleteTypeGroup 在使用中拒绝 (409 TypeGroupInUse + usageCount) 镜像 round-54 防悬空 object_type_groups assignment 行；仍未对 Interface 自动属性 / Search analyzer 联动 |
| OSS | Where DSL | 🟢 | n/a | 🟢 | **90%** | 15+ 子句类型，Bleve query compile |
| OSS | ObjectSet 15 变体 | 🟢 | n/a | 🟡 | **70%** | base/filter/union/intersect/subtract/searchAround/static/reference/nearestNeighbors/asType/asBaseObjectTypes/interfaceBase/withProperties/interfaceLinkSearchAround/methodInput 路由就位；**深度不一** |
| OSS | Search / Load / Count | 🟢 | 🟢 Bleve | 🟢 | **90%** | select 强制、cursor 分页 |
| OSS | Linked Objects (FK / M2M) | 🟢 | 🟢 | 🟡 | **80%** | FK forward+reverse 均 OK，M2M join_table OK。**M2M 在 ObjectSet 内部 searchAround 仍待验证** |
| OSS | Aggregation | 🟢 | 🟢 Bleve facet | 🟢 | **95%** | count/sum/avg/min/max/stddev/variance/approxDistinct/approxPercentile + 5 种 groupBy；**Phase 6 Gate**: 多层 groupBy 稳定性 + ACCURATE/APPROXIMATE accuracy badge 覆盖 (US-039 + Playwright `aggregation-multi-groupby.spec.ts`) |
| OSS | withProperties (derived) | 🟢 | n/a | 🟢 | **90%** | **Phase 6 Gate**: count/sum/avg/min/max 全端到端通过 (US-003, US-004, US-005, US-040)；composite-cursor 分页稳定性锁定；Playwright `withproperties-derived.spec.ts` 绿 |
| OSS | nearestNeighbors (KNN) | 🟢 | 🟢 pgvector | 🟡 | **80%** | 单字段+多字段（min-distance / RRF 两种 fusionStrategy）；仍无混合搜索（BM25+vector） |
| OSS | Interface 多态 Load | 🟢 | 🟢 | 🟢 | **90%** | **Phase 6 Gate**: 多态 Load + composite/multi-type cursor + heap merge 全绿 (US-006..US-008, US-041)；Playwright `interface-multitype-paging.spec.ts` 驱动 3-type Northwind HasOwner interface paging |
| OSS | ObjectSet 持久化 | 🟢 | 🟢 PG `saved_object_sets` | 🟢 | **85%** | temporary TTL 通过 store；`createTemporary` 已接入 |
| Actions | 参数 / 规则 / 编辑生成 | 🟢 | 🟢 | 🟡 | **80%** | 规则引擎能 run；**submission criteria 表达力浅**，无内嵌脚本 |
| Actions | applyBatch / applyWithOverrides | 🟢 | 🟢 | 🟢 | **90%** | atomic/bestEffort 两种模式；**Phase 6 Gate**: optimistic concurrency + user-edit-wins + edit-only ingest 全链路 (US-035..US-037)；Playwright `optimistic-concurrency.spec.ts` + `editonly-ingest.spec.ts` 绿 |
| Actions | Function-backed | 🟢 HTTP + Goja | 🟢 | 🟢 | **90%** | `pkg/functions/goja_runtime.go` 用 `dop251/goja` 嵌入式沙箱 JS runtime（US-218 + US-476）+ ontology 客户端 shim（`goja_shim_functions.go`）+ progress（`goja_shim_progress.go`）+ cache + typed errors；`pkg/actions/goja_dispatcher.go` 路由 Function-backed action（US-066）；`pkg/queryexec/goja.go` executeQuery via function（US-067）；HTTP dispatcher 仍可用作 fallback；Gap-A5 Phase 8 W1 全 ✅ |
| Actions | Side effects | 🟢 | 🟢 | 🟡 | **60%** | 结构体存在；**webhook 通知未验证**，无重试 |
| Funnel | Publisher / Consumer | 🟢 | 🟢 JetStream | 🟢 | **90%** | per-ontology subject、DLQ、offset、broadcast |
| Funnel | 实时客户端订阅 | 🟢 | 🟢 replay tail | 🟡 | **70%** | `pkg/funnel/broadcast.go` 经 `pkg/oss/subscribe_sse.go` 暴露 SSE ObjectSet 订阅；`pkg/subscriptions` 提供 WebSocket 订阅；剩余深度在跨节点 fan-out 与更完整断线恢复矩阵 |
| Indexing | per-ObjectType Bleve | 🟢 | 🟢 filesystem | 🟢 | **90%** | 增量更新 OK；**Phase 6 Gate**: TypeClass (analyzer.not_analyzed / keyword / english) 驱动 Bleve field mapping 全端到端 (US-001, US-012)；not_analyzed 路径由 US-040 FK-link resolver 闭环 |
| Indexing | Funnel ↔ Index 一致性 | 🟢 | 🟢 | 🟡 | **75%** | Consumer 同步更新 Bleve；**rehydrate 路径存在但无 offset 回放测试矩阵** |
| Indexing | Parquet 冷存 | 🔴 | — | 🔴 | **5%** | 技术架构文档计划有，当前未落地（没有 pkg/dataset/parquet_writer 真实链路） |
| Auth | dev / token / JWT | 🟢 | 🟢 | 🟢 | **90%** | RS256、refresh token 轮换、bootstrap admin |
| Auth | API Key | 🟢 | 🟢 | 🟢 | **90%** | `wvk_` 前缀、SHA-256 hash、revoke、last_used |
| Auth | RBAC (4 role × 26 perm) | 🟢 | 🟢 | 🟡 | **70%** | 全局 + per-ontology 角色；**未参与行/列/Marking 级过滤** |
| Security | Marking / 分级 | 🟢 | 🟢 | 🟢 | **90%** | 已合并进 `pkg/security/policy_engine.go`（`SetMarkingsEnabled` / `MarkingsEnabled` / `AllowedForIngest`），user-context markings 从 `auth.User.Attributes[MarkingsAttributeKey]` 注入，贯穿 row + property 决策；`auto_marking_test.go` 覆盖继承；无单独 marking_filter.go（Gap-S3 done） |
| Security | Object / Property Security Policy | 🟢 | 🟢 | 🟢 | **90%** | `pkg/security/policy_engine.go::Engine.Evaluate` (RLS query AND 进主链路) + `AllowedProperties` (column-level) + CEL DSL (`cel_evaluator.go`) + decision/policy 缓存（`decision_cache.go` + `policy_cache.go`）；挂在 `pkg/oss/service_impl.go`；row / aggregate / CEL 三套 integration_test + BDD + bench（Gap-S1 + S2 done） |
| Security | Edit conflict / concurrency | 🟢 | 🟢 `object_history` | 🟢 | **90%** | **Phase 6 Gate**: user-edit-wins / most-recent-timestamp + optimistic version check + edit-only property 全链路 (US-035..US-037)；Playwright 双 context 竞争场景覆盖 |
| Types | 21 基础 + 强制转换 | 🟢 | n/a | 🟡 | **80%** | Vector / Struct / Attachment / MediaRef / Cipher / TimeSeries / GeoTemporal 都能声明；**Struct 嵌套深度与校验未完整** |
| Types | TypeClass | 🟢 存储 | 🟢 `properties.type_config` | 🟢 | **85%** | **Phase 6 Gate**: type_config analyzer hints (not_analyzed / keyword / english) 通过 `pkg/index/mapping_builder.go` 注入 Bleve FieldMapping；FK link resolver 依赖此路径 (US-001, US-012, US-040) |
| Special | Attachment Blob | 🟢 | 🟢 本地 | 🟢 | **85%** | 4 全局端点 + 4 property 端点；无 S3/minio 后端 |
| Special | MediaReference | 🟢 | 🟢 | 🟢 | **80%** | 3 端点就位 |
| Special | TimeSeries | 🟢 7 端点 | 🟢 PG `timeseries_points` + optional VM | 🟡 | **72%** | `/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}` 提供 Vertex window aggregation；`/firstPoint`、`/lastPoint`、`/streamPoints`、`/points` 提供基础读写；`/api/v2/ontologies/{ontologyApiName}/timeseries/transform` 支持链式 transform + `resample`；`pkg/oss/handlers_timeseries_transform.go` 会把单步 resample 下推到 `pkg/timeseries/downsample.go` 的 `DownsampleSpec` / `DownsamplePoints`；`pkg/timeseries/pg_store.go` 使用 `timeseries_cagg_5min` + `RunCAGGRefreshLoop`，`pkg/timeseries/vm_store.go` 的 `NewVMStore` 走 VictoriaMetrics `query_range`；支持 `avg/sum/min/max/count/first/last`，remaining depth gaps 是 calendar alignment、retention policy、multi-resolution materialization 与生产调优 |
| Special | GeoTemporal | 🟢 2 端点 | 🟢 PG `geotemporal_values` | 🟡 | **60%** | latestValue / streamHistoricValues 由 `cmd/server/main.go` 在 PG 可用时接入 PG-backed `PgStore`；无 PG 时使用 in-process MemoryStore as degraded mode |
| Special | CipherText (AES-GCM) | 🟢 | 🟢 | 🟢 | **80%** | decrypt 端点 + 信封加密 |
| Special | Transaction (preview) | 🟢 | 🟡 | 🟡 | **65%** | `/transactions/{id}/edits` 已就位；round 59 新增 `GET /transactions/{id}` 与 `DELETE /transactions/{id}` （both 需 `?preview=true`，DELETE 幂等），SDK 可读回累计 edits 与 abort 实验。仍未与 Action commit/atomic-apply 集成 |
| Special | SQL Query | 🟢 | 🟢 | 🟢 | **90%** | execute 端点 + `pkg/sqlqueries/safety.go::ValidateQuery` 全 SQL tokenizer（白名单 SELECT / WITH + 黑名单 30+ 关键字 + 系统表防御 + stacked-statement 防御）；`pkg/sqlqueries/engine.go::PGEngine` 强制 `pgx.ReadOnly` + `context.WithTimeout`（5s 默认 US-468）+ MaxRows 流式截断（10K）+ `ErrQueryTimeout` 类型化映射（Gap-S5 done） |
| Observability | Prometheus metrics | 🟢 | 🟢 | 🟡 | **65%** | 基础 metric；**无业务指标 dashboard** |
| Observability | OpenTelemetry | 🟢 | 🟢 | 🟢 | **85%** | `pkg/tracing` 完整 Init（otlp/stdout/none 三种 exporter）+ HTTPMiddleware（chi route 模板做 span 名，5xx 翻 status Error）+ BaggageMiddleware（request_id/user_id 注入）+ PgxTracer（DB 查询 span）+ `pkg/funnel/tracing.go` 跨 NATS 注入/抽取 TraceContext；round 52 HTTPDispatcher 出站 W3C TraceContext 注入；round 53 webhook side-effect 出站注入（context-aware retry loop + 每次 attempt 一个 client-kind span）。剩余深度是外部 sampler 配置矩阵与多租户 trace 隔离 |
| Observability | Audit log | 🟢 `audit_events` | 🟢 PG + hash chain | 🟡 | **78%** | `pkg/audit` 的 `AuditEvent` / `NewPGStore` 对应 `migrations/000020_audit_events.up.sql`；`migrations/000062_audit_hash_chain.up.sql` 加 `chain_seq` / `prev_hash` / `entry_hash`，`VerifyChain` 与 `cmd/weave-audit-verify` 校验链路，`RootHashPublisher` 可锚定 root hash，`RedactingStore` 处理 GDPR redaction；`cmd/server/admin_audit.go` 暴露 `/api/v2/admin/auditEvents` 与 `/api/admin/audit`，支持 `resourceRid`；`pkg/oms/audited_repository.go` 的 `NewAuditedRepository` 记录 OMS metadata create/update/delete；`migrations/000061_object_type_data_access_audit.up.sql` + `pkg/oss/data_access_audit.go` / `cmd/server/data_access_audit_adapter.go` 的 `NewDataAccessAuditor` 记录 `data.access`；`pkg/auth/login_handler.go`、`pkg/auth/refresh_handler.go`、`pkg/auth/api_key_handlers.go` 覆盖 `login_failed` / `token_refresh` / `api_key_create`；remaining depth gaps 是 policy-change breadth、SIEM/retention deployment hardening 与 audit UX aggregation |
| DevOps | Docker + compose | 🟢 | 🟢 | 🟢 | **90%** | 多阶段 Dockerfile、weave service、health probes |
| DevOps | 迁移 / 回滚 | 🟢 | 🟢 | 🟡 | **70%** | 17 个迁移；**回滚脚本覆盖率未评估** |
| 前端 | 页面（Dashboard/Explorer/Browser/ObjectSet/Action/Aggregation/Login） | 🟢 | n/a | 🟡 | **70%** | 主流程可用；**测试覆盖率偏低（22 test 对 40+ component）** |
| 前端 | 实时 & 订阅 | 🟢 | n/a | 🟡 | **65%** | `web/src/hooks/useObjectSetSubscription.ts`、`web/src/components/browser/BrowserPage.tsx` realtime mode、`web/src/components/objectsets/ObjectSetLivePage.tsx` 已接 SSE/WS；剩余是大规模断线恢复和可观测性 polish |
| 前端 | AIP 助手 | 🟡 | n/a | 🟡 | **40%** | semantic search + MCP 工具在后端；前端未暴露交互 |
| 上层体验 | Vertex graph/scenario workspace | 🟢 | 🟢 PG + memory fallback | 🟡 | **70%** | `web/src/vertex` 提供 `/vertex/:rid` workspace；`pkg/vertex/graphsvc` 提供 `/api/vertex/v1/graphs`、share-links 与 widget surface；`pkg/vertex/scenarioruns` + `migrations/000105_vertex_scenarios.up.sql` / `migrations/000109_vertex_scenario_runs.up.sql` 覆盖 scenarios 与 scenario_runs；remaining depth gaps 是 scenario-run server wiring breadth、diagramming/ops polish 与大图性能 |
| 上层体验 | Quiver time-series workbench | 🟢 | 🟢 PG | 🟡 | **72%** | `web/src/components/quiver` 提供 `/quiver/:ontology` 与分享视图；`pkg/quiver` 提供 dashboard CRUD、`/api/v2/quiver/dashboards/{rid}/data` 与 `/sparklines`，由 `cmd/server/quiver_timeseries_adapter.go` 接 TimeSeries store；remaining depth gaps 是跨 dashboard 模板、告警联动与大规模多序列缓存 |
| 上层体验 | Dashboards / notifications / reactions / permission requests | 🟢 | 🟢 PG | 🟡 | **75%** | `pkg/dashboards` 提供 `/api/v2/dashboards` 全 CRUD + round 62 `POST /api/v2/dashboards/{id}/duplicate` (Foundry "Duplicate" 菜单契约：源可见性按 owner-or-public，clone 始终归当前调用者且 IsPublic 重置，名字自动添加 "(copy)"/"(copy 2)"/"(copy 3)" 后缀避免 409)；`pkg/notifications` + OMS `/api/v2/notifications` 支持通知中心与 fan-out；`pkg/reactions` 提供 `/api/v2/reactions` 给 ObjectDetail ReactionBar；`pkg/permissionrequests` 提供 `/api/v2/permission-requests` request-access workflow + round 63 `DELETE /api/v2/permission-requests/{id}` (Foundry "Cancel request" 契约：仅 requester 本人可撤销 pending 行，soft-cancel 转入 CANCELLED 终态保留审计轨迹，已 decided 行返回 409，非 requester 返回 403，admins 用 reject 不能 cancel)；remaining depth gaps 是审计串联、批量治理体验与通知渠道生产化 |
| SDK (Python) | 核心 CRUD + Action | 🟢 | n/a | 🟢 | **80%** | 核心齐全、iter_all 支持 |
| SDK (Python) | ObjectSet 组合 DSL | 🟡 | n/a | 🔴 | **40%** | 仅 raw dict，无 Pythonic builder |
| SDK (Python) | Aggregation | 🟢 | n/a | 🟢 | **80%** | builders.py 暴露完整 metric (count/sum/avg/min/max/approxDistinct/exactDistinct/stddev/variance/collectList/approxPercentile)+ groupBy (exact/fixedWidth/range/duration) helpers + `parse_aggregation_response` 拆出 accuracy / data / sub-aggs；剩余 topValues/geohash groupBy 与 having clause 仍是 raw dict |
| SDK (Python) | Transactions (preview) | 🟢 | n/a | 🟢 | **90%** | round 60 sync `client.transactions` + round 61 async `WeaveAsyncClient.transactions` 镜像：`append_edits` / `get` / `abort` 三方法包装 OntologyTransaction preview 端点，dataclass `Transaction` + `TransactionAppendResponse` 复用，`?preview=true` 自动附着，`abort` 幂等；剩余 commit 端点待 server 端 commit/atomic-apply 落地 |
| CLI | auth / ontology / object | 🟢 | n/a | 🟢 | **80%** | 基础齐全、JSON/表格输出 |
| CLI | action / aggregate / objectset | 🟢 | n/a | 🟡 | **65%** | `cmd/weave-cli/cmd_action.go` 暴露 `weave action apply`；`cmd_aggregate.go` 暴露 `weave aggregate`；`cmd_objectset.go` 暴露 `weave objectset load` / `weave objectset create-temporary`；`cmd/weave-cli/cli_us304_test.go` 覆盖命令契约；remaining depth gaps 是更高阶 helper、别名、发现文档和输出 polish |
| MCP | 7 基础 + 4 AI 工具 | 🟢 | n/a | 🟢 | **84%** | `docs/mcp.md` 记录 HTTP `/mcp`、`prompts/list` / `prompts/get`、`resources/list` / `resources/read` / `resources/subscribe` / `resources/unsubscribe`；实现位于 `pkg/mcp/prompts.go` 与 `pkg/mcp/resources.go`，资源 URI 包含 `weave://objecttype/<ontology>/<objectType>`；剩余是 sampling 与部署认证 polish |
| MCP stdio 独立二进制 | 🟢 bridge | — | 🟡 | **60%** | `cmd/weave-mcp/http_bridge.go` 在 `WEAVE_MCP_URL` 存在时提供 stdio HTTP bridge，转发到运行中的 `/mcp` 并复用 tools/prompts/resources；remaining local-standalone gap 是不自启 PG/NATS/Bleve |

**总评（加权）**：**Weave 整体完成度 ≈ 72%**。其中：

- **已达到 Foundry 同形状** 的模块：**OMS / Auth / Funnel 后端 / Attachment / CipherText / 前端主页面**（85%+）
- **语义未完全对齐** 的模块：**OSS 高级 ObjectSet / Aggregation / Interface 多态 / Security 应用 / Action 冲突策略 / TypeClass / TimeSeries/GeoTemporal 持久与聚合 / SQL Query 沙箱**（50~75%）
- **缺失或存根** 的模块：**分支与版本 / Parquet 冷存 / Function 运行时 / Derived property 真正计算 / 行列级安全应用**（0~35%）

### 2.2 "声称 vs 真实"差异清单（需要在 v2 诚实面对）

| 声称 | 真实 | 差距来源 |
|---|---|---|
| "Phase 4 gate: **100% Foundry 对齐** 68/68" | 路由 68/68 就位；**端点语义与 Foundry 不等价**的至少 10+ 个 | 上表黄红部分 |
| "Phase 5: RBAC 完成" | Ontology 作用域 + role-permission 矩阵完成；**行/列/Marking 未纳入查询过滤** | `pkg/oss/policy_filter.go` / `marking_filter.go` 存在但未接主链路 |
| "nearestNeighbors + MCP AI tools 交付" | 单字段 KNN + 4 MCP tool；**无混合检索 / 无 reranking / 无跨 ObjectType** | `pkg/oss/objectset/nn.go` 仅 PropertyIdentifier |
| "TimeSeries / GeoTemporal 存储后端" | TimeSeries 有 PG store、VictoriaMetrics store、transform/downsample pushdown 与 Vertex window aggregation；GeoTemporal 也有 `pkg/geotemporal/pg_store.go` | TimeSeries 基础读写路由、`/timeseries/transform`、`DownsamplePoints`、`timeseries_cagg_5min`、VM `query_range` 已落地；GeoTemporal 使用 `migrations/000205_geotemporal_values.up.sql` 持久化，并由 `migrations/000208_geotemporal_spatial_indexes.up.sql` 加强 bbox + 时间过滤索引 |
| "Function-backed Actions" | HTTP dispatcher 可用；**无内嵌运行时**，依赖外部 function server | `pkg/actions/function_dispatcher.go` + `http_dispatcher.go` |
| "Edit → NATS → Bleve → Broadcast" | 后端管线通，并已通过 SSE/WS 暴露给客户端；深度风险在多实例广播、replay window 和断线恢复矩阵 | `pkg/funnel/broadcast.go`、`pkg/oss/subscribe_sse.go`、`pkg/subscriptions` |
| "Interface 完整" | 元数据 CRUD + 多态查询端点；**跨子类型排序 + 分页稳定性未测试** | 无集成测试覆盖"多 ObjectType 实现同一 interface 的 load + sort"路径 |

**这份差异不是给项目打分的，是告诉我们下一阶段的真正战线在哪里**。

### 2.3 Palantir 基线对齐（MUST/SHOULD/MAY 映射）

参见 Palantir baseline agent 的 §4 分层，映射到 Weave 现状：

| 基线 | Palantir 条目 | Weave 现状 | 需要做什么 |
|---|---|:---:|---|
| MUST 1 | OMS CRUD + RID | 🟢 | — |
| MUST 2 | ObjectSet 代数 + createTemporary | 🟢 | 加深 interfaceBase / asBaseObjectTypes 测试 |
| MUST 3 | Load/Search/Get + cursor + orderBy + select + filter DSL | 🟢 | — |
| MUST 4 | Aggregation 6 metric × 4 groupBy | 🟢 | 多层嵌套稳定性测试 + 近似精度基准 |
| MUST 5 | Action pipeline: params→rules→Edit→NATS→Bleve | 🟢 | 加 conflict 策略 |
| MUST 6 | applyAction + applyActionBatch + returnEdits + 冲突策略 | 🟡 | **加 user-edit-wins 策略** (新 US) |
| MUST 7 | per-ObjectType 全文索引 | 🟡 | **TypeClass 驱动 field mapping** (新 US) |
| MUST 8 | 声明式 FK 链接 | 🟢 | M2M-in-ObjectSet 集成测试 |
| MUST 9 | 稳定 /api/v2/ontologies 形状 | 🟢 | — |
| MUST 10 | dev+token auth + 策略挂接点 | 🟢 | **策略评估落地** (见 SHOULD 9) |
| SHOULD 1 | Interface + shared property 映射 + 多态 Load | 🟡 | **多态稳定性** (新 US) |
| SHOULD 2 | Derived property / withProperties (≥1 hop 聚合) | 🔴 | **真正实现 withProperties 计算** (新 US) |
| SHOULD 3 | Semantic search nearestNeighbors | 🟡 | **多字段 / 混合检索** (新 US) |
| SHOULD 4 | Change subscription (WebSocket/SSE) | 🟡 | 已有客户端订阅端点；补跨节点 fan-out、恢复矩阵与运维指标 |
| SHOULD 5 | TypeClass (analyzer / hubble / hierarchy) | 🔴 | **落地 type class 行为** (新 US) |
| SHOULD 6 | Action log (持久审计) | 🟢 | 扩展到元数据/权限变更 |
| SHOULD 7 | Query functions (executeQuery) | 🟡 | **Goja/Wasm runtime** (新 US) |
| SHOULD 8 | Streaming ingest (NATS subject 上做 ingest) | 🟡 | **NATS ingest 端点 + subject 规范** (新 US) |
| SHOULD 9 | 行/列/Marking 过滤 | 🔴 | **Granular policy 执行引擎** (新 US) |
| SHOULD 10 | Ontology 只读分支 + semver | 🔴 | **branch + version 挂到 RID** (新 US) |
| MAY 1-10 | 企业级多租户 / 完整 AIP Logic / Workshop / Slate 全套产品面 | 🔴 | 明确排除或单独立项；Vertex/Quiver/Dashboard/协作通知已作为本地上层体验 MVP 纳入现状矩阵 |

---

## 3. 目标与非目标

### 3.1 v2 核心目标

1. **把语义深度从 ≈ 72% 推到 ≈ 90%**，其中 MUST 层 100%、SHOULD 层 ≥ 70%。
2. **建立"Foundry 等价行为测试套件"**：选定一套代表性输入，对每个已对齐端点运行"Weave vs 期望输出"的对照测试（期望来自公开 SDK 示例、Palantir docs 例子）。
3. **把安全模型做成可演示的端到端链路**：从 JWT → role → marking → policy → 查询过滤，至少一条路径可见、可测。
4. **深化实时订阅**：已能从 Browser/ObjectSet Live 订阅 ObjectSet 变更事件；下一步补跨节点 fan-out、断线恢复矩阵与运维指标。
5. **把 Function-backed action 做成"可本机嵌入 + 可 HTTP 外挂"**，不要只有 HTTP dispatch。

### 3.2 明确的非目标

1. ❌ 不做 Workshop / Slate 全套产品面；Vertex / Quiver / Dashboard / 通知协作已作为本地上层体验 MVP 落地，本阶段只补状态文档与语义深度，不扩成全套产品线。
2. ❌ 不做企业级多租户 / 组织层级 / Project / Restricted View。
3. ❌ 不追求 AIP Logic 完整块图；AIP 能力保持"通过 MCP + nearestNeighbors 提供 AI 调用入口"即可。
4. ❌ 不引入 Kubernetes 依赖；单机 / docker-compose 为一等公民。
5. ❌ 不做 TypeScript/Java OSDK 代码生成器（Python SDK + CLI + MCP 够用）。
6. ❌ 不追求 exactly-once 流处理，at-least-once 可接受。

### 3.3 本阶段成功的标志

- `make test && make test-integration && make web-test && pytest sdk/python` 全绿；
- 新增 `make test-contract` 命令运行 "Foundry 行为等价矩阵" ≥ 100 个样例；
- 新增 `pkg/security/policy_engine.go` 完整 policy evaluation 链，**至少 10 个端到端集成测试覆盖 row/column filter**；
- 已有 `/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe` SSE 端点、`/api/v2/ontologies/{ontologyApiName}/subscriptions/ws` WebSocket 端点，前端有 Browser realtime mode 与 ObjectSet Live 页；
- `pkg/geotemporal/pg_store.go` + `pkg/timeseries` 时间分桶聚合，`/timeseries/transform` 下推到 PG/VM downsampler，`make bench` 基准存在；
- `pkg/functions/` 新包内嵌 Goja，可执行 TS-like 函数；
- `docs/CHANGES-v2.md` 记录 v2 所有改动与 breaking changes。

---

## 4. 差距分析（按层）

### 4.1 查询层（OSS）

**Gap-Q1 — Interface 多态稳定性**
- 现状：✅ 已落地。`pkg/oss/pagination/composite_cursor.go` 实现 `{objectTypeApiName, innerCursor}` composite cursor，`pkg/oss/objectset/us463_interface_cursor_stability_test.go` + Playwright `interface-multitype-paging.spec.ts` 覆盖 3-type Northwind HasOwner interface paging；多子类型按 sort key heap merge 稳定，分页不再漏或重。
- 剩余：表 §3 行 99 已记录 90%；Phase 6 Gate 通过 (US-006..US-008, US-041)。

**Gap-Q2 — withProperties 真实计算**
- 现状：✅ 已落地。`pkg/oss/objectset/executor.go::executeWithProperties`（101 行实现）真正执行跨 link 计算并附到 `Result.DerivedValues`，`executeWithPropertiesPolymorphic` 处理 Interface 多态路径；`handler_aggregate_derived.go::aggregationNeedsDerivedPath` 决定是否走 derived path 让 metric 引用 derived property；
  - **单 hop**：`withproperties_test.go` 覆盖 count / sum / avg / min / max + 反向 link count + 空集 / 类型不匹配 / 缺字段 / cursor stability / metric 校验共 12 子用例。
  - **公式表达**：`withproperties_formula_test.go` 覆盖 FullName 组合 / 算术 / 多 DP / 校验缺失 formula 共 5 子用例。
  - **反向语义**：`withproperties_reverse_test.go` 锁定反向 link 计算（reportsCount 这类）。
  - **derived 排除**：`aggregate_derived_us382_test.go` 锁定 derived-excluded items 行为。
  - **lineage 集成**：`handler_lineage_test.go::TestObjectSetLineage_WithPropertiesAggregation` 把 derived 列入 lineage。
  - **多 hop / M2M**：multi-hop searchAround（US-366）+ M2M traversal（US-210）接入 `ErrQueryTooLarge` 防爆。
- 剩余：自定义 reducer DSL（除内置 5 类外）保留为 SHOULD 层；二 hop 以上 cross-link 聚合已通过 multi-hop searchAround + withProperties 组合可达。

**Gap-Q3 — Aggregation 多层嵌套 + 精度标记**
- 现状：✅ 已落地。multi-groupBy end-to-end 覆盖在 `test/foundry_parity/us015_multi_groupby.json` (105-doc fixture: 5 countries × 3 quarters × 7 prices = 105 orders) 驱动 `test/integration/aggregation_multigroupby_test.go::TestMultiGroupBy_NorthwindOrders`，三层 groupBy `ExactValue × FixedWidth × Duration` 组合走通 PG-backed OMS → `pkg/index.BuildMapping` → `pkg/oss/aggregation.Engine` 全链路，leaf 行的 count/sum/avg metrics 与手算 expected 一一对齐；`pkg/oss/aggregation/multi_groupby_test.go` 三个单测覆盖嵌套键 shape (`TestMultiGroupBy_ThreeLayerNested`) / 稳定 bucket order (`TestMultiGroupBy_StableBucketOrder`) / null group key 行为 (`TestMultiGroupBy_NullGroupKey`)；`accuracy=APPROXIMATE` 标记的触发条件在 `pkg/oss/aggregation/accuracy_test.go::TestAggregationAccuracyMarker` 6 子场景里全部断言 (simple avg / standardDeviation / approximatePercentile / groupBy + truncated leaf 触发 APPROXIMATE；count-only / fits-all-docs 保持 ACCURATE)。
- 剩余：documentation 已对齐 implementation；结构性无缺口，进一步的 cube/rollup 风格回归补在 `pkg/oss/aggregation/cube_rollup_test.go`。

**Gap-Q4 — nearestNeighbors 多字段 / 混合搜索**
- 现状：✅ "filter then KNN" 已支持（CandidatePKs 路由）；✅ 多 vector column（`PropertyIdentifiers []PropertyIdentifier`）已支持（round 49）；✅ `fusionStrategy` 选择 `min`（默认）或 `rrf`（Reciprocal Rank Fusion，k=60）已支持（round 50）。
- 仍缺：BM25 + vector 的真·混合检索；cross-encoder reranking；自定义权重融合。
- 影响：当前已能支持 "搜两列向量按 RRF 重排" 这类 Foundry 写法；缺的混合检索属于 Foundry SHOULD 层，不阻塞 MUST 对齐。

**Gap-Q5 — ObjectSet 跨 ontology 不支持**
- 现状：Definition 里没有 ontology 字段，单一执行上下文。
- 影响：企业级 "一个 action 跨两个域" 写法不支持。
- 建议：v2 不做跨域，v3 再议。文档明确。

### 4.2 写入层（Actions）

**Gap-A1 — Edit 冲突解决策略**
- 现状：✅ 已落地。Edit payload 携带 `source: user | funnel | edit_only`，`pkg/actions/edit_source_test.go` 覆盖 user-edit-wins 路径：Funnel ingest 在 `pkg/funnel/consumer.go` 应用前比对 `object_history` 最近一次 user-source edit 的 timestamp，若用户 edit 更新则跳过；edit-only property 走单独路径 (US-035..US-037)。
- 剩余：`always_apply` 覆盖开关、跨节点 conflict 仲裁（多机部署）保留为 Foundry SHOULD 层。

**Gap-A2 — Optimistic concurrency**
- 现状：✅ 已落地。apply 路径接受 `options.expectedVersion`（HTTP body）+ `If-Match` header (US-035 / US-471)，`pkg/actions/optimistic_test.go` + `pkg/actions/us471_optimistic_multitarget_test.go` 覆盖单对象与多对象批量；mismatch 返回 409 `OptimisticVersionConflict` 携带 actualVersion 用于客户端冲突合并；Playwright `optimistic-concurrency.spec.ts` 验证双 context 同时编辑场景。
- 剩余：跨批次"intent token"风格的语义合并保留 Foundry SHOULD 层。

**Gap-A3 — Submission criteria 表达力**
- 现状：✅ 已落地（rounds 133 / 134 / 135 / 136 + Gap-A3 partial）。`pkg/actions/criteria.go` 在基础 `parameterMatch` / `always` 之外补齐了三块表达力：
  - **`parameterCompare`** 跨字段比较运算（`gt` / `gte` / `lt` / `lte` / `eq` / `neq`）：`parameterCompareValue` (criteria.go:69) 与 `evaluateSingleCriteria` 分发（`case "parameterCompare"` criteria.go:129）覆盖 "参数 A > 参数 B" 等约束（commit 9bd0f2b）。
  - **`and` / `or` / `not`** 复合 group criteria（commit c8bb4ba），构成完整 boolean 代数，可任意嵌套上面两类原子。
  - **入口校验 + SDK 闭环**：admin save 时结构化 reject 不合法 criteria 树（commit a0a8079），SDK 端伴随 `WeaveValidationError` 类型化 400 InvalidParameter（commit c0bb215），并提供 `criteria builders`（`always` / `parameterMatch` / `parameterCompare` / `and_` / `or_` / `not_`）让 Python SDK 用户拼装（commit c7725c1）。
- 剩余：CEL-lite 形式的更复杂表达式（算术、函数调用、字符串操作）与 Goja 嵌入仍保留为 Foundry SHOULD 层，被 Gap-A5 Function-backed action 跟进。

**Gap-A4 — Side effects 真实触发**
- 现状：✅ 已落地。`pkg/actions/effects.go` 实现 webhook + log 两种 side-effect dispatcher：webhook 路径覆盖完整重试循环（exponential backoff、429/408/5xx 重试、4xx fail-fast，round 30）+ per-effect outcomes 持久化到 `action_logs.side_effect_status` JSONB 列（round 31）+ DLQ 表 `action_log_side_effect_dlq` 失败行重放（round 33）+ admin replay 端点（round 35）+ round 53 全链路 W3C TraceContext 注入与每次 attempt 一个 client-kind span。
- 剩余：notification / function-call 等其它 Foundry side-effect 类型；webhook signed-request (HMAC) 验证保留为 SHOULD 层。

**Gap-A5 — Function-backed action 内嵌运行时**
- 现状：✅ 已落地。`pkg/functions/goja_runtime.go` 用 `github.com/dop251/goja` 嵌入式 ECMAScript 引擎搭建受限运行时（无 fs / net 出口），`pkg/functions/goja_shim_functions.go` 暴露 ontology 客户端 shim（getObjectsByPk / loadLinks / aggregate 等），`pkg/functions/goja_shim_progress.go` 提供 long-running function 的 progress 回调；缓存与错误分类分别在 `pkg/functions/cache/` 与 `pkg/functions/fnerrors/fnerrors.go`，调用入口在 `pkg/functions/fncall/fncall.go`。
  - **Function-backed action（US-066）**：`pkg/actions/goja_dispatcher.go` 把 ActionType 的 implementation = "function" 路由到 goja runtime，替代纯 HTTP dispatcher 路径；测试 `goja_dispatcher_test.go` 覆盖。
  - **executeQuery via function（US-067）**：`pkg/queryexec/goja.go` 让 QueryType 通过同一 runtime 执行，含 Goja / HTTP dispatch 二选一（commit 607dad4 Phase 6-8 QueryType executeQuery Goja/HTTP dispatch）。
  - **OMS 层接入**：`pkg/oms/function_executor.go::FunctionExecutor` 定义抽象，handlers_function 路径调用进 runtime。
  - **测试矩阵**：单元 `goja_runtime_test.go` / `goja_runtime_us218_test.go`（US-218 sandbox boundaries）/ `goja_runtime_us476_test.go`（US-476 后续硬化）+ shim 测试 `goja_shim_functions_test.go` + `goja_shim_progress_test.go` + 集成 `test/integration/goja_runtime_test.go` 全套覆盖。
- 剩余：multi-runtime fan-out / 跨 function dependency graph / TypeScript 静态校验仍属 Foundry SHOULD 层；Function-as-a-Service-style horizontal scale 留作部署侧。

### 4.3 安全与治理层

**Gap-S1 — 行级安全（Row-Level Security）**
- 现状：✅ 已落地。`pkg/security/policy_engine.go` 提供 `Engine.Evaluate(ctx, user, oms.ObjectType) (query.Query, error)` 接口（正是 PRD 建议的形状），返回的 BleveQuery 与用户 where 子句在 `pkg/oss/service_impl.go` 主查询路径上做 `AND`；CEL DSL 在 `pkg/security/cel_evaluator.go` 评估 row-level 条件，`pkg/security/policy_cache.go` 缓存 policy 解析、`decision_cache.go` 缓存 per-request decision；集成测试 `pkg/oss/row_policy_integration_test.go` / `policy_engine_integration_test.go` / `row_policy_cel_integration_test.go`、聚合路径 `handlers_aggregate_policy_test.go`、BDD `cmd/server/rls_cel_us487_bdd_test.go`、性能基线 `pkg/security/rls_bench_test.go` 全套覆盖。
- 剩余：跨实例 policy hot-reload 与租户级 isolation 仍属 Foundry SHOULD 层。

**Gap-S2 — 列级 / 属性级安全**
- 现状：✅ 已落地。`pkg/security/policy_engine.go::AllowedProperties(ctx, user, ot) []string` 按用户 context 计算可见 property 集合，内部 `propertyRuleMatches(Rule, *auth.User)` 匹配 policy rule 的 user / role / scope 子句；WireObject 序列化路径（`pkg/oss/service_impl.go`）据此过滤字段。
- 剩余：动态属性级 marking 衍生与属性级 redaction（masking）仍属 SHOULD 层，由 Gap-S4 audit 流程联动。

**Gap-S3 — Marking 评估链路**
- 现状：✅ 已落地。Marking 已并入 `pkg/security/policy_engine.go`：`SetMarkingsEnabled` / `MarkingsEnabled` 控制每 ObjectType 是否启用 marking 过滤，`AllowedForIngest(ctx, user, ot)` 用 user-context markings 阻止越权写入；`pkg/security/auto_marking_test.go` 覆盖自动 marking 继承；用户 context markings 由 `auth.User.Attributes[MarkingsAttributeKey]` 注入并贯穿 row-level + property-level 决策（无单独 `marking_filter.go`，统一在 policy_engine）。
- 剩余：marking 升级 / 撤销时的反向 propagation 与外部 marking 同步保留运维流程，不影响 1:1 对齐。

**Gap-S4 — Audit policy breadth 与运行硬化**
- 现状：✅ 已落地。
  - **核心 audit 通道**：`pkg/audit/audit.go` + `pg_store.go`（持久化）+ `chain.go`（hash chain）+ `context.go`（请求上下文）+ `redaction.go`（PII redaction），`pkg/oms/audited_repository.go` 把 OMS 写路径整条接入 audit。
  - **SIEM 投递**：`pkg/audit/export/exporter.go` 抽象，`syslog.go` / `s3.go` 两种 sink，`batched.go` 批量重试，`tee.go::TeeStore` 让内部 store 与 SIEM exporter 同时收到事件；`cmd/server/audit_retention.go` 装配 `AuditExportConfig` + `S3Uploader` 让部署侧配置 SIEM target。
  - **Root-hash 发布**：`pkg/audit/roothash.go::RootHashPublisher` 周期（默认 24h）发布前一 UTC 日的链根，写到锚定路径供外部 verifier 比对（US-266 tamper-proof audit logs）。
  - **运维入口**：admin audit query 端点 + retention / export / redaction hooks 已暴露。
- 剩余：批量审计 UX（Web 操作侧）与部署级 root-hash 操作手册（plumbed-into-runbook 文档）仍属运维 polish；row / column / marking 改动接入统一 audit taxonomy 由 Gap-S1 / S2 / S3 联动后自然落入 `audit_events` 流。

**Gap-S5 — SQL Query 沙箱与资源限制**
- 现状：✅ 已落地。`pkg/sqlqueries/safety.go` `ValidateQuery` 实现全 SQL tokenizer（line/block/dollar-quoted 字符串与注释剥离、双引号 identifier、反引号拒绝）+ 白名单仅 SELECT/WITH/VALUES/TABLE + 黑名单 30+ DML/DDL/DCL/事务/存储过程关键字 + body 嵌入 INSERT/UPDATE/DELETE 防御 + pg_* / information_schema / pg_catalog / pg_toast 系统表防御 + stacked-statement 防御。`pkg/sqlqueries/engine.go` PGEngine 强制 `pgx.TxOptions{AccessMode: pgx.ReadOnly}` + `context.WithTimeout`（默认 5s，US-468 契约）+ MaxRows 流式截断（默认 10K）+ 超时错误映射为 `ErrQueryTimeout` 便于 SDK 分类。
- 剩余：`EXPLAIN` 前置成本估算可选；当前 timeout + row cap 已足够防止资源失控。

### 4.4 实时与事件层

**Gap-R1 — 客户端订阅深度**
- 现状：`pkg/funnel/broadcast.go` 的 applied-edit broadcast 已通过 `pkg/oss/subscribe_sse.go` 暴露为 `GET /api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe`，同时 `pkg/subscriptions` 挂载 `/api/v2/ontologies/{ontologyApiName}/subscriptions/ws` WebSocket 订阅。SSE 支持 `Last-Event-ID` 与 `since` query parameter replay，并带 per-user connection guard。
- 前端：`web/src/hooks/useObjectSetSubscription.ts` 基于 EventSource；`web/src/components/browser/BrowserPage.tsx` 提供 realtime mode；`web/src/components/objectsets/ObjectSetLivePage.tsx` 提供 ObjectSet Live 页。
- 剩余：多实例 fan-out、replay window 运维指标、断线恢复矩阵与端到端压测仍需补齐。

**Gap-R2 — 数据摄入 stream**
- 现状：✅ 已落地。`pkg/oss/stream_ingest.go` 实现专用 streaming ingest 路径，绕过 Action rule 直接生成 Edit batch 推送 NATS subject（仍经 funnel → bleve），适用于 ETL 大批量导入；`pkg/oss/stream_ingest_validation.go` 在摄入侧执行 `types.ValidateConstraints` 防止脏数据；BDD 覆盖 `pkg/oss/stream_ingest_dog003_bdd_test.go` (Dog 数据集) + `stream_ingest_self102_bdd_test.go` (自驱动场景)。
- 剩余：双写 (Kafka Connect 风格) 与背压策略保留运维侧选项。

**Gap-R3 — rehydrate 路径测试矩阵**
- 现状：✅ 已落地。`pkg/index/rehydrate_test.go` 覆盖 EnsureAllIndexes 创建 / 幂等 / 空仓 / nil-guard / analyzer 传播 / ListOntologies 错误共 7 条路径；`pkg/index/rebuild_us408_test.go` 锁定 RebuildWithOptions 的 RebuildMarker 生命周期 + 5 个 RebuildStage 事件 + BatchSize 行为；`pkg/index/rebuild_test.go::TestRebuild_DropsAndReindexesFromSource` 锁定 drop + reindex 把 stale doc 清掉；`pkg/index/rehydrate_disaster_recovery_bdd_test.go::TestBDD_Rehydrate_KillBleveDirAndRebuildFromSource`（round 146）实施 PRD 建议的端到端契约："杀 Bleve 目录（`os.RemoveAll(dataDir)`）→ 新 manager → `RebuildWithOptions` 从源（PG / Parquet 通过 `LatestDocumentSource` 抽象）重建 → 同样的 `country=USA` / `country=Mexico` term 查询返回与重建前等价的命中数（1 + 2）"，IndexedCount 与 ScopedKey 跨重建保持稳定。
- 剩余：跨 ontology 大规模并发 rebuild、SIGKILL 中断恢复、Parquet snapshot 差量 catch-up 属运维压力测试，由 deploy 侧灾备演练覆盖。

### 4.5 语义层（Types / Interfaces / Derived）

**Gap-T1 — TypeClass 驱动索引**
- 现状：✅ 已落地（Bleve 端）。`pkg/index/mapping_builder.go` 读取 `property.typeclass` (`analyzer.not_analyzed`/`analyzer.keyword`/`analyzer.english`) 决定 Bleve `FieldMapping`，FK link resolver (US-040) 依赖 not_analyzed 闭环；`pkg/index/mapping_builder_test.go` 覆盖每条映射规则。
- 剩余：`hubble.icon` / `hubble.media_url` 前端 hint 与 `hierarchy.parent` Explorer 树形视图属于 UI 侧渲染层，由 `web/src/lib/typeclass-hints.ts` 等单独 owner 管理。

**Gap-T2 — Struct / Array 深度序列化**
- 现状：✅ 已落地。`pkg/types/validate.go` 递归校验 Struct 字段（present-only 语义，宽容 MODIFY 部分更新）+ Array 元素（带类型化 SubType 时逐元素），错误路径携带 `struct field "x":` / `array element [i]:` 前缀；测试覆盖 `pkg/types/validate_deep_bdd_test.go`、`us010_test.go`、`union_test.go`。
- 剩余：Vector / GeoShape 等高级类型的嵌套校验保持宽容，待业务驱动。

**Gap-T3 — ValueType 约束执行**
- 现状：✅ 已落地。`pkg/types/constraints.go` `ValidateConstraints` 实现 regex/pattern/minLength/maxLength/min/max/enum 全套；`EnumViolationError` 携带 AllowedValues 用于结构化 422 响应；调用点：`pkg/oss/stream_ingest_validation.go:151`（stream 摄入）+ `pkg/actions/executor.go:815`（action edits）；ValueType 链解析 `pkg/types/valuetype.go` ResolveValueType 防 cycle + depth-limit。
- 剩余：DSL 形式（CEL）的更复杂条件约束未实现，普通声明式约束已满足 Foundry 1:1 范围。

**Gap-T4 — Ontology 分支与语义版本**
- 现状：✅ 全部建议已落地（rounds 39 / 91 / 92 / 117 / 118 / 119 / 120 / 121 + Gap-T4 partial）。
  - **`ontology_branches` 表已建**：migration `000024_ontology_branches.up.sql` 落 `branch_id` / `branch_name` / `ontology_rid` / `base_version` / `is_experimental` 列；`000025_ontology_proposals` 提交 proposal 也跟到 branch；`000091_ontology_branches_parent_tx` 补 `parent_tx` 列做分支谱系；`000086_aip_messages_branch` 让 AIP 消息也带 branch 维度。
  - **RID `@vN` 后缀解析**：`pkg/rid/rid.go` 的 `Version` 字段 + `splitVersionSuffix` 函数（commit 72b37ba P91 / 镜像 07e304e SDK92），同 ID 不同 `@vN` 视为不同 RID，malformed `@v` 立即拒绝以避免脏数据。
  - **`?branch=` / `X-Weave-Branch` 读路径**：`pkg/oms/branch_scope.go::BranchHeader` 常量 + handlers.go:238 dispatch（commit 3716931 Gap-T4 partial），query 与 header 同存时 query 优先；handlers_function.go 也尊重 header 让 function dispatch 同步落 branch。
  - **只读分支的 typed 拒绝**：8 个 Get 端点（GetObjectType + 7 个 sibling）收到 `@vN` 时返回 `501 VersionedLookupNotSupported`（commits 8bc0005 P117 pilot / ed6f78b P119 family），SDK 端 `WeaveVersionedLookupError` 类型化异常（commits 265cffd SDK118 + 61b1d80 SDK120 contract），OpenAPI 在 7 个 Get op 上文档化 501 response（commit 33a8233 P121）。
- 剩余：写路径仍默认 HEAD（按 PRD 明确"避免真正的写分支"）；branch-level snapshot dump / merge / proposal-driven 写入 等 Foundry 写分支语义仍属 SHOULD 层，不阻塞 1:1 对齐。

### 4.6 可观测性与运维

**Gap-O1 — 业务指标**
- 现状：✅ 已落地。go runtime / chi request 指标暴露之外，`pkg/metrics/oss.go` 注册 `weave_objectset_execute_duration_seconds` / `weave_objectset_load_duration_seconds`，`pkg/metrics/actions.go` 注册 `weave_actions_apply_duration_seconds` / `weave_actions_applied_total`，`pkg/metrics/funnel.go` 注册 `weave_funnel_lag_messages`。
- 剩余：业务侧 dashboard JSON 模板未随源码 ship（运维独立维护）。

**Gap-O2 — Trace propagation**
- 现状：✅ 已落地。`pkg/tracing/tracing.go` HTTPMiddleware + BaggageMiddleware + PgxTracer（chi 模板做 span 名、5xx 翻 Error、request_id/user_id W3C baggage、DB 查询 span），`pkg/funnel/tracing.go` natsHeaderCarrier 跨 NATS JetStream 注入/抽取 TraceContext，round 52 `pkg/actions/http_dispatcher.go` 出站函数调用注入，round 53 `pkg/actions/effects.go` 出站 webhook 注入 + 每次 attempt client-kind span。
- 剩余：外部 sampler 配置矩阵、多租户 trace 隔离。

**Gap-O3 — Audit log 聚合**
- 见 Gap-S4。

**Gap-O4 — Health check 深度**
- 现状：✅ 已落地。`cmd/server/health.go` ReadinessHandlerWithState 依次探测 PG / NATS / Bleve / Funnel；ProbeFunnel 返回 `ErrFunnelLagDegraded`（带 lag/threshold）时 wire 上 status="degraded" 仍 HTTP 200（k8s readiness 保持绿色，dashboard 摆 banner）；硬失败走 503 unready。`/healthz/ready` 是 k8s 习惯别名，`StateStarting`/`StateReady`/`StateDraining` lifecycle 一次性升降。
- 剩余：runtime 自我恢复（DB reconnect 后 ProbeFunnel 自愈）已 OK；细粒度的子系统配额仍待补。

### 4.7 开发体验

**Gap-D1 — Python SDK ObjectSet builder**
- 现状：✅ 已落地（commit a042fa5）。`sdk/python/weave_client/objectsets.py::ObjectSetBuilder` 提供 Pythonic chaining：`ObjectSetBuilder(client).base("Employee").filter({"field":"age","op":"gt","value":30}).search_around("team").build()`，可链式 union / intersect / subtract / searchAround / withProperties；与 `sdk/python/weave_client/builders.py` 的 aggregation / criteria builders 配套；`sdk/python/tests/test_objectsets.py` + `test_builders.py` 覆盖。
- 剩余：纯 Pythonic 属性比较 DSL（`Employee.age > 30`）尚需 metaclass-based field proxy 层；当前 dict-based 输入已能完整表达 ObjectSet 操作语义。

**Gap-D2 — Python SDK Aggregation / TimeSeries / Attachment**
- 现状：✅ 已落地（commits 863a19e + 751d9dc + 66a675d）。
  - `sdk/python/weave_client/aggregation.py::AggregationAPI` 完整 metric + groupBy builder + typed response（commit 863a19e）。
  - `sdk/python/weave_client/timeseries.py::TimeSeriesAPI` 暴露 property TimeSeries 端点（commit 751d9dc Gap-D2 partial）。
  - `sdk/python/weave_client/attachments.py::AttachmentsAPI` 暴露 attachment 上传 / 读取（commit 66a675d Gap-D2 close-out）。
  - 测试 `sdk/python/tests/test_aggregation_builders.py` / `test_timeseries.py` / `test_attachments.py` 全套覆盖。
- 剩余：批量 attachment 上传与 streaming download 仍待用例驱动。

**Gap-D3 — CLI action / aggregate / objectset 深度**
- 现状：✅ 已落地（commit fc6ef44）。`weave action apply`、`weave aggregate`、`weave objectset load`、`weave objectset create-temporary` 完整入口在 `cmd/weave-cli/cmd_action.go` / `cmd_aggregate.go` / `cmd_objectset.go`，由 `cmd/weave-cli/cli_us304_test.go` 覆盖；`docs/cli.md` 在 `### weave action apply` (L141) / `### weave aggregate` (L177) / `### weave objectset <load|create-temporary>` (L220) 三个章节提供命令参考 + 常用 body 模板 + 真实 northwind 示例（131 行 docs 改动）；BDD 守哨 `scripts/ci/cli_docs_bdd_test.go` 防止任一章节被误删或 wording drift。
- 剩余：`objectset run` 便捷别名与更丰富的 table 输出仍属可选 polish，不阻塞 1:1 对齐。

**Gap-D4 — MCP prompts / resources / completion / sampling**
- 现状：`pkg/mcp/prompts.go` 已实现 `prompts/list` / `prompts/get`，从 OMS ActionType 元数据合成 prompt；`pkg/mcp/resources.go` 已实现 `resources/list` / `resources/read` / `resources/subscribe` / `resources/unsubscribe`，能列出 ontology、ObjectType 与临时 ObjectSet 资源，ObjectType URI 形如 `weave://objecttype/<ontology>/<objectType>`；`pkg/mcp/completion.go` + `pkg/mcp/completion_ontology_source.go` 实现 `completion/complete`（commits 1a1065e + fb5f90c），AI 客户端在 URI 路径补全时（如键入 `weave://objecttype/`）从 OMS 拉取 ontology / objectType apiName 实时建议，BDD 覆盖 `completion_bdd_test.go` + `completion_ontology_source_bdd_test.go`；对外契约见 `docs/mcp.md`。
- 建议：下一步补 MCP sampling（客户端反向请求 LLM 完成的 RPC）以及生产认证/部署说明；prompts / resources / completion 不再是缺失入口。

**Gap-D5 — weave-mcp stdio 真可用**
- 现状：`weave-mcp` 已有 bridge 模式：设置 `WEAVE_MCP_URL` 后，`cmd/weave-mcp/http_bridge.go` 会把本地 stdio JSON-RPC 转发到运行中的 `/mcp`，并复用同一套 tools/prompts/resources；也会透传 `WEAVE_MCP_TOKEN` / `WEAVE_MCP_API_KEY`，且 `WEAVE_MCP_HTTP_TIMEOUT` 可限制上游 HTTP stall。
- 建议：remaining local-standalone gap 是独立启动本地服务并嵌入 PG/NATS/Bleve 的模式；bridge 模式已经可供本地 AI 客户端使用。

---

## 5. 产品路线图

### 5.1 Phase 划分

| Phase | 代号 | 时长 | 重点 | 退出标准 |
|---|---|---|---|---|
| Phase 6 | **Deep Parity I: 语义正确性** | 4 周 | withProperties / Interface 多态 / Aggregation 精度 / Edit 冲突 / TypeClass 索引 | Foundry 行为对照套件 ≥ 80 个样例全绿 |
| Phase 7 | **Deep Parity II: 安全 + 实时** | 4 周 | Policy engine / Marking 应用 / SSE 订阅 / Audit log / Stream ingest | Row/Column/Marking 过滤端到端测试 ≥ 15 个；前端 subscribe demo |
| Phase 8 | **Deep Parity III: 语义深度 + 运行时** | 4 周 | Function runtime (Goja) / Ontology 分支 + semver / TimeSeries 分桶 / GeoTemporal 持久化 / withProperties 二阶 | 在 Chinook+Northwind 上跑完一组 demo notebook，覆盖 function + derived + branch |
| Phase 9 | **Production Hardening** | 持续 | Parquet 冷存 / 性能基准 / 文档 / 运维 | Bench 报告 / 运维手册 / v1.0 tag |

### 5.2 Phase 6 详细：Deep Parity I — 语义正确性（4 周）

**里程碑 M6-W1 — 基线**
- 搭建 `test/foundry_parity/` 目录，**至少 20 个**"Weave HTTP 响应 vs Foundry 期望 JSON"对照。
- 引入 `make test-parity`。

**里程碑 M6-W2 — withProperties + Interface 多态**
- US-048（withProperties 单 hop 聚合）
- US-049（Interface 多态 composite cursor）
- US-050（TypeClass 驱动 Bleve mapping）

**里程碑 M6-W3 — Aggregation 精度 + 多层**
- US-051（多层 groupBy 稳定性 + accuracy 标记）
- US-052（approximatePercentile 精度基准）

**里程碑 M6-W4 — Edit 冲突 + Optimistic concurrency**
- US-053（user-edit-wins 策略）
- US-054（Optimistic version check）
- US-055（edit-only property 始终应用）
- 退出评审：parity 套件 ≥ 80 样例全绿。

### 5.3 Phase 7 详细：Deep Parity II — 安全 + 实时（4 周）

**W1 — Policy engine 骨架**
- US-056（`pkg/security/policy_engine.go` row-filter）
- US-057（property-level 序列化过滤）

**W2 — Marking 接入**
- US-058（Marking evaluator + OSP 合并）
- US-059（用户 context 注入 marking set）

**W3 — 实时订阅**
- US-060（SSE `/objectSets/subscribe` 端点）
- US-061（前端 `useObjectSetSubscription` hook + Browser 演示）
- US-062（Stream ingest 端点）

**W4 — Audit log + 元数据审计**
- US-063（audit policy breadth + operations hardening）
- US-064（Security header / rate limit 细化）
- 退出评审：row+column+marking 集成测试 ≥ 15；前端 subscribe demo 可复现。

### 5.4 Phase 8 详细：Deep Parity III — 语义深度 + 运行时（4 周）

**W1 — Function runtime**
- US-065（`pkg/functions/goja_runtime.go` 内嵌 JS 运行时）
- US-066（Function-backed action 真实执行）
- US-067（Query function executeQuery 真正调用 function）

**W2 — Ontology 分支 + semver**
- US-068（`ontology_branches` 表 + 读分支 API）
- US-069（`?branch=` query param + header）
- US-070（RID version suffix 解析）

**W3 — TimeSeries / GeoTemporal**
- US-071（TimeSeries calendar alignment / retention / multi-resolution materialization）
- US-072（GeoTemporal PG 持久化 + PostGIS 可选）

**W4 — withProperties 二阶 + Aggregation on ObjectSet**
- US-073（withProperties 二阶跨 link）
- US-074（Aggregation 作用于 ObjectSet definition，而不止 ObjectType）
- 退出评审：demo notebook 跑通 function + derived + branch。

### 5.5 Phase 9（持续）

- US-075（Parquet 冷存 + 从 Parquet rehydrate）
- US-076（性能基准套件 + criterion 报告）
- US-077（OpenTelemetry 全链路 trace）
- US-078（Python SDK ObjectSet builder + Aggregation）
- US-079（CLI action/aggregate 子命令）
- US-080（weave-mcp stdio 真可用）
- US-081（Docs：运维手册 / 配置参考 / 故障排查）

---

## 6. User Story Backlog（US-048 ~ US-081）

> 所有 US 遵循 TDD 红绿重构；每个 US 提交前必须跑完 `go test ./... && make web-test && pytest sdk/python`。

### US-048 — withProperties 单 hop 聚合

**As** 一个 SDK 使用者，**I want** `withProperties` 定义能跨单 hop link 计算 count/sum/avg/min/max，**so that** 我能像 Foundry 一样写 `employee.withProperty("reportsCount", pivotTo("reports").aggregate(count))`。

**Acceptance Criteria**
- `pkg/oss/objectset/executor.go` 的 `executeWithProperties` 实现单 hop 聚合逻辑（count/sum/avg/min/max）；
- 返回对象 JSON 中包含 derived property 字段；
- cursor 分页稳定：同一 derived value + base primaryKey 排序；
- 不允许作为 primary key / 不允许 text-search 过滤 / 不允许 action 编辑 → 返回 `DerivedPropertyReadOnly`；
- 测试：Northwind "customers with orderCount" ≥ 10 场景；
- parity 套件对应 fixture 绿色。

### US-049 — Interface 多态 composite cursor

**As** Foundry SDK，**I want** `loadObjectsOrInterfaces` 跨多个实现类型的分页稳定，**so that** 翻页不漏不重。

**Acceptance Criteria**
- composite cursor = base64(`{objectTypeApiName, innerCursor}`)；
- executor 对多个 ObjectType 并行拉取 + heap merge，按 sortKey 排序；
- 给定 3 个 ObjectType × 每类 ≥ 20 条的样例数据，按默认排序翻到结尾结果集稳定；
- 测试：定义一个 `HasOwner` interface by Customer + Employee + Supplier。

### US-050 — TypeClass 驱动 Bleve mapping

**Acceptance Criteria**
- 新增 `pkg/index/mapping_builder.go` 从 property.typeClasses 构造 `bleve.IndexMapping`：
  - `analyzer.not_analyzed` → `KeywordField`
  - `analyzer.not_indexed` → 跳过（仍 store）
  - `analyzer.standard`（默认）→ `TextField` with standard analyzer
- 重建 indexes 命令 `weave index rebuild --ontology X` 应用新 mapping；
- 测试：对 `country`（not_analyzed）过滤 `USA` 与 `usa` 区分大小写；对 `description`（standard）支持词干匹配。

### US-051 — Aggregation 多层 groupBy 稳定性 + accuracy

**Acceptance Criteria**
- 测试覆盖 `groupBy=[ExactValue(country), FixedWidth(age, 10), Duration(orderDate, P1M)]` 至少 5 组数据集；
- 当 `maxDocScanSize` 被触发时返回 `accuracy=APPROXIMATE`；
- parity 样例对比 Foundry 文档里的 groupBy 响应结构（字段顺序、名称）。

### US-052 — approximatePercentile 精度基准

**Acceptance Criteria**
- 基于 HdrHistogram/t-digest 或 Bleve facet 实现；
- 与 exactPercentile 的误差 ≤ 5% on 10k 随机数据集；
- bench 输出入库 `bench/aggregation_percentile.md`。

### US-053 — Edit 冲突：user-edit-wins 策略

**Acceptance Criteria**
- `funnel.Edit` payload 增加 `source: user | ingest` 字段；
- Consumer 在应用 ingest edit 前检查 `object_history.last_user_edit_ts`，若更新则跳过非 `always_apply` 字段；
- 测试：并发 user + ingest 各 100 条随机 edit，user 的永不丢失；
- options `returnEdits=ALL_V2_WITH_DELETIONS` 的 tombstone 语义验证。

### US-054 — Optimistic concurrency check

**Acceptance Criteria**
- `ApplyOptions.expectedVersion` 新字段；
- apply 路径加载目标对象后对比版本，mismatch 返回 409 + `StaleObject` 错误码；
- 前端 Action Console 在 submit 时带上版本；
- parity: 对同一对象并发两次 apply，第二次返回 409。

### US-055 — Edit-only property always apply

**Acceptance Criteria**
- Property schema 加 `"editOnly": true`；
- 无论冲突策略如何，该 property 的 user edit 永远覆盖 ingest；
- 测试：用户给 `Order.notes` 写内容 + ingest 覆盖 → notes 保留。

### US-056 — Row-level Policy Engine

**Acceptance Criteria**
- 新增 `pkg/security/policy_engine.go`，接口 `Evaluate(ctx, user, objectType) (BleveQuery, error)`；
- 支持 policy rule DSL：`user.attribute == object.property`、`marking ⊆ user.markings`；
- 绑定到 OSS Load/Search/Aggregate 的 query 生成链；
- 测试：3 个用户 × 3 条 policy × 5 个对象的正确过滤矩阵。

### US-057 — Property-level filtering on serialization

**Acceptance Criteria**
- 在 WireObject 序列化时按 user context 过滤 property；
- 被过滤字段不出现在 JSON（而不是 null）；
- 测试：manager 能看 `salary`，employee 不能看。

### US-058 — Marking evaluator

**Acceptance Criteria**
- `pkg/auth/marking.go` 的 `EvaluateMarkings(userMarkings, objectMarkings) bool` 实现 AND 语义；
- Policy engine 自动合并 marking gate；
- 测试：ACME 用户看不到 ACME2 专属对象。

### US-059 — User context markings injection

**Acceptance Criteria**
- JWT claim 新增 `markings: string[]`；
- Middleware 注入到 context；
- `/api/v2/me` 返回当前用户 markings。

### US-060 — SSE ObjectSet subscription

**Acceptance Criteria**
- 端点 `GET /api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe` 已挂载；
- SSE 事件流，event data 为 `{eventType: ADDED_OR_UPDATED|DELETED, object: WireObject}`；
- 从 `pkg/funnel/broadcast.go` 订阅对应 objectType，在 `pkg/oss/subscribe_sse.go` 做 Where 过滤；
- 支持 `Last-Event-ID` 与 `since` query parameter 断点续传；
- 服务端具备 per-user connection guard，避免单用户长连接耗尽 worker；
- 测试：订阅 + 应用 action，客户端收到事件且顺序正确。

### US-061 — Frontend `useObjectSetSubscription` hook

**Acceptance Criteria**
- `web/src/hooks/useObjectSetSubscription.ts` 基于 `EventSource`；
- `web/src/components/browser/BrowserPage.tsx` Browser 页面"实时模式"切换开关；
- `web/src/components/objectsets/ObjectSetLivePage.tsx` ObjectSet Live 页；
- Vitest 测试 mock EventSource。

### US-062 — Stream ingest endpoint

**Acceptance Criteria**
- `POST /api/v2/ontologies/{ontology}/streams/{objectType}/ingest` 接收 Edit[] 直接下到 NATS subject；
- 绕过 Action rules，**但经过 policy_engine**；
- 限速（默认 1000 rps/ontology）；
- 测试：Northwind 批量灌入 1000 条，Bleve 索引正确更新。

### US-063 — Audit policy breadth + operations hardening

**Acceptance Criteria**
- `audit_events` / OMS metadata audit / auth audit / data-access audit / admin audit query 保持现有回归测试绿；
- row policy、column mask、cell marking、security policy 与 permission workflow 变更写入统一 action taxonomy；
- `cmd/weave-audit-verify` 的 hash-chain/root-file 检查纳入运维 runbook；
- retention/export/redaction 路径提供可面向审计员的 evidence dashboard。

### US-064 — Security header / rate limit refinement

**Acceptance Criteria**
- CSP 细化到允许 `/api/` `/mcp` self；
- 每端点速率限制配置表；
- 登录端点 5 rps/ip 强制。

### US-065 — Goja embedded function runtime

**Acceptance Criteria**
- 新包 `pkg/functions/goja_runtime.go`；
- 提供 sandbox：禁用 `fs`/`net`/`process`；
- 提供 ontology 客户端 shim：`ontology.load(rid)` / `ontology.search(...)`;
- 函数存储于 `functions` 表（name, source, version）；
- 测试：`hello world` function + `findOrdersOver($amount)` function。

### US-066 — Function-backed action with goja

**Acceptance Criteria**
- ActionType.isFunctionBacked + functionRid 走 goja 执行；
- 函数返回 Edit[]；
- 回退到 HTTP dispatcher 如果 functionRid 是 URL；
- 测试：`sendWelcomeEmail` function action。

### US-067 — executeQuery via function

**Acceptance Criteria**
- `/api/v2/ontologies/{o}/queries/{query}/execute` 调用 QueryType.functionRid；
- 支持输入参数 + 返回 ObjectSet reference；
- 测试：`topCustomers(limit)` query function。

### US-068 — Ontology branches table

**Acceptance Criteria**
- 新表 `ontology_branches(name, ontology_rid, base_version, is_experimental, created_at)`；
- OMS API 增加 branch CRUD；
- 只读分支：在 branch 上的元数据读取返回 branch 视图，写入仍回 HEAD。

### US-069 — `?branch=` query param + header

**Acceptance Criteria**
- 所有 V2 读端点支持 `?branch=xxx` 或 `X-Weave-Branch`；
- 未指定默认 HEAD；
- 测试：experimental branch 上加一个新的 ObjectType 不影响 HEAD。

### US-070 — RID version suffix

**Acceptance Criteria**
- RID parser 支持 `ri.service.realm.type.uuid@v3`；
- Snapshot load 按 version 返回历史元数据；
- 测试：v1 时 ObjectType 有 3 属性，v2 有 4 属性，分别按 version 取到正确 shape。

### US-071 — TimeSeries production semantics after downsample

**Acceptance Criteria**
- 现有路径保持绿色：`/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}`、`/firstPoint`、`/lastPoint`、`/streamPoints`、`/points`、`/api/v2/ontologies/{ontologyApiName}/timeseries/transform`；
- `pkg/oss/handlers_timeseries_transform.go` 对单步 `resample` 保持 `DownsamplePoints` 下推；`pkg/timeseries/downsample.go` 的 `DownsampleSpec` 继续覆盖 `avg/sum/min/max/count/first/last`；
- `pkg/timeseries/pg_store.go` 继续通过 `timeseries_cagg_5min` + `RunCAGGRefreshLoop` 服务 PG/TimescaleDB 聚合，`pkg/timeseries/vm_store.go` 的 `NewVMStore` 继续通过 VictoriaMetrics `query_range` 服务大序列；
- 新增工作聚焦 remaining depth gaps：calendar-aware bucket（P1H/P1D/P1W 与时区）、retention/downsample policy、multi-resolution materialization、生产级压测预算；
- 测试：保留 `pkg/oss/handlers_timeseries_transform_us435_test.go`、`pkg/timeseries/us467_*`、`pkg/timeseries/vm_store_test.go` 现有 pushdown/CAGG/VM 覆盖，并新增 calendar/retention 语义用例。

### US-072 — GeoTemporal PG store 已落地 + 可选 PostGIS 索引

**Acceptance Criteria**
- `pkg/geotemporal/pg_store.go` 使用普通 PG 存储，表结构在 `migrations/000205_geotemporal_values.up.sql`；
- `latestValue` 和 `streamHistoricValues` 走 PG 查询；`cmd/server/main.go` 在 PG pool 可用时默认接入 PG-backed `PgStore`，否则使用 in-process MemoryStore as degraded mode；
- `SpatialTemporalQuerier` / `QueryBBoxRange` 支持 bbox + time range 查询，`migrations/000208_geotemporal_spatial_indexes.up.sql` 提供经纬度函数索引和可选 PostGIS GIST 索引；
- 测试：重启进程数据不丢失，bbox/time range 查询结果稳定。

### US-073 — withProperties 二阶

**Acceptance Criteria**
- 支持 `A.withProperty("deepCount", pivotTo("team").pivotTo("manager").aggregate(count))`；
- 单次查询深度限制 ≤ 3；
- 测试：覆盖 2/3 hop。

### US-074 — Aggregation on ObjectSet definition

**Acceptance Criteria**
- `/api/v2/ontologies/{o}/objectSets/aggregate` 已有；校验能接受 filter/union/intersect/subtract/searchAround 定义并执行聚合；
- parity: 与 Foundry 聚合结构一致（buckets, metrics, accuracy 字段）。

### US-075 ~ US-081 — Production hardening（大纲）

- **US-075**：Parquet 冷存 + rehydrate 路径（pkg/dataset/parquet_writer.go + `weave materialize`）；
- **US-076**：`make bench` 性能基准（Northwind 1M 对象 baseline）；
- **US-077**：OpenTelemetry 全链路 trace；
- **US-078**：Python SDK ObjectSet builder + Aggregation + TimeSeries + Attachment；
- **US-079**：CLI action/aggregate/objectset 深度：`action apply`、`aggregate`、`objectset load/create-temporary` 已存在；后续补命令参考、body 模板、便捷别名和输出 polish；
- **US-080**：`weave-mcp` stdio HTTP bridge 已可通过 `WEAVE_MCP_URL` 复用运行中 `/mcp`；后续补独立本地模式（启动本地服务嵌入 PG + NATS in-memory）；
- **US-081**：文档：operating-guide / upgrade-guide / troubleshooting。

---

## 7. 风险与陷阱

### 7.1 技术风险（按优先级）

| # | 风险 | 可能性 | 影响 | 缓解 |
|---|---|---|---|---|
| R1 | **Interface 多态 paging 复杂度高** | 高 | 高 | 先做 "same sort key + stable ordering" 简化实现；容忍跨子类型 skew |
| R2 | **withProperties 破坏 cursor 稳定性** | 中 | 高 | derived column 用 materialize-into-temp-objectSet 换稳定性 |
| R3 | **Goja 运行时沙箱逃逸** | 低 | 高 | 黑名单 + 超时 + 内存限制 + code review |
| R4 | **Marking/Policy 查询性能** | 中 | 中 | 预编译 policy → bleve query 缓存；policy version 变更 invalidate |
| R5 | **PostGIS 可选引入 migration 分叉** | 中 | 中 | 以 feature flag 开启；无 PostGIS 时降级为纯 lat/lon 列 |
| R6 | **Edit 冲突策略 + Optimistic concurrency 并存可能过度复杂** | 中 | 中 | 先单独落地 user-edit-wins，ExpectedVersion 视需要 |
| R7 | **SSE 长连接吃住 HTTP worker** | 中 | 中 | 单连接独立 goroutine + ping；max per-user 连接数 |
| R8 | **Ontology 分支引入 RID 格式变更** | 低 | 高 | Version suffix 可选；默认不开启；CHANGES-v2 文档警告 |
| R9 | **parity 套件的 expected JSON 获取成本** | 中 | 中 | 从 Palantir 公开 docs 的 example 里抓；也允许手工构造并 sign off |

### 7.2 Palantir baseline 的 6 个陷阱（v2 中必须明确覆盖）

1. **searchAround 循环 + 深度**：在 executor 里加 `visited set` 与 `maxDepth=3` plan-time 检查。新增 US 不需要，并入 US-049 或独立 Hotfix。
2. **Interface 多态**：Gap-Q1 / US-049。
3. **Edit conflict**：US-053 / US-054 / US-055。
4. **Derived property 分页稳定性**：US-048 / US-073。
5. **TypeClass 驱动索引**：US-050。
6. **Ontology 分支与 semver**：US-068 / US-069 / US-070。

**6 个陷阱全部被 v2 backlog 覆盖**，这是 v2 PRD 相对 v1 的核心价值。

### 7.3 项目风险

- **"US 炸弹" 复返**：Phase 6~8 容易再次走回"开 24 个 US 并行"的节奏。建议 Phase 6 内部一次不超过 3 个 in_progress US，确保每个 US 有对应 parity fixture。
- **文档债**：v1 的 47 个 US 有 `notes` 字段但没有 "测试场景" 字段。v2 每个 US 强制带 parity fixture 编号。
- **依赖漂移**：Goja 版本、Bleve 版本、pgx 版本升级策略写入 `CONTRIBUTING.md`。

---

## 8. 验收与度量

### 8.1 每 Phase 退出标准

| Phase | 硬性 | 软性 |
|---|---|---|
| 6 | `make test-parity` ≥ 80 样例绿 / 所有 US-048~055 测试绿 | withProperties demo notebook 可跑 |
| 7 | 15 个 row+column+marking 集成测试绿 / SSE demo 可复现 | 前端 Browser 实时模式演示 |
| 8 | Goja 执行 function + branch 读 + TimeSeries 分桶 三条路径 end-to-end | demo notebook 覆盖 v2 全部新能力 |
| 9 | 性能基准报告 / v1.0 tag | Docs 站点发布 |

### 8.2 关键指标（KPI）

- **语义对齐度**：parity 套件通过率 ≥ 95%
- **模块完成度加权平均**：≥ 88%
- **测试覆盖率**：Go ≥ 70%，前端 ≥ 55%，Python SDK ≥ 80%
- **p95 延迟**：Load 对象 ≤ 50ms / Aggregation 单层 ≤ 150ms / KNN(k=10) ≤ 100ms
- **Funnel lag**：稳态 ≤ 200ms
- **审计覆盖**：100% 的 Action / 100% 的 OMS 变更 / 100% 的登录

### 8.3 度量如何采集

- Parity 套件通过率：CI 报告 + `test/foundry_parity/report.json`
- 模块完成度：每 Phase 退出重新跑本 PRD §2.1 的自评分 → 归档到 `docs/status/phase-N.md`
- 测试覆盖率：`go test -cover`、`vitest --coverage`、`pytest --cov`
- 性能：`make bench` 写入 `bench/history/`
- Funnel lag：Prometheus metric `weave_funnel_lag_seconds`

---

## 9. 实施建议与变更管理

### 9.1 分支策略

- `main`：稳定 v1 / 可 tag `v1.0.0`
- `ralph/foundry-osv2-api-alignment`：当前分支，**不再接受新 v1 US**；v2 PRD 批准后该分支 merge 回 main
- `v2/phase-6`：Phase 6 开发分支，合并前触发 `make test-parity`
- `v2/phase-7`、`v2/phase-8`：同上

### 9.2 文档同步

- 每个 Phase 退出更新：
  - `README.md` 功能表
  - `docs/单机复刻 Palantir OSv2 本体层 — 完整技术架构.md` 架构图
  - `docs/api/` OpenAPI spec
  - `docs/CHANGES-v2.md` 变更日志
- 新增文档：
  - `docs/security/policy-model.md`
  - `docs/functions/goja-runtime.md`
  - `docs/subscriptions/sse.md`
  - `docs/branches/ontology-versioning.md`

### 9.3 Breaking changes（v2 相对 v1）

v2 允许有限的 breaking changes，需在 `CHANGES-v2.md` 显式标注：

- ✅ 允许：RID 中引入可选 `@vN` 后缀（向后兼容，默认忽略）
- ✅ 允许：Edit payload 新增 `source` 字段（默认 `user`）
- ✅ 允许：Subscribe 端点属于新路径
- ❌ 禁止：现有 v1 路由的 JSON shape 变化
- ❌ 禁止：OMS 表主键 / RID 格式变更
- ❌ 禁止：Python SDK / CLI / MCP 现有方法签名变化

---

## 10. 附录

### 10.1 术语表（Weave ↔ Foundry）

| Weave 术语 | Foundry 术语 | 备注 |
|---|---|---|
| Ontology | Ontology | — |
| ObjectType | Object Type | — |
| LinkType | Link Type | 支持 FK/M2M |
| ActionType | Action Type | 参数+规则+edits |
| Interface | Interface | 抽象多态 |
| ValueType | Value Type | 约束 |
| ObjectSet | Object Set | 15 定义变体 |
| searchAround | searchAround | 链接遍历 |
| withProperties | Derived Property | v2 深化 |
| nearestNeighbors | Semantic Search / KNN | v2 多字段 |
| Funnel | Object Data Funnel | NATS + Bleve |
| Marking | Marking | v2 启用 |
| Branch | Branch | v2 只读 |
| RID | Resource Identifier | `ri.service.realm.type.uuid` |

### 10.2 关键代码位置索引

| 能力 | 文件 |
|---|---|
| OMS Repository interface | `pkg/oms/repository.go` |
| OMS PG 实现 | `pkg/oms/pg_repository.go` |
| ObjectSet executor | `pkg/oss/objectset/executor.go` |
| Where DSL | `pkg/oss/where/types.go` |
| Aggregation | `pkg/oss/aggregation/` |
| Actions executor | `pkg/actions/executor.go` |
| Funnel publisher/consumer | `pkg/funnel/publisher.go`, `consumer.go` |
| Broadcast (后端) | `pkg/funnel/broadcast.go` |
| ObjectSet SSE subscribe | `pkg/oss/subscribe_sse.go`, `/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe` |
| WebSocket subscriptions | `pkg/subscriptions`, `/api/v2/ontologies/{ontologyApiName}/subscriptions/ws` |
| Realtime UI | `web/src/hooks/useObjectSetSubscription.ts`, `web/src/components/browser/BrowserPage.tsx`, `web/src/components/objectsets/ObjectSetLivePage.tsx` |
| Links resolver | `pkg/links/resolver.go`, `fk_resolver.go`, `join_table_resolver.go` |
| Auth JWT / API Key | `pkg/auth/jwt_signer.go`, `api_key.go` |
| Policy filter (未接入主链路) | `pkg/oss/policy_filter.go`, `marking_filter.go` |
| TimeSeries | `pkg/oss/handlers_timeseries_transform.go`, `pkg/oss/handlers_vertex_timeseries.go`, `pkg/timeseries/downsample.go`, `pkg/timeseries/pg_store.go`, `pkg/timeseries/vm_store.go` |
| GeoTemporal | `pkg/geotemporal/pg_store.go`, `pkg/geotemporal/memory_store.go` |
| Cipher | `pkg/cipher/` |
| MCP | `pkg/mcp/`, `cmd/weave-mcp/` |
| Routes 总表 | `cmd/server/routes.go` |
| 迁移 | `migrations/` |

### 10.3 参考资料（Palantir 公开文档）

- Foundry Object Backend Overview — palantir.com/docs/foundry/object-backend/overview
- Ontology Core Concepts — palantir.com/docs/foundry/ontology/core-concepts
- Interfaces — palantir.com/docs/foundry/interfaces/interface-overview
- Action Types / Rules / Side Effects / Action Log — palantir.com/docs/foundry/action-types/*
- How User Edits Are Applied (OSv2 conflict resolution) — palantir.com/docs/foundry/object-edits/how-edits-applied
- ObjectSets v2 API Reference — palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/*
- Actions v2 API Reference — palantir.com/docs/foundry/api/ontologies-v2-resources/actions/*
- Semantic Search (nearestNeighbors) — palantir.com/docs/foundry/ontology/overview-semantic-search
- Markings / Granular Policies — palantir.com/docs/foundry/security/* / platform-security-management/manage-granular-policies
- Derived Properties — palantir.com/docs/foundry/object-link-types/derived-properties
- TypeScript Subscriptions — palantir.com/docs/foundry/ontology-sdk/typescript-subscriptions
- Breaking Changes OSv1→OSv2 — palantir.com/docs/foundry/object-backend/object-storage-v2-breaking-changes

### 10.4 前序项目文档

- `docs/Palantir Foundry Ontology Layer 完整技术蓝图.md`（能力树）
- `docs/单机复刻 Palantir OSv2 本体层 — 完整技术架构.md`（v1 架构决策）
- `docs/Palantir ObjectSet & OntologyAggregation 完整语法参考.md`（DSL 语法）
- `docs/palantir_aip_analyst_engine_analysis.md`（AIP 分析，v2 不追）
- `prd.json`（v1 的 47 个 US）
- `progress.txt`（v1 的施工日志 + codebase patterns）

---

## 11. 评审清单（给评审人）

- [ ] §2.2 "声称 vs 真实"差异表是否准确？有无过度悲观或过度乐观的条目？
- [ ] §4 各 Gap 是否抓住了最关键的语义差距？有遗漏吗？
- [ ] §5 Phase 6~8 的 12 周节奏是否现实？给 Ralph 单人/双人团队是否合理？
- [ ] §6 US-048 ~ US-074 的 acceptance criteria 是否可执行？有没有漏关键验证？
- [ ] §7.2 6 个陷阱是否全部被 backlog 覆盖？
- [ ] §8 的 KPI 是否足够但不过度？
- [ ] §9 的 breaking changes 边界是否可接受？
- [ ] 是否同意"v2 核心是**加深语义**而不是**再加端点**"？

**评审通过后**：
1. 把本 PRD 拆分为 `prd-v2.json`（结构化 US 列表，v1 格式兼容）；
2. 创建 `v2/phase-6` 分支；
3. 编写第一批 parity fixture（至少覆盖 US-048、049、050）；
4. Ralph/team 模式开跑。

---

*文档结束。*
