# Examples

Self-contained, copy-paste-runnable Weave projects you can clone or
reference when learning the API. Each directory is independent; pick the
language you're using and follow its README.

| Directory | Stack | What you'll see |
|---|---|---|
| [`py-quickstart/`](./py-quickstart/) | Python 3.9+ via the in-repo [`weave-client`](../sdk/python) SDK | Construct client → list ontologies → list objects |
| [`ts-quickstart/`](./ts-quickstart/) | TypeScript 5 / Node 18+ via global `fetch` | Construct client → list ontologies → list objects |
| [`go-quickstart/`](./go-quickstart/) | Go 1.21+ via `net/http` | Construct client → list ontologies → list objects |

Each quickstart targets a 5-minute experience: clone the repo, start a
local Weave server with `make dev`, then `cd` into the language folder
and run the command in its README.

The Go and TS quickstarts use the raw REST API to keep them
zero-dependency for "look around the API" use; once you're past hello
world, the recommended next step is to generate a typed SDK from your
ontology with `weave-cli sdk gen --lang {go,ts,python}`.
