# Palantir Foundry Ontology Layer 完整技术蓝图

**Palantir Foundry 的 Ontology Layer（本体层）是一个构建在数据集之上的语义操作层，充当组织的"数字孪生"。** 它将原始数据（Dataset）映射为业务实体（Object），通过类型系统、关系模型、Action/Function 执行引擎以及细粒度权限模型，为上层应用提供统一的领域驱动数据访问接口。本报告系统梳理了 Ontology 的完整架构、~43 个 REST API v2 端点的详细规范、数据读写机制和内部设计理念，为用 Golang 复刻该系统提供可执行的技术蓝图。

---

## 一、三层架构：语义层、动力层与治理层

Ontology 在 Foundry 技术栈中处于**数据集成层与终端应用之间的中间层**，其架构从下到上由三个概念层组成：

**语义层（Semantic Layer）** 定义核心数据模型——Object Types（对象类型）、Properties（属性）、Link Types（链接类型）和 Interfaces（接口），将底层 Dataset 的行列映射为业务实体及其关系。**动力层（Kinetic Layer）** 赋予模型"行为"能力——Action Types 封装原子化写操作，Functions 提供任意复杂度的代码逻辑（TypeScript/Python）。**治理层（Dynamic Layer）** 则通过对象安全策略、属性安全策略和 Marking 机制实现行列级动态权限控制。

> **参考文档**：
> - [Ontology Overview](https://www.palantir.com/docs/foundry/ontology/overview)
> - [The Ontology System (Architecture Center)](https://www.palantir.com/docs/foundry/architecture-center/ontology-system)
> - [Understanding Palantir's Ontology: Semantic, Kinetic, and Dynamic Layers Explained (Medium)](https://pythonebasta.medium.com/understanding-palantirs-ontology-semantic-kinetic-and-dynamic-layers-explained-c1c25b39ea3c)

完整的后端微服务架构如下：

```
[终端应用: Workshop / Object Explorer / OSDK 客户端 / 自定义应用]
                           ↕ REST API (v1/v2)
  [Ontology Metadata Service (OMS)] — 定义所有类型元数据
  [Object Set Service (OSS)]        — 服务所有读请求（搜索/过滤/聚合）
  [Actions Service]                 — 验证并执行写操作
  [Object Data Funnel]              — 编排数据写入/索引
                           ↕
  [Object Databases: Object Storage V2] — 索引化对象存储
                           ↕
  [Foundry 数据集成层: Datasets / Streams / Restricted Views]
                           ↕
  [底层存储: S3 / HDFS / Blob Storage]
```

其中 **OMS** 管理所有本体元数据（对象类型定义、链接类型、Action 类型等），**OSS** 处理所有读查询并路由到合适的后端，**Object Data Funnel** 从数据源读取数据并建立索引，**Object Storage V2** 是当前一代对象数据库，将索引与查询关注点分离以实现水平扩展。

> **参考文档**：
> - [Object Backend Overview](https://www.palantir.com/docs/foundry/object-backend/overview)

---

## 二、核心概念与类型系统的完整定义

### Object Type：对象类型是一切的基础

Object Type 等价于数据库中的"表定义"，每个实例（Object）对应一行数据。其元数据结构包含以下关键字段：

| 字段 | 说明 | 示例 |
|------|------|------|
| `apiName` | API 唯一标识符（不可变） | `employee` |
| `rid` | 系统生成的资源标识符 | `ri.ontology.main.object-type.xxx` |
| `displayName` / `pluralDisplayName` | 显示名称 | `Employee` / `Employees` |
| `primaryKey` | 主键属性（唯一标识每个对象） | `employeeId` |
| `titleKey` | 标题属性（UI 显示名） | `fullName` |
| `status` | 状态枚举 | `ACTIVE` / `EXPERIMENTAL` / `DEPRECATED` |
| `visibility` | 可见性 | `prominent` / `normal` / `hidden` |
| `properties` | 属性字典 | 见下文 |

API 返回的 JSON Schema（`GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}`）：

```json
{
  "apiName": "employee",
  "displayName": "Employee",
  "pluralDisplayName": "Employees",
  "status": "ACTIVE",
  "primaryKey": "employeeId",
  "rid": "ri.ontology.main.object-type.xxxxx",
  "properties": {
    "employeeId": {
      "dataType": {"type": "integer"},
      "rid": "ri.ontology.main.property.571d3d4d-..."
    },
    "fullName": {
      "dataType": {"type": "string"},
      "rid": "ri.ontology.main.property.5721baa7-..."
    }
  },
  "visibility": "NORMAL"
}
```

> **参考文档**：
> - [Get Object Type API](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/object-types/get-object-type)
> - [Core Concepts](https://www.palantir.com/docs/foundry/ontology/core-concepts)

### 属性类型系统：18 种基础类型

**Foundry 支持 18 种属性基础类型**，这是 Golang 复刻时需要完整实现的类型映射：

| 基础类型 | 可作主键 | 可作标题 | Go 映射建议 | 备注 |
|---------|---------|---------|------------|------|
| **String** | ✅ | ✅ | `string` | 最常用类型 |
| **Integer** | ✅ | ✅ | `int32` | |
| **Short** | ✅ | ✅ | `int16` | |
| **Long** | ⚠️ | ✅ | `string` | JS 精度问题，建议字符串传输 |
| **Float** | ❌ | ✅ | `float32` | |
| **Double** | ❌ | ✅ | `float64` | |
| **Boolean** | ⚠️ | ✅ | `bool` | |
| **Byte** | ⚠️ | ✅ | `int8` | |
| **Date** | ⚠️ | ✅ | `string` (ISO 8601) | 格式 `2024-01-15` |
| **Timestamp** | ⚠️ | ✅ | `string` (ISO 8601) | 格式 `2010-10-01T00:00:00Z` |
| **Decimal** | ❌ | ✅ | `string` | 高精度十进制 |
| **Array** | ❌ | ✅ | `[]T` | 不可嵌套数组（OSv2） |
| **Struct** | ❌ | ❌ | `map[string]interface{}` | 不可嵌套 |
| **Vector** | ❌ | ❌ | `[]float64` | 语义搜索嵌入向量 |
| **Geopoint** | ❌ | ✅ | GeoJSON struct | 地理坐标点 |
| **Geoshape** | ❌ | ❌ | GeoJSON struct | 多边形/线 |
| **Attachment** | ❌ | ❌ | `string` (RID) | 文件引用 |
| **TimeSeries** | ❌ | ❌ | 专用结构体 | 时间序列数据 |
| **MediaReference** | ❌ | ❌ | JSON struct | 媒体引用 |
| **Marking** | ❌ | ❌ | `string` | 安全标记 |
| **Cipher** | ❌ | ✅ | `string` | 加密字符串 |

**OSv2 限制：每个 Object Type 最多 2,000 个属性。**

> **参考文档**：
> - [Properties Overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview)
> - [Types Reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference)
> - [Value Types Overview](https://www.palantir.com/docs/foundry/object-link-types/value-types-overview)

### Link Type：三种关系基数模型

链接类型定义对象间的关系，支持三种基数：

**一对一/一对多（Foreign Key）**：通过属性值匹配实现——一个对象类型的某个属性值与另一个对象类型的主键匹配，无需额外数据集。例如 `Employee.departmentId` 匹配 `Department.departmentId`。

**多对多（Join Table）**：需要一个独立的连接表数据集，包含两个对象类型的主键列。例如 `Employee-Project` 关系需要一个包含 `employeeId` 和 `projectId` 列的数据集。

**对象支撑的链接（Object-backed Link）**：链接本身是一个对象类型（如"航班清单"连接"飞机"和"航班"），允许在链接上附加属性和元数据。

> **参考文档**：
> - [Create a Link Type](https://www.palantir.com/docs/foundry/object-link-types/create-link-type)

### Interface：对象类型的多态抽象

Interface 定义了对象类型的"形状"约束——共享属性和链接类型。多个对象类型可以"实现"同一个接口，实现**对象类型多态性**。接口支持继承（`extends`），为跨类型统一查询提供基础。

### Value Type：带约束的语义类型包装器

Value Type 是对基础类型的语义封装，附带验证约束（如邮箱格式的正则表达式）。它们在 Space 级别管理，支持版本控制和跨对象类型复用。

---

## 三、Ontology REST API v2 完整端点规范

### 共 ~43 个端点的完整列表

所有端点基础 URL：`https://{HOSTNAME}/api/v2/ontologies/{ontology}/...`

**认证方式**：`Authorization: Bearer {ACCESS_TOKEN}`（支持 OAuth2 授权码 + PKCE 和客户端凭证两种模式）。

**核心 OAuth2 Scope**：`api:ontologies-read`（读操作）、`api:ontologies-write`（写操作）。

> **参考文档**：
> - [API Introduction](https://www.palantir.com/docs/foundry/api/general/overview/introduction)
> - [Authentication](https://www.palantir.com/docs/foundry/api/general/overview/authentication)

#### 本体与类型元数据（11 个端点）

| 方法 | 端点 | 功能 | 参考链接 |
|------|------|------|---------|
| GET | `/ontologies` | 列出可见的本体 | [List Ontologies](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontologies/list-ontologies) |
| GET | `/ontologies/{ontology}` | 获取本体详情 | |
| GET | `/ontologies/{ontology}/fullMetadata` | 获取完整元数据（preview） | |
| GET | `/.../objectTypes` | 列出对象类型 | |
| GET | `/.../objectTypes/{objectType}` | 获取对象类型详情 | [Get Object Type](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/object-types/get-object-type) |
| GET | `/.../objectTypes/{objectType}/outgoingLinkTypes` | 列出出向链接类型 | |
| GET | `/.../objectTypes/{objectType}/outgoingLinkTypes/{linkType}` | 获取链接类型详情 | |
| GET | `/.../actionTypes` | 列出 Action 类型 | |
| GET | `/.../actionTypes/{actionType}` | 获取 Action 类型详情 | |
| GET | `/.../queryTypes` | 列出查询类型 | |
| GET | `/.../queryTypes/{queryType}` | 获取查询类型详情 | |

#### 对象 CRUD 与查询（6 个端点）

| 方法 | 端点 | 功能 | 参考链接 |
|------|------|------|---------|
| GET | `/.../objects/{objectType}` | 分页列出对象 | [List Objects](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/list-objects) |
| GET | `/.../objects/{objectType}/{primaryKey}` | 按主键获取对象 | [Get Object](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/get-object) |
| POST | `/.../objects/{objectType}/search` | 搜索对象（带过滤条件） | [Search Objects](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/search-objects) |
| POST | `/.../objects/{objectType}/aggregate` | 聚合对象 | |
| GET | `/.../objects/{objectType}/{pk}/links/{linkType}` | 列出关联对象 | |
| GET | `/.../objects/{objectType}/{pk}/links/{linkType}/{linkedPK}` | 获取特定关联对象 | |

#### ObjectSet 操作（5 个端点）

| 方法 | 端点 | 功能 | 参考链接 |
|------|------|------|---------|
| POST | `/.../objectSets/loadObjects` | **核心端点**：加载对象集 | [Load Object Set](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set) |
| POST | `/.../objectSets/loadObjects/multipleObjectTypes` | 加载多类型对象集 | |
| POST | `/.../objectSets/loadObjectsOrInterfaces` | 加载对象或接口（preview） | |
| POST | `/.../objectSets/aggregate` | 聚合对象集 | |
| POST | `/.../objectSets/createTemporary` | 创建临时对象集（1小时过期） | |

#### Action 执行（2 个端点）

| 方法 | 端点 | 功能 | 参考链接 |
|------|------|------|---------|
| POST | `/.../actions/{actionType}/apply` | 执行 Action | [Apply Action](https://www.palantir.com/docs/foundry/api/ontology-resources/actions/apply-action) |
| POST | `/.../actions/{actionType}/applyBatch` | 批量执行 Action（最多20个） | [Apply Action Batch](https://www.palantir.com/docs/foundry/api/ontology-resources/actions/apply-action-batch) |

#### 查询执行（1 个端点）

| 方法 | 端点 | 功能 |
|------|------|------|
| POST | `/.../queries/{queryApiName}/execute` | 执行查询函数 |

#### 附件/媒体/时间序列等特殊属性（约 15 个端点）

涵盖 Attachment（上传/下载/列表）、MediaReference（读取/上传内容）、TimeSeries（首点/末点/流式读取）、TimeSeriesValueBank（最新值/流式值）、CipherText（解密）以及 Interface/ValueType 操作。

### 核心 JSON 线缆协议详解

#### 对象响应格式（V2）

V2 将属性**扁平化到顶层**，元数据字段以 `__` 前缀标识：

```json
{
  "__rid": "ri.phonograph2-objects.main.object.5b5dbc28-...",
  "__primaryKey": 50030,
  "__apiName": "Employee",
  "employeeId": 50030,
  "firstName": "John",
  "lastName": "Smith",
  "age": 21
}
```

**关键差异**：V1 将属性嵌套在 `"properties"` 字段下，字段名需加 `properties.` 前缀；V2 扁平化处理，字段名直接使用 apiName。V1 使用 ontology RID，V2 可使用 API 名称或 RID。

> **参考文档**：
> - [Search Objects V1 Details](https://www.palantir.com/docs/foundry/api/ontology-resources/objects/search)
> - [OSv1 vs OSv2 Breaking Changes](https://www.palantir.com/docs/foundry/object-backend/object-storage-v2-breaking-changes)
> - [foundry-platform-python OntologyObject](https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Ontologies/OntologyObject.md)

#### 搜索查询 Where 子句（核心过滤系统）

采用基于 `type` 字段的**判别联合体（Discriminated Union）** JSON 格式：

```json
// 等值过滤
{"type": "eq", "field": "age", "value": 21}

// 范围过滤
{"type": "gte", "field": "salary", "value": 50000}

// 空值检查
{"type": "isNull", "field": "email", "value": true}

// 全文搜索
{"type": "containsAllTerms", "field": "description", "value": "machine learning"}

// 数组包含
{"type": "contains", "field": "tags", "value": "urgent"}

// 逻辑组合（最多嵌套 3 层）
{
  "type": "and",
  "value": [
    {"type": "gte", "field": "age", "value": 18},
    {"type": "lt", "field": "age", "value": 65},
    {"type": "not", "value": {"type": "eq", "field": "status", "value": "inactive"}}
  ]
}
```

**完整的过滤操作符列表**：`eq`、`lt`、`gt`、`lte`、`gte`、`isNull`、`contains`、`not`、`and`、`or`、`containsAllTerms`、`containsAnyTerm`、`containsAllTermsInOrder`、`containsAllTermsInOrderPrefixLastTerm`（`startsWith` 为其已废弃别名）。术语按空白字符和 `?!,:;-[](){}'\"~` 分割。

> **参考文档**：
> - [Search Objects V2](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/search-objects)

#### ObjectSet 定义格式（可组合 JSON 结构）

ObjectSet 是 V2 API 的**一等公民**，支持声明式组合：

```json
// 基础集合
{"type": "base", "objectType": "Employee"}

// 过滤集合
{"type": "filter", "objectSet": {"type": "base", "objectType": "Employee"},
 "where": {"type": "eq", "field": "city", "value": "NYC"}}

// 并集
{"type": "union", "objectSets": ["<set1>", "<set2>"]}

// 交集
{"type": "intersect", "objectSets": ["<set1>", "<set2>"]}

// 差集
{"type": "subtract", "objectSets": ["<set1>", "<set2>"]}

// 链接遍历（Search Around）
{"type": "searchAround", "objectSet": "<set>", "link": "directReport"}

// 引用已保存的对象集
{"type": "reference", "reference": "ri.object-set.main.object-set.c32ccba5-..."}
```

> **参考文档**：
> - [Load Object Set](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set)
> - [foundry-platform-python OntologyObjectSet](https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Ontologies/OntologyObjectSet.md)
> - [Functions on Objects — Object Sets](https://www.palantir.com/docs/foundry/functions/api-object-sets)

#### 聚合请求/响应格式

```json
// 请求
{
  "aggregation": [
    {"type": "count", "name": "total"},
    {"type": "avg", "field": "salary", "name": "avg_salary"},
    {"type": "max", "field": "tenure", "name": "max_tenure"},
    {"type": "approximateDistinct", "field": "department", "name": "dept_count"}
  ],
  "groupBy": [
    {"field": "city", "type": "exact"},
    {"field": "age", "type": "fixedWidth", "fixedWidth": 10},
    {"field": "startDate", "type": "duration", "value": {"unit": "DAYS", "value": 30}},
    {"field": "salary", "type": "range", "ranges": [
      {"startValue": "0", "endValue": "50000"},
      {"startValue": "50000", "endValue": "100000"}
    ]}
  ],
  "where": {"type": "eq", "field": "active", "value": true},
  "accuracy": "REQUIRE_ACCURATE"
}

// 响应
{
  "excludedItems": 0,
  "data": [
    {
      "group": {"city": "New York", "age": {"startValue": "20"}},
      "metrics": [
        {"name": "total", "value": 150},
        {"name": "avg_salary", "value": 85000.50}
      ]
    }
  ]
}
```

**聚合类型**：`min`、`max`、`avg`、`sum`、`count`、`approximateDistinct`。**GroupBy 类型**：`exact`、`fixedWidth`、`range`、`duration`、`topValues`。最多 10K 个分桶。

#### 分页机制

采用**游标令牌分页**：首次请求省略 `pageToken`，后续请求使用响应中的 `nextPageToken`。当响应中无 `nextPageToken` 字段时表示数据已读完。默认页大小 1,000。设置 `snapshot: true` 可保证分页一致性（避免遍历过程中的数据变更导致重复/遗漏）。**OSv1 限制总计 10,000 个对象，OSv2 无限制。**

#### 统一错误响应格式

```json
{
  "errorCode": "NOT_FOUND",
  "errorName": "ObjectNotFound",
  "errorInstanceId": "00813215-0844-4716-be7b-a3fe0fce9e42",
  "parameters": {
    "objectType": "employee",
    "primaryKey": {"id": 10000}
  }
}
```

---

## 四、数据写入机制：Action 是唯一合法写入路径

### 写入流程的完整链路

在 OSv2 架构中，**所有对象修改必须通过 Action 执行**，不存在裸 CRUD 端点直接写入对象。完整写入链路：

```
API 客户端 → POST /actions/{actionType}/apply → Actions Service（参数验证 + 权限检查 + 对象版本加载）
→ 执行 Action 规则（简单规则 或 Function-backed 逻辑）→ 生成编辑指令
→ Funnel Service（存入托管队列，offset 追踪）→ Object Databases（立即更新索引）
→ [异步] Materialized Datasets（持久化到 Foundry 数据集）
```

**强一致性保证**：在 OSv2 中，如果一次对象读取发生在用户修改发送之后，该读取**保证包含用户编辑**。

> **参考文档**：
> - [How User Edits Are Applied](https://www.palantir.com/docs/foundry/object-edits/how-edits-applied)

### Action 的完整定义结构

一个 Action Type 由以下组件构成：

**参数（Parameters）**：支持的类型包括 String、Integer、Long、Double、Boolean、Timestamp、Date、Object Reference（对象引用）、Object Set、Attachment、GeoPoint、GeoJSON、Struct、Array 等。

**规则（Rules）**：定义操作的具体行为——创建对象（Create Object）、修改对象（Modify Object）、删除对象（Delete Object）、创建链接（Create Link）、删除链接（Delete Link）。对于复杂逻辑，可使用 **Function-backed Actions**，将逻辑委托给 TypeScript/Python 函数。

**提交条件（Submission Criteria）**：细粒度的执行权限控制，支持基于用户 ID、用户组、参数条件的组合判断。

**副作用（Side Effects）**：Action 成功后触发通知（Notifications）或 Webhook。

> **参考文档**：
> - [Action Types — Function-backed Actions Overview](https://www.palantir.com/docs/foundry/action-types/function-actions-overview)
> - [Action Types — Function-backed Actions Getting Started](https://www.palantir.com/docs/foundry/action-types/function-actions-getting-started)
> - [Action Types — Permissions](https://www.palantir.com/docs/foundry/action-types/permissions)
> - [Use Actions in Workshop](https://www.palantir.com/docs/foundry/workshop/actions-use)

### Action 执行的完整生命周期

Action 从触发到完成经历 9 个阶段：参数验证 → 提交条件评估 → 对象加载（带版本追踪） → 规则执行 → 编辑折叠（多个操作合并为最少编辑） → 原子事务提交 → 副作用触发 → 写入 Object Storage → 审计日志记录。批量执行通过 `applyBatch` 端点支持，每次最多 **20 个 Action**。

### Writeback / Materialization 机制

OSv2 将写回数据集重命名为**物化数据集（Materialized Datasets）**。用户编辑首先写入 Object Databases（索引层，被视为临时数据），然后异步物化到持久 Foundry 数据集。物化支持两种模式：**自动传播**（分钟级延迟）和**定期构建**（如每 6 小时或上游数据更新时）。物化数据集使用 `__is_deleted` 和 `__patch_offset` 元数据列进行去重。

> **参考文档**：
> - [Materializations](https://www.palantir.com/docs/foundry/object-edits/materializations)

---

## 五、Function 机制：代码即逻辑的执行引擎

### 两代运行时并存

**TypeScript v1**（旧版）：基于类和装饰器模式，使用 `@Function()`、`@OntologyEditFunction()`、`@Query()` 等装饰器，从 `@foundry/functions-api` 导入。

**TypeScript v2**（当前版，Node.js 运行时）：文件级函数导出，使用 `@osdk/functions` 和 `@ontology/sdk`，每个函数一个文件。

```typescript
// V2 查询函数示例
import { Client } from "@osdk/client";
import { Employee } from "@ontology/sdk";

export default function getLocation(client: Client, employee: Employee): string {
  return `${employee.city}, ${employee.country}`;
}

// V2 编辑函数示例
import { createEditBatch, Edits } from "@osdk/functions";
export default function createTicket(client: Client): OntologyEdit[] {
  const batch = createEditBatch<OntologyEdit>(client);
  const ticket = batch.create(Ticket, { title: "New Issue", status: "Open" });
  return batch.getEdits();
}
```

> **参考文档**：
> - [TypeScript v2 Getting Started](https://www.palantir.com/docs/foundry/functions/typescript-v2-getting-started)
> - [Use Functions in the Platform](https://www.palantir.com/docs/foundry/functions/use-functions)
> - [Ontology Edits (Functions)](https://www.palantir.com/docs/foundry/functions/api-ontology-edits)
> - [foundry-platform-python Query Functions](https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Functions/Query.md)

### 两类函数的本质区别

**查询函数（Query Functions）**：只读，通过 `POST /api/v2/ontologies/{ontology}/queries/{queryApiName}/execute` 调用，支持流式响应（NDJSON），不可修改任何数据。

**编辑函数（Edit Functions）**：可创建/修改/删除对象和链接，但**必须绑定到 Action 才能实际生效**——独立运行不会修改数据。这是一个关键的设计约束。

**支持的输入输出类型**：标量类型、Object/ObjectSet、Interface、Array/Set/Map、自定义 TypeScript 接口、TwoDimensionalAggregation/ThreeDimensionalAggregation、Optional/Nullable、Struct、Marking/MediaReference 等。

---

## 六、数据流与存储架构的内部实现

### 从外部系统到 Ontology 的完整数据链路

数据经过四个阶段到达 Ontology：**数据连接（Data Connection）** 从外部源（S3、ADLS、数据库、Kafka、Kinesis 等）拉取数据；**转换层** 使用 Apache Spark（批处理）和 Apache Flink（流处理）进行 ETL，默认输出 Apache Parquet 格式；**DAG 编排** 自动感知上游数据变更并触发下游重建；**Ontology 映射** 在 Ontology Manager 中将策展后的数据集列映射为对象属性。

> **参考文档**：
> - [Connecting to Data](https://www.palantir.com/docs/foundry/data-integration/connecting-to-data)
> - [Streaming Guide](https://www.palantir.com/docs/foundry/data-integration/streaming-guide)
> - [Pipeline Builder Overview](https://www.palantir.com/docs/foundry/pipeline-builder/overview)

### Object Storage V2 的分离式架构

OSv2 从第一性原理出发重新设计，核心创新是**索引与查询的关注点分离**：

**Object Data Funnel** 负责所有写入编排——从数据源读取数据和用户编辑，建立索引。支持批处理管道（定期重索引）和流式管道（**平均 15 秒内**将流式数据索引到 Ontology）。默认启用增量索引。

**Object Databases** 是专用索引存储，针对不同使用场景优化，支持水平扩展。所有索引数据被视为**临时数据**（ephemeral），可从持久数据集重建。

**Object Set Service (OSS)** 作为统一读服务，处理所有查询并路由到合适的后端。底层查询能力包括全文搜索（带模糊匹配，基于 Levenshtein 距离，暗示使用 ElasticSearch）、范围过滤、地理空间过滤、链接遍历和 SQL 查询。

> **参考文档**：
> - [Object Backend Overview](https://www.palantir.com/docs/foundry/object-backend/overview)
> - [Indexing Overview](https://www.palantir.com/docs/foundry/object-indexing/overview)
> - [Ontology Volume Usage](https://www.palantir.com/docs/foundry/ontologies/volume-usage)

### 数据版本控制："Git for Data"

Foundry 的版本控制模型类比 Git：**Transaction** 等价于 commit，分为 SNAPSHOT（全量替换）、APPEND（追加）、UPDATE（修改）、DELETE 四种类型。**Branch** 类似 Git 分支，但不支持合并——每个非根分支有且只有一个父级。数据集在任意时间点的视图通过从最近的 SNAPSHOT 开始叠加后续 APPEND/UPDATE 事务计算得出。支持 **Pipeline Rollback**——回退上游数据集及所有下游依赖。底层使用自定义的 Hadoop FileSystem 实现透明版本控制。

> **参考文档**：
> - [Datasets](https://www.palantir.com/docs/foundry/data-integration/datasets)
> - [Branching](https://www.palantir.com/docs/foundry/data-integration/branching)
> - [Pipeline Rollback](https://www.palantir.com/docs/foundry/data-lineage/pipeline-rollback)
> - [On Dataset Versioning in Palantir Foundry (Blog)](https://blog.palantir.com/on-dataset-versioning-in-palantir-foundry-8f23de22cc4c)
> - [foundry-platform-python Branch](https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Datasets/Branch.md)

### 多层存储策略

- **热缓冲（Hot Buffer）**：基于 Kafka 的流式层，亚秒延迟
- **冷存储（Cold Storage）**：Parquet 文件存储在 S3/HDFS，定期归档
- **混合视图（Hybrid View）**：同时读取热+冷数据提供完整视图
- **索引层（Object Databases）**：快速访问的索引视图，可重建

> **参考文档**：
> - [Streams](https://www.palantir.com/docs/foundry/data-integration/streams)

---

## 七、实时订阅与流式处理

### WebSocket 对象订阅（OSDK v2.1+）

OSDK 提供基于 WebSocket 的实时订阅，当对象集中的对象发生变化时推送更新：

```typescript
client(Country).where({ continent: { $eq: "Europe" } }).subscribe({
  onChange: (update) => {
    // update.state: "ADDED_OR_UPDATED" | "DELETED"
    // update.object: 完整对象数据
  },
  onSuccessfulSubscription: () => {},
  onOutOfDate: () => { /* 需要重新加载完整对象集 */ },
  onError: () => { /* 订阅已关闭 */ }
}, { properties: ["population", "name"] });  // 可选属性过滤
```

核心回调：`onChange`（对象增删改）、`onOutOfDate`（客户端状态过期，需全量重载）、`onError`（连接断开）。支持属性选择以减少网络负载。

### Stream API

```
POST /api/v2/streams/datasets/{datasetRid}/streams  — 创建流
POST /api/v2/streams/datasets/{datasetRid}/streams/{branchName}/records  — 推送记录
```

支持两种流类型：`LOW_LATENCY`（低延迟优先）和 `HIGH_THROUGHPUT`（高吞吐优先），一致性保证可选 `AT_LEAST_ONCE` 或 `EXACTLY_ONCE`。流式数据支撑的对象类型可在 Ontology 中实时查询。

---

## 八、权限模型：从元数据到数据的分层安全

### 两级授权结构

**第一级：本体资源权限（Schema 级）**——控制谁能查看、编辑或管理对象类型、链接类型、Action 类型等定义。基于项目的角色模型（Owner/Editor/Viewer）。

**第二级：对象数据权限（Data 级）**——控制谁能看到哪些对象实例和属性值。包含两种策略：

**对象安全策略（Object Security Policies）**：行级安全，控制对象实例的可见性。配置在对象类型上，效果**近乎即时生效**。

**属性安全策略（Property Security Policies）**：列级安全，控制特定属性值的可见性。与 Marking 机制结合实现强制访问控制。

**安全在查询时执行**——不同用户查询相同 API 端点会看到不同数据子集，基于其组织、用户组、Marking 和安全策略进行动态过滤。

---

## 九、OSDK 架构与 Golang 复刻蓝图

### OSDK 的两层 SDK 架构

**Platform SDK**（通用层）：从 Foundry API 的 OpenAPI 规范自动生成，提供所有 API 端点的类型化绑定。已有 TypeScript（`@osdk/foundry.*`）、Python（`foundry-platform-sdk`）实现。

**Ontology SDK**（定制层）：从 Developer Console 为特定 Ontology 生成，产生带类型的客户端代码（如 `Employee` 类型带类型化属性）。支持 TypeScript、Python、Java 和 **OpenAPI 导出**（可用 openapi-generator 生成任意语言客户端）。

> **参考文档**：
> - [Java OSDK](https://www.palantir.com/docs/foundry/ontology-sdk/java-osdk)

### Golang 复刻的推荐架构

基于完整调研，建议采用以下分层架构：

```
┌─────────────────────────────────────────────────┐
│  Generated Ontology Client (代码生成的类型化客户端)  │
│  - 类型化 Object/Action/Function 定义              │
│  - 从 OpenAPI/元数据自动生成                        │
├─────────────────────────────────────────────────┤
│  Core Client (@osdk/client 等价物)                 │
│  - ObjectSet 构建器 (where/aggregate/pivotTo)      │
│  - Where 子句 DSL → Wire JSON 转换器               │
│  - 分页迭代器 (pageToken 自动管理)                   │
│  - Action 执行器 (apply/applyBatch)                │
│  - Query 执行器                                    │
│  - 订阅管理器 (WebSocket)                           │
├─────────────────────────────────────────────────┤
│  HTTP Transport Layer                              │
│  - OAuth2 客户端 (PKCE + Client Credentials)       │
│  - 自动 Token 刷新                                 │
│  - 重试策略 (401 自动重试一次)                       │
│  - 错误解析 (结构化错误响应)                         │
├─────────────────────────────────────────────────┤
│  Server-Side Implementation (如果要复刻后端)         │
│  - OMS: 元数据存储 (PostgreSQL/etcd)               │
│  - OSS: 查询路由 + ElasticSearch 集成               │
│  - Funnel: 写入编排 + 队列管理                      │
│  - Actions Service: 验证 + 执行 + 版本检查          │
│  - 索引管道: 增量/流式索引                           │
└─────────────────────────────────────────────────┘
```

### 关键实现要点

**Where 子句转换器**是核心组件。TypeScript OSDK 中 `modernToLegacyWhereClause.ts` 将 `$eq`/`$gte` 等 MongoDB 风格语法转换为 `{"type": "eq", "field": ..., "value": ...}` 线缆格式。Go 实现可用 functional options 或 builder pattern：

```go
// 建议的 Go DSL 设计
query := Where(
    And(
        Eq("city", "NYC"),
        Gte("age", 18),
        Not(IsNull("email", true)),
    ),
)
// 序列化为:
// {"type":"and","value":[
//   {"type":"eq","field":"city","value":"NYC"},
//   {"type":"gte","field":"age","value":18},
//   {"type":"not","value":{"type":"isNull","field":"email","value":true}}
// ]}
```

**ObjectSet 组合器**同样关键——需实现 `base`、`filter`、`union`、`intersect`、`subtract`、`searchAround`、`reference` 七种类型的 JSON 序列化。

**RID 格式**贯穿整个系统：`ri.{service}.{realm}.{type}.{uuid}`，如 `ri.ontology.main.object-type.xxx`、`ri.phonograph2-objects.main.object.xxx`。

---

## 十、公开资源索引与开发入口

### 官方文档核心页面

| 主题 | 链接 |
|------|------|
| Ontology Overview | https://www.palantir.com/docs/foundry/ontology/overview |
| Core Concepts | https://www.palantir.com/docs/foundry/ontology/core-concepts |
| Object Backend Overview | https://www.palantir.com/docs/foundry/object-backend/overview |
| The Ontology System (Architecture) | https://www.palantir.com/docs/foundry/architecture-center/ontology-system |
| API Introduction | https://www.palantir.com/docs/foundry/api/general/overview/introduction |
| API Authentication | https://www.palantir.com/docs/foundry/api/general/overview/authentication |
| OSv1→OSv2 Breaking Changes | https://www.palantir.com/docs/foundry/object-backend/object-storage-v2-breaking-changes |
| OSv1→OSv2 Migration | https://www.palantir.com/docs/foundry/object-backend/osv1-osv2-migration |
| Object & Link Types — Properties | https://www.palantir.com/docs/foundry/object-link-types/properties-overview |
| Object & Link Types — Types Reference | https://www.palantir.com/docs/foundry/object-link-types/type-reference |
| Object & Link Types — Value Types | https://www.palantir.com/docs/foundry/object-link-types/value-types-overview |
| Object & Link Types — Create Link Type | https://www.palantir.com/docs/foundry/object-link-types/create-link-type |
| Object Edits — How Edits Are Applied | https://www.palantir.com/docs/foundry/object-edits/how-edits-applied |
| Object Edits — Materializations | https://www.palantir.com/docs/foundry/object-edits/materializations |
| Indexing — Overview | https://www.palantir.com/docs/foundry/object-indexing/overview |
| Indexing — Funnel Batch Pipelines | https://www.palantir.com/docs/foundry/object-indexing/funnel-batch-pipelines |
| Indexing — FAQ | https://www.palantir.com/docs/foundry/object-indexing/faq |
| Action Types — Function-backed Overview | https://www.palantir.com/docs/foundry/action-types/function-actions-overview |
| Action Types — Getting Started | https://www.palantir.com/docs/foundry/action-types/function-actions-getting-started |
| Action Types — Permissions | https://www.palantir.com/docs/foundry/action-types/permissions |
| Functions — TypeScript v2 Getting Started | https://www.palantir.com/docs/foundry/functions/typescript-v2-getting-started |
| Functions — Use Functions | https://www.palantir.com/docs/foundry/functions/use-functions |
| Functions — Ontology Edits | https://www.palantir.com/docs/foundry/functions/api-ontology-edits |
| Functions — Object Sets | https://www.palantir.com/docs/foundry/functions/api-object-sets |
| Data Integration — Overview | https://www.palantir.com/docs/foundry/data-integration/overview |
| Data Integration — Datasets | https://www.palantir.com/docs/foundry/data-integration/datasets |
| Data Integration — Branching | https://www.palantir.com/docs/foundry/data-integration/branching |
| Data Integration — Streams | https://www.palantir.com/docs/foundry/data-integration/streams |
| Data Integration — Connecting to Data | https://www.palantir.com/docs/foundry/data-integration/connecting-to-data |
| Data Integration — Streaming Guide | https://www.palantir.com/docs/foundry/data-integration/streaming-guide |
| Pipeline Builder Overview | https://www.palantir.com/docs/foundry/pipeline-builder/overview |
| Pipeline Rollback | https://www.palantir.com/docs/foundry/data-lineage/pipeline-rollback |
| Ontology Volume Usage | https://www.palantir.com/docs/foundry/ontologies/volume-usage |
| Ontology Query Compute Usage | https://www.palantir.com/docs/foundry/ontologies/query-compute-usage |
| Ontology SDK — Java OSDK | https://www.palantir.com/docs/foundry/ontology-sdk/java-osdk |
| Workshop — Use Actions | https://www.palantir.com/docs/foundry/workshop/actions-use |

### API Reference 核心端点

| 端点 | 链接 |
|------|------|
| List Ontologies | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontologies/list-ontologies |
| Get Object Type | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/object-types/get-object-type |
| List Objects | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/list-objects |
| Get Object | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/get-object |
| Search Objects (V2) | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/search-objects |
| Load Object Set | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set |
| Apply Action (V1) | https://www.palantir.com/docs/foundry/api/ontology-resources/actions/apply-action |
| Apply Action Batch | https://www.palantir.com/docs/foundry/api/ontology-resources/actions/apply-action-batch |
| Search Objects Details (V1) | https://www.palantir.com/docs/foundry/api/ontology-resources/objects/search |

### GitHub 开源仓库

| 仓库 | 链接 | 说明 |
|------|------|------|
| `palantir/osdk-ts` | https://github.com/palantir/osdk-ts | TypeScript OSDK 源码，pnpm monorepo |
| `palantir/foundry-platform-typescript` | https://github.com/palantir/foundry-platform-typescript | Platform SDK (TypeScript) |
| `palantir/foundry-platform-python` | https://github.com/palantir/foundry-platform-python | Platform SDK (Python) |
| Python SDK — OntologyObject 文档 | https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Ontologies/OntologyObject.md | 对象模型文档 |
| Python SDK — OntologyObjectSet 文档 | https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Ontologies/OntologyObjectSet.md | ObjectSet 文档 |
| Python SDK — Query 文档 | https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Functions/Query.md | 查询函数文档 |
| Python SDK — Branch 文档 | https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Datasets/Branch.md | 分支模型文档 |

### 技术博客

| 标题 | 链接 |
|------|------|
| On Dataset Versioning in Palantir Foundry | https://blog.palantir.com/on-dataset-versioning-in-palantir-foundry-8f23de22cc4c |
| Understanding Palantir's Ontology Layers (Medium) | https://pythonebasta.medium.com/understanding-palantirs-ontology-semantic-kinetic-and-dynamic-layers-explained-c1c25b39ea3c |

### OpenAPI 规范获取

Foundry Developer Console 支持导出实例特定的 OpenAPI YAML（路径：Application API → SDK Generation → Export as OpenAPI），这是生成 Go 客户端脚手架的最直接路径。Python SDK v0.16.0 及更早版本的 Git 历史中可能仍包含通用 OpenAPI 规范。

---

## 结论：复刻路径与架构决策

本次调研揭示了 Ontology Layer 的几个核心设计决策值得在 Golang 复刻中借鉴：**Action-only 写入模型**强制所有变更通过声明式规则或函数执行，确保了原子性、可审计性和权限控制的统一；**ObjectSet 作为可组合的一等公民**使得查询表达能力远超传统 ORM；**索引层与持久层的分离**（Object Databases 临时 + Dataset 持久）实现了查询性能与数据可靠性的平衡。

对于 Go 复刻，最务实的起步路径是：首先从 Foundry 实例导出 OpenAPI 规范并用 `oapi-codegen` 生成基础 HTTP 客户端骨架；然后实现 Where 子句构建器和 ObjectSet 组合器这两个核心 DSL 组件；接着构建 Action 执行管道（参数验证→规则执行→编辑折叠→原子提交）；最后实现基于 ElasticSearch 的索引层和基于 Kafka 的写入队列。整个类型系统的 18 种基础类型需要逐一映射为 Go 类型，特别注意 Long 类型使用字符串传输以及 Timestamp 使用 ISO 8601 格式的约定。
