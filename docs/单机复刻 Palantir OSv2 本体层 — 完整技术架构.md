# 单机复刻 Palantir OSv2 本体层 — 完整技术架构

> **目标**：在一台高配电脑上，用 Golang 复刻 Palantir Foundry Ontology Layer (OSv2) 的完整能力。
> 本文档覆盖系统分层、技术选型、数据模型、API 设计、数据流、存储引擎和实现路线图。

---

## 一、系统总览

### 1.1 原版 OSv2 架构 vs 单机复刻映射

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Palantir 原版 (分布式)          单机复刻 (Go)     │
├─────────────────────────────────────────────────────────────────────┤
│ Kubernetes (Rubix)                    →  Docker Compose / 裸进程    │
│ Object Set Service (OSS)              →  Go HTTP Server (查询路由)  │
│ Ontology Metadata Service (OMS)       →  Go Service + PostgreSQL    │
│ Actions Service                       →  Go Service (嵌入式)        │
│ Object Data Funnel                    →  Go Worker + NATS JetStream │
│ Object Databases (Lucene 索引节点)     →  Bleve / Zinc / OpenSearch  │
│ Dataset 持久层 (Parquet + Iceberg)     →  Parquet + DuckDB           │
│ 流式层 (Kafka/Flink)                  →  NATS JetStream + Go Worker │
│ 按需 Spark 集群                       →  DuckDB 并行查询             │
│ S3/HDFS                               →  本地 MinIO 或文件系统       │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.2 核心设计原则

1. **单进程多模块**：所有服务编译为一个 Go 二进制，通过内部接口解耦，未来可拆分微服务
2. **Action-Only 写入**：所有数据修改必须通过 Action，保证原子性和可审计性
3. **索引与存储分离**：索引层（Bleve）是临时数据，可从持久层（Parquet）随时重建
4. **API 兼容**：REST API 线缆格式完全兼容 Palantir V2 JSON 协议

---

## 二、分层架构详解

### 2.0 架构总图

```
                        ┌──────────────────────────────┐
                        │         API Gateway           │
                        │    (Chi Router / net/http)     │
                        │   Bearer Token + OAuth2 PKCE   │
                        └──────────┬───────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                     │
     ┌────────▼────────┐ ┌────────▼────────┐  ┌────────▼────────┐
     │  OMS (元数据)    │ │  OSS (查询路由)  │  │ Actions Service │
     │  ─────────────  │ │  ─────────────   │  │ ─────────────── │
     │ ObjectType 注册  │ │ Where→Bleve转换  │  │ 参数验证         │
     │ LinkType 管理    │ │ ObjectSet 组合   │  │ 规则执行引擎     │
     │ ActionType 定义  │ │ 聚合计算         │  │ 编辑折叠         │
     │ Interface 管理   │ │ 分页游标         │  │ 事务提交         │
     │                  │ │ 权限过滤         │  │ 副作用触发       │
     └───────┬──────────┘ └────────┬────────┘  └────────┬────────┘
             │                     │                     │
             │ PostgreSQL          │ Bleve               │
             │                     │                     │
     ┌───────▼──────────────────────────────────────────▼────────┐
     │                   Object Data Funnel                       │
     │              (写入编排 + 索引构建 Worker)                    │
     │                                                             │
     │  ┌─────────────┐  ┌──────────────┐  ┌───────────────────┐  │
     │  │ NATS Queue  │  │ Changelog    │  │ Index Builder     │  │
     │  │ (编辑队列)   │  │ Engine       │  │ (增量/全量)        │  │
     │  │ offset追踪  │  │ (差异计算)    │  │ Bleve段文件构建    │  │
     │  └──────┬──────┘  └──────┬───────┘  └──────┬────────────┘  │
     │         │                │                  │               │
     └─────────┼────────────────┼──────────────────┼───────────────┘
               │                │                  │
     ┌─────────▼────────────────▼──────────────────▼───────────────┐
     │                     存储引擎层                                │
     │                                                              │
     │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
     │  │ Bleve Index  │  │ PostgreSQL   │  │ Parquet Files    │   │
     │  │ (对象索引)    │  │ (元数据)      │  │ (持久数据)        │   │
     │  │ 全文搜索      │  │ 类型定义      │  │ Dataset 存储     │   │
     │  │ 范围过滤      │  │ 权限策略      │  │ 版本控制         │   │
     │  │ 地理空间      │  │ Action日志   │  │ 物化数据集       │   │
     │  └──────────────┘  └──────────────┘  └──────────────────┘   │
     │                                                              │
     │  ┌──────────────┐  ┌──────────────┐                         │
     │  │ DuckDB       │  │ 本地文件系统  │                         │
     │  │ (聚合/分析)   │  │ 或 MinIO     │                         │
     │  │ Parquet读取   │  │ (对象存储)    │                         │
     │  └──────────────┘  └──────────────┘                         │
     └──────────────────────────────────────────────────────────────┘
```

### 2.1 Layer 0: 存储引擎层

| 组件 | 技术选型 | 用途 | 数据特征 |
|------|---------|------|---------|
| **PostgreSQL** | 原生 | 本体元数据、类型定义、Action 日志、权限策略 | 强一致、低量、高频读 |
| **Bleve** | `github.com/blevesearch/bleve/v2` | 对象索引、全文搜索、范围过滤、地理空间 | 临时数据、可重建 |
| **Parquet** | `github.com/parquet-go/parquet-go` | 对象持久存储、Dataset 版本控制 | 持久数据、列式存储 |
| **DuckDB** | `github.com/marcboeker/go-duckdb` | 复杂聚合查询、大规模 Search Around、分析 | 按需查询引擎 |
| **NATS JetStream** | `github.com/nats-io/nats.go` | 编辑队列、事件总线、实时订阅 | 消息队列+持久化 |
| **本地文件系统** | `os` 标准库 | Parquet 文件存储、索引段文件、附件 | 可选换 MinIO |

#### 为什么选这些引擎：

- **Bleve vs ElasticSearch**：Bleve 是纯 Go 的全文搜索库，嵌入式运行无需外部进程；支持自定义分析器、地理空间索引、facet 聚合——覆盖 Palantir Where 子句的所有操作符。单机场景下性能足够（百万级对象）。如果后续需要更强性能可平滑切换到 Zinc（基于 Bleve 的 ES 兼容层）或 OpenSearch。

- **DuckDB vs Spark**：DuckDB 是嵌入式分析数据库，直接读 Parquet 文件做 OLAP 查询，性能媲美小型 Spark 集群。用它替代"按需 Spark 集群"处理大规模聚合和 Search Around（超 10 万对象的链接遍历）。

- **NATS JetStream vs Kafka**：NATS 是单二进制、零依赖的消息系统，JetStream 提供持久化和恰好一次语义。单机部署远比 Kafka 轻量，同时支持 offset 追踪——完美替代 Funnel 的编辑队列。

- **PostgreSQL**：元数据存储的黄金标准，JSONB 支持灵活的类型定义扩展，`FOR UPDATE` 支持 Action 的乐观锁定。

---

### 2.2 Layer 1: Ontology Metadata Service (OMS)

**职责**：管理所有本体类型的元数据定义。

#### PostgreSQL Schema 设计

```sql
-- 核心表：本体
CREATE TABLE ontologies (
    rid         TEXT PRIMARY KEY,        -- ri.ontology.main.ontology.<uuid>
    api_name    TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ DEFAULT now(),
    updated_at  TIMESTAMPTZ DEFAULT now()
);

-- 对象类型
CREATE TABLE object_types (
    rid             TEXT PRIMARY KEY,
    ontology_rid    TEXT REFERENCES ontologies(rid),
    api_name        TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    plural_display_name TEXT,
    description     TEXT,
    primary_key_prop TEXT NOT NULL,       -- 指向 properties.api_name
    title_property  TEXT,
    status          TEXT DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','EXPERIMENTAL','DEPRECATED')),
    visibility      TEXT DEFAULT 'NORMAL' CHECK (visibility IN ('PROMINENT','NORMAL','HIDDEN')),
    icon_name       TEXT,
    color           TEXT,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

-- 属性定义
CREATE TABLE properties (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT REFERENCES object_types(rid) ON DELETE CASCADE,
    api_name        TEXT NOT NULL,
    display_name    TEXT,
    description     TEXT,
    base_type       TEXT NOT NULL,        -- 18种基础类型之一
    type_config     JSONB DEFAULT '{}',   -- 类型特定配置(precision, scale, array子类型等)
    is_array        BOOLEAN DEFAULT false,
    is_nullable     BOOLEAN DEFAULT true,
    is_searchable   BOOLEAN DEFAULT true,
    is_sortable     BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(object_type_rid, api_name)
);

-- 链接类型
CREATE TABLE link_types (
    rid                 TEXT PRIMARY KEY,
    ontology_rid        TEXT REFERENCES ontologies(rid),
    api_name            TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    description         TEXT,
    source_object_type  TEXT REFERENCES object_types(rid),
    target_object_type  TEXT REFERENCES object_types(rid),
    cardinality         TEXT NOT NULL CHECK (cardinality IN ('ONE_TO_ONE','ONE_TO_MANY','MANY_TO_MANY')),
    -- 外键链接配置
    foreign_key_config  JSONB,       -- {"sourceProperty":"deptId","targetProperty":"id"}
    -- 多对多链接的join表配置
    join_table_config   JSONB,       -- {"datasetRid":"...","sourceColumn":"...","targetColumn":"..."}
    is_required         BOOLEAN DEFAULT false,
    created_at          TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

-- Action 类型
CREATE TABLE action_types (
    rid             TEXT PRIMARY KEY,
    ontology_rid    TEXT REFERENCES ontologies(rid),
    api_name        TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    description     TEXT,
    status          TEXT DEFAULT 'ACTIVE',
    parameters      JSONB NOT NULL DEFAULT '[]',      -- 参数定义列表
    rules           JSONB NOT NULL DEFAULT '[]',      -- 规则列表(create/modify/delete object/link)
    submission_criteria JSONB DEFAULT '{}',            -- 提交条件
    side_effects    JSONB DEFAULT '[]',                -- 通知/Webhook
    -- Function-backed 配置
    function_rid    TEXT,                              -- 关联函数RID
    is_function_backed BOOLEAN DEFAULT false,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

-- Interface 定义
CREATE TABLE interfaces (
    rid             TEXT PRIMARY KEY,
    ontology_rid    TEXT REFERENCES ontologies(rid),
    api_name        TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    extends_rid     TEXT REFERENCES interfaces(rid),  -- 继承
    shared_properties JSONB DEFAULT '[]',              -- 共享属性定义
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(ontology_rid, api_name)
);

-- 对象类型实现的接口
CREATE TABLE object_type_interfaces (
    object_type_rid TEXT REFERENCES object_types(rid) ON DELETE CASCADE,
    interface_rid   TEXT REFERENCES interfaces(rid) ON DELETE CASCADE,
    property_mapping JSONB NOT NULL DEFAULT '{}',  -- 接口属性→对象属性映射
    PRIMARY KEY (object_type_rid, interface_rid)
);

-- Value Types
CREATE TABLE value_types (
    rid             TEXT PRIMARY KEY,
    api_name        TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    base_type       TEXT NOT NULL,
    constraints     JSONB DEFAULT '{}',   -- 验证规则(regex, min, max等)
    version         INTEGER DEFAULT 1,
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- Dataset 注册（Ontology Backing Datasource）
CREATE TABLE datasource_bindings (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT REFERENCES object_types(rid),
    dataset_rid     TEXT NOT NULL,         -- 指向持久层的数据集
    branch          TEXT DEFAULT 'main',
    column_mapping  JSONB NOT NULL,        -- 数据集列→属性映射
    is_primary      BOOLEAN DEFAULT true,  -- 主数据源
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- 安全策略
CREATE TABLE security_policies (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT REFERENCES object_types(rid),
    policy_type     TEXT NOT NULL CHECK (policy_type IN ('OBJECT','PROPERTY')),
    rules           JSONB NOT NULL,        -- 行/列级安全规则
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- Action 执行日志
CREATE TABLE action_logs (
    id              BIGSERIAL PRIMARY KEY,
    action_type_rid TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    parameters      JSONB NOT NULL,
    edits           JSONB NOT NULL,         -- 实际产生的编辑列表
    status          TEXT NOT NULL,           -- SUCCESS/FAILED/REJECTED
    error_message   TEXT,
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- 创建常用索引
CREATE INDEX idx_properties_object_type ON properties(object_type_rid);
CREATE INDEX idx_link_types_source ON link_types(source_object_type);
CREATE INDEX idx_link_types_target ON link_types(target_object_type);
CREATE INDEX idx_action_logs_type ON action_logs(action_type_rid);
CREATE INDEX idx_action_logs_created ON action_logs(created_at);
CREATE INDEX idx_datasource_bindings_obj ON datasource_bindings(object_type_rid);
```

#### OMS Go 接口

```go
// pkg/oms/service.go

type OntologyMetadataService interface {
    // Ontology
    ListOntologies(ctx context.Context) ([]Ontology, error)
    GetOntology(ctx context.Context, ontologyID string) (*Ontology, error)

    // Object Types
    ListObjectTypes(ctx context.Context, ontologyID string) ([]ObjectType, error)
    GetObjectType(ctx context.Context, ontologyID, objectTypeID string) (*ObjectType, error)
    CreateObjectType(ctx context.Context, ontologyID string, def ObjectTypeDefinition) (*ObjectType, error)
    UpdateObjectType(ctx context.Context, ontologyID, objectTypeID string, patch ObjectTypePatch) (*ObjectType, error)

    // Link Types
    ListLinkTypes(ctx context.Context, ontologyID, objectTypeID string) ([]LinkType, error)
    GetLinkType(ctx context.Context, ontologyID, objectTypeID, linkTypeID string) (*LinkType, error)

    // Action Types
    ListActionTypes(ctx context.Context, ontologyID string) ([]ActionType, error)
    GetActionType(ctx context.Context, ontologyID, actionTypeID string) (*ActionType, error)

    // Interfaces
    ListInterfaces(ctx context.Context, ontologyID string) ([]Interface, error)

    // Internal: Schema变更通知（触发Funnel重建索引）
    OnSchemaChange(ctx context.Context, event SchemaChangeEvent) error
}
```

---

### 2.3 Layer 2: Object Set Service (OSS) — 查询引擎

**职责**：处理所有读请求，路由到合适的存储后端。

#### 核心组件

```go
// pkg/oss/service.go

type ObjectSetService interface {
    // 按主键获取
    GetObject(ctx context.Context, req GetObjectRequest) (*OntologyObject, error)

    // 列出对象（分页）
    ListObjects(ctx context.Context, req ListObjectsRequest) (*ObjectPage, error)

    // 搜索（Where 子句过滤）
    SearchObjects(ctx context.Context, req SearchObjectsRequest) (*ObjectPage, error)

    // 加载对象集（核心端点——支持声明式组合）
    LoadObjectSet(ctx context.Context, req LoadObjectSetRequest) (*ObjectPage, error)

    // 聚合
    AggregateObjects(ctx context.Context, req AggregateRequest) (*AggregateResponse, error)

    // 链接遍历
    ListLinkedObjects(ctx context.Context, req LinkedObjectsRequest) (*ObjectPage, error)

    // 实时订阅
    SubscribeObjectSet(ctx context.Context, req SubscribeRequest) (<-chan ObjectUpdate, error)
}
```

#### Where 子句 → Bleve Query 转换器

这是**最核心的组件之一**，将 Palantir 的 JSON Where 子句转换为 Bleve 搜索查询：

```go
// pkg/oss/where/converter.go

type WhereClause struct {
    Type  string      `json:"type"`            // 操作类型
    Field string      `json:"field,omitempty"` // 属性名
    Value interface{} `json:"value"`           // 值或嵌套子句
}

// 转换入口
func ConvertToBleveQuery(where *WhereClause) (bleve.Query, error) {
    switch where.Type {
    case "eq":
        return bleve.NewTermQuery(fmt.Sprintf("%v", where.Value)).SetField(where.Field), nil

    case "gt", "gte", "lt", "lte":
        return convertRangeQuery(where)

    case "isNull":
        if where.Value.(bool) {
            // 字段不存在
            boolQ := bleve.NewBooleanQuery()
            boolQ.AddMustNot(bleve.NewWildcardQuery("*").SetField(where.Field))
            return boolQ, nil
        }
        return bleve.NewWildcardQuery("*").SetField(where.Field), nil

    case "contains":
        return bleve.NewTermQuery(fmt.Sprintf("%v", where.Value)).SetField(where.Field), nil

    case "containsAllTerms":
        // 所有词项都必须匹配
        terms := splitTerms(where.Value.(string))
        boolQ := bleve.NewBooleanQuery()
        for _, term := range terms {
            boolQ.AddMust(bleve.NewMatchQuery(term).SetField(where.Field))
        }
        return boolQ, nil

    case "containsAnyTerm":
        return bleve.NewMatchQuery(where.Value.(string)).SetField(where.Field), nil

    case "containsAllTermsInOrder":
        return bleve.NewMatchPhraseQuery(where.Value.(string)).SetField(where.Field), nil

    case "containsAllTermsInOrderPrefixLastTerm":
        // 前缀匹配最后一个词
        return convertPrefixPhraseQuery(where)

    case "and":
        boolQ := bleve.NewBooleanQuery()
        for _, sub := range where.Value.([]interface{}) {
            subClause := parseWhereClause(sub)
            q, err := ConvertToBleveQuery(subClause)
            if err != nil { return nil, err }
            boolQ.AddMust(q)
        }
        return boolQ, nil

    case "or":
        boolQ := bleve.NewBooleanQuery()
        boolQ.SetMinShould(1)
        for _, sub := range where.Value.([]interface{}) {
            subClause := parseWhereClause(sub)
            q, err := ConvertToBleveQuery(subClause)
            if err != nil { return nil, err }
            boolQ.AddShould(q)
        }
        return boolQ, nil

    case "not":
        subClause := parseWhereClause(where.Value)
        q, err := ConvertToBleveQuery(subClause)
        if err != nil { return nil, err }
        boolQ := bleve.NewBooleanQuery()
        boolQ.AddMustNot(q)
        return boolQ, nil

    default:
        return nil, fmt.Errorf("unsupported where type: %s", where.Type)
    }
}
```

#### ObjectSet 组合器

```go
// pkg/oss/objectset/definition.go

type ObjectSetDefinition struct {
    Type       string                 `json:"type"`
    ObjectType string                 `json:"objectType,omitempty"`  // base
    ObjectSet  *ObjectSetDefinition   `json:"objectSet,omitempty"`   // filter, searchAround
    ObjectSets []*ObjectSetDefinition `json:"objectSets,omitempty"`  // union, intersect, subtract
    Where      *WhereClause           `json:"where,omitempty"`       // filter
    Link       string                 `json:"link,omitempty"`        // searchAround
    Reference  string                 `json:"reference,omitempty"`   // reference
}

// 解析并执行对象集
func (e *ObjectSetExecutor) Execute(ctx context.Context, def *ObjectSetDefinition) ([]string, error) {
    switch def.Type {
    case "base":
        // 返回该类型所有对象的主键列表（由索引层处理）
        return e.index.AllPrimaryKeys(ctx, def.ObjectType)

    case "filter":
        basePKs, err := e.Execute(ctx, def.ObjectSet)
        if err != nil { return nil, err }
        // 在基础集上应用 Where 过滤
        return e.index.FilterByWhere(ctx, def.ObjectSet.ObjectType, basePKs, def.Where)

    case "union":
        var result []string
        seen := make(map[string]bool)
        for _, sub := range def.ObjectSets {
            pks, err := e.Execute(ctx, sub)
            if err != nil { return nil, err }
            for _, pk := range pks {
                if !seen[pk] { result = append(result, pk); seen[pk] = true }
            }
        }
        return result, nil

    case "intersect":
        sets := make([]map[string]bool, 0, len(def.ObjectSets))
        for _, sub := range def.ObjectSets {
            pks, err := e.Execute(ctx, sub)
            if err != nil { return nil, err }
            set := make(map[string]bool)
            for _, pk := range pks { set[pk] = true }
            sets = append(sets, set)
        }
        // 取交集
        return intersectSets(sets), nil

    case "subtract":
        // sets[0] - sets[1] - sets[2] ...
        basePKs, _ := e.Execute(ctx, def.ObjectSets[0])
        for i := 1; i < len(def.ObjectSets); i++ {
            subPKs, _ := e.Execute(ctx, def.ObjectSets[i])
            basePKs = subtractSlice(basePKs, subPKs)
        }
        return basePKs, nil

    case "searchAround":
        // 链接遍历：获取源对象集，通过链接找到关联对象
        sourcePKs, err := e.Execute(ctx, def.ObjectSet)
        if err != nil { return nil, err }
        return e.linkResolver.Traverse(ctx, sourcePKs, def.Link)

    case "reference":
        // 加载已保存的对象集定义
        savedDef, err := e.objectSetStore.Load(ctx, def.Reference)
        if err != nil { return nil, err }
        return e.Execute(ctx, savedDef)

    default:
        return nil, fmt.Errorf("unsupported objectset type: %s", def.Type)
    }
}
```

#### 聚合引擎（小数据量走 Bleve，大数据量走 DuckDB）

```go
// pkg/oss/aggregation/engine.go

type AggregationEngine struct {
    bleveIndex bleve.Index    // 小规模聚合
    duckDB     *sql.DB        // 大规模聚合，直接读Parquet
    threshold  int            // 切换阈值，默认100,000
}

func (e *AggregationEngine) Aggregate(ctx context.Context, req AggregateRequest) (*AggregateResponse, error) {
    estimatedCount := e.bleveIndex.DocCount()

    if estimatedCount < uint64(e.threshold) {
        return e.aggregateViaBleveFactets(ctx, req)
    }
    // 大规模聚合：构建DuckDB SQL查询，直接扫Parquet
    return e.aggregateViaDuckDB(ctx, req)
}
```

#### 分页机制（游标令牌）

```go
// pkg/oss/pagination/cursor.go

type PageCursor struct {
    ObjectType  string `json:"ot"`
    LastPK      string `json:"pk"`       // 最后一个主键
    SnapshotID  string `json:"sid"`      // 快照一致性ID
    CreatedAt   int64  `json:"ts"`       // 创建时间戳
}

// 编码为 Base64 URL-safe 令牌
func (c *PageCursor) Encode() string {
    data, _ := json.Marshal(c)
    return base64.RawURLEncoding.EncodeToString(data)
}

func DecodeCursor(token string) (*PageCursor, error) {
    data, err := base64.RawURLEncoding.DecodeString(token)
    if err != nil { return nil, err }
    var cursor PageCursor
    return &cursor, json.Unmarshal(data, &cursor)
}
```

---

### 2.4 Layer 3: Actions Service — 写入引擎

**核心设计**：所有写操作的唯一入口。

#### Action 执行的 9 步生命周期

```go
// pkg/actions/executor.go

type ActionExecutor struct {
    oms        OntologyMetadataService
    index      ObjectIndex
    queue      EditQueue           // NATS JetStream
    funcEngine FunctionEngine      // 可选的函数执行引擎
}

func (e *ActionExecutor) Apply(ctx context.Context, req ApplyActionRequest) (*ActionResult, error) {
    // Step 1: 加载 Action 类型定义
    actionType, err := e.oms.GetActionType(ctx, req.OntologyID, req.ActionType)
    if err != nil { return nil, err }

    // Step 2: 参数验证（类型检查 + 必填检查 + 约束检查）
    if err := e.validateParameters(actionType.Parameters, req.Parameters); err != nil {
        return nil, &ActionError{Code: "INVALID_PARAMETERS", Message: err.Error()}
    }

    // Step 3: 提交条件评估（权限检查）
    if err := e.evaluateSubmissionCriteria(ctx, actionType, req); err != nil {
        return nil, &ActionError{Code: "SUBMISSION_CRITERIA_FAILED", Message: err.Error()}
    }

    // Step 4: 加载相关对象（带版本追踪）
    objects, versions, err := e.loadObjectsWithVersions(ctx, actionType, req.Parameters)
    if err != nil { return nil, err }

    // Step 5: 执行规则 / 函数，生成编辑指令
    var edits []ObjectEdit
    if actionType.IsFunctionBacked {
        edits, err = e.funcEngine.Execute(ctx, actionType.FunctionRID, req.Parameters, objects)
    } else {
        edits, err = e.executeRules(ctx, actionType.Rules, req.Parameters, objects)
    }
    if err != nil { return nil, err }

    // Step 6: 编辑折叠（合并冗余操作）
    edits = collapseEdits(edits)

    // Step 7: 乐观锁版本检查
    if err := e.checkVersions(ctx, objects, versions); err != nil {
        return nil, &ActionError{Code: "STALE_OBJECT", Message: "object modified during action"}
    }

    // Step 8: 原子事务提交到编辑队列
    offset, err := e.queue.Publish(ctx, &EditBatch{
        ActionRID:  req.ActionType,
        UserID:     ctx.Value("user_id").(string),
        Edits:      edits,
        Timestamp:  time.Now(),
    })
    if err != nil { return nil, err }

    // Step 9: 等待索引确认 + 触发副作用
    if err := e.waitForIndexConfirmation(ctx, offset); err != nil {
        // 索引异步完成，不阻塞返回
        log.Warn("index confirmation timeout, edits will be eventually consistent")
    }
    go e.triggerSideEffects(ctx, actionType.SideEffects, edits)

    // 记录审计日志
    e.logAction(ctx, actionType, req, edits, "SUCCESS")

    return &ActionResult{Edits: edits}, nil
}
```

#### 编辑指令格式

```go
// pkg/actions/edits.go

type ObjectEdit struct {
    Type        EditType               `json:"type"`        // CREATE, MODIFY, DELETE
    ObjectType  string                 `json:"objectType"`
    PrimaryKey  interface{}            `json:"primaryKey"`
    Properties  map[string]interface{} `json:"properties,omitempty"`  // 设置的属性值
}

type LinkEdit struct {
    Type           EditType `json:"type"`           // CREATE_LINK, DELETE_LINK
    LinkType       string   `json:"linkType"`
    SourceObjectPK interface{} `json:"sourcePK"`
    TargetObjectPK interface{} `json:"targetPK"`
}

type EditType string
const (
    EditTypeCreate     EditType = "CREATE"
    EditTypeModify     EditType = "MODIFY"
    EditTypeDelete     EditType = "DELETE"
    EditTypeCreateLink EditType = "CREATE_LINK"
    EditTypeDeleteLink EditType = "DELETE_LINK"
)
```

#### 批量执行（最多 20 个）

```go
func (e *ActionExecutor) ApplyBatch(ctx context.Context, req ApplyBatchRequest) (*BatchResult, error) {
    if len(req.Requests) > 20 {
        return nil, &ActionError{Code: "BATCH_TOO_LARGE", Message: "max 20 actions per batch"}
    }
    results := make([]*ActionResult, len(req.Requests))
    // 在同一事务中执行所有 Action
    for i, r := range req.Requests {
        result, err := e.Apply(ctx, r)
        if err != nil { return nil, err } // 任一失败，全部回滚
        results[i] = result
    }
    return &BatchResult{Results: results}, nil
}
```

---

### 2.5 Layer 4: Object Data Funnel — 索引管道

**职责**：编排数据写入，维护索引与持久层的一致性。

```go
// pkg/funnel/service.go

type ObjectDataFunnel struct {
    nats           *nats.Conn
    js             nats.JetStreamContext
    bleveIndex     bleve.Index
    parquetWriter  *ParquetWriter
    duckDB         *sql.DB
    oms            OntologyMetadataService
}

// 启动消费循环
func (f *ObjectDataFunnel) Start(ctx context.Context) error {
    // 订阅编辑队列
    sub, err := f.js.Subscribe("ontology.edits.>", func(msg *nats.Msg) {
        var batch EditBatch
        json.Unmarshal(msg.Data, &batch)

        // 1. 立即更新 Bleve 索引（保证强一致性读）
        for _, edit := range batch.Edits {
            switch edit.Type {
            case EditTypeCreate, EditTypeModify:
                f.indexObject(ctx, edit)
            case EditTypeDelete:
                f.deleteFromIndex(ctx, edit)
            }
        }

        // 2. 追踪 offset（用于一致性保证）
        f.updateOffset(batch.Offset)

        // 3. 发布变更事件（用于 WebSocket 订阅推送）
        f.publishChangeEvent(ctx, batch)

        msg.Ack()
    }, nats.Durable("funnel-worker"), nats.ManualAck())

    // 4. 启动定期物化任务（每6小时将编辑持久化到Parquet）
    go f.runMaterializationLoop(ctx, 6*time.Hour)

    // 5. 启动增量索引管道（监听 Dataset 更新）
    go f.runIncrementalIndexLoop(ctx)

    return nil
}

// 将对象写入 Bleve 索引
func (f *ObjectDataFunnel) indexObject(ctx context.Context, edit ObjectEdit) error {
    doc := make(map[string]interface{})
    doc["__primaryKey"] = edit.PrimaryKey
    doc["__apiName"] = edit.ObjectType
    doc["__updatedAt"] = time.Now().UTC().Format(time.RFC3339)

    for k, v := range edit.Properties {
        doc[k] = v
    }

    // Bleve 使用 "objectType:primaryKey" 作为文档 ID
    docID := fmt.Sprintf("%s:%v", edit.ObjectType, edit.PrimaryKey)
    return f.bleveIndex.Index(docID, doc)
}

// 物化：将编辑持久化到 Parquet 数据集
func (f *ObjectDataFunnel) materialize(ctx context.Context, objectType string) error {
    // 从 Bleve 导出该类型所有对象
    allObjects, err := f.exportFromIndex(ctx, objectType)
    if err != nil { return err }

    // 写入新的 Parquet 文件（作为新 Transaction）
    path := fmt.Sprintf("data/datasets/%s/transactions/%d/data.parquet",
        objectType, time.Now().UnixMilli())

    return f.parquetWriter.WriteObjects(path, allObjects)
}
```

#### NATS JetStream 配置

```go
// pkg/funnel/nats_setup.go

func SetupJetStream(nc *nats.Conn) (nats.JetStreamContext, error) {
    js, _ := nc.JetStream()

    // 编辑队列：持久化、恰好一次
    js.AddStream(&nats.StreamConfig{
        Name:       "ONTOLOGY_EDITS",
        Subjects:   []string{"ontology.edits.>"},
        Storage:    nats.FileStorage,
        Retention:  nats.WorkQueuePolicy,  // 消费后删除
        MaxAge:     7 * 24 * time.Hour,    // 保留7天
        Replicas:   1,                      // 单机
    })

    // 变更事件流：用于 WebSocket 订阅推送
    js.AddStream(&nats.StreamConfig{
        Name:       "ONTOLOGY_CHANGES",
        Subjects:   []string{"ontology.changes.>"},
        Storage:    nats.FileStorage,
        Retention:  nats.LimitsPolicy,
        MaxAge:     1 * time.Hour,          // 保留1小时
        Replicas:   1,
    })

    return js, nil
}
```

---

### 2.6 Layer 5: 持久数据层 — Dataset 版本控制

模仿 Foundry 的 "Git for Data" 模型：

```go
// pkg/dataset/service.go

type TransactionType string
const (
    TxSnapshot TransactionType = "SNAPSHOT"
    TxAppend   TransactionType = "APPEND"
    TxUpdate   TransactionType = "UPDATE"
    TxDelete   TransactionType = "DELETE"
)

type Transaction struct {
    ID            string          `json:"id"`
    DatasetRID    string          `json:"datasetRid"`
    Branch        string          `json:"branch"`
    Type          TransactionType `json:"type"`
    Status        string          `json:"status"`   // OPEN, COMMITTED, ABORTED
    ParquetFiles  []string        `json:"files"`    // 关联的Parquet文件路径
    CreatedAt     time.Time       `json:"createdAt"`
    CommittedAt   *time.Time      `json:"committedAt,omitempty"`
}

type DatasetService interface {
    // Dataset CRUD
    CreateDataset(ctx context.Context, name string, schema Schema) (*Dataset, error)
    GetDataset(ctx context.Context, rid string) (*Dataset, error)

    // Transaction 管理
    OpenTransaction(ctx context.Context, datasetRID, branch string, txType TransactionType) (*Transaction, error)
    CommitTransaction(ctx context.Context, txID string) error
    AbortTransaction(ctx context.Context, txID string) error

    // 数据视图：从最近的 SNAPSHOT 开始叠加后续事务
    GetCurrentView(ctx context.Context, datasetRID, branch string) ([]string, error) // 返回Parquet文件路径列表

    // 分支管理
    CreateBranch(ctx context.Context, datasetRID, branchName, parentBranch string) error
    ListBranches(ctx context.Context, datasetRID string) ([]Branch, error)

    // 回滚
    Rollback(ctx context.Context, datasetRID, branch string, toTxID string) error
}
```

#### 文件系统布局

```
data/
├── datasets/
│   ├── employee/                         # 一个 Dataset
│   │   ├── metadata.json                 # Schema + 分支信息
│   │   ├── branches/
│   │   │   └── main/
│   │   │       └── transactions.json     # 事务链
│   │   └── transactions/
│   │       ├── tx_001/                   # SNAPSHOT
│   │       │   └── data.parquet
│   │       ├── tx_002/                   # APPEND
│   │       │   └── data.parquet
│   │       └── tx_003/                   # UPDATE (来自用户编辑的物化)
│   │           └── data.parquet
│   ├── department/
│   │   └── ...
│   └── employee_project_join/            # 多对多链接的join表
│       └── ...
├── indexes/
│   ├── employee.bleve/                   # Bleve 索引目录
│   └── department.bleve/
├── materializations/
│   ├── employee_edits/                   # 编辑物化数据集
│   │   └── ...
│   └── ...
└── attachments/
    └── <uuid>/                           # 附件文件
```

---

### 2.7 Layer 6: 实时订阅层

```go
// pkg/realtime/websocket.go

type SubscriptionManager struct {
    js          nats.JetStreamContext
    connections sync.Map  // connID → *WebSocketConn
}

type Subscription struct {
    ObjectType  string
    Where       *WhereClause     // 可选过滤条件
    Properties  []string         // 需要推送的属性列表
}

func (sm *SubscriptionManager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, _ := upgrader.Upgrade(w, r, nil)
    connID := uuid.New().String()

    // 读取订阅定义
    var sub Subscription
    conn.ReadJSON(&sub)

    // 订阅 NATS 变更事件
    natsSub, _ := sm.js.Subscribe(
        fmt.Sprintf("ontology.changes.%s", sub.ObjectType),
        func(msg *nats.Msg) {
            var event ChangeEvent
            json.Unmarshal(msg.Data, &event)

            // 检查是否匹配 Where 条件
            if sub.Where != nil && !matchesWhere(event.Object, sub.Where) {
                return
            }

            // 过滤属性
            filtered := filterProperties(event.Object, sub.Properties)

            // 推送到 WebSocket
            conn.WriteJSON(ObjectUpdate{
                State:  event.State, // "ADDED_OR_UPDATED" | "DELETED"
                Object: filtered,
            })
        },
    )

    sm.connections.Store(connID, &WebSocketConn{conn, natsSub})

    // 清理
    defer func() {
        natsSub.Unsubscribe()
        conn.Close()
        sm.connections.Delete(connID)
    }()

    // 保持连接
    for {
        _, _, err := conn.ReadMessage()
        if err != nil { break }
    }
}
```

---

## 三、REST API 路由设计

完全兼容 Palantir V2 线缆格式：

```go
// cmd/server/routes.go

func SetupRoutes(r chi.Router, svc *Services) {
    r.Route("/api/v2/ontologies", func(r chi.Router) {
        // 认证中间件
        r.Use(AuthMiddleware)

        // 本体
        r.Get("/", svc.OMS.HandleListOntologies)
        r.Get("/{ontology}", svc.OMS.HandleGetOntology)

        r.Route("/{ontology}", func(r chi.Router) {
            // 对象类型元数据
            r.Get("/objectTypes", svc.OMS.HandleListObjectTypes)
            r.Get("/objectTypes/{objectType}", svc.OMS.HandleGetObjectType)
            r.Get("/objectTypes/{objectType}/outgoingLinkTypes", svc.OMS.HandleListLinkTypes)
            r.Get("/objectTypes/{objectType}/outgoingLinkTypes/{linkType}", svc.OMS.HandleGetLinkType)

            // Action 类型元数据
            r.Get("/actionTypes", svc.OMS.HandleListActionTypes)
            r.Get("/actionTypes/{actionType}", svc.OMS.HandleGetActionType)

            // 查询类型元数据
            r.Get("/queryTypes", svc.OMS.HandleListQueryTypes)
            r.Get("/queryTypes/{queryType}", svc.OMS.HandleGetQueryType)

            // 对象 CRUD + 搜索
            r.Get("/objects/{objectType}", svc.OSS.HandleListObjects)
            r.Get("/objects/{objectType}/{primaryKey}", svc.OSS.HandleGetObject)
            r.Post("/objects/{objectType}/search", svc.OSS.HandleSearchObjects)
            r.Post("/objects/{objectType}/aggregate", svc.OSS.HandleAggregateObjects)

            // 链接遍历
            r.Get("/objects/{objectType}/{pk}/links/{linkType}", svc.OSS.HandleListLinkedObjects)

            // ObjectSet 操作（核心端点）
            r.Post("/objectSets/loadObjects", svc.OSS.HandleLoadObjectSet)
            r.Post("/objectSets/aggregate", svc.OSS.HandleAggregateObjectSet)
            r.Post("/objectSets/createTemporary", svc.OSS.HandleCreateTempObjectSet)

            // Action 执行
            r.Post("/actions/{actionType}/apply", svc.Actions.HandleApply)
            r.Post("/actions/{actionType}/applyBatch", svc.Actions.HandleApplyBatch)

            // 查询执行
            r.Post("/queries/{queryApiName}/execute", svc.Functions.HandleExecuteQuery)

            // Attachments
            r.Post("/objects/{objectType}/{pk}/attachments/upload", svc.Attachments.HandleUpload)
            r.Get("/attachments/{attachmentRid}/content", svc.Attachments.HandleDownload)
        })
    })

    // WebSocket 订阅
    r.Get("/api/v2/ontologies/{ontology}/subscriptions", svc.Realtime.HandleWebSocket)

    // 健康检查
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(200)
        w.Write([]byte(`{"status":"ok"}`))
    })
}
```

---

## 四、类型系统 Go 实现

```go
// pkg/types/property_types.go

type BaseType string

const (
    TypeString           BaseType = "string"
    TypeInteger          BaseType = "integer"
    TypeShort            BaseType = "short"
    TypeLong             BaseType = "long"
    TypeFloat            BaseType = "float"
    TypeDouble           BaseType = "double"
    TypeBoolean          BaseType = "boolean"
    TypeByte             BaseType = "byte"
    TypeDate             BaseType = "date"
    TypeTimestamp        BaseType = "timestamp"
    TypeDecimal          BaseType = "decimal"
    TypeArray            BaseType = "array"
    TypeStruct           BaseType = "struct"
    TypeVector           BaseType = "vector"
    TypeGeopoint         BaseType = "geopoint"
    TypeGeoshape         BaseType = "geoshape"
    TypeAttachment       BaseType = "attachment"
    TypeTimeSeries       BaseType = "timeseries"
    TypeMediaReference   BaseType = "mediaReference"
    TypeMarking          BaseType = "marking"
    TypeCipherText       BaseType = "cipherText"
)

// 属性值验证器
type PropertyValidator struct {
    BaseType  BaseType
    Config    TypeConfig  // 类型特定配置
}

type TypeConfig struct {
    // Decimal
    Precision *int `json:"precision,omitempty"`
    Scale     *int `json:"scale,omitempty"`
    // Array
    SubType   *BaseType `json:"subType,omitempty"`
    // Vector
    Dimensions *int `json:"dimensions,omitempty"`
    // Struct
    Fields     map[string]BaseType `json:"fields,omitempty"`
}

func (v *PropertyValidator) Validate(value interface{}) error {
    if value == nil { return nil } // nullable检查在上层

    switch v.BaseType {
    case TypeString:
        if _, ok := value.(string); !ok {
            return fmt.Errorf("expected string, got %T", value)
        }
    case TypeInteger:
        return validateNumericRange(value, -2147483648, 2147483647)
    case TypeLong:
        // 接受 string 或 number（JS精度问题）
        switch value.(type) {
        case string, float64, int64: return nil
        default: return fmt.Errorf("long must be string or number")
        }
    case TypeTimestamp:
        s, ok := value.(string)
        if !ok { return fmt.Errorf("timestamp must be ISO8601 string") }
        _, err := time.Parse(time.RFC3339, s)
        return err
    case TypeDate:
        s, ok := value.(string)
        if !ok { return fmt.Errorf("date must be ISO8601 string") }
        _, err := time.Parse("2006-01-02", s)
        return err
    case TypeGeopoint:
        return validateGeoJSON(value, "Point")
    case TypeGeoshape:
        return validateGeoJSON(value, "Polygon", "MultiPolygon", "LineString")
    // ... 其他类型
    }
    return nil
}
```

---

## 五、RID 生成器

```go
// pkg/rid/generator.go

// RID 格式: ri.{service}.{realm}.{type}.{uuid}
func NewObjectTypeRID() string {
    return fmt.Sprintf("ri.ontology.main.object-type.%s", uuid.New().String())
}

func NewPropertyRID() string {
    return fmt.Sprintf("ri.ontology.main.property.%s", uuid.New().String())
}

func NewLinkTypeRID() string {
    return fmt.Sprintf("ri.ontology.main.link-type.%s", uuid.New().String())
}

func NewObjectRID(objectType string) string {
    return fmt.Sprintf("ri.phonograph2-objects.main.object.%s", uuid.New().String())
}

func NewActionTypeRID() string {
    return fmt.Sprintf("ri.ontology.main.action-type.%s", uuid.New().String())
}
```

---

## 六、项目结构

```
ontology-engine/
├── cmd/
│   └── server/
│       ├── main.go              # 入口：初始化所有服务 + 启动 HTTP
│       └── routes.go            # 路由注册
│
├── pkg/
│   ├── oms/                     # Ontology Metadata Service
│   │   ├── service.go           # 接口定义
│   │   ├── postgres.go          # PostgreSQL 实现
│   │   └── handlers.go          # HTTP handlers
│   │
│   ├── oss/                     # Object Set Service
│   │   ├── service.go           # 接口定义
│   │   ├── handlers.go          # HTTP handlers
│   │   ├── where/               # Where 子句 DSL
│   │   │   ├── types.go         # WhereClause 类型定义
│   │   │   └── converter.go     # → Bleve Query 转换器
│   │   ├── objectset/           # ObjectSet 组合器
│   │   │   ├── definition.go    # 定义类型
│   │   │   └── executor.go      # 执行引擎
│   │   ├── aggregation/         # 聚合引擎
│   │   │   └── engine.go        # Bleve/DuckDB 双模式
│   │   └── pagination/          # 游标分页
│   │       └── cursor.go
│   │
│   ├── actions/                 # Actions Service
│   │   ├── service.go           # 接口定义
│   │   ├── executor.go          # 9步执行引擎
│   │   ├── edits.go             # 编辑指令类型
│   │   ├── rules.go             # 规则执行器
│   │   ├── validation.go        # 参数验证
│   │   └── handlers.go          # HTTP handlers
│   │
│   ├── funnel/                  # Object Data Funnel
│   │   ├── service.go           # 索引管道服务
│   │   ├── nats_setup.go        # NATS JetStream 配置
│   │   ├── indexer.go           # Bleve 索引写入
│   │   ├── materializer.go      # Parquet 物化
│   │   └── changelog.go         # 增量变更计算
│   │
│   ├── index/                   # Bleve 索引管理
│   │   ├── manager.go           # 多索引管理（每个 ObjectType 一个）
│   │   ├── mapping.go           # 属性类型→Bleve字段映射
│   │   └── search.go            # 搜索执行
│   │
│   ├── dataset/                 # Dataset 版本控制
│   │   ├── service.go           # Git-for-Data 实现
│   │   ├── transaction.go       # 事务管理
│   │   ├── parquet_io.go        # Parquet 读写
│   │   └── branch.go            # 分支管理
│   │
│   ├── links/                   # 链接解析器
│   │   ├── resolver.go          # FK/Join Table 链接解析
│   │   └── traversal.go         # Search Around 实现
│   │
│   ├── realtime/                # 实时订阅
│   │   ├── websocket.go         # WebSocket 处理
│   │   └── subscription.go      # 订阅管理
│   │
│   ├── auth/                    # 认证/授权
│   │   ├── oauth2.go            # OAuth2 + PKCE
│   │   ├── middleware.go         # HTTP 认证中间件
│   │   └── security_policy.go   # 对象/属性安全策略执行
│   │
│   ├── types/                   # 类型系统
│   │   ├── property_types.go    # 18种基础类型定义
│   │   ├── validator.go         # 属性值验证
│   │   └── value_types.go       # Value Type（语义包装）
│   │
│   ├── rid/                     # RID 生成器
│   │   └── generator.go
│   │
│   └── errors/                  # 统一错误格式
│       └── api_error.go         # Palantir 兼容的错误响应
│
├── internal/
│   └── config/                  # 配置管理
│       └── config.go
│
├── migrations/                  # PostgreSQL Schema 迁移
│   ├── 001_initial.sql
│   └── ...
│
├── data/                        # 运行时数据目录
│   ├── datasets/
│   ├── indexes/
│   ├── materializations/
│   └── attachments/
│
├── docker-compose.yml           # PostgreSQL + NATS + 主服务
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 七、Docker Compose 部署

```yaml
# docker-compose.yml
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: ontology
      POSTGRES_USER: ontology
      POSTGRES_PASSWORD: ontology_secret
    ports:
      - "5432:5432"
    volumes:
      - pg_data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d

  nats:
    image: nats:2.10-alpine
    command: ["--jetstream", "--store_dir=/data"]
    ports:
      - "4222:4222"   # Client
      - "8222:8222"   # Monitoring
    volumes:
      - nats_data:/data

  ontology-engine:
    build: .
    ports:
      - "8080:8080"    # REST API
      - "8081:8081"    # WebSocket
    environment:
      POSTGRES_DSN: "postgres://ontology:ontology_secret@postgres:5432/ontology?sslmode=disable"
      NATS_URL: "nats://nats:4222"
      DATA_DIR: "/data"
      LOG_LEVEL: "info"
    volumes:
      - engine_data:/data
    depends_on:
      - postgres
      - nats

volumes:
  pg_data:
  nats_data:
  engine_data:
```

---

## 八、核心 Go 依赖清单

```
go.mod 关键依赖:

github.com/go-chi/chi/v5           # HTTP 路由
github.com/blevesearch/bleve/v2    # 全文搜索索引
github.com/jackc/pgx/v5            # PostgreSQL 驱动
github.com/nats-io/nats.go         # NATS 客户端
github.com/parquet-go/parquet-go   # Parquet 读写
github.com/marcboeker/go-duckdb    # DuckDB 嵌入式分析
github.com/google/uuid             # UUID 生成
github.com/gorilla/websocket       # WebSocket
github.com/golang-jwt/jwt/v5       # JWT 解析
golang.org/x/oauth2                # OAuth2 客户端
github.com/rs/zerolog              # 日志
github.com/golang-migrate/migrate  # 数据库迁移
```

---

## 九、分阶段实现路线图

### Phase 1: 骨架搭建（1-2 周）
- [ ] 项目初始化，go mod, docker-compose
- [ ] PostgreSQL schema 迁移
- [ ] OMS 基础 CRUD（Ontology, ObjectType, Property, LinkType）
- [ ] HTTP 路由骨架 + 认证中间件（先用简单 Bearer Token）
- [ ] RID 生成器

### Phase 2: 读路径（2-3 周）
- [ ] Bleve 索引管理器（创建/删除索引，属性类型映射）
- [ ] Where 子句转换器（全部 14 个操作符）
- [ ] OSS 基础端点：GetObject, ListObjects, SearchObjects
- [ ] 分页游标实现
- [ ] ObjectSet 组合器（7 种类型）
- [ ] LoadObjectSet 端点

### Phase 3: 写路径（2-3 周）
- [ ] NATS JetStream 配置
- [ ] ActionType 定义 + 参数验证
- [ ] Action 执行器（简单规则版本：create/modify/delete object/link）
- [ ] Object Data Funnel：编辑队列消费 → Bleve 索引更新
- [ ] ApplyAction + ApplyBatch 端点
- [ ] Action 审计日志

### Phase 4: 持久层 + 物化（2 周）
- [ ] Dataset 服务（Transaction 管理，分支）
- [ ] Parquet 读写器
- [ ] 物化任务（定期将编辑写回 Parquet）
- [ ] 从 Parquet 重建 Bleve 索引的能力

### Phase 5: 高级功能（2-3 周）
- [ ] 聚合引擎（Bleve facets + DuckDB 双模式）
- [ ] 链接解析器（FK + Join Table + Search Around）
- [ ] 实时 WebSocket 订阅
- [ ] 对象/属性安全策略执行
- [ ] DuckDB 大规模查询溢出

### Phase 6: 完善与优化（持续）
- [ ] 增量索引（只索引变更数据）
- [ ] 流式数据源支持
- [ ] Function 执行引擎（可选：嵌入 V8/Goja 运行 JS）
- [ ] OpenAPI 规范导出 + OSDK 代码生成
- [ ] 性能测试与调优
- [ ] OAuth2 PKCE 完整实现

---

## 十、性能预估（单机）

基于合理硬件配置（64GB RAM, NVMe SSD, 16核 CPU）：

| 指标 | 预估值 | 瓶颈 |
|------|-------|------|
| 对象总量 | 1000 万+ | Bleve 索引大小 / 磁盘 |
| 对象类型数 | 数百个 | PostgreSQL 元数据 |
| 搜索延迟（简单查询） | < 10ms | Bleve 内存索引 |
| 搜索延迟（复杂聚合） | < 500ms | DuckDB Parquet 扫描 |
| Action 写入吞吐 | 1000-5000 ops/s | NATS + Bleve 索引 |
| WebSocket 推送延迟 | < 50ms | NATS pub/sub |
| 物化周期 | 可配置（默认 6h） | Parquet 写入 |
| 索引重建时间（100万对象） | 5-15 分钟 | Bleve 批量索引 |
