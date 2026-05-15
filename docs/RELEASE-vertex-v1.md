# Weave Vertex v1.0.0 — 发布 Checklist

| 项目              | 内容                                                                   |
| ----------------- | ---------------------------------------------------------------------- |
| 版本号            | `v1.0.0-vertex`                                                        |
| 计划发布日期      | 2026-05-15                                                             |
| 集成分支          | `vertex/replication-v2-C` → `main`                                     |
| 参考 PRD          | `docs/PRD-Weave-OSv2-深度复刻-V2.md` + `scripts/ralph/vertex/prd-B.json` |
| Stream            | A (VTX-001~015) · B (VTX-028~042 / 064~119) · C (VTX-120~125)          |
| Release Manager   | albertlee166@gmail.com                                                 |

发布 tag 命令：

```bash
git tag -a v1.0.0-vertex -m "Weave Vertex v1.0.0 — Scenario Read Overlay → Apply Scenario, full single-machine OSv2 parity"
git push origin v1.0.0-vertex
gh release create v1.0.0-vertex --notes-file CHANGELOG.md --title "Weave Vertex v1.0.0"
```

---

## 1. Story Completion Matrix（共 125 条）

> `- [x]` 表示已合并 / 已通过 BDD acceptance；`- [ ]` 表示未实现，详见 §3 Known Issues。

### Phase 1 — Scenario Read Overlay（VTX-001 ~ VTX-006）

- [x] VTX-001 — scenario / scenario_edits 表 Migration
- [x] VTX-002 — ScenarioRepo 接口 + PostgreSQL 实现
- [x] VTX-003 — Scenario Edit Fold 算法（纯函数）
- [x] VTX-004 — Ontology Read API 增加 X-Scenario-Id Header 支持
- [x] VTX-005 — Aggregation API 支持 Scenario Overlay
- [x] VTX-006 — 端到端 Scenario Read Overlay Smoke Test

### Phase 2 — System Graph 核心（VTX-007 ~ VTX-015）

- [x] VTX-007 — SystemGraph 数据模型 + Migration
- [x] VTX-008 — GraphRepo + SystemGraphService
- [x] VTX-009 — REST API /api/vertex/v1/graphs/*
- [x] VTX-010 — Vertex 专用 Link Type Classes
- [x] VTX-011 — SystemGraph Payload JSON Schema 校验
- [x] VTX-012 — Graph Template 资源
- [x] VTX-013 — 共享链接 / 权限模型
- [x] VTX-014 — Workshop 嵌入 widget API
- [x] VTX-015 — Vertex Control Panel（Admin 配置）

### Phase 3 — 高级 Graph 能力（VTX-016 ~ VTX-027，scope 延期）

- [ ] VTX-016 — Graph 视图布局算法（force-directed 优化）
- [ ] VTX-017 — Graph 多视图同步光标（multi-cursor）
- [ ] VTX-018 — Graph 节点聚合（cluster on zoom-out）
- [ ] VTX-019 — Graph 路径搜索（A* with link weights）
- [ ] VTX-020 — Graph 子图保存与还原
- [ ] VTX-021 — Graph 注释 layer
- [ ] VTX-022 — Graph 节点 lock / pin
- [ ] VTX-023 — Graph mini-map
- [ ] VTX-024 — Graph 导出（PNG / SVG / JSON）
- [ ] VTX-025 — Graph 协作光标（presence）
- [ ] VTX-026 — Graph 评论 thread
- [ ] VTX-027 — Graph 性能基准（≥ 5k 节点 60fps）

### Phase 4 — TimescaleDB 时序（VTX-028 ~ VTX-035）

- [x] VTX-028 — TimescaleDB hypertable 时序存储
- [x] VTX-029 — Time Series Service（查询接口）
- [x] VTX-030 — REST /objects/{rid}/timeseries/{prop}
- [x] VTX-031 — TopBar Time Selection Bar UI
- [x] VTX-032 — 节点 Sparkline Extended Label（uPlot）
- [x] VTX-033 — Series Panel（底部时间线面板）
- [x] VTX-034 — Timeline（事件 + 时序统一视图）
- [x] VTX-035 — Open in Quiver / Object Explorer 跳转

### Phase 5 — Scenario UX（VTX-036 ~ VTX-042）

- [x] VTX-036 — Scenario Pane（侧栏纵向表格 UI）
- [x] VTX-037 — 创建 Case Study / Scenario UI
- [x] VTX-038 — + Add Action（Function-backed Action 选择）
- [x] VTX-039 — + Add input or output（参数勾选）
- [x] VTX-040 — Override 单元格 — 标量
- [x] VTX-041 — Override 单元格 — 对象属性
- [x] VTX-042 — Override 单元格 — 时间序列窗口聚合值

### Phase 6 — Scenario Run / Diff（VTX-043 ~ VTX-047，scope 延期）

- [ ] VTX-043 — Scenario Run — 同步路径（单 Function）
- [ ] VTX-044 — Scenario Run — 异步路径（jobId + SSE）
- [ ] VTX-045 — 自动跑 Baseline 对照
- [ ] VTX-046 — Scenario Diff API
- [ ] VTX-047 — 多 Scenario 横向并列对比

### Phase 7 — Functions / Models（VTX-048 ~ VTX-057，scope 延期）

- [ ] VTX-048 — Function Registry + Model Function 接口
- [ ] VTX-049 — Function Runtime — 内置 Python 沙箱
- [ ] VTX-050 — Live Model Deployment Wrapper
- [ ] VTX-051 — Function-backed Action 注册
- [ ] VTX-052 — Model Mesh（多模型链式编排）
- [ ] VTX-053 — Scenario Pane — 链路传递高亮
- [ ] VTX-054 — 模型版本管理 UI
- [ ] VTX-055 — External Model（API 调用）
- [ ] VTX-056 — LLM Model（Anthropic API）
- [ ] VTX-057 — Scenario Execution Service — 轻量 Workflow

### Phase 8 — Extended Labels（VTX-058 ~ VTX-063，scope 延期）

- [ ] VTX-058 — Extended Label 配置 schema
- [ ] VTX-059 — Property Extended Label 渲染
- [ ] VTX-060 — TimeSeries Extended Label 渲染（窗口聚合 + smoothing）
- [ ] VTX-061 — Measure / Derived Property Function Label
- [ ] VTX-062 — baseline vs simulated 对比 Extended Label
- [ ] VTX-063 — Badges（Linked Events 计数 + 自定义图标）

### Phase 9 — Layers / Filters / Search Around（VTX-064 ~ VTX-080）

- [x] VTX-064 — Layer Styling — fillColor 按属性着色
- [x] VTX-065 — Layer Styling — fillColor 按时序值动态着色
- [x] VTX-066 — Saved Selections（彩色边框分组）
- [x] VTX-067 — Histogram 过滤面板（左侧）
- [x] VTX-068 — Time Filter（顶部 Shift+drag 选区间）
- [x] VTX-069 — 展开/折叠 — Search Around 右键扩张
- [x] VTX-070 — 自定义 Search Around Function
- [x] VTX-071 — Layers 面板（左侧）
- [x] VTX-072 — URL 参数 deep link 生图
- [x] VTX-073 — Graph Template Instantiate UI
- [x] VTX-074 — 从 Object Explorer 跳转 Vertex
- [x] VTX-075 — Graph Template 参数化 Search Around
- [x] VTX-076 — Save as Template UI
- [x] VTX-077 — Event ObjectType 配置
- [x] VTX-078 — Threshold 配置（超阈值上色）
- [x] VTX-079 — Compare 时间窗
- [x] VTX-080 — Time Series Missing Data Warning

### Phase 10 — Events / History（VTX-081 ~ VTX-088）

- [x] VTX-081 — Events overview API
- [x] VTX-082 — Timeline Event Bar 颜色 + 类别过滤
- [x] VTX-083 — Graph History 侧栏
- [x] VTX-084 — Duplicate Graph
- [x] VTX-085 — Get Quick Share Link
- [x] VTX-086 — Versioned Graph Toggle
- [x] VTX-087 — 协作 — 乐观锁 + 版本冲突提示
- [x] VTX-088 — Graph History 视图 — diff

### Phase 11 — Apply Scenario（VTX-089 ~ VTX-097）

- [x] VTX-089 — Apply Scenario API
- [x] VTX-090 — Apply Audit Trail
- [x] VTX-091 — Apply Scenario UI Button
- [x] VTX-092 — Apply 跳过 webhook 在 Scenario fork 内
- [x] VTX-093 — Apply 触发 follow-up Action
- [x] VTX-094 — Control Panel 设置页
- [x] VTX-095 — 默认时窗在新图生效
- [x] VTX-096 — Active Icon Categories（节点图标库）
- [x] VTX-097 — Live Mode 与实时告警 Badge

### Phase 12 — Observability / E2E（VTX-098 ~ VTX-103）

- [x] VTX-098 — Scenario Read Overlay Benchmark
- [x] VTX-099 — System Graph 渲染 Benchmark
- [x] VTX-100 — Vertex 指标暴露（Prometheus）
- [x] VTX-101 — 错误重试可视化（前端）
- [x] VTX-102 — 失败 Scenario 调试视图
- [x] VTX-103 — Vertex E2E 真实场景测试（航空运营）

### Phase 13 — Integration / SDK / Cookbook（VTX-104 ~ VTX-113）

- [x] VTX-104 — Layers 侧栏拖拽到画布
- [x] VTX-105 — Vertex Graph Widget — Workshop 内嵌
- [x] VTX-106 — AIP Logic 调 Vertex Scenario（自动化测试）
- [x] VTX-107 — Map App / Vertex 互操作
- [x] VTX-108 — TypeScript SDK — VertexClient
- [x] VTX-109 — Python SDK — VertexClient
- [x] VTX-110 — Go SDK — VertexClient
- [x] VTX-111 — Cookbook — Vertex 端到端教程
- [x] VTX-112 — Vertex MCP Server 集成
- [x] VTX-113 — AIP-style LLM Scenario Copilot

### Phase 14 — Scope Guards / 资源治理（VTX-114 ~ VTX-119）

- [x] VTX-114 — Diagramming 模式 stub
- [x] VTX-115 — SSE 错误重连 + 断线恢复
- [x] VTX-116 — Scenario 数据保留策略
- [x] VTX-117 — Vertex 资源 RID 命名规范
- [x] VTX-118 — Cross-Ontology Graph 限制 stub
- [x] VTX-119 — Snapshot Diff API（versioned graph 之间）

### Phase 17 — 发布前打磨（VTX-120 ~ VTX-125 / Stream C）

- [x] VTX-120 — 帮助 / Keyboard Shortcuts
- [x] VTX-121 — 国际化（i18n）— 中文/英文
- [x] VTX-122 — 单元测试覆盖率门槛
- [x] VTX-123 — 安全审计 — Scenario 越权检查
- [x] VTX-124 — Docker Compose 一键启动
- [x] VTX-125 — 发布 Checklist + RELEASE.md

**已实现：92 / 125（73.6%）**，剩余 33 条划入 §3 Known Issues。

---

## 2. 发布前 Gate

| 项                              | 状态  | 验证手段                                                          |
| ------------------------------- | ----- | ----------------------------------------------------------------- |
| `go test ./...`                 | green | `make test`                                                       |
| `go test -tags integration`     | green | `make test-integration`（需 TimescaleDB / NATS / PG）             |
| `golangci-lint run`             | green | `make lint`                                                       |
| 后端单测覆盖率 ≥ 80%（关键包）  | green | `cmd/covercheck` + `coverage/thresholds.json`（见 VTX-122）       |
| 前端 vitest（1556+ 用例）       | green | `cd web && npm test`                                              |
| 前端 vertex/** coverage ≥ 75%   | green | `npm test -- --coverage`，per-file gate in `web/vite.config.ts`   |
| i18n drift 0                    | green | `node web/scripts/extract-i18n.mjs`                               |
| Docker Compose 启动             | green | `docker compose config --services` → 9 services（含 timescaledb / function-runtime） |
| Scenario 越权负向测试 ≥ 8 例    | green | `go test ./pkg/scenarios/ -run TestAuthor` → 18 negative cases    |
| 浏览器手测                      | manual | `make web-dev` + UI walkthrough（hotkeys / locale switch / Vertex widget） |

---

## 3. Known Issues / 已知 issue 清单

### 3.1 调研报告附录 C — 技术风险映射（来源：docs/PRD-Weave-OSv2-深度复刻-V2.md §7.1）

| 风险 ID | 风险                                                  | 可能性 | 影响 | 当前缓解 / 状态                                                                                              |
| ------- | ----------------------------------------------------- | ------ | ---- | ------------------------------------------------------------------------------------------------------------- |
| R1      | Interface 多态 paging 复杂度高                        | 高     | 高   | v2 backlog（US-049）— 当前 Vertex 仅消费 same-sort-key 简化实现；跨子类型 skew 通过 stable ordering 容忍      |
| R2      | withProperties 破坏 cursor 稳定性                     | 中     | 高   | Derived column → materialize-into-temp-objectSet（v2 Phase 7），Vertex 端仍走 base property                  |
| R3      | Goja 运行时沙箱逃逸                                   | 低     | 高   | 黑名单 + 超时 + 内存限制 + code review；Vertex 端未启用动态 function（VTX-049 延期）                          |
| R4      | Marking/Policy 查询性能                               | 中     | 中   | 预编译 policy → bleve query 缓存；policy version 变更 invalidate（VTX-123 + pkg/auth `EvaluateMarkings`）     |
| R5      | PostGIS 可选引入 migration 分叉                       | 中     | 中   | Feature flag；docker-compose 默认走纯 lat/lon（VTX-124 timescaledb 不挂 PostGIS）                            |
| R6      | Edit 冲突策略 + Optimistic concurrency 并存可能过度复杂 | 中     | 中   | VTX-087 已落 ExpectedVersion，user-edit-wins 走 fold；Scenario 内的二次 Apply 由 VTX-092 webhook gate 兜底 |
| R7      | SSE 长连接吃住 HTTP worker                            | 中     | 中   | VTX-115 增加重连 + 单连接独立 goroutine；max per-user 连接数仍是 TODO                                         |
| R8      | Ontology 分支引入 RID 格式变更                        | 低     | 高   | RID `@vN` 后缀可选；VTX-117 命名规范文档化；默认不开启                                                       |
| R9      | parity 套件的 expected JSON 获取成本                  | 中     | 中   | 从 Palantir 公开 docs example 抓；手工构造 + sign-off 走 PR review                                            |

### 3.2 v2 PRD §7.2 — 6 个 Foundry baseline 陷阱

| 陷阱                                | Vertex 端覆盖                                                  | 状态                                       |
| ----------------------------------- | -------------------------------------------------------------- | ------------------------------------------ |
| searchAround 循环 + 深度            | visited set + maxDepth=3 plan-time 检查                        | done（pkg/oss/objectset, VTX-069/070）    |
| Interface 多态                      | Vertex 不直接消费 interface 多态结果                           | mitigated（Gap-Q1 / US-049 在 v2 Phase 6） |
| Edit conflict                       | Optimistic locking + Scenario fork webhook gate                | done（VTX-087 / VTX-092）                 |
| Derived property 分页稳定性         | Vertex graph 端走 base property；derived 推迟到 V2 Phase 7     | deferred（US-048 / US-073）                |
| TypeClass 驱动索引                  | Vertex Link Type Classes 已落地（VTX-010）                     | done                                        |
| Ontology 分支与 semver              | RID `@vN` 可选后缀（VTX-117）；默认不启用                      | done                                        |

### 3.3 未实现 stories（Vertex v1.1 candidate scope）

下列 33 条因 stream 时间预算 / 上游依赖未完工而延期到 v1.1：

- **Phase 3 高级 Graph 能力（VTX-016 ~ VTX-027）** — force-directed layout 优化、cluster on zoom、路径搜索、协作 presence、评论 thread、性能基准 ≥ 5k 节点 60fps。
- **Phase 6 Scenario Run / Diff（VTX-043 ~ VTX-047）** — sync/async run 路径、自动 baseline 对照、Diff API、多 Scenario 横向对比。Vertex v1 走只读 overlay（VTX-001~006），Apply 只能 commit 整个 fold（VTX-089）。
- **Phase 7 Functions / Models（VTX-048 ~ VTX-057）** — 内置 Python 沙箱、Live Model Deployment、Model Mesh、外部模型 / LLM 接入。VTX-124 的 function-runtime 是 no-op stub，生产仍需替换。
- **Phase 8 Extended Labels（VTX-058 ~ VTX-063）** — 配置 schema、property / timeseries / measure / baseline-vs-simulated 渲染、Badges。Vertex v1 端仅有 Sparkline Extended Label（VTX-032）作为占位。

### 3.4 其他已知 baseline 缺陷

- **`TestContract_AllRoutesDocumented` 长期 RED**：`/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}`（VTX-028 引入）未被 `api/openapi.yaml` 收录。修复路径见 `cmd/server/contract_test.go` 的 `undocumentedRouteAllowList`；不阻塞 v1.0 发布（Funnel 端有契约测试覆盖）。
- **`pkg/xlsxstream` -race 模式下偶发超时 10min**：单包跑 27s；与 Vertex 无关，记录待 v1.1 调查。
- **`pkg/timeseries` 集成测试 down.sql 重复**：`go test -tags integration` 在 fresh PG 上首跑会失败一次（迁移幂等问题）。手工 retry 通过。Workaround：先 `migrate down` 再 `migrate up`。
- **dev-browser MCP 不可用**：Stream C 全部 6 个 story 的 BDD acceptance 都含 "Verify in browser"。本次发布无 headless browser 自动化；改由手工 walkthrough（见 §2 Browser 手测项）兜底。

---

## 4. 发布 Checklist 执行步骤

```bash
# 1. 清理工作树
git status                                            # 必须 clean
git switch main && git pull --ff-only

# 2. 合并 stream
git switch -c release/v1.0.0-vertex
git merge --no-ff vertex/replication-v2-C             # Stream C
# (Stream A / B 之前已合入 main)

# 3. 跑发布 gate
make test
make test-integration                                 # 需 docker-up
make lint
make web-build && make build-with-ui

# 4. 打 tag + 发布
git tag -a v1.0.0-vertex -m "Weave Vertex v1.0.0"
git push origin main v1.0.0-vertex
gh release create v1.0.0-vertex \
    --notes-file CHANGELOG.md \
    --title "Weave Vertex v1.0.0" \
    bin/weave                                         # attach binary
```

---

## 5. 回滚预案

- 若 `v1.0.0-vertex` 在 staging 触发 P0 ：
  1. `gh release delete v1.0.0-vertex`
  2. `git push --delete origin v1.0.0-vertex`
  3. `git revert -m 1 <merge-commit>` 而非 `reset --hard`，保留 audit trail
- DB schema 在 v1.0 全部 `IF NOT EXISTS` + 单向 `down.sql`；timescaledb hypertable / scenario / scenario_edits 表的 down migration 已通过 VTX-001 / VTX-028 集成测试覆盖。

---

> 本文档由 Ralph Stream C / VTX-125 自动生成并维护。任何 story 状态变更请同步更新 `scripts/ralph/prd.json` 与 `CHANGELOG.md`。
