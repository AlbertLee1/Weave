# Palantir ObjectSet & OntologyAggregation 完整语法参考

本文档基于 [Palantir Foundry 官方 API 文档](https://www.palantir.com/docs/foundry/) 整理，覆盖 ObjectSet V2 和 OntologyAggregation 的完整语法规范。

---

## 目录

1. [ObjectSet](#1-objectset)
   - [API 端点](#11-api-端点)
   - [ObjectSet 定义树](#12-objectset-定义树)
   - [过滤器（Where）](#13-过滤器where)
   - [SearchAround（链接遍历）](#14-searcharound链接遍历)
   - [集合运算](#15-集合运算)
   - [排序与分页](#16-排序与分页)
   - [Select 字段选择](#17-select-字段选择)
2. [OntologyAggregation](#2-ontologyaggregation)
   - [API 端点](#21-api-端点)
   - [聚合类型](#22-聚合类型)
   - [GroupBy 分组](#23-groupby-分组)
   - [SegmentBy 多维分段](#24-segmentby-多维分段)
   - [OrderBy 与 Limit](#25-orderby-与-limit)
   - [精度控制](#26-精度控制)
   - [响应格式](#27-响应格式)
3. [OSDK 客户端语法](#3-osdk-客户端语法)
4. [Functions API 语法](#4-functions-api-语法)
5. [本项目实现映射](#5-本项目实现映射)

---

## 1. ObjectSet

### 1.1 API 端点

Palantir Foundry V2 API 提供两个核心端点：

```
POST /api/v2/ontologies/{ontology}/objectSets/loadObjects     # 加载对象集合
POST /api/v2/ontologies/{ontology}/objects/{objectType}/search # 搜索单类型对象
```

### 1.2 ObjectSet 定义树

ObjectSet 采用**惰性求值**（lazy evaluation）模式。所有操作（`.where()`、`.pivotTo()`、`.union()` 等）仅在内存中构建一棵 **ObjectSet 定义树**（discriminated union JSON 结构），只有当调用终端操作（`.fetchPage()`、`.asyncIter()`、`.aggregate()`）时才会序列化为 JSON 并通过 HTTP POST 发送到服务端执行。

定义树节点类型：

| 节点类型 | 说明 | 示例 |
|---------|------|------|
| `base` | 基础对象集合，指定对象类型 | `{"type": "base", "objectType": "Employee"}` |
| `filter` | 对对象集合施加过滤条件 | `{"type": "filter", "objectSet": {...}, "where": {...}}` |
| `searchAround` | 通过链接类型遍历关联对象 | `{"type": "searchAround", "objectSet": {...}, "link": "passengers"}` |
| `union` | 并集运算 | `{"type": "union", "objectSets": [{...}, {...}]}` |
| `intersect` | 交集运算 | `{"type": "intersect", "objectSets": [{...}, {...}]}` |
| `subtract` | 差集运算 | `{"type": "subtract", "objectSets": [{...}, {...}]}` |
| `reference` | 引用已注册的 ObjectSet | `{"type": "reference", "reference": "ri.object-set.main..."}` |
| `nearestNeighbors` | KNN 向量搜索（OSv2） | 见下方 |
| `withProperties` | 添加派生属性 | 见下方 |

#### base 节点

```json
{
  "type": "base",
  "objectType": "Employee"
}
```

#### filter 节点

```json
{
  "type": "filter",
  "objectSet": {"type": "base", "objectType": "Employee"},
  "where": {
    "type": "eq",
    "field": "department",
    "value": "Engineering"
  }
}
```

#### searchAround 节点

```json
{
  "type": "searchAround",
  "objectSet": {
    "type": "filter",
    "objectSet": {"type": "base", "objectType": "Flight"},
    "where": {"type": "eq", "field": "departureAirport", "value": "SFO"}
  },
  "link": "passengers"
}
```

限制：最多 **3 层**链式 SearchAround。OSv2 结果集不超过 **1000 万对象**。

#### nearestNeighbors 节点（OSv2 独有）

```json
{
  "type": "nearestNeighbors",
  "objectSet": {"type": "base", "objectType": "Movie"},
  "propertyIdentifier": {"property": {"apiName": "vectorField"}},
  "numNeighbors": 10,
  "similarityThreshold": 0.5,
  "query": {
    "vector": {"value": [0.1, 0.2, 0.3]},
    "text": {"value": "search text"}
  }
}
```

限制：K ≤ 100，向量维度 ≤ 2048。

---

### 1.3 过滤器（Where）

#### 比较运算符

| 类型 | 说明 | JSON 格式 |
|------|------|-----------|
| `eq` | 等于 | `{"type": "eq", "field": "name", "value": "Alice"}` |
| `gt` | 大于 | `{"type": "gt", "field": "age", "value": 18}` |
| `gte` | 大于等于 | `{"type": "gte", "field": "salary", "value": 50000}` |
| `lt` | 小于 | `{"type": "lt", "field": "score", "value": 60}` |
| `lte` | 小于等于 | `{"type": "lte", "field": "price", "value": 100.5}` |

适用类型：number、string、date（`YYYY-MM-DD`）、timestamp（`YYYY-MM-DDTHH:mm:SSZ`）

#### 字符串搜索运算符

| 类型 | 说明 | JSON 格式 |
|------|------|-----------|
| `contains` | 包含子串（或数组包含值） | `{"type": "contains", "field": "tags", "value": "important"}` |
| `startsWith` | 前缀匹配 | `{"type": "startsWith", "field": "name", "value": "Dr."}` |
| `containsAllTerms` | 包含所有词（任意顺序，空格分词） | `{"type": "containsAllTerms", "field": "desc", "value": "red car"}` |
| `containsAllTermsInOrder` | 按顺序包含所有词 | `{"type": "containsAllTermsInOrder", "field": "desc", "value": "red car"}` |
| `containsAnyTerm` | 包含任一词 | `{"type": "containsAnyTerm", "field": "desc", "value": "cat dog"}` |
| `wildcard` | 通配符匹配（`*` 任意多字符，`?` 单字符） | `{"type": "wildcard", "field": "name", "value": "fo*o"}` |

**分词规则**：词项以空格或标点 `?!,:;-[](){}'\"~` 分隔。

#### 空值检查

```json
{"type": "isNull", "field": "email", "value": true}
```

`value` 为 `true` 表示字段为 null，`false` 表示字段不为 null。

#### 逻辑运算符

**and — 所有条件为真：**

```json
{
  "type": "and",
  "value": [
    {"type": "gte", "field": "age", "value": 18},
    {"type": "lt", "field": "age", "value": 65}
  ]
}
```

> 注意：Palantir REST API 中 `and`/`or` 使用 `"queries"` 字段名，Search Objects API 中使用 `"value"` 字段名。

**or — 任一条件为真：**

```json
{
  "type": "or",
  "value": [
    {"type": "eq", "field": "status", "value": "active"},
    {"type": "eq", "field": "status", "value": "pending"}
  ]
}
```

**not — 取反：**

```json
{
  "type": "not",
  "value": [{"type": "eq", "field": "status", "value": "deleted"}]
}
```

#### 嵌套深度限制

过滤条件最多 **3 层嵌套**。

#### 地理空间运算符（OSv2）

| 类型 | 说明 |
|------|------|
| `withinBoundingBox` | 在矩形范围内 |
| `intersectsBoundingBox` | 与矩形相交 |
| `withinPolygon` | 在多边形范围内 |
| `intersectsPolygon` | 与多边形相交 |
| `withinDistanceOf` | 在指定距离内 |

> **注意**：以上地理空间过滤器为 Palantir OSv2 独有功能，本项目未实现。

---

### 1.4 SearchAround（链接遍历）

SearchAround 不是独立 API，而是 ObjectSet 定义树中的一个节点类型。它通过 `link`（链接类型 API 名称）遍历对象之间的关联关系。

#### REST API 格式

```json
{
  "type": "searchAround",
  "objectSet": {
    "type": "base",
    "objectType": "Flight"
  },
  "link": "passengers"
}
```

#### 多层链式（最多 3 层）

```json
{
  "type": "searchAround",
  "objectSet": {
    "type": "searchAround",
    "objectSet": {"type": "base", "objectType": "Company"},
    "link": "employees"
  },
  "link": "projects"
}
```

#### 执行引擎

- < 10 万对象：OSv2 索引引擎直接执行
- \> 10 万对象：自动溢出到按需 Spark 集群

#### Functions API 语法

```typescript
const passengers = Objects.search()
  .flights()
  .filter(flight => flight.departureAirportCode.exactMatch("SFO"))
  .searchAroundPassengers();
```

---

### 1.5 集合运算

三种集合运算，要求所有参与的 ObjectSet 具有**相同的 objectType**：

| 运算 | REST JSON | 说明 |
|------|-----------|------|
| 并集 | `{"type": "union", "objectSets": [{...}, {...}]}` | 对象在任一集合中即包含 |
| 交集 | `{"type": "intersect", "objectSets": [{...}, {...}]}` | 对象必须在所有集合中 |
| 差集 | `{"type": "subtract", "objectSets": [{...}, {...}]}` | 从第一个集合中移除第二个集合的对象 |

---

### 1.6 排序与分页

#### orderBy

```json
{
  "orderBy": {
    "fields": [
      {"field": "salary", "direction": "desc"},
      {"field": "name", "direction": "asc"}
    ]
  }
}
```

也支持按相关性排序：

```json
{
  "orderBy": {"orderType": "relevance"}
}
```

#### 分页

```json
{
  "pageSize": 1000,
  "pageToken": "v1.QnVpbGQ...",
  "snapshot": true
}
```

- `pageSize`：每页返回数量
- `pageToken`：首次请求不填，后续取响应中的 `nextPageToken`
- `snapshot`：启用快照一致性分页（OSv2 独有）
- OSv1 限制最多 10,000 对象；OSv2 无上限

---

### 1.7 Select 字段选择

**基础格式：**

```json
{
  "select": ["name", "department", "salary"]
}
```

**V2 格式（支持结构体字段）：**

```json
{
  "selectV2": [
    {"property": {"apiName": "name"}},
    {
      "structField": {
        "propertyApiName": "address",
        "structFieldApiName": "city"
      }
    }
  ]
}
```

未指定 `select` 时返回所有字段（向量属性除外，必须显式包含）。

---

### 1.8 完整请求示例

```json
{
  "objectSet": {
    "type": "filter",
    "objectSet": {"type": "base", "objectType": "Employee"},
    "where": {
      "type": "and",
      "value": [
        {"type": "gte", "field": "salary", "value": 50000},
        {"type": "eq", "field": "department", "value": "Engineering"}
      ]
    }
  },
  "select": ["name", "salary", "department"],
  "orderBy": {
    "fields": [{"field": "salary", "direction": "desc"}]
  },
  "pageSize": 100
}
```

**响应：**

```json
{
  "data": [
    {
      "__rid": "ri.phonograph2-objects.main.object.abc123",
      "__primaryKey": 50030,
      "__apiName": "Employee",
      "name": "John Smith",
      "salary": 120000,
      "department": "Engineering"
    }
  ],
  "nextPageToken": "v1.QnVpbGQ...",
  "totalCount": "256"
}
```

---

## 2. OntologyAggregation

### 2.1 API 端点

两个端点，能力不同：

```
# 针对单个对象类型的聚合
POST /api/v2/ontologies/{ontology}/objects/{objectType}/aggregate

# 针对任意 ObjectSet 定义的聚合（更强大，支持嵌套 ObjectSet）
POST /api/v2/ontologies/{ontology}/objectSets/aggregate
```

#### 基于 ObjectSet 的聚合请求体

```json
{
  "objectSet": {"type": "base", "objectType": "Employee"},
  "aggregation": [
    {"type": "avg", "field": "salary", "name": "avg_salary"},
    {"type": "count", "name": "total"}
  ],
  "groupBy": [
    {"field": "department", "type": "exact"},
    {"field": "hireDate", "type": "fixedWidth", "fixedWidth": 365}
  ],
  "accuracy": "ALLOW_APPROXIMATE"
}
```

#### 基于单类型的聚合请求体

```json
{
  "aggregation": [
    {"type": "sum", "field": "revenue", "name": "total_revenue"},
    {"type": "count", "name": "count"}
  ],
  "where": {
    "type": "gte",
    "field": "date",
    "value": "2025-01-01"
  },
  "groupBy": [
    {"field": "region", "type": "exact"}
  ]
}
```

---

### 2.2 聚合类型

| 类型 | 说明 | 是否需要 field | 额外参数 |
|------|------|:-------------:|---------|
| `count` | 计数 | 否 | — |
| `sum` | 求和 | 是 | — |
| `avg` | 平均值 | 是 | — |
| `min` | 最小值 | 是 | — |
| `max` | 最大值 | 是 | — |
| `approximateDistinct` | 近似去重计数 | 是 | — |
| `exactDistinct` | 精确去重计数（OSv2 独有） | 是 | — |
| `approximatePercentile` | 近似百分位数（OSv2 独有） | 是 | `percentile`: 0-100 |
| `standardDeviation` | 标准差 | 是 | — |
| `variance` | 方差 | 是 | — |

#### 聚合项 JSON 格式

```json
{
  "type": "avg",
  "field": "salary",
  "name": "avg_salary",
  "direction": "DESC"
}
```

- `type`（必填）：聚合类型
- `field`（count 可省略）：聚合字段名。也可用 `propertyIdentifier` 替代（二者互斥）
- `name`（可选）：结果列别名
- `direction`（可选）：`ASC` / `DESC`，用于按聚合值排序结果

#### 各类型示例

**count（无 field）：**

```json
{"type": "count", "name": "total"}
```

**approximateDistinct：**

```json
{"type": "approximateDistinct", "field": "customer_id", "name": "unique_customers"}
```

**approximatePercentile：**

```json
{
  "type": "approximatePercentile",
  "field": "response_time",
  "name": "p99_latency",
  "percentile": 99
}
```

**standardDeviation：**

```json
{"type": "standardDeviation", "field": "amount", "name": "amount_stddev"}
```

---

### 2.3 GroupBy 分组

四种分桶策略：

#### exact — 精确值分组（默认）

```json
{
  "field": "department",
  "type": "exact",
  "maxGroupCount": 100
}
```

- `maxGroupCount`（可选）：最大分组数，超过时截断尾部，响应中返回 `excludedItems` 计数

#### fixedWidth — 等宽数值分桶

```json
{
  "field": "salary",
  "type": "fixedWidth",
  "fixedWidth": 10000
}
```

产生分桶：0-10000、10000-20000、20000-30000...

#### range — 自定义区间分桶

```json
{
  "field": "score",
  "type": "range",
  "ranges": [
    {"startValue": 0, "endValue": 60},
    {"startValue": 60, "endValue": 80},
    {"startValue": 80, "endValue": 100}
  ]
}
```

每个 range 对象包含 `startValue`（含）和 `endValue`（不含）。也支持日期范围：

```json
{
  "field": "startDate",
  "type": "range",
  "ranges": [
    {"startValue": "2024-01-01", "endValue": "2024-07-01"},
    {"startValue": "2024-07-01", "endValue": "2025-01-01"}
  ]
}
```

#### duration — 时间周期分桶

```json
{
  "field": "created_at",
  "type": "duration",
  "duration": "P1M"
}
```

Palantir REST API 使用 **ISO 8601 duration** 格式：

| ISO 8601 值 | 含义 |
|-------------|------|
| `P1D` | 按天 |
| `P1W` | 按周 |
| `P1M` | 按月 |
| `P3M` | 按季度 |
| `P1Y` | 按年 |

> 注意：Functions API 中使用方法名 `.byDays()`、`.byMonth()` 等，与 REST API 格式不同。

#### 多级分组

支持多个 groupBy 字段，产生交叉分组：

```json
{
  "groupBy": [
    {"field": "department", "type": "exact"},
    {"field": "hireDate", "type": "duration", "duration": "P1Y"}
  ]
}
```

---

### 2.4 SegmentBy 多维分段

`segmentBy` 在 Functions API 中原生支持，在 `groupBy` 基础上再按额外维度细分，返回嵌套结构。

#### Functions API 语法

```typescript
Objects.search().employees()
  .groupBy(e => e.department.topValues())    // 主分组
  .segmentBy(e => e.role.topValues())        // 分段
  .count();
```

返回 `ThreeDimensionalAggregation<string, string>`。

#### REST API 等效实现

REST API 没有独立的 `segmentBy` 参数，通过在 `groupBy` 数组中添加多个字段实现等效效果：

```json
{
  "groupBy": [
    {"field": "department", "type": "exact"},
    {"field": "role", "type": "exact"}
  ],
  "aggregation": [{"type": "count", "name": "headcount"}]
}
```

---

### 2.5 OrderBy 与 Limit

#### 按聚合值排序

通过 `aggregation` 项中的 `direction` 参数控制：

```json
{
  "aggregation": [
    {"type": "sum", "field": "revenue", "name": "total_revenue", "direction": "DESC"}
  ],
  "groupBy": [{"field": "region", "type": "exact"}]
}
```

#### 限制分组数

通过 groupBy 的 `maxGroupCount` 参数：

```json
{
  "groupBy": [
    {"field": "city", "type": "exact", "maxGroupCount": 10}
  ]
}
```

---

### 2.6 精度控制

请求中通过 `accuracy` 参数控制：

| 值 | 说明 |
|----|------|
| `ALLOW_APPROXIMATE` | 默认值，允许近似结果（性能更好） |
| `REQUIRE_ACCURATE` | 要求精确结果，无法保证时返回错误 `AggregationAccuracyNotSupported` |

---

### 2.7 响应格式

```json
{
  "data": [
    {
      "group": {
        "department": "Engineering",
        "hireDate": {"startValue": "2020-01-01", "endValue": "2021-01-01"}
      },
      "metrics": [
        {"name": "avg_salary", "value": 125000},
        {"name": "total", "value": 42}
      ]
    },
    {
      "group": {
        "department": "Sales",
        "hireDate": {"startValue": "2020-01-01", "endValue": "2021-01-01"}
      },
      "metrics": [
        {"name": "avg_salary", "value": 95000},
        {"name": "total", "value": 28}
      ]
    }
  ],
  "accuracy": "ACCURATE",
  "excludedItems": 0,
  "computeUsage": 4.5
}
```

- `accuracy`：实际精度，`"ACCURATE"` 或 `"APPROXIMATE"`
- `excludedItems`：因 `maxGroupCount` 截断而被排除的分组数
- `computeUsage`：查询消耗的 compute-seconds

---

### 2.8 完整请求示例

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  "https://$HOSTNAME/api/v2/ontologies/palantir/objectSets/aggregate" \
  -d '{
    "objectSet": {
      "type": "filter",
      "objectSet": {"type": "base", "objectType": "Employee"},
      "where": {"type": "gte", "field": "salary", "value": 50000}
    },
    "aggregation": [
      {"type": "min", "field": "tenure", "name": "min_tenure"},
      {"type": "avg", "field": "tenure", "name": "avg_tenure"},
      {"type": "approximateDistinct", "field": "department", "name": "dept_count"}
    ],
    "groupBy": [
      {
        "field": "startDate",
        "type": "range",
        "ranges": [
          {"startValue": "2020-01-01", "endValue": "2020-07-01"},
          {"startValue": "2020-07-01", "endValue": "2021-01-01"}
        ]
      },
      {"field": "city", "type": "exact", "maxGroupCount": 50}
    ],
    "accuracy": "ALLOW_APPROXIMATE"
  }'
```

---

## 3. OSDK 客户端语法

### Python OSDK

```python
from ontology_sdk.ontology.objects import Employee

# 过滤
employees = client.ontology.objects.Employee.where(
    (Employee.object_type.salary >= 50000) &
    (Employee.object_type.department == "Engineering")
)

# 字符串搜索
results = client.ontology.objects.Employee.where(
    Employee.object_type.bio.contains_all_terms(["python", "senior"])
)

# 空值检查
results = client.ontology.objects.Employee.where(
    ~Employee.object_type.email.is_null()
)

# 排序
results = client.ontology.objects.Employee.where(
    Employee.object_type.salary >= 50000
).order_by(
    Employee.object_type.salary.desc()
)

# 分页
page = client.ontology.objects.Employee.page(page_size=30)
next_page = client.ontology.objects.Employee.page(
    page_size=30, page_token=page.next_page_token
)

# 迭代
all_employees = list(client.ontology.objects.Employee.iterate())

# 聚合
avg_salary = client.ontology.objects.Employee.avg(
    Employee.object_type.salary
).compute()

# 分组聚合
result = client.ontology.objects.Employee.where(
    ~Employee.object_type.department.is_null()
).group_by(
    Employee.object_type.department.exact()
).count().compute()

# 近似去重
unique_count = client.ontology.objects.Employee.approximate_distinct(
    Employee.object_type.department
).compute()
```

### TypeScript OSDK

```typescript
import { Objects, Filters } from "@foundry/functions-api";

// 过滤
const engineers = Objects.search().employees()
  .filter(e => Filters.and(
    e.salary.range().gte(50000),
    e.department.exactMatch("Engineering")
  ));

// 字符串搜索
const results = Objects.search().employees()
  .filter(e => e.bio.matchAllTokens("python senior"));

// 排序 + 限制
const top10 = Objects.search().employees()
  .orderBy(e => e.salary.desc())
  .take(10);

// SearchAround
const projects = Objects.search().employees()
  .filter(e => e.department.exactMatch("Engineering"))
  .searchAroundProjects();

// 聚合
const byDept = Objects.search().employees()
  .groupBy(e => e.department.topValues())
  .segmentBy(e => e.hireDate.byYear())
  .count();
```

---

## 4. Functions API 语法

### 过滤器方法映射

| Functions API 方法 | REST API type | 说明 |
|-------------------|---------------|------|
| `.exactMatch(v)` | `eq` | 精确匹配 |
| `.range().gt(v)` | `gt` | 大于 |
| `.range().gte(v)` | `gte` | 大于等于 |
| `.range().lt(v)` | `lt` | 小于 |
| `.range().lte(v)` | `lte` | 小于等于 |
| `.matchAllTokens(s)` | `containsAllTerms` | 包含所有词 |
| `.matchAnyToken(s)` | `containsAnyTerm` | 包含任一词 |
| `.phrase(s)` | `containsAllTermsInOrder` | 按顺序包含所有词 |
| `.phrasePrefix(s)` | `containsAllTermsInOrderPrefixLastTerm` | 前缀短语 |
| `.isNull()` | `isNull` | 空值检查 |
| `.contains(v)` | `contains` | 数组包含 |
| `.startsWith(s)` | `startsWith` | 前缀匹配 |
| `.isTrue()` / `.isFalse()` | `eq` (boolean) | 布尔匹配 |
| `Filters.and(...)` | `and` | 逻辑与 |
| `Filters.or(...)` | `or` | 逻辑或 |
| `Filters.not(...)` | `not` | 逻辑非 |

### GroupBy 分桶方法映射

| Functions API 方法 | REST API type + 参数 | 说明 |
|-------------------|---------------------|------|
| `.topValues()` | `exact` | 按精确值分组 |
| `.exactValues({maxBuckets: n})` | `exact` + `maxGroupCount` | 限制分组数 |
| `.byFixedWidth(w)` | `fixedWidth` + `fixedWidth: w` | 等宽分桶 |
| `.byRanges({min, max})` | `range` + `ranges: [...]` | 自定义区间 |
| `.byYear()` | `duration` + `duration: "P1Y"` | 按年 |
| `.byQuarter()` | `duration` + `duration: "P3M"` | 按季度 |
| `.byMonth()` | `duration` + `duration: "P1M"` | 按月 |
| `.byWeek()` | `duration` + `duration: "P1W"` | 按周 |
| `.byDays()` | `duration` + `duration: "P1D"` | 按天 |
| `.byHours()` | `duration` + `duration: "PT1H"` | 按小时 |

### 聚合方法映射

| Functions API 方法 | REST API type | 说明 |
|-------------------|---------------|------|
| `.count()` | `count` | 计数 |
| `.sum(e => e.field)` | `sum` | 求和 |
| `.average(e => e.field)` | `avg` | 平均值 |
| `.min(e => e.field)` | `min` | 最小值 |
| `.max(e => e.field)` | `max` | 最大值 |
| `.cardinality(e => e.field)` | `approximateDistinct` | 近似去重 |

> 注意：Functions API 中的 `.cardinality()` 在 REST API 中对应 `approximateDistinct`。

---

## 5. 本项目实现映射

本项目（db_analyst）对 Palantir 语法进行了适配，以下是关键差异和映射关系。

### ObjectSet 差异

| 维度 | Palantir REST API | 本项目实现 | 说明 |
|------|------------------|-----------|------|
| SearchAround 字段 | `"link": "linkName"` | `"linkTypeApiName": "linkName"` | 本项目使用更具描述性的字段名 |
| 过滤器嵌套字段 | `"queries": [...]` 或 `"value": [...]` | `"value": [...]` | 统一使用 `value` |
| 惰性注册 | 无（每次发送完整定义树） | `ref_id` 注册机制 | 本项目支持 ObjectSet 复用 |
| 集合运算 | 定义树嵌套 | `"union"/"intersect"/"subtract": ["os_1", "os_2"]` | 引用 ref_id |
| 扩展过滤器 | 无 | `ne`、`in`、`endsWith`、`hasProperty`、`fuzzyMatch`、`regex` | 本项目额外支持 |

### OntologyAggregation 差异

| 维度 | Palantir REST API | 本项目实现 | 说明 |
|------|------------------|-----------|------|
| ObjectSet 引用 | 内联 ObjectSet 定义树 | `"objectSetRef": "os_1"` | 引用已注册的 ObjectSet |
| duration 格式 | ISO 8601（`"P1M"`） | 简写（`"month"`） | 本项目使用 day/week/month/quarter/year |
| groupBy 排序 | 无独立字段 | `"order": "asc"/"desc"` | groupBy 内置排序 |
| groupBy 最大分组数 | `maxGroupCount` | `maxGroupCount` | 一致 |
| segmentBy | Functions API 独有 | 独立 `segmentBy` 数组 | 返回嵌套结构 |
| orderBy | 聚合项内 `direction` | 独立 `orderBy` 数组 | `[{"field": "别名", "direction": "desc"}]` |
| limit | `maxGroupCount` | 独立 `limit` 参数 | 限制返回分组数 |
| percentile 参数 | 未在 REST API 文档中明确 | `"percentile": 50` | 0-100 的数值 |
| 精度控制 | `accuracy` 参数 | 无 | — |

### 聚合类型命名对照

| Palantir REST API | Functions API | 本项目 |
|------------------|---------------|--------|
| `count` | `.count()` | `count` |
| `sum` | `.sum()` | `sum` |
| `avg` | `.average()` | `avg` |
| `min` | `.min()` | `min` |
| `max` | `.max()` | `max` |
| `approximateDistinct` | `.cardinality()` | `approximateDistinct` |
| `exactDistinct` | — | — |
| `approximatePercentile` | — | `approximatePercentile` |
| `standardDeviation` | — | `standardDeviation` |
| `variance` | — | `variance` |

### 过滤器类型命名对照

| Palantir REST API | Functions API | 本项目 |
|------------------|---------------|--------|
| `eq` | `.exactMatch()` | `eq` |
| `gt` | `.range().gt()` | `gt` |
| `gte` | `.range().gte()` | `gte` |
| `lt` | `.range().lt()` | `lt` |
| `lte` | `.range().lte()` | `lte` |
| `contains` | `.contains()` | `contains` |
| `startsWith` | `.startsWith()` | `startsWith` |
| `containsAllTerms` | `.matchAllTokens()` | `containsAllTerms` |
| `containsAllTermsInOrder` | `.phrase()` | `containsAllTermsInOrder` |
| `containsAnyTerm` | `.matchAnyToken()` | `containsAnyTerm` |
| `wildcard` | — | `wildcard` |
| `isNull` | `.isNull()` | `isNull` |
| `not` | `Filters.not()` | `not` |
| `and` | `Filters.and()` | `and` |
| `or` | `Filters.or()` | `or` |
| `hasProperty` | `.isNotNull()` | `hasProperty` |
| — | — | `ne`（扩展） |
| — | — | `in`（扩展） |
| — | — | `endsWith`（扩展） |
| — | — | `regex`（扩展） |
| — | — | `fuzzyMatch`（扩展） |

---

## 参考链接

| 文档 | URL |
|------|-----|
| Load Object Set API | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set |
| Aggregate Object Set API | https://www.palantir.com/docs/foundry/api/v2/ontologies-v2-resources/ontology-object-sets/aggregate-object-set |
| Aggregate Objects API | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/aggregate-objects |
| Search Objects API | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/search-objects |
| Functions on Objects | https://www.palantir.com/docs/foundry/functions/api-object-sets |
| Python OSDK | https://www.palantir.com/docs/foundry/ontology-sdk/python-osdk |
| AIP Analyst Overview | https://www.palantir.com/docs/foundry/aip-analyst/overview |
| OntologySQL | https://www.palantir.com/docs/foundry/object-explorer/analyze-sql |
| Ontology Architecture (OSv2) | https://www.palantir.com/docs/foundry/object-backend/overview |
| Query Compute Usage | https://www.palantir.com/docs/foundry/ontologies/query-compute-usage |
