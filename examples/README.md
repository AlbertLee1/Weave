# Examples

Self-contained, copy-paste-runnable Weave projects you can clone or
reference when learning the API. Each directory is independent; pick the
language you're using and follow its README.

| Directory | Stack | What you'll see |
|---|---|---|
| [`py-quickstart/`](./py-quickstart/) | Python 3.9+ via the in-repo [`weave-client`](../sdk/python) SDK | Construct client → list ontologies → list objects |
| [`ts-quickstart/`](./ts-quickstart/) | TypeScript 5 / Node 18+ via global `fetch` | Construct client → list ontologies → list objects |
| [`go-quickstart/`](./go-quickstart/) | Go 1.21+ via `net/http` | Construct client → list ontologies → list objects |
| [`java-quickstart/`](./java-quickstart/) | Java 11+ via `java.net.http` | Construct client → list ontologies → list objects |

Each quickstart targets a 5-minute experience: clone the repo, start a
local Weave server with `make dev`, then `cd` into the language folder
and run the command in its README. Each one also accepts
`WEAVE_BASE_URL=http://localhost:9090` so you can point it at the offline
[`weave-mock`](../cmd/weave-mock) fixture without standing up the full
stack.

The Go, TS, and Java quickstarts use the raw REST API to keep them
zero-dependency for "look around the API" use; once you're past hello
world, the recommended next step is to generate a typed SDK from your
ontology with `weave-cli sdk gen --lang {go,ts,python,java}`.

## Cross-language contract tests

`contract_test.go` boots an in-process `weave-mock` (backed by
`api/openapi.yaml`), pins the same canonical fixture for the three
endpoints all four quickstarts hit, then runs each quickstart against
the mock and asserts every available SDK surfaces the same canonical
output lines. Each language gates on its toolchain so the suite stays
runnable on a minimal laptop — Go is always exercised, the others skip
when their toolchain isn't reachable.
