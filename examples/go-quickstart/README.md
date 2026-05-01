# Weave Go — Quickstart

A 5-minute hello-world that lists ontologies and a few objects via the
Weave REST API. Uses `net/http` from the standard library — no SDK
package required.

## Prerequisites

- Go 1.21+
- A Weave server reachable at `http://localhost:9117`. From the repo root:

  ```bash
  make dev
  ```

## Run

```bash
cd examples/go-quickstart
go run .
```

You should see something like:

```
=== Ontologies ===
- northwind     Northwind
=== Object types in northwind ===
- Customer      Customer
- Order         Order
- ...
=== First 5 Customer ===
- ALFKI         {"__primaryKey":"ALFKI","__apiName":"Customer","companyName":"Alfreds Futterkiste",...}
```

## Configuration

| Env var | Default | Notes |
|---|---|---|
| `WEAVE_BASE_URL` | `http://localhost:9117` | Server URL |
| `WEAVE_TOKEN` | _(unset)_ | Bearer token; only required when `AUTH_MODE=token` |

## Module isolation

This quickstart ships its own `go.mod` so it doesn't get pulled into the
parent Weave module's `go test ./...` and so it can be cloned in
isolation. Verify it on its own:

```bash
go vet ./...
go build ./...
```

## What's next

- Generate a fully-typed SDK from your ontology:

  ```bash
  weave-cli sdk gen --lang go --ontology northwind --out ./sdk
  ```

  The generated client gives you typed `Client.Customer.Get(ctx, pk)`,
  `Apply<ActionName>(ctx, params)`, retry / telemetry middleware, and
  more.

- Search with a filter, follow links, apply actions — see the API
  reference at [`/swagger`](http://localhost:9117/swagger) on a running
  server.
