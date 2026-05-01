# Weave TypeScript — Quickstart

A 5-minute hello-world that lists ontologies and a few objects via the
Weave REST API. Uses the global `fetch` (Node 18+ / modern browsers /
Deno / Bun) so there's no client library to install.

## Prerequisites

- Node 18+ (or any runtime with global `fetch`)
- A Weave server reachable at `http://localhost:9117`. From the repo root:

  ```bash
  make dev
  ```

## Run

```bash
cd examples/ts-quickstart
npm install
npx tsx src/main.ts        # or: node --experimental-strip-types src/main.ts
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

## Type-check only

```bash
npx tsc --noEmit
```

This runs in isolation — the quickstart ships ambient declarations for
`process` / `console` in `src/globals.d.ts` so it type-checks without
`@types/node`. Real projects should `npm install --save-dev @types/node`
and drop the `globals.d.ts` shim.

## What's next

- Generate a fully-typed SDK from your ontology:

  ```bash
  weave-cli sdk gen --lang ts --ontology northwind --out ./sdk
  ```

  The generated client gives you `WeaveClient.Customer.list()`,
  `apply<ActionName>()`, `useRetry()`, `useTelemetry()`, and friends.

- Search with a filter, follow links, apply actions — see the API
  reference at [`/swagger`](http://localhost:9117/swagger) on a running
  server.
