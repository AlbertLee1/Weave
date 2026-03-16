# Palantir AIP Analyst 底层执行引擎全栈解剖

**核心结论：AIP Analyst 的工具运行在混合架构上——ObjectSet、Aggregation、SearchAround 主要由 Object Set Service (OSS) 在自研索引引擎上执行（非 Spark），而 OntologySQL 则完全走 Spark SQL 执行路径。** 这一混合架构的证据来自 Palantir 官方文档对 OSv2 计算路径的明确描述：基本查询使用自研索引引擎（4 compute-seconds 固定开销），大规模 SearchAround（>10万对象）和批量写回（>1万对象）则自动溢出到按需 Spark 集群。理解这一架构分界线是像素级复刻 AIP Analyst 的关键前提。

---

## 一、Ontology 存储架构：从 Elasticsearch 到自研索引引擎

**参考文档：**
- [Ontology architecture（OSv2 官方架构概览）](https://www.palantir.com/docs/foundry/object-backend/overview)
- [Object Storage V1 (Phonograph) Legacy](https://www.palantir.com/docs/foundry/object-databases/object-storage-v1)
- [Object Indexing FAQ](https://www.palantir.com/docs/foundry/object-indexing/faq)
- [Funnel batch pipelines](https://www.palantir.com/docs/foundry/object-indexing/funnel-batch-pipelines)
- [May 2023 Foundry Announcements（OSv2 GA）](https://www.palantir.com/docs/foundry/announcements/2023-05)

Palantir 的 Ontology 后端经历了两代架构。**Object Storage V1（代号 Phonograph）底层是 Elasticsearch**，官方文档明确描述其为"distributed document store and search engine"，使用 ElasticSearch 风格的查询语法。Phonograph 是一个单体服务，将索引、存储、查询、用户编辑和写回全部耦合在一起，存在 **10,000 对象分页硬限制**。Phonograph 计划于 2026 年 6 月 30 日停止支持。

**Object Storage V2（OSv2）于 2023 年 5 月 GA**，是一次从零开始的完全重构。OSv2 将关注点解耦为五个独立微服务：

- **Ontology Metadata Service (OMS)**：定义对象类型、链接类型、Action 类型等元数据
- **Object Data Funnel（"Funnel"）**：编排数据写入和索引，分为四阶段流水线——Changelog（差分计算）→ Merge Changes（合并用户编辑）→ Indexing（Spark 作业转换为专有索引文件）→ Hydration（索引文件下载到搜索节点磁盘）
- **Object Database（搜索节点）**：存储索引数据，服务查询
- **Object Set Service (OSS)**：统一的读 API 层，负责所有搜索、过滤、聚合、加载操作
- **Actions Service**：处理用户对对象的编辑操作

**关键存储细节**：对象数据并非直接存储在 Foundry 的 Parquet 文件上。原始数据来自 Foundry 数据集（Parquet 格式），但经过 Funnel 的四阶段流水线后被转换为 Palantir **专有的"增强索引格式"**（enhanced indexing format），存储在专用搜索节点的磁盘上。官方文档指出"Ontology volume can be larger than dataset volume because Ontology data cannot be compressed, and Ontology indexing requires additional storage to facilitate faster queries"。中间的 changelog、merged changes 和 index files 都是 Funnel 管控的内部数据集，用户无法直接访问。

**Phonograph2** 这个名称出现在对象 RID 中（格式：`ri.phonograph2-objects.main.object.<UUID>`），无论底层是 OSv1 还是 OSv2 都使用此 RID 格式。它是 API/服务层的版本标识，与 OSv1/OSv2 的存储后端架构版本是不同维度的概念。

---

## 二、四大工具的执行路径判定与证据链

### ObjectSet / Filtering：OSS 自研索引引擎（非 Spark）

**参考文档：**
- [Load Object Set（API 端点详情）](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set)
- [Load Object Set Objects Or Interfaces](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set-objects-or-interfaces)
- [Functions on objects – Object sets](https://www.palantir.com/docs/foundry/functions/api-object-sets)
- [Search Objects](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/search-objects)
- [List Objects](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/list-objects)
- [Python OSDK](https://www.palantir.com/docs/foundry/ontology-sdk/python-osdk)
- [Java OSDK](https://www.palantir.com/docs/foundry/ontology-sdk/java-osdk)
- [Ontology query compute usage](https://www.palantir.com/docs/foundry/ontologies/query-compute-usage)
- [Ontologies API – DeepWiki](https://deepwiki.com/palantir/foundry-platform-python/4.3-ontologies-api)
- [OntologyObject.md – GitHub](https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Ontologies/OntologyObject.md)

**判定：主要运行在 OSv2 自研索引引擎上。**

ObjectSet 在 SDK 层实现了经典的 **惰性求值（lazy evaluation）** 模式。OSDK（TypeScript/Python）中，`.where()`、`.pivotTo()`、`.union()`、`.intersect()`、`.subtract()` 等操作不触发任何网络调用，仅在内存中构建一棵 **ObjectSet 定义树**（discriminated union JSON 结构）。只有当调用终端操作（`.fetchPage()`、`.asyncIter()`、`.aggregate()`）时，整棵定义树才被序列化为 JSON 并通过 HTTP POST 发送到服务端。

**终端操作的 API 调用**：

```
POST /api/v2/ontologies/{ontology}/objectSets/loadObjects
Body: {
  "objectSet": {"type": "filter", "objectSet": {"type": "base", "objectType": "Employee"}, ...},
  "pageSize": 10000,
  "select": ["name", "department"],
  "orderBy": {"field": "name", "direction": "asc"}
}
```

服务端收到请求后，**Object Set Service (OSS) 解析 ObjectSet 定义树**，在 OSv2 的搜索节点上执行索引遍历和剪枝（index pruning）。官方文档明确描述："Object Storage V2 stores objects in an enhanced indexing format... the Ontology query engine can avoid processing large swaths of data during its search by traversing the index. This process is known as 'pruning'. Using this engine, you can search through billions of records by evaluating up to 1000x fewer records."

**性能特征**：OSv2 基本查询固定开销为 **4 compute-seconds**（OSv1 为 16），这个量级说明底层不是 Spark 作业（Spark 作业启动开销通常在秒级），而是索引引擎的内存计算。OSv2 无分页上限（OSv1 限制 10,000），支持 `snapshot=true` 的一致性分页。

支持的过滤器类型包括：`eq`、`lt`、`gt`、`not`、`and`、`or`、`isNull`、`contains`、`startsWith`、`containsAllTerms`、`containsAnyTerm`、`containsAllTermsInOrder`，以及 OSv2 独有的 **`nearestNeighbors`（KNN 向量搜索，K≤100，≤2048 维度）**。

---

### OntologyAggregation：OSS 索引引擎（常规规模）/ Spark（大规模）

**参考文档：**
- [Aggregate Object Set（API 端点详情）](https://www.palantir.com/docs/foundry/api/v2/ontologies-v2-resources/ontology-object-sets/aggregate-object-set)
- [Aggregate Objects](https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/aggregate-objects)
- [Ontology query compute usage](https://www.palantir.com/docs/foundry/ontologies/query-compute-usage)

**判定：主要运行在 OSv2 自研索引引擎上，大规模时可能溢出到 Spark。**

聚合操作的 API 端点有两个：

```
# 针对单个对象类型的聚合
POST /api/v2/ontologies/{ontology}/objects/{objectType}/aggregate

# 针对任意 ObjectSet 定义的聚合（更强大）
POST /api/v2/ontologies/{ontology}/objectSets/aggregate
```

请求体结构包含 `aggregation`（聚合指标数组）和 `groupBy`（分组维度数组）：

```json
{
  "objectSet": {"type": "base", "objectType": "Employee"},
  "aggregation": [
    {"type": "avg", "field": "properties.salary", "name": "avg_salary"},
    {"type": "count", "name": "total"}
  ],
  "groupBy": [
    {"field": "department", "type": "exact", "maxGroupCount": 100},
    {"field": "hireDate", "type": "fixedWidth", "fixedWidth": 365}
  ]
}
```

支持的聚合类型：**count、sum、avg、min、max、approximateDistinct（cardinality）、approximatePercentile、standardDeviation、variance**。GroupBy 类型：**exact、range、fixedWidth、duration**。响应中包含 `accuracy` 字段（`"ACCURATE"` 或 `"APPROXIMATE"`），以及 `excludedItems` 计数。

**执行路径分析**：聚合操作与 ObjectSet 查询走相同的 OSS → 搜索节点路径。OSv2 的"增强索引格式"明确为聚合做了优化（文档中提到"more accurate aggregations through a Spark-based query execution layer"），对于大规模聚合，OSv2 能够启动 Spark 计算层来保证精确性。这一点与 Elasticsearch 的聚合模型（近似计算 + 分布式节点聚合）在架构上是类似的——索引引擎处理常规规模，超大规模时溢出到 Spark。

---

### OntologySQL：完全运行在 Spark SQL 上

**参考文档：**
- [Analyze using SQL（OntologySQL 官方文档）](https://www.palantir.com/docs/foundry/object-explorer/analyze-sql)
- [Object Explorer – Ontology SQL](https://www.palantir.com/docs/foundry/object-explorer/ontology-sql)
- [SQL warehousing – SQL dialect](https://www.palantir.com/docs/foundry/sql-warehousing/sql-dialect)
- [Execute SQL Query（API 端点）](https://www.palantir.com/docs/foundry/api/sql-queries-v2-resources/sql-queries/execute-sql-query)
- [SqlQuery.md – GitHub](https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/SqlQueries/SqlQuery.md)
- [Contour compute usage](https://www.palantir.com/docs/foundry/contour/compute-usage)
- [Error in Ontology SQL Tool during AIP Logic LLM Calls（社区讨论）](https://community.palantir.com/t/error-in-ontology-sql-tool-during-aip-logic-llm-calls/6113)

**判定：100% Spark SQL 执行路径，绕过 OSS 和索引引擎。**

这是四个工具中唯一被明确证实完全走 Spark 的工具。官方文档直接声明：

> "The SQL engine supports the **Spark SQL dialect**. In Spark SQL, identifiers such as table names should be quoted using backticks (`) rather than single or double quotes."

> "Each query runs on the **entire dataset** and uses the **same compute backend as Contour**."

> "Analyze using SQL works by querying the **backing datasource or the materialization** of an Ontology entity."

**关键发现：OntologySQL 不查询 OSv2 索引，而是直接查询底层的 Foundry 数据集（或物化视图）。** 它的执行路径是：OntologySQL → 解析对象类型 API 名称/RID → 定位到对应的 backing dataset 或 materialization → 提交给 Contour 的自动扩缩 Spark 集群 → 返回结果。

**API 端点**：`POST /api/v2/sqlQueries/execute`，返回 **Apache Arrow 格式**，默认限制 100 万行。在 AIP Logic 的错误信息中可以看到 `(dialect: SPARK)` 标识，社区确认"Ontology SQL is actually backed by Spark SQL"。

**OntologySQL vs Dataset SQL vs Foundry SQL 的区别**：

| 维度 | OntologySQL | Dataset SQL | Foundry SQL (Furnace) |
|------|-------------|-------------|----------------------|
| 查询目标 | 对象类型（通过 API 名称） | 数据集（通过路径/RID） | 数据集（通过路径/RID） |
| 底层执行 | Spark SQL (Contour 后端) | Spark SQL (Contour 后端) | Apache Calcite → 多引擎路由 |
| 方言 | Spark SQL | Spark SQL | Spark SQL 子集 + ANSI |
| 状态 | Beta | GA | GA |
| 数据源 | backing dataset / materialization | 原始数据集 | 原始数据集 |

**对于启用编辑的对象类型**，OntologySQL 查询的是物化视图（materialization），数据新鲜度有最多 **30 秒的延迟**。对于多数据源对象类型，必须先创建物化视图才能使用 OntologySQL。

---

### SearchAround / Pivot：OSS 索引引擎（<10万对象）/ Spark（>10万对象）

**参考文档：**
- [Ontology architecture（OSv2 架构及 SearchAround 阈值）](https://www.palantir.com/docs/foundry/object-backend/overview)
- [Ontology query compute usage（计费与阈值）](https://www.palantir.com/docs/foundry/ontologies/query-compute-usage)
- [Object Indexing Overview](https://www.palantir.com/docs/foundry/object-indexing/overview)
- [Paging（分页机制）](https://www.palantir.com/docs/foundry/api/general/overview/paging)

**判定：混合执行，阈值为 10 万对象。**

SearchAround 在 API 层不是独立端点，而是 **ObjectSet 定义树中的一个节点类型**：

```json
{
  "objectSet": {
    "type": "searchAround",
    "objectSet": {
      "type": "filter",
      "objectSet": {"type": "base", "objectType": "Flight"},
      "where": {"type": "eq", "field": "departureAirport", "value": "SFO"}
    },
    "link": "passengers"
  }
}
```

这个定义树被发送到 `POST /api/v2/ontologies/{ontology}/objectSets/loadObjects` 或 `/objectSets/aggregate`。服务端 OSS 解析后执行链接遍历。

**执行引擎选择的阈值**（官方文档原文）："OSv2 supports also **on-demand Spark cluster searches when running search-arounds on over 100,000 objects**, or running writeback operations on over 10,000 objects in a single request. These Spark clusters utilize usage in the same way as all other Spark-based applications on the platform."

**Palantir 没有独立的图数据库**。链接遍历是在 OSv2 的索引结构中通过预计算的索引实现的（官方描述 OSv2 的索引格式"optimized for high-speed indexing, Search Arounds, and writeback"），而不是通过 Spark join 或图数据库查询。大规模时（>10 万输入对象），才溢出到 Spark。

**硬限制**：最多 **3 层**链式 SearchAround；OSv2 结果集不超过 **1000 万对象**；OSv1 输入集不超过 10 万对象。

---

## 三、AIP Analyst 的工具调用栈与数据流

**参考文档：**
- [AIP Analyst Overview](https://www.palantir.com/docs/foundry/aip-analyst/overview)
- [AIP Logic – Blocks](https://www.palantir.com/docs/foundry/logic/blocks)
- [Slate – Optimize indexes and schema design](https://www.palantir.com/docs/foundry/slate/references-indexes)

AIP Analyst 的工具调用遵循以下架构链路：

```
用户输入 → AIP Runtime (LLM) → Tool Call 请求 → AIP Logic 执行层
    → 权限检查 (invoking user's permissions)
    → 路由到对应 API 端点：
        ├── ObjectSet → POST /api/v2/ontologies/{ont}/objectSets/loadObjects → OSS → 搜索节点
        ├── Aggregation → POST /api/v2/ontologies/{ont}/objectSets/aggregate → OSS → 搜索节点
        ├── OntologySQL → POST /api/v2/sqlQueries/execute → Contour Spark 集群
        ├── SearchAround → (作为 ObjectSet 定义节点) → OSS → 搜索节点 / Spark
        ├── DatasetSQL → POST /api/v2/sqlQueries/execute → Contour Spark 集群
        └── Object Search → POST /api/v2/ontologies/{ont}/objects/{type}/search → OSS
    → 结果返回 LLM → 下一轮 tool call 或最终响应
```

官方文档明确指出："LLMs do not have direct access to tools; LLMs can only **ask** to use tools, and these tool calls are then executed by AIP Logic within the invoking user's permissions."

**AIP Analyst 暴露给 LLM 的完整工具集**：

| 类别 | 工具名 | 描述 |
|------|--------|------|
| 发现 | Object type search | 基于元数据搜索相关对象类型 |
| 发现 | Object type lookup | 获取特定对象类型的详细元数据（属性、链接） |
| 发现 | Object search | 跨整个 Ontology 搜索对象 |
| 数据 | Object set | 创建 ObjectSet，支持过滤、SearchAround、语义搜索 |
| 数据 | Object lookup | 获取单个对象的所有属性值 |
| 数据 | Ontology aggregation | 在 ObjectSet 上执行聚合 + 分组 |
| 数据 | Ontology SQL | 对 ObjectSet 执行 SQL 查询 |
| 数据 | Dataset SQL | 对数据集执行 SQL 查询 |
| 可视化 | Create Visualization | 从聚合/SQL 结果构建 Vega 图表 |
| 可视化 | Map Visualization | 地理空间数据可视化 |

AIP Agent 的会话 API：

```
POST /api/v2/aipAgents/agents/{agentRid}/sessions            # 创建会话
POST /api/v2/aipAgents/sessions/{sessionId}/blockingContinue  # 同步继续
POST /api/v2/aipAgents/sessions/{sessionId}/streamingContinue # 流式继续
GET  /api/v2/aipAgents/sessions/{sessionId}/content/{contentId} # 获取内容
```

---

## 四、延迟特征分析与引擎判断

**参考文档：**
- [Ontology query compute usage（计费模型详情）](https://www.palantir.com/docs/foundry/ontologies/query-compute-usage)
- [Ontology indexing compute](https://www.palantir.com/docs/foundry/ontologies/compute-usage)
- [Ontology volume usage](https://www.palantir.com/docs/foundry/ontologies/volume-usage)
- [Resource Management – Usage types](https://www.palantir.com/docs/foundry/resource-management/usage-types/index.html)

**执行延迟是判断底层引擎的关键信号**：

| 操作 | 预期延迟 | 引擎判断依据 |
|------|----------|-------------|
| ObjectSet filter（小规模） | **100-500ms** | OSv2 索引查询，4 cs 固定开销，亚秒级响应 |
| ObjectSet filter（大规模分页） | **1-5s** 每页 | OSv2 索引 + 分页，仍非 Spark |
| Aggregation（常规） | **200-800ms** | OSv2 索引聚合 |
| Aggregation（百万级） | **2-10s** | 可能触发 Spark 辅助 |
| OntologySQL | **3-30s** | Spark SQL，Contour 集群，冷启动可能更长 |
| SearchAround（<10万） | **500ms-2s** | OSv2 索引遍历 |
| SearchAround（>10万） | **5-30s** | 按需 Spark 集群启动 |
| Dataset SQL | **3-30s** | Spark SQL，同 OntologySQL |

**计费模型差异进一步确认引擎分离**：Ontology 查询使用 **compute-seconds** 计量（固定开销 4-16 cs），Spark 操作使用 **core-seconds × memory-to-core ratio** 跨多个 executor 计量。这是两套完全不同的计费体系，底层必然是不同的执行引擎。

---

## 五、像素级复刻所需的核心组件清单

### 如果复刻 OSS 路径（ObjectSet + Aggregation + SearchAround）

需要实现的核心组件：

1. **ObjectSet 定义解析器**：解析包含 `base`、`filter`、`searchAround`、`union`、`intersect`、`subtract` 节点的嵌套 JSON 定义树
2. **索引存储引擎**：可用 **Elasticsearch/OpenSearch** 替代（OSv1 就是 ES，OSv2 是其自研演进版本），需支持倒排索引、范围查询、聚合
3. **ObjectSet REST API**：实现 `POST /objectSets/loadObjects`、`POST /objectSets/aggregate`、`POST /objects/{type}/search`、`POST /objects/{type}/aggregate`
4. **链接索引**：为 SearchAround 预计算对象间的链接关系索引（类似 ES 的 parent-child 或 nested），支持高效的跨类型遍历
5. **分页机制**：实现 `pageToken` + `nextPageToken` 的游标分页，支持 snapshot 一致性模式
6. **数据同步管道**：从数据源（数据集）增量同步到索引引擎，类似 Funnel 的 changelog → merge → index → hydrate 流程

### 如果复刻 OntologySQL 路径

需要实现的核心组件：

1. **SQL 解析层**：接收 Spark SQL 方言的 SQL，解析对象类型名称到底层数据集
2. **Spark SQL 执行引擎**：可直接使用 Apache Spark 的 SQL 引擎，或用 DuckDB/Trino 替代小规模查询
3. **对象类型 → 数据集映射**：维护 Ontology 元数据，将 SQL 中的对象类型名称解析为实际的 Parquet/数据集路径
4. **结果序列化**：返回 Apache Arrow 格式的结果集

### 混合架构的推荐复刻方案

基于分析结果，**最实际的复刻方案**是：

- **Elasticsearch/OpenSearch** 作为核心索引引擎（替代 OSv2 搜索节点），处理 ObjectSet、Aggregation、SearchAround 的 90%+ 请求
- **Apache Spark 或 Trino** 作为 SQL 执行引擎，处理 OntologySQL 和大规模溢出查询
- **阈值路由器**：根据查询规模自动选择引擎（<10万对象走 ES，>10万走 Spark）
- **Funnel 式数据管道**：使用 Spark 从源数据集读取 → 转换 → 写入 ES 索引，支持增量更新

这一方案与 Palantir 的实际架构高度对齐——OSv1 就是 Elasticsearch，OSv2 是其自研演进版本，核心查询模式和 API 都与 ES 的能力高度吻合（倒排索引、过滤、聚合、近似计算）。

---

## 结论：被忽视的架构分界线

复刻 AIP Analyst 最大的认知陷阱是假设所有操作都走 Spark。实际上，**只有 OntologySQL 完全走 Spark**，其余三个工具的日常执行路径都在 OSv2 的自研索引引擎上——这是一个类 Elasticsearch 的分布式索引系统，不是 Spark DataFrame。OntologySQL 之所以走 Spark，是因为它根本不查询索引，而是直接查询底层的原始数据集。

这意味着复刻时，**核心工程量在于构建一个高效的对象索引服务**（类似 ES 集群 + ObjectSet 定义解析器），而非搭建 Spark 集群。Spark 只需作为 SQL 查询的后备引擎和大规模操作的溢出通道。AIP Analyst 的工具调用本质上是 LLM 生成结构化 JSON（ObjectSet 定义树 / 聚合参数 / SQL 字符串），由 AIP Logic 层路由到对应的 Foundry V2 API 端点执行。

---

## 完整参考链接索引

| 文档 | URL |
|------|-----|
| Ontology architecture (OSv2) | https://www.palantir.com/docs/foundry/object-backend/overview |
| Object Storage V1 (Phonograph) Legacy | https://www.palantir.com/docs/foundry/object-databases/object-storage-v1 |
| Object Indexing FAQ | https://www.palantir.com/docs/foundry/object-indexing/faq |
| Object Indexing Overview | https://www.palantir.com/docs/foundry/object-indexing/overview |
| Funnel batch pipelines | https://www.palantir.com/docs/foundry/object-indexing/funnel-batch-pipelines |
| May 2023 Foundry Announcements | https://www.palantir.com/docs/foundry/announcements/2023-05 |
| Load Object Set (API) | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set |
| Load Object Set Objects Or Interfaces (API) | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-object-sets/load-object-set-objects-or-interfaces |
| Aggregate Object Set (API v2) | https://www.palantir.com/docs/foundry/api/v2/ontologies-v2-resources/ontology-object-sets/aggregate-object-set |
| Aggregate Objects (API) | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/aggregate-objects |
| Search Objects (API) | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/search-objects |
| List Objects (API) | https://www.palantir.com/docs/foundry/api/ontologies-v2-resources/ontology-objects/list-objects |
| Search Objects Details (V1 API) | https://www.palantir.com/docs/foundry/api/ontology-resources/objects/search |
| Functions on objects – Object sets | https://www.palantir.com/docs/foundry/functions/api-object-sets |
| Python OSDK | https://www.palantir.com/docs/foundry/ontology-sdk/python-osdk |
| Java OSDK | https://www.palantir.com/docs/foundry/ontology-sdk/java-osdk |
| Ontology query compute usage | https://www.palantir.com/docs/foundry/ontologies/query-compute-usage |
| Ontology indexing compute | https://www.palantir.com/docs/foundry/ontologies/compute-usage |
| Ontology volume usage | https://www.palantir.com/docs/foundry/ontologies/volume-usage |
| Resource Management – Usage types | https://www.palantir.com/docs/foundry/resource-management/usage-types/index.html |
| Analyze using SQL (OntologySQL) | https://www.palantir.com/docs/foundry/object-explorer/analyze-sql |
| Object Explorer – Ontology SQL | https://www.palantir.com/docs/foundry/object-explorer/ontology-sql |
| SQL warehousing – SQL dialect | https://www.palantir.com/docs/foundry/sql-warehousing/sql-dialect |
| Execute SQL Query (API) | https://www.palantir.com/docs/foundry/api/sql-queries-v2-resources/sql-queries/execute-sql-query |
| Contour compute usage | https://www.palantir.com/docs/foundry/contour/compute-usage |
| Paging (API) | https://www.palantir.com/docs/foundry/api/general/overview/paging |
| AIP Analyst Overview | https://www.palantir.com/docs/foundry/aip-analyst/overview |
| AIP Logic – Blocks | https://www.palantir.com/docs/foundry/logic/blocks |
| Slate – Optimize indexes | https://www.palantir.com/docs/foundry/slate/references-indexes |
| Slate – Platform widgets | https://www.palantir.com/docs/foundry/slate/widgets-platform |
| Quiver best practices | https://www.palantir.com/docs/foundry/quiver/quiver-best-practices |
| OntologyObject.md (GitHub) | https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/Ontologies/OntologyObject.md |
| SqlQuery.md (GitHub) | https://github.com/palantir/foundry-platform-python/blob/develop/docs/v2/SqlQueries/SqlQuery.md |
| Ontologies API – DeepWiki | https://deepwiki.com/palantir/foundry-platform-python/4.3-ontologies-api |
| Error in Ontology SQL Tool (社区) | https://community.palantir.com/t/error-in-ontology-sql-tool-during-aip-logic-llm-calls/6113 |
