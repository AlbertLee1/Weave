# Security Policy Model

Weave implements Attribute-Based Access Control (ABAC) through a policy engine that enforces row-level and column-level visibility on ontology objects. Policies are evaluated at query time against the requesting user's attributes, producing Bleve queries that transparently filter results.

## Overview

| Layer | Scope | Effect |
|-------|-------|--------|
| Row-level (`OBJECT`) | Which objects a user can see | Bleve query ANDed into every Load / Search / Aggregate path |
| Column-level (`PROPERTY`) | Which properties a user can read | Property allow-list applied after row-level filter |

Both layers are **fail-closed**: when a policy references a user attribute that is missing or empty, the user sees nothing rather than everything.

## Policy Storage

Policies are stored in the `security_policies` table:

```sql
CREATE TABLE security_policies (
    rid             TEXT PRIMARY KEY,
    object_type_rid TEXT NOT NULL REFERENCES object_types(rid),
    policy_type     TEXT NOT NULL CHECK (policy_type IN ('OBJECT', 'PROPERTY')),
    rules           JSONB NOT NULL,
    created_at      TIMESTAMPTZ DEFAULT now()
);
```

- **`rid`** -- Unique policy identifier (e.g. `ri.policy.dept-filter`).
- **`object_type_rid`** -- The ObjectType this policy applies to, referenced by RID (not API name).
- **`policy_type`** -- `OBJECT` for row-level filtering, `PROPERTY` for column-level visibility.
- **`rules`** -- JSONB array of `Rule` objects (see below).

Policies are loaded at server startup from the database into the in-memory `security.Engine`.

## Rule Types

Each policy contains one or more rules. A rule maps a **user attribute** to an **object property** and defines how they must relate.

### Rule Schema

```json
{
  "type": "eq | in | markingSubset",
  "userAttr": "string -- key in user.Attributes",
  "objectProperty": "string -- Bleve field on the object",
  "values": ["string"],
  "properties": ["string"]
}
```

| Field | OBJECT scope | PROPERTY scope |
|-------|-------------|----------------|
| `type` | Required (`eq`, `in`, `markingSubset`) | Omitted |
| `userAttr` | Required | Optional (guard condition) |
| `objectProperty` | Required (Bleve field) | Omitted |
| `values` | Unused | Optional (guard values) |
| `properties` | Unused | Required (granted property names) |

### `eq` -- Exact Match

The user's attribute must equal the object's property value.

```json
{
  "type": "eq",
  "userAttr": "department",
  "objectProperty": "dept"
}
```

A user with `Attributes["department"] = "Engineering"` sees only objects where `dept == "Engineering"`.

**Compiled query:** `TermQuery(value, field=objectProperty)`

### `in` -- List Membership

The user's attribute (a list of values) must overlap with the object's property value. At least one value must match.

```json
{
  "type": "in",
  "userAttr": "regions",
  "objectProperty": "region"
}
```

A user with `Attributes["regions"] = ["US", "EU"]` sees objects where `region` is `"US"` or `"EU"`.

**Compiled query:** `BooleanQuery(Should=[TermQuery(v) for v in values], MinShould=1)`

The user attribute can be `[]string`, `[]any` (JSON-decoded), or a scalar `string` (auto-wrapped to a single-element list).

### `markingSubset` -- Mandatory Access Control

The object's markings must be a subset of the user's markings. This enforces classification-based access: users can only see objects whose markings they hold.

```json
{
  "type": "markingSubset",
  "userAttr": "markings",
  "objectProperty": "_markings"
}
```

User markings are always read from `user.Attributes["markings"]` (the fixed key `markings`). The object marking field is typically `_markings`, a reserved Bleve field populated during indexing.

**Compiled query:** Same `BooleanQuery` shape as `in` -- at least one of the user's markings must appear on the object.

## Evaluation Order

When `Engine.Evaluate()` is called for a user and ObjectType:

1. **Cache check** -- Look up `(userID, objectTypeRID, policyVersion)` in the optional LRU cache.
2. **Compile rules** -- For each `OBJECT`-scope policy registered against the ObjectType:
   - Compile each rule to a Bleve query via `compileRule()`.
   - **Fail-fast**: if any rule produces `MatchNoneQuery` (missing user attribute), return `MatchNoneQuery` immediately.
3. **Auto-marking** -- If the ObjectType has markings enabled (`SetMarkingsEnabled`), append a synthetic `markingSubset` clause.
4. **Combine**:
   - 0 clauses -> `MatchAllQuery` (no policy = allow all).
   - 1 clause -> return it directly.
   - N clauses -> `ConjunctionQuery(clauses...)` (AND all rules together).
5. **Cache store** -- Save the compiled query for reuse.

The resulting Bleve query is ANDed into every OSS Load, Search, and Aggregate path, so policy enforcement is transparent to callers.

## Marking AND Semantics

When both explicit rules and markings are active for an ObjectType, they combine with AND semantics:

```
Final Query = Rule1 AND Rule2 AND ... AND MarkingClause
```

Example: An ObjectType has an `eq` rule on `department` and markings enabled. A user in department `"Sales"` with markings `["CONFIDENTIAL", "INTERNAL"]` must satisfy:
- `dept == "Sales"` (eq rule)
- AND object `_markings` includes at least one of `["CONFIDENTIAL", "INTERNAL"]` (marking clause)

Both conditions must pass. Failing either returns zero results.

### Enabling Markings

```go
engine.SetMarkingsEnabled(objectTypeRID, true)
```

When enabled, every `Evaluate()` call automatically appends the marking-subset clause. This is independent of any explicit `markingSubset` rules in the policy -- it is an additional enforcement layer.

### User Marking Storage

User markings are stored in `user.Attributes["markings"]` and can be:
- `[]string` -- the standard form
- `[]any` -- JSON-decoded arrays (each element coerced to string)
- `string` -- a single marking (auto-wrapped to `["value"]`)

The helper `auth.Markings(ctx)` normalizes extraction and filters empty strings.

## Column-Level Visibility

`PROPERTY`-scope policies control which object properties a user can read.

### Guard Semantics

Each rule in a `PROPERTY` policy acts as a conditional grant:

| `userAttr` | `values` | Behavior |
|------------|----------|----------|
| (empty) | (empty) | **Unconditional** -- always grants listed properties |
| (empty) | non-empty | **Invalid** -- never matches (fail-closed) |
| set | (empty) | **Presence check** -- grants if user has any non-empty value for the attribute |
| set | non-empty | **Value check** -- grants if user's attribute value overlaps `values` |

### Example

```json
{
  "rid": "ri.policy.employee-visibility",
  "objectTypeRid": "ri.ontology.default.objectType.Employee",
  "policyType": "PROPERTY",
  "rules": [
    {
      "properties": ["name", "email", "title"]
    },
    {
      "userAttr": "department",
      "values": ["HR", "Finance"],
      "properties": ["salary", "ssn"]
    },
    {
      "userAttr": "clearance",
      "properties": ["performanceRating"]
    }
  ]
}
```

- **Rule 1** (no guard): Everyone sees `name`, `email`, `title`.
- **Rule 2** (value check): Users in HR or Finance also see `salary`, `ssn`.
- **Rule 3** (presence check): Users with any `clearance` attribute also see `performanceRating`.

The final allow-list is the **union** of all matching rules' `properties`. Properties not in the union are stripped from the response.

### Return Convention

| Scenario | Return | Handler behavior |
|----------|--------|------------------|
| No `PROPERTY` policy registered | `nil` | Pass all properties through |
| Some rules match | `["name", "email", ...]` | Only listed properties visible |
| No rules match | `[]` (empty non-nil) | All properties stripped |

## Fail-Closed Behavior

### Row-Level (OBJECT)

| Scenario | Result |
|----------|--------|
| No policies for ObjectType | `MatchAllQuery` -- default allow |
| User lacks a referenced attribute | `MatchNoneQuery` -- deny all rows |
| User attribute is empty list | `MatchNoneQuery` -- deny all rows |
| Multiple rules, any fails | `MatchNoneQuery` -- fast-fail entire evaluation |
| All rules compile | `ConjunctionQuery` -- AND all rules |
| Markings enabled, user has no markings | `MatchNoneQuery` -- deny all rows |

### Column-Level (PROPERTY)

| Scenario | Result |
|----------|--------|
| No PROPERTY policy for ObjectType | `nil` -- all properties visible |
| No rule guards match | `[]` -- no properties visible |
| Some rule guards match | Union of matching `properties` |

The fail-closed design ensures that misconfigured policies or missing user attributes never accidentally expose data.

## Cache Invalidation

The policy engine includes an optional in-memory LRU + TTL cache for compiled policy queries.

### Cache Key

```
(userID, objectTypeRID, policyVersion)
```

### Configuration

```go
cache := security.NewPolicyCache(1024, 5*time.Minute)
engine.SetCache(cache)
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `max` | 1024 | Maximum cached entries |
| `ttl` | 5 minutes | Time-to-live per entry |

### Invalidation Triggers

1. **Version-based**: Each ObjectType RID maintains a version counter. Calling `SetPolicies()` bumps the version, causing all cached entries for that RID to become stale on next access.
2. **Explicit**: `SetPolicies()` calls `InvalidateObjectType(rid)` which drops all cache entries matching that RID.
3. **TTL-based**: Entries past their TTL are treated as cache misses. Cleanup is lazy (no background eviction thread).
4. **Marking toggle**: `SetMarkingsEnabled()` also bumps the version counter, invalidating cached entries.

### Cache Statistics

```go
stats := cache.Stats() // PolicyCacheStats{Hits, Misses, Size}
rate := cache.HitRate() // float64 in [0, 1]
```

## Integration Points

The policy engine integrates with the query pipeline through narrow interfaces:

### Row-Level Filtering

```
ObjectSet Executor  -->  PolicyQueryProvider.PolicyQuery(ctx, objectType)
OSS Service         -->  engine.Evaluate(ctx, user, objectType)
```

Both paths AND the policy query into the Bleve search request.

### Column-Level Filtering

```
ObjectSet Handler  -->  PropertyFilterProvider.AllowedProperties(ctx, objectType)
Aggregate Handler  -->  PropertyFilterProvider.AllowedProperties(ctx, objectType)
```

Properties not in the allow-list are stripped from the wire response.

### Adapter Pattern

The `cmd/server/policy_provider.go` adapters translate between ObjectType API names (used by handlers) and ObjectType RIDs (used by the policy engine). This keeps `pkg/oss/objectset` free of a direct `pkg/security` import.

```
Handler (API name) --> Adapter (API name -> RID via OMS) --> Engine (RID)
```

### SSE Event Filtering

Live SSE streams apply per-event policy filtering through the `SSEEventFilter` interface, ensuring users only receive events for objects they are authorized to see. See [SSE Subscriptions](../subscriptions/sse.md) for details.

## Thread Safety

- **Engine**: Fully thread-safe via `sync.RWMutex` over policies and version counters.
- **Cache**: Thread-safe via `sync.Mutex` over entries and LRU list.
- **User attributes**: Read-only during evaluation -- no mutations.

## Engine Implementation Details

The sections above describe the contract; this section names the concrete
files behind it so contributors know where to extend, debug, or benchmark.

### CEL Expression DSL

`pkg/security/cel_evaluator.go` provides a Google CEL evaluator for rule
predicates that exceed the declarative `eq` / `in` / `markingSubset`
shapes. A policy rule may carry a `cel` field whose value is a CEL
expression evaluated against `{user, object, request}`. The evaluator
returns a typed boolean — non-boolean results fail closed in the same
manner as a missing user attribute.

```json
{
  "type": "cel",
  "cel": "user.region == object.salesRegion && size(user.markings) > 0"
}
```

The CEL path is opt-in: rules without a `cel` field follow the original
declarative dispatcher. Both paths compose under `ConjunctionQuery`, so a
policy can mix declarative `eq` rules with one CEL rule for the unusual
case. End-to-end coverage lives in
`pkg/oss/row_policy_cel_integration_test.go` and
`cmd/server/rls_cel_us487_bdd_test.go`.

### Decision Cache vs Policy Cache

Two separate caches sit in front of `Engine.Evaluate`:

| Cache | Key | TTL | Purpose |
|-------|-----|-----|---------|
| `policy_cache.go::PolicyCache` | `(userID, objectTypeRID, policyVersion)` | configurable (default 5 min) | Compiled Bleve query for the row-level policy tree |
| `decision_cache.go::DecisionCache` | per-request scoped | request lifetime | Memoize repeated `Evaluate` / `AllowedProperties` calls inside a single request that touches the same ObjectType multiple times (e.g. a Load with N hops) |

The decision cache is request-local, so it does not need invalidation —
the request context expires it. The policy cache is process-local and
follows the version-bump invalidation rules described above.

### Marking Write Guard

In addition to the read-side `MatchAllQuery` / `MatchNoneQuery` branches,
the engine exposes `AllowedForIngest(ctx, user, objectType) (bool, error)`
on the write path. The Action executor calls it before applying an Edit
that touches a marking-enabled ObjectType, returning `403
MarkingPolicyDenied` when the user's marking set fails the AND-semantics
check against the object's stamped markings. This closes the
"read-blocked-but-write-leaks" hole that pure query-time filtering
cannot.

### Auto-Marking Inheritance

Many ontology shapes carry markings transitively — e.g. a Customer is
"PII", and every Order referring to that Customer should inherit "PII"
automatically. `pkg/security/auto_marking_test.go` locks the inheritance
semantics: when a parent property carries a marking, the dependent
ObjectType picks it up at write time so downstream readers see the
combined set. Auto-marking is opt-in per ObjectType and stops at the
configured propagation depth to avoid runaway fan-out.

### Performance Baseline

`pkg/security/rls_bench_test.go` ships a benchmark baseline that exercises
`Engine.Evaluate` at p50 / p99 latencies with a representative policy
fan-out (8 rules per ObjectType, 4 user attributes, marking enabled).
Run via `make bench` or directly with `go test -bench`. The baseline is
the floor the policy hot path must keep below — regressions are caught
by `make test-cover-check` against a tracked baseline file in
`pkg/security/testdata/`.

## Quick Reference

### Adding a New Row-Level Policy

```sql
INSERT INTO security_policies (rid, object_type_rid, policy_type, rules)
VALUES (
  'ri.policy.region-filter',
  'ri.ontology.default.objectType.Order',
  'OBJECT',
  '[{"type": "eq", "userAttr": "region", "objectProperty": "salesRegion"}]'
);
```

After inserting, reload the engine (restart server or call `loadPoliciesFromDB`).

### Adding a New Column-Level Policy

```sql
INSERT INTO security_policies (rid, object_type_rid, policy_type, rules)
VALUES (
  'ri.policy.pii-visibility',
  'ri.ontology.default.objectType.Customer',
  'PROPERTY',
  '[
    {"properties": ["name", "email"]},
    {"userAttr": "role", "values": ["admin"], "properties": ["ssn", "address"]}
  ]'
);
```

This grants everyone access to `name` and `email`, but restricts `ssn` and `address` to admins.
