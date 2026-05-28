# Weave 认证指南

本文档说明 Weave 的三种认证模式、默认端口、以及如何生成/管理账号密码。

## 默认服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| Weave API | **9117** | Go 后端（可通过 `WEAVE_PORT` 覆盖） |
| Vite 前端开发 | 5173 | `make web-dev`（生产模式嵌入 API 端口） |
| PostgreSQL | 5432 | Docker Compose 启动 |
| NATS | 4222 | Docker Compose 启动 |

端口历史：`8080` 容易与其他本地服务冲突，自 2026-04-08 起默认值改为 `9117`。

## 三种认证模式

Weave 通过 `AUTH_MODE` 环境变量选择认证模式：

### 1. `AUTH_MODE=dev`（默认，本地开发）

- **无需登录**。所有请求被 `pkg/auth/middleware.go` 自动注入一个虚拟 admin 用户：
  ```
  User{
    ID: "dev-user",
    Roles: []string{"admin"},
    OntologyRoles: map[string]string{"*": "admin"},
  }
  ```
- 前端登录页 `/login` 会直接通过 `useAuth().user` 检查后跳回主页
- 适用：`make dev`、本地 CI、单元测试

**启动示例**：
```bash
make dev   # AUTH_MODE 默认为 dev
```

### 2. `AUTH_MODE=token`（已弃用）

- 接受任何非空 Bearer token
- 无签名验证、无过期检查
- 启动时会打印 deprecation warning：
  ```
  [AUTH] WARNING: AUTH_MODE=token is deprecated and accepts unauthenticated tokens.
  ```
- **不要在生产中使用**，保留仅为兼容既有部署

### 3. `AUTH_MODE=jwt`（生产推荐）

完整的 JWT 登录/刷新/登出流程：
- RS256 签名，15 分钟访问令牌 + 7 天刷新令牌
- 密码通过 bcrypt（cost 12）存储
- 支持 API Key 替代 JWT 用于 S2S 场景（`wvk_` 前缀）

需要的环境变量：

| 变量 | 必填 | 说明 |
|------|------|------|
| `AUTH_MODE` | ✅ | 设为 `jwt` |
| `WEAVE_JWT_PRIVATE_KEY_PATH` | ✅\* | RSA 私钥 PEM 文件路径 |
| `WEAVE_JWT_PUBLIC_KEY_PATH` | ✅\* | RSA 公钥 PEM 文件路径 |
| `WEAVE_JWT_PRIVATE_KEY_PEM` | ✅\* | 或直接提供 PEM 字符串（容器友好） |
| `WEAVE_JWT_PUBLIC_KEY_PEM` | ✅\* | 或直接提供公钥 PEM 字符串 |
| `WEAVE_JWT_ISSUER` | ❌ | 默认 `weave` |
| `WEAVE_JWT_AUDIENCE` | ❌ | 默认 `weave-api` |
| `WEAVE_JWT_ACCESS_TOKEN_TTL` | ❌ | 默认 `15m` |
| `WEAVE_JWT_REFRESH_TOKEN_TTL` | ❌ | 默认 `168h`（7 天） |
| `WEAVE_BCRYPT_COST` | ❌ | 默认 `12` |
| `WEAVE_BOOTSTRAP_ADMIN` | ✅ 首次 | 首个 admin 用户 ID，格式 `user:<email>` |
| `WEAVE_BOOTSTRAP_ADMIN_PASSWORD` | ✅ 首次 | 首个 admin 的明文密码（仅首次启动读取） |

\* `PATH` 和 `PEM` 变量二选一。

## 首次启动：生成管理员账号

### 步骤 1: 生成 RSA 密钥对

```bash
# 2048 位 RSA 私钥（最低要求）
openssl genrsa -out /etc/weave/jwt-private.pem 2048

# 从私钥导出公钥
openssl rsa -in /etc/weave/jwt-private.pem -pubout -out /etc/weave/jwt-public.pem

# 保护文件权限
chmod 600 /etc/weave/jwt-private.pem
chmod 644 /etc/weave/jwt-public.pem
```

### 步骤 2: 设置启动环境变量

```bash
export AUTH_MODE=jwt
export WEAVE_JWT_PRIVATE_KEY_PATH=/etc/weave/jwt-private.pem
export WEAVE_JWT_PUBLIC_KEY_PATH=/etc/weave/jwt-public.pem

# 首次启动的管理员种子
export WEAVE_BOOTSTRAP_ADMIN=user:admin@weave.local
export WEAVE_BOOTSTRAP_ADMIN_PASSWORD='letmein123'
```

> ⚠️ `WEAVE_BOOTSTRAP_ADMIN_PASSWORD` 在日志中会被遮蔽，但仍建议通过 systemd `EnvironmentFile`、Docker secret 或 Kubernetes secret 传递，而不是直接写到 shell history。

### 步骤 3: 启动服务

```bash
./bin/weave
```

首次启动时 `cmd/server/main.go:315` 会：

1. 读取 `WEAVE_BOOTSTRAP_ADMIN` 邮箱
2. 幂等插入 `users` 表（通过 `auth.BootstrapAdmin`）
3. 授予全局 `admin` 角色（通过 `user_roles` 表）
4. 如果 `WEAVE_BOOTSTRAP_ADMIN_PASSWORD` 也已设置，用 bcrypt 哈希后存入 `users.password_hash`
5. 输出日志：
   ```
   [RBAC] Bootstrapped initial admin: user:admin@weave.local
   [AUTH] Bootstrapped admin password for user:admin@weave.local
   [AUTH] JWT tier B (RS256) enabled, issuer=weave
   ```

### 步骤 4: 首次登录

打开浏览器访问 `http://localhost:9117/`（或 `http://localhost:5173/` 用 Vite 开发代理），跳转到 `/login`：

- **Email**: `admin@weave.local`
- **Password**: `letmein123`

登录成功后：
- 访问令牌存储在 Zustand 内存 store（避免 XSS 通过 localStorage 窃取）
- 刷新令牌以 httpOnly + SameSite=Strict cookie 存储（路径限定 `/api/auth`）
- 所有后续 API 请求自动附加 `Authorization: Bearer <access>`
- 401 响应会触发单例刷新流程并重试原请求

## 后续账号管理

### 添加新用户（手动 SQL）

Weave 目前没有"注册"页面；新用户需管理员通过 SQL 或未来的 user admin API 创建：

```sql
-- 1. 插入用户
INSERT INTO users (id, email, name, password_hash)
VALUES (
  'user:alice@example.com',
  'alice@example.com',
  'Alice',
  -- 用 bcrypt cost 12 预先哈希（见下方 Go 脚本）
  '$2a$12$...'
);

-- 2. 授予角色（全局）
INSERT INTO user_roles (user_id, role)
VALUES ('user:alice@example.com', 'editor');

-- 或者授予特定 ontology 范围的角色
INSERT INTO user_ontology_roles (user_id, ontology_rid, role)
VALUES ('user:alice@example.com', 'ri.ontology.main.northwind', 'ontology-owner');
```

四种角色（`pkg/auth/permissions.go`）：

| 角色 | 典型权限 |
|------|---------|
| `admin` | 全部 26 个权限 |
| `ontology-owner` | 单个 ontology 内的完整 CRUD + 动作执行 |
| `editor` | 对象与动作的写权限，不包括 schema 修改 |
| `viewer` | 只读 |

### 生成 bcrypt 密码哈希

```bash
# 使用 weave-cli 工具（Tier 2.5）
./bin/weave-cli auth hash-password 'alice-new-password'

# 或用 Go 一行脚本
go run -tags tools ./scripts/hash_password.go 'alice-new-password'

# 或用 htpasswd（bcrypt 模式，cost 12）
htpasswd -bnBC 12 '' 'alice-new-password' | cut -d: -f2
```

### API Keys（服务器到服务器）

对于 Airflow、CI、Python SDK 等非交互场景，使用 API Key 而不是密码登录：

```bash
# 作为管理员登录后调用
curl -X POST http://localhost:9117/api/admin/api-keys \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "airflow-pipeline", "expiresAt": "2027-01-01T00:00:00Z"}'
```

响应（**原始 key 仅在创建时返回一次**）：
```json
{
  "id": "01HG...",
  "name": "airflow-pipeline",
  "rawKey": "wvk_abcd1234_<32-bytes-base32>",
  "prefix": "abcd1234",
  "expiresAt": "2027-01-01T00:00:00Z",
  "createdAt": "2026-04-08T10:00:00Z"
}
```

使用 API Key：
```bash
curl http://localhost:9117/api/v2/ontologies \
  -H "Authorization: Bearer wvk_abcd1234_<32-bytes-base32>"
```

API Key 特性：
- 格式 `wvk_<8-char-prefix>_<32-byte-random>`
- 数据库只存 SHA-256 哈希（`api_keys.key_hash`）
- 前缀用于索引查找，常量时间哈希比较
- 撤销通过 `revoked_at` 软删除，立即生效
- 支持过期时间 + `last_used_at` 审计

## 密钥轮换

### 轮换 JWT 签名密钥

当前实现为单密钥签名（Phase 3）。轮换步骤：

1. 生成新密钥对
2. 替换 `WEAVE_JWT_PRIVATE_KEY_PATH` 和 `WEAVE_JWT_PUBLIC_KEY_PATH`
3. 重启服务
4. **所有现有访问令牌立即失效**（15 分钟窗口内客户端会收到 401 并用 refresh token 重新签发）
5. 超过 7 天的 refresh token 也会失效，用户需重新登录

多密钥 + `kid` header 轮换（零停机）在 Phase 4 路线图中，未实现。

### 轮换 Bootstrap 管理员密码

1. 登录管理员账号
2. 调用密码更新 API（未实现；当前需手动 SQL 更新 `users.password_hash`）
3. 或重设 `WEAVE_BOOTSTRAP_ADMIN_PASSWORD` 并删除 `users` 行，重启服务

## 故障排查

### 登录时收到 401 Invalid credentials

- 检查 `users` 表中是否存在该 email 的行
- 检查 `password_hash` 字段是否非空
- 确认 `AUTH_MODE=jwt` 已设置（`dev` 模式不走登录流程）
- 检查服务器日志是否有 `[AUTH] password verify failed` 警告

### 启动时 `[AUTH] FATAL: AUTH_MODE=jwt but key load failed`

- 检查密钥文件路径存在且可读
- 检查 RSA 位数 ≥ 2048（Weave 强制）
- 支持的格式：PKCS#1、PKCS#8、PKIX
- 测试密钥：
  ```bash
  openssl rsa -in /etc/weave/jwt-private.pem -check
  ```

### 所有请求返回 401 即使 Bearer token 有效

- 检查 `Issuer` 和 `Audience` 与签发时一致
- 检查服务器时间与客户端时间偏差小于 5 分钟（JWT 的 `exp` / `nbf` 检查）
- 检查日志是否有 `ErrTokenExpired`、`ErrInvalidSignature`、`ErrInvalidIssuer`、`ErrInvalidAudience`

### 首次启动后 `/api/v2/me` 返回 401

- 确认在 `AUTH_MODE=jwt` 下已用 `/api/auth/login` 获取 access token
- 检查 Authorization header 格式：`Bearer <token>`（注意 `Bearer` 后有一个空格）

## 会话管理（rounds 101-102）

JWT 模式下登录会在服务端登记一个 session 行，方便用户多设备登录后单独
撤销某条会话或一键登出其他设备。三个端点：

| 方法 | 路径 | 用途 |
|------|------|------|
| `GET` | `/api/auth/sessions` | 列出当前用户所有 active sessions（含 `id` / `createdAt` / `lastSeenAt` / `userAgent` / `ipHash`） |
| `POST` | `/api/auth/sessions/{id}/revoke` | 撤销指定 session（自己的或 admin 代撤），下次该 session token 命中 middleware 即 401 |
| `POST` | `/api/auth/sessions/revoke-others` | 撤销当前用户除当前请求 session 外的所有其他 sessions（"sign out other devices"，round 101 / commit a29e9d2） |

Python SDK 镜像（round 102 / commit c737af2）：

```python
from weave_client import WeaveClient

client = WeaveClient("http://localhost:9117", token=...)

# 列出 active sessions
for s in client.sessions.list():
    print(s.id, s.user_agent, s.last_seen_at)

# 一键登出其他设备
client.sessions.revoke_others()

# 撤销具体 session
client.sessions.revoke(session_id="ses-abc123")
```

异步客户端同名方法：`await async_client.sessions.list()` 等。

### 错误分类

| Exception | HTTP | Notes |
|---|---|---|
| `WeaveAuthError` | 401 / 403 | base auth class — 凭证缺失 / 失效 / 已撤销均归此类；`exc.error_name` 区分 `MissingAuthenticatedUser` / `InvalidCredentials` / `SessionRevoked` 子情况 |
| `WeaveError`（基类） | 任意非 2xx | 兜底；现有 `except WeaveError:` 块继续兼容 |

`WeaveAuthError.error_instance_id` 始终带服务端生成的 UUID，方便对照 audit log 中的 `login_failed` / `token_refresh` 事件定位。

## 相关代码位置

| 功能 | 文件 |
|------|------|
| 认证中间件三模式分支 | `pkg/auth/middleware.go` |
| Bootstrap admin 逻辑 | `pkg/auth/bootstrap.go`, `cmd/server/main.go:315` |
| JWT 签名/验证 | `pkg/auth/jwt_signer.go` |
| 登录 handler（密码验证 + rate limit） | `pkg/auth/login_handler.go` |
| 刷新/登出 handlers | `pkg/auth/refresh_handler.go`, `pkg/auth/logout_handler.go` |
| Password hashing | `pkg/auth/password.go` |
| API Key 生成/验证 | `pkg/auth/api_key.go` |
| RBAC 权限矩阵 | `pkg/auth/permissions.go` |
| Role resolver（含 LRU 缓存） | `pkg/auth/role_resolver.go` |
| 前端 AuthContext | `web/src/auth/AuthContext.tsx` |
| 前端 LoginPage | `web/src/auth/LoginPage.tsx` |
| 前端 authStore | `web/src/auth/authStore.ts` |
| 401 拦截器 + 自动刷新 | `web/src/auth/interceptor.ts` |

## 快速命令参考

```bash
# 1. 开发模式（零配置，无登录）
make dev
# → 访问 http://localhost:9117/ 或 http://localhost:5173/

# 2. JWT 模式首次启动
openssl genrsa -out /tmp/weave-jwt.pem 2048
openssl rsa -in /tmp/weave-jwt.pem -pubout -out /tmp/weave-jwt.pub

AUTH_MODE=jwt \
WEAVE_JWT_PRIVATE_KEY_PATH=/tmp/weave-jwt.pem \
WEAVE_JWT_PUBLIC_KEY_PATH=/tmp/weave-jwt.pub \
WEAVE_BOOTSTRAP_ADMIN=user:admin@weave.local \
WEAVE_BOOTSTRAP_ADMIN_PASSWORD='letmein123' \
./bin/weave

# 3. 登录并获取 token
curl -X POST http://localhost:9117/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@weave.local", "password": "letmein123"}'

# 4. 用 token 访问 /api/v2/me
TOKEN='...'
curl http://localhost:9117/api/v2/me -H "Authorization: Bearer $TOKEN"
```
