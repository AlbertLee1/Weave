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
| Stdio | `cmd/weave-mcp` binary (stdio HTTP bridge) | Local AI clients that spawn an MCP subprocess |

The HTTP transport is the canonical server surface. The stdio binary is a
thin bridge for local AI clients: when `WEAVE_MCP_URL` is set, `weave-mcp`
reads newline-delimited JSON-RPC from stdin, forwards each request to the
running Weave server's `/mcp` endpoint, and writes the upstream JSON-RPC
response back to stdout.

Typical local-client configuration:

```json
{
  "mcpServers": {
    "weave": {
      "command": "/usr/local/bin/weave-mcp",
      "env": {
        "WEAVE_MCP_URL": "http://127.0.0.1:9117/mcp",
        "WEAVE_MCP_TOKEN": "<jwt-or-bearer-access-token>",
        "WEAVE_MCP_API_KEY": "wvk_..."
      }
    }
  }
}
```

`WEAVE_MCP_TOKEN` and its alias `WEAVE_MCP_BEARER` are forwarded as
`Authorization: Bearer ...`. `WEAVE_MCP_API_KEY` is forwarded as
`X-Weave-API-Key`. If both are present, the bearer token wins.

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
| `prompts/list` | List prompts synthesized from OMS ActionType metadata |
| `prompts/get` | Render one ActionType prompt with supplied arguments |
| `resources/list` | List ontologies, ObjectTypes, and temporary ObjectSets as MCP resources |
| `resources/read` | Return the schema for an ontology, ObjectType, or stored ObjectSet definition |
| `resources/subscribe` | Subscribe to a known ontology, ObjectType, or ObjectSet resource URI |
| `resources/unsubscribe` | Idempotently remove a resource subscription |
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
#    "capabilities":{
#      "tools":{"listChanged":false},
#      "prompts":{"listChanged":false},
#      "resources":{"listChanged":false,"subscribe":true}},
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

## Prompts

`prompts/list` synthesizes one prompt per OMS ActionType across all
visible ontologies. Prompt names use `ontology__action`, for example
`northwind__create-order`. Each prompt argument mirrors the ActionType
parameter declaration so an AI client can collect the same fields it would
pass to `weave_apply_action`.

```bash
# prompts/list
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"prompts/list","params":{}}'

# prompts/get
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":5,"method":"prompts/get","params":{
        "name":"northwind__create-order",
        "arguments":{"customer":"ALFKI"}}}'
```

`prompts/get` returns a single user-role text message instructing the
client model to call `weave_apply_action` with the same ontology,
ActionType, and parameter values.

## Authentication

The `/mcp` endpoint is registered in the public route group of
`cmd/server/main.go` for the MVP. Production deployments that require
authentication should put a reverse proxy (or move the route under the
`api.Use(auth.Middleware(...))` group) so that the existing JWT/RBAC
checks gate every JSON-RPC call. The MCP server itself never inspects
credentials — it executes tools against whatever `context.Context` the
HTTP layer hands it. Local stdio clients can still send auth to the HTTP
transport through `WEAVE_MCP_TOKEN`, `WEAVE_MCP_BEARER`, or
`WEAVE_MCP_API_KEY` on the `weave-mcp` bridge.

## Resources

`resources/list` returns one entry per ontology, one entry per ObjectType
under each ontology, and one entry per temporary ObjectSet created via
`POST .../objectSets/createTemporary`. URIs follow the
`weave://<kind>/<id>` convention:

| URI | Returned by `resources/read` |
|---|---|
| `weave://ontology/<rid>` | JSON bundle of `ontology` + `objectTypes` + `linkTypes` + `actionTypes` |
| `weave://objecttype/<ontology>/<objectType>` | JSON bundle of `objectType` + `properties` + `outgoingLinkTypes` |
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

# resources/read for an ObjectType
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{
        "uri":"weave://objecttype/demo/User"}}'

# resources/subscribe for an ontology
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"resources/subscribe","params":{
        "uri":"weave://ontology/ri.weave.main.ontology.demo"}}'

# resources/unsubscribe for the same URI
curl -s -X POST http://localhost:9117/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":5,"method":"resources/unsubscribe","params":{
        "uri":"weave://ontology/ri.weave.main.ontology.demo"}}'
```

Reading a resource for an ObjectSet returns the Definition only —
materialise the rows by POSTing the Definition to
`/api/v2/ontologies/{ontology}/objectSets/loadObjects`.

`resources/subscribe` validates the URI against the live catalogue before
recording the subscription, so malformed or unknown resources return a
JSON-RPC error instead of a silent success. `resources/unsubscribe` is
idempotent and succeeds even if the caller has already removed the
subscription.

## Limitations (MVP)

- No JSON Schema validator: argument validation is field presence + a
  primitive type check (string/integer/boolean/object/array).
- `cmd/weave-mcp` depends on a separately running Weave server when
  `WEAVE_MCP_URL` is set; it does not bootstrap PG/NATS/Bleve itself.
- In degraded in-memory mode with no OMS repository, prompts and schema
  resources are empty because there is no metadata source to enumerate.
- The action executor receives the request user via the auth middleware,
  so anonymous MCP calls run as the `system` user.
