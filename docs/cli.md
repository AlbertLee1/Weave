# `weave` CLI

The `weave` command-line tool is a Go binary that talks to a Weave server
over HTTP. It is built on top of `internal/cliclient`, the same Go HTTP
wrapper that powers the SDK's smoke tests.

The CLI source lives in [`cmd/weave-cli/`](../cmd/weave-cli/).

## Installation

```bash
# Build the binary
go build -o bin/weave ./cmd/weave-cli

# Install into $GOPATH/bin
go install ./cmd/weave-cli
```

The resulting binary is named `weave-cli` when installed via `go install`. If
you prefer the bare name `weave`, use `go build -o bin/weave ...` and put it
on your `PATH`.

## Configuration

`weave` reads its config from `~/.config/weave/config.toml` by default. The
location can be overridden with `WEAVE_CONFIG_DIR=/path/to/dir` (the file is
always called `config.toml`).

```toml
[default]
base_url     = "http://localhost:9117"
access_token = ""
api_key      = ""
```

`access_token` takes precedence over `api_key`. Set values via the CLI:

```bash
weave config set base_url     http://localhost:9117
weave config set access_token eyJhbGciOi…
# or
weave config set api_key      wvk_secret
```

Read them back:

```bash
weave config get               # dump everything
weave config get base_url      # one key
```

## Authentication

```bash
weave auth login --email admin@example.com --password ******
# Logged in as admin@example.com (token expires in 900 seconds).

weave auth status
# logged in (token set, base_url=http://localhost:9117)

weave auth logout
# Logged out.
```

`weave auth login` exchanges the credentials for a JWT access token via
`POST /api/auth/login` and persists it to the config file.

## Ontology commands

```bash
# List every ontology the caller can see
weave ontology list

# Same, but emit raw JSON for piping into jq
weave ontology list --json | jq '.[].apiName'

# Fetch a single ontology
weave ontology get northwind
```

Sample table output:

```
API NAME  DISPLAY NAME  VERSION  RID
northwind Northwind     3        ri.ontology.main.ontology.northwind
chinook   Chinook       1        ri.ontology.main.ontology.chinook
```

## Object commands

```bash
# List objects of a type (default page size 50)
weave object list --ontology northwind --type Customer --limit 10

# Walk a specific page
weave object list --ontology northwind --type Customer \
                  --limit 10 --page-token <token>

# Order results
weave object list --ontology northwind --type Customer --order-by customerId

# Get a single object by primary key
weave object get --ontology northwind --type Customer --pk ALFKI

# Where-clause search (where is parsed as JSON)
weave object search --ontology northwind --type Customer \
    --where '{"type":"eq","field":"country","value":"Germany"}'
```

The `list` and `search` outputs use a tabular layout where columns come from
the union of property keys (excluding the `__rid`/`__primaryKey`/`__apiName`
meta fields). Long string values are truncated at 40 characters.

## Command reference

### `weave config <get|set>`

| Subcommand | Flags / args | Description |
|---|---|---|
| `get` | (none) | dump every key |
| `get` | `<key>` | read one key (`base_url`, `access_token`, `api_key`) |
| `set` | `<key> <value>` | persist one key |

### `weave ontology <list|get>`

| Subcommand | Flags | Description |
|---|---|---|
| `list` | `--json` | list ontologies (table or raw JSON) |
| `get`  | `<api_name>` | fetch one ontology by api name or RID |

### `weave object <list|get|search>`

All require `--ontology <name>` and `--type <api_name>`.

| Subcommand | Extra flags | Description |
|---|---|---|
| `list` | `--limit N`, `--page-token TOK`, `--order-by FIELD`, `--json` | list one page |
| `get` | `--pk PK` | fetch by primary key |
| `search` | `--where '<json>'` | POST a where clause |

### `weave action apply`

Invokes an ActionType against the server. Parameters can come from a
single JSON blob (`--params`) **or** from repeated `--param key=value`
flags, which are convenient for ad-hoc smoke tests. The two are
mutually exclusive — `--params` wins when both are given.

| Flag | Description |
|---|---|
| `--ontology <name>` | ontology api name (required) |
| `--action <api-name>` | ActionType api name (required) |
| `--params '<json>'` or `--params @file.json` | parameter object, literal JSON or `@/path/to/file.json` |
| `--param key=value` | single string-valued parameter (repeatable) |
| `--mode VALIDATE_ONLY \| VALIDATE_AND_EXECUTE` | Foundry-style mode toggle (server default = execute) |
| `--returnEdits ALL \| ALL_V2 \| NONE` | edits return policy (server default = `ALL`) |
| `--json` | emit raw JSON envelope (default `true`) |

```bash
# Submit a CreateOrder action against the Northwind ontology.
weave action apply \
    --ontology northwind \
    --action createOrder \
    --params '{"customerId":"ALFKI","employeeId":"5","shipCity":"Berlin"}'

# Repeated --param flags for one-off shapes (string-typed values only).
weave action apply --ontology northwind --action createCustomer \
    --param customerId=NEWCO \
    --param companyName='New Co LLC' \
    --param country=DE

# Dry-run via VALIDATE_ONLY; the response carries `validation.result` only.
weave action apply --ontology northwind --action createOrder \
    --params @order-shape.json \
    --mode VALIDATE_ONLY
```

### `weave aggregate`

Runs a single `POST .../objects/{type}/aggregate` request. The request
body is forwarded verbatim — the CLI does NOT validate the
`aggregation` / `groupBy` shape locally so server-side error messages
surface unchanged.

| Flag | Description |
|---|---|
| `--ontology <name>` | ontology api name (required) |
| `--type <api-name>` | ObjectType api name (required) |
| `--body '<json>'` or `--body @file.json` | aggregation body (required) |
| `--output json \| table` | response rendering (default `json`); `table` flattens one-level groupBy + metric results |

```bash
# Count orders per country.
weave aggregate --ontology northwind --type Order \
    --body '{
      "aggregation": [{"type": "count", "name": "n"}],
      "groupBy":     [{"field": "country", "type": "exact"}]
    }' \
    --output table
```

Common body templates:

```jsonc
// Total revenue.
{"aggregation": [{"type": "sum", "field": "freight", "name": "total"}]}

// Two-level groupBy with multiple metrics.
{
  "aggregation": [
    {"type": "count", "name": "orderCount"},
    {"type": "sum",   "field": "freight", "name": "totalFreight"}
  ],
  "groupBy": [
    {"field": "country", "type": "exact"},
    {"field": "year",    "type": "exact"}
  ]
}
```

### `weave objectset <load|create-temporary>`

Composable ObjectSet operations. Both subcommands take a JSON body
(literal or `@file`) and forward it to the server unchanged.

| Subcommand | Flags | Description |
|---|---|---|
| `load` | `--ontology <name>` `--body '<json>'` | `POST .../objectSets/loadObjects`; resolves the ObjectSet and returns the matching objects + pagination cursor |
| `create-temporary` | `--ontology <name>` `--body '<json>'` | `POST .../objectSets/createTemporary`; persists the ObjectSet definition for ~1h reuse and returns the assigned `objectSetRid` |

```bash
# Filter then load a page of objects.
weave objectset load --ontology northwind --body '{
  "objectSet": {
    "type": "filter",
    "objectSet": {"type": "base", "objectType": "Customer"},
    "where":     {"type": "eq", "field": "country", "value": "Germany"}
  },
  "select":   ["customerId", "companyName"],
  "pageSize": 25
}'

# Persist a re-usable ObjectSet (RID is returned for later reference).
weave objectset create-temporary --ontology northwind --body '{
  "objectSet": {
    "type": "searchAround",
    "objectSet": {"type": "base", "objectType": "Customer"},
    "link":      "orders"
  }
}'
```

Body templates worth memorising:

```jsonc
// Set difference: Customers minus VIPs.
{"objectSet": {
  "type": "subtract",
  "objectSets": [
    {"type": "base", "objectType": "Customer"},
    {"type": "base", "objectType": "VipCustomer"}
  ]
}}

// Multi-hop searchAround Customer → orders → orderItems.
{"objectSet": {
  "type": "searchAround",
  "objectSet": {"type": "base", "objectType": "Customer"},
  "path": [{"link": "orders"}, {"link": "items"}]
}}
```

### `weave auth <login|logout|status>`

| Subcommand | Flags | Description |
|---|---|---|
| `login` | `--email E --password P` | exchange credentials, persist access token |
| `logout` | (none) | revoke server-side and clear local token |
| `status` | (none) | print whether a token is configured |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | success |
| `1` | runtime failure (HTTP error, IO failure, etc.) |
| `2` | usage error (missing flag, unknown command, etc.) |

## Example session

```bash
$ weave config set base_url http://localhost:9117
set base_url

$ weave auth login --email admin@example.com --password password
Logged in as admin@example.com (token expires in 900 seconds).

$ weave ontology list
API NAME  DISPLAY NAME  VERSION  RID
northwind Northwind     3        ri.ontology.main.ontology.northwind

$ weave object list --ontology northwind --type Customer --limit 3
PK     CITY    COMPANYNAME              CONTACTNAME       COUNTRY
ALFKI  Berlin  Alfreds Futterkiste      Maria Anders      Germany
ANATR  México  Ana Trujillo Emparedados Ana Trujillo      Mexico
ANTON  México  Antonio Moreno Taquería  Antonio Moreno    Mexico

$ weave auth logout
Logged out.
```
