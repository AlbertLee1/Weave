# Multi-Language SDK Cookbook (US-424)

Ten task-oriented recipes shown in **four languages** side by side — Python,
TypeScript, Go, and Java. Every chapter exercises the same wire endpoint
across all four SDKs so the reader can compare ergonomics, copy the snippet
that matches their stack, and verify behaviour stays consistent under
`weave-mock` contract testing (US-423).

| # | Chapter | Endpoint | Languages |
|---|---------|----------|-----------|
| 1 | [Login](01-login.md) | `POST /api/auth/login` + Bearer header | py · ts · go · java |
| 2 | [Load](02-load.md) | `GET /api/v2/ontologies/{ontology}/objects/{objectType}` | py · ts · go · java |
| 3 | [Aggregate](03-aggregate.md) | `POST /api/v2/ontologies/{ontology}/objects/{objectType}/aggregate` | py · ts · go · java |
| 4 | [Action](04-action.md) | `POST /api/v2/ontologies/{ontology}/actions/{actionType}/apply` | py · ts · go · java |
| 5 | [Subscribe](05-subscribe.md) | `WS /api/v2/subscriptions/objects?token=...` (US-380) | py · ts · go · java |
| 6 | [Saga](06-saga.md) | `POST /api/v2/ontologies/{ontology}/actions/applySaga` (US-369) | py · ts · go · java |
| 7 | [Function](07-function.md) | `POST /api/v2/ontologies/{ontology}/functions/{rid}/execute` | py · ts · go · java |
| 8 | [Branch](08-branch.md) | `POST /api/v2/ontologies/{ontology}/branches` (US-383) | py · ts · go · java |
| 9 | [Lineage](09-lineage.md) | `GET /api/v2/lineage/property/{rid}` (US-377) | py · ts · go · java |
| 10 | [Batch](10-batch.md) | `POST /api/v2/ontologies/{ontology}/actions/{actionType}/applyBatch` | py · ts · go · java |

## Conventions

Every snippet honours the same two environment variables so a single
recipe runs against any deployment without code edits:

```bash
export WEAVE_BASE_URL=http://localhost:9117   # default
export WEAVE_TOKEN=eyJhbGciOi...              # AUTH_MODE=token only
```

Under `AUTH_MODE=dev` the token is unused; the SDKs treat an empty
`WEAVE_TOKEN` as "no auth header" rather than a bug. Chapter 1 covers the
`AUTH_MODE=token` exchange explicitly.

## SDK source pointers

| Language | Package | Source |
|---|---|---|
| Python | `weave_client` (PyPI: `weave-client`) | [`sdk/python/weave_client/`](../../../sdk/python/weave_client) |
| TypeScript | OSDK quickstart (template) | [`examples/ts-quickstart/src/`](../../../examples/ts-quickstart/src) |
| Go | `weave-cli sdk gen --lang go` | [`pkg/sdkgen/`](../../../pkg/sdkgen) |
| Java | `weave-cli sdk gen --lang java` | [`pkg/sdkgen/`](../../../pkg/sdkgen) |

The Python SDK is the most feature-complete (typed clients, async iterators,
WS subscribe). TypeScript ships a four-client OSDK template under
`examples/ts-quickstart` (US-419). The Go and Java SDKs are generated per
ontology by `weave-cli sdkgen` (US-420 / US-421); the cookbook examples use
the stdlib `net/http` and `java.net.http` shapes for portability — once you
generate a typed SDK the call sites collapse to the typed equivalents.

## Pairing with weave-mock

To run the snippets without spinning up Postgres / NATS, point them at
`pkg/mockserver` (US-423):

```bash
go run ./cmd/weave-mock --port 9117 &
export WEAVE_BASE_URL=http://localhost:9117
```

The mock server serves the OpenAPI baseline plus the canonical fixtures
the contract suite pins, so every chapter's "happy path" returns the same
shape on the mock as on a real server.
