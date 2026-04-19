# Zero-Downtime Upgrade Runbook (US-275)

Weave 单机 Ontology 引擎在生产部署中通常以多实例 (例如 2 个 pod) 滚动方式
升级。本文档描述 Weave 用以保证滚动升级期间零中断的两条机制 — 前向兼容迁移
策略与独立的 liveness / readiness 探针 — 以及 `scripts/rolling-upgrade.sh`
双实例演练脚本如何在本地复现该流程。

## 1. Liveness vs Readiness 探针

| Endpoint        | 用途                                         | 期望状态码 | 适用 Probe              |
|-----------------|----------------------------------------------|------------|-------------------------|
| `GET /health`        | 进程存活 (legacy 别名,与 `/health/live` 等价)  | 200        | livenessProbe (legacy)  |
| `GET /health/live`   | 进程存活 (Kubernetes 约定路径)               | 200        | livenessProbe           |
| `GET /health/ready`  | 依赖就绪 (PG / NATS / Bleve)                 | 200 / 503  | readinessProbe          |

Liveness 永远返回 200,只反映进程能否响应 HTTP 请求 — kubelet 据此决定是否
重启 pod。Readiness 在 PG / NATS / Bleve 任一未就绪时返回 503,kubelet 据此
将 pod 从 Service 端点池中摘除,但**不会**重启容器。两者必须分离,否则:

- 若 readiness 失败时 livenessProbe 也变红,kubelet 会反复重启一个本应等待
  下游恢复的健康进程,放大故障窗口。
- 若 readiness 与 liveness 共用同一个端点,upgrade-rollout 期间新 pod 还在
  等待 PG 连接池预热时会被立即提升为 Ready,引流到尚未准备好的进程上。

推荐的 Kubernetes manifest 片段:

```yaml
livenessProbe:
  httpGet: { path: /health/live, port: 9117 }
  periodSeconds: 10
  failureThreshold: 6
readinessProbe:
  httpGet: { path: /health/ready, port: 9117 }
  periodSeconds: 5
  failureThreshold: 3
```

`/health/live` 是 Kubernetes 社区约定路径,`/health` 保留为兼容别名 (旧
manifest 不需要任何修改即可继续工作)。两条路径返回完全相同的
`{"status":"alive"}` 负载。

## 2. 前向兼容迁移策略 (forward-compatible schema)

滚动升级期间,**v(N) 与 v(N+1) 同时连接到一个 PG 实例**。如果迁移已经在 PG
上 apply 但 v(N) pod 还没下线,那么 v(N) 仍然要能正常读写新 schema。Weave
的迁移规则因此约束如下:

1. **`ADD COLUMN ... IF NOT EXISTS`** — 所有 `ADD COLUMN` 必须带
   `IF NOT EXISTS`。原因:幂等的 migrate up 是滚动部署的最低保证 — v(N+1)
   在不同 pod 上启动时可能并发首次跑迁移,IF NOT EXISTS 让重复 apply 收敛。
2. **`NOT NULL ADD COLUMN` 必须带 `DEFAULT`** — 否则 v(N) (尚未感知新列)
   做 INSERT 时漏写新列将触发 `null value in column "<x>" violates not-null`,
   滚动升级窗口直接 5xx。
3. **禁止在 up.sql 中做破坏性变更** — `DROP COLUMN`、`DROP TABLE`、
   `RENAME COLUMN`、`RENAME TABLE` 都会让 v(N) 立刻报 `column does not
   exist` / `relation does not exist`。这类操作必须放在 v(N+2) 的迁移里 —
   先发布 v(N+1) 让所有 reader 知道不再用旧名字,再用 v(N+2) 真正删除。
   `down.sql` 仍然可以 DROP,因为 `migrate down` 是显式回滚动作,SRE 知道
   它会破坏当前 reader。
4. **`ALTER COLUMN ... TYPE` 仅在白名单里允许** — 类型变更基本无法做到 v(N)
   兼容 (例如 INTEGER→TEXT 之后 v(N) 把 TEXT 扫到 int 字段会失败)。这类
   变更必须计划成一次有意识的破坏性发布:
   - 升级前在 README / 发版邮件里通告;
   - 在 maintenance window 中执行;
   - 在 `internal/upgrade/forward_compat_test.go` 的 `allowedNonForwardCompat`
     map 里登记,并在本文档列入下表。

### 已登记的破坏性迁移

| Migration                                  | 变更                          | 关联 Story | 备注                                                   |
|--------------------------------------------|-------------------------------|------------|--------------------------------------------------------|
| `000041_function_semver_version.up.sql`    | `functions.version` INTEGER → TEXT | US-217   | 一次性 maintenance window;v(N) 读 TEXT 入 int 失败。 |

`internal/upgrade/forward_compat_test.go` 中的四组 `TestForwardCompat_*`
在 `go test ./internal/upgrade/...` 时执行,对所有 `migrations/*.up.sql`
做静态扫描,任何新引入的破坏性 SQL 在 PR 提交时就会被 CI 拦下。

## 3. 双实例演练脚本 (rolling-upgrade.sh)

`scripts/rolling-upgrade.sh` 在本地或 CI 中以 `9117` (旧) / `9118` (新)
两个 weave server 实例模拟滚动升级:

1. 检查 v(N) 与 v(N+1) 二进制 (`bin/weave.old` / `bin/weave.new`) 存在 —
   实践中通过 `BIN_OLD=/path/to/old/weave BIN_NEW=$(pwd)/bin/weave` 注入。
2. 以 `WEAVE_PORT=9117` 启动旧实例,等待 `/health/live` = 200。
3. 调用 `migrate up` (或假定迁移由部署系统应用) 后,再以 `WEAVE_PORT=9118`
   启动新实例。
4. 轮询新实例 `/health/ready` 直到 200,期间旧实例继续接受流量。
5. 优雅关闭旧实例 (`SIGTERM`),验证关闭期间 `curl /health/ready` 始终在
   两个端口至少一个上返回 200 — 这是滚动升级的核心承诺:**永不出现两个
   pod 同时 unready 的窗口**。
6. 退出码 0 表示演练通过;任意一步超时 / 状态码异常即非 0 退出。

脚本通过 `set -euo pipefail` 严格模式 + 显式 `kill` cleanup trap 保证测试
中断时不留僵尸进程。详细的环境变量与超时参数见脚本注释头。

`internal/upgrade/scripts_test.go` 验证脚本存在、可执行、`bash -n` 通过
并包含必需的 invariants (双探针轮询、双端口启动、cleanup trap 等),与
`internal/backup/scripts_test.go` 共用同一套静态校验风格。

## 4. 操作手册

```bash
# 准备旧/新二进制
git worktree add ../weave-prev <prev-tag>
( cd ../weave-prev && make build && cp bin/weave $(pwd)/../Weave/bin/weave.old )
make build && cp bin/weave bin/weave.new

# 跑演练 (PG_DSN / NATS_URL 默认本地 docker-compose)
BIN_OLD=$(pwd)/bin/weave.old BIN_NEW=$(pwd)/bin/weave.new \
  ./scripts/rolling-upgrade.sh
```

CI 中可以用 `BIN_OLD=$(pwd)/bin/weave BIN_NEW=$(pwd)/bin/weave` 退化成
"同一版本双实例" 的烟雾测试,保证脚本本身始终绿。
