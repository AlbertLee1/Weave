# Weave TypeScript OSDK — Quickstart

A 5-minute hello-world that lists ontologies and a few objects via the
Weave REST API. Doubles as a fully-typed OSDK with four sub-clients you
can lift directly into your own project:

| Client | Surface |
|---|---|
| `client.objects` | `/api/v2/ontologies/{ont}/objects/...` (list, get, search, links, async iterator) |
| `client.actions` | `/api/v2/ontologies/{ont}/actions/{name}/{apply,applyBatch}` |
| `client.functions` | `/api/v2/ontologies/{ont}/functions/{ref}/execute` |
| `client.subscribe` | WebSocket `/subscriptions/ws` with cursor + replay (US-380) |

OpenAPI wire-format types live in `src/openapi.ts` so consumers can
share the same shapes with downstream services.

## Prerequisites

- Node 18+ (or any runtime with global `fetch`). Node 20+ if you want
  to run the OSDK's `node:test` unit suite.
- A Weave server reachable at `http://localhost:9117`. From the repo root:

  ```bash
  make dev
  ```

## Run the quickstart

```bash
cd examples/ts-quickstart
npm install
npm run start            # runs src/main.ts via --experimental-strip-types
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

## Use the OSDK

```ts
import { WeaveClient } from 'weave-ts-quickstart';

const client = new WeaveClient({
  baseUrl: 'http://localhost:9117',
  token: process.env.WEAVE_TOKEN,
});

// Object client
interface Customer { __primaryKey: string; companyName: string }
const customers = client.objects.of<Customer>('northwind', 'Customer');
const page = await customers.list({ pageSize: 10 });
const alfki = await customers.get('ALFKI');
const usaCustomers = await customers.search({
  where: { type: 'eq', field: 'country', value: 'USA' },
});

// Action client
const result = await client.actions.apply(
  'northwind', 'createOrder',
  { customerId: 'ALFKI' },
  { returnEdits: 'CHANGES' },
);

// Function client (typed result)
const top = await client.functions.execute<{ id: string; total: number }[]>(
  'northwind', 'topProducts', { limit: 10 },
);

// Subscribe client (WebSocket — Node 22+ globally, browsers, Deno)
const sub = await client.subscribe.objects('northwind', { objectType: 'Customer' });
for await (const evt of sub) {
  console.log(evt.state, evt.object['__primaryKey'], 'cursor=', evt.cursor);
}
```

## Configuration

| Env var | Default | Notes |
|---|---|---|
| `WEAVE_BASE_URL` | `http://localhost:9117` | Server URL |
| `WEAVE_TOKEN` | _(unset)_ | Bearer token; only required when `AUTH_MODE=token` |

## Type-check, build, test

```bash
npm run typecheck        # tsc --noEmit
npm run build            # tsc → dist/ with .d.ts + sourcemaps
npm test                 # tsc + node --test dist/__tests__/*.test.js
```

`npm run build` emits one `.js` + `.d.ts` + `.d.ts.map` + `.js.map` per
source file under `dist/`. Consumers importing the package as
`weave-ts-quickstart` get full IntelliSense via the published
`dist/index.d.ts`.

The unit tests live in `src/__tests__/*.test.ts` and use a recording
`MockHttp` plus a scripted `SubscribeTransport` so they never open real
sockets. Run `npm test` from a clean checkout — no extra installs
needed.

The quickstart ships ambient declarations for `process` / `console` /
`node:test` / `node:assert/strict` in `src/globals.d.ts` so it
type-checks without `@types/node`. Real projects should
`npm install --save-dev @types/node` and drop the shim.

## What's next

- Generate a fully-typed SDK from your ontology (richer than the
  hand-written types in `src/openapi.ts`):

  ```bash
  weave-cli sdk gen --lang ts --ontology northwind --out ./sdk
  ```

- Search with a filter, follow links, apply actions — see the API
  reference at [`/swagger`](http://localhost:9117/swagger) on a running
  server.
