# Weave MCP Server

Weave ships with a built-in [Model Context Protocol](https://modelcontextprotocol.io/specification)
server so AI agents (Claude Desktop, Cursor, GPT desktop apps, custom
agents) can read and write the ontology directly via JSON-RPC 2.0.

The server is a thin façade over Weave's existing OSS / OMS / Action
layers — it does not duplicate business logic. The same auth, indexes,
links, and event pipeline back the MCP tools as back the REST API.

## Transports

Two transports are available:

| Transport | Endpoint | Use case |
|---|---|---|
| HTTP | `POST /mcp` on the running Weave server | Remote AI clients, sandboxed agents, CI bots |
| Stdio | `cmd/weave-mcp` binary (stub) | Local AI clients that spawn an MCP subprocess |

The HTTP transport is the supported MVP path. The stdio binary is a
stub that demonstrates the framing — full PG/NATS-backed bootstrap is
tracked separately. For local AI clients today, point them at the HTTP
endpoint of a running Weave server.

## Protocol

The wire format is plain JSON-RPC 2.0:

```jsonc
// Request
{ "jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {} }

// Response (success)
{ "jsonrpc": "2.0", "id": 1, "result": { "tools": [ ... ] } }

// Response (error)
{ "jsonrpc": "2.0", "id": 1, "error": { "code": -32601, "message": "..." } }
```

Per JSON-RPC 2.0, **all** protocol errors (parse error, invalid request,
method not found, invalid params, internal error, application error)
are returned in the response envelope with **HTTP 200**. Non-200 status
codes (`405`, `204`) are reserved for transport-level conditions.

### Methods

| Method | Description |
|---|---|
| `initialize` | Handshake; returns `protocolVersion`, `capabilities`, and `serverInfo` |
| `tools/list` | List the registered Weave tools and their input schemas |
| `tools/call` | Invoke a tool by `name` with an `arguments` object |
| `prompts/list` | Returns an empty list (Weave does not yet expose prompts) |
| `resources/list` | List ontologies and temporary ObjectSets as MCP resources |
| `resources/read` | Return the schema for an ontology or the stored definition for an ObjectSet |
| `ping` | Liveness check; returns `{}` |

Notifications (requests with no `id`) such as `notifications/initialized`
are accepted and dispatched but never receive a response (HTTP 204 over
the HTTP transport).

### Standard error codes

| Code | Meaning |
|---|---|
| `-32700` | Parse error (invalid JSON) |
| `-32600` | Invalid request |
| `-32601` | Method (or tool) not found |
| `-32602` | Invalid params (validation failure) |
| `-32603` | Internal error |
| `-32000` | Tool execution error (Weave-defined, in the JSON-RPC server-error range) |

## Tool catalogue (MVP)

Seven tools are registered out of the box. All take a JSON object as
their `arguments`; required fields are checked before the underlying
service is called.

### `weave_list_ontologies`
List all ontologies in the instance.
```json
{ "name": "weave_list_ontologies", "arguments": {} }
```

### `weave_list_object_types`
List all object types in an ontology.
```json
{
  "name": "weave_list_object_types",
  "arguments": { "ontology": "demo" }
}
```

### `weave_get_object`
Fetch a single object by primary key.
```json
{
  "name": "weave_get_object",
  "arguments": {
    "ontology": "demo",
    "objectType": "User",
    "primaryKey": "u-42"
  }
}
```

### `weave_list_objects`
List objects of a type with cursor pagination.
```json
{
  "name": "weave_list_objects",
  "arguments": {
    "ontology": "demo",
    "objectType": "User",
    "pageSize": 50,
    "pageToken": "..."
  }
}
```

### `weave_search_objects`
Search a type with a Palantir-style where clause.
```json
{
  "name": "weave_search_objects",
  "arguments": {
    "ontology": "demo",
    "objectType": "User",
    "where": { "type": "eq", "field": "email", "value": "alice@example.com" }
  }
}
```

### `weave_list_action_types`
List all action types in an ontology (returns name, displayName, status).
```json
{
  "name": "weave_list_action_types",
  "arguments": { "ontology": "demo" }
}
```

### `weave_apply_action`
Execute an action by API name with parameters. Returns the resulting
edits and batch id.
```json
{
  "name": "weave_apply_action",
  "arguments": {
    "ontology": "demo",
    "actionType": "createUser",
    "parameters": { "email": "bob@example.com", "displayName": "Bob" }
  }
}
```

## End-to-end example

A complete handshake → list → call sequence over HTTP:

```bash
# 1. initialize
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
# {"jsonrpc":"2.0","id":1,"result":{
#    "protocolVersion":"2024-11-05",
#    "capabilities":{"tools":{"listChanged":false}},
#    "serverInfo":{"name":"weave-mcp","version":"0.1.0"}}}

# 2. tools/list
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

# 3. tools/call → weave_list_ontologies
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
        "name":"weave_list_ontologies",
        "arguments":{}}}'
```

## Authentication

The `/mcp` endpoint is registered in the public route group of
`cmd/server/main.go` for the MVP. Production deployments that require
authentication should put a reverse proxy (or move the route under the
`api.Use(auth.Middleware(...))` group) so that the existing JWT/RBAC
checks gate every JSON-RPC call. The MCP server itself never inspects
credentials — it executes tools against whatever `context.Context` the
HTTP layer hands it.

## Resources

`resources/list` returns one entry per ontology and one entry per
temporary ObjectSet (created via `POST .../objectSets/createTemporary`).
URIs follow the `weave://<kind>/<id>` convention:

| URI | Returned by `resources/read` |
|---|---|
| `weave://ontology/<rid>` | JSON bundle of `ontology` + `objectTypes` + `linkTypes` + `actionTypes` |
| `weave://objectset/<id>` | JSON object with the stored ObjectSet `definition` and `createdAt` |

```bash
# resources/list
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}'

# resources/read for an ontology
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{
        "uri":"weave://ontology/ri.weave.main.ontology.demo"}}'
```

Reading a resource for an ObjectSet returns the Definition only —
materialise the rows by POSTing the Definition to
`/api/v2/ontologies/{ontology}/objectSets/loadObjects`.

## Limitations (MVP)

- No JSON Schema validator: argument validation is field presence + a
  primitive type check (string/integer/boolean/object/array).
- The stdio binary is a stub; it accepts JSON-RPC framing but does not
  yet bootstrap a live PG/NATS-backed Weave instance.
- `prompts/list` always returns an empty array.
- The action executor receives the request user via the auth middleware,
  so anonymous MCP calls run as the `system` user.
