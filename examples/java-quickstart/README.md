# Weave Java quickstart

A 5 minute hello-world that talks to a local Weave server (or `weave-mock`
fixture) over its REST API using `java.net.http` — JDK 11+ standard library
only, no third-party dependencies required.

## Run against a real Weave server

```bash
make dev                  # from repo root, starts Weave on :9117
javac Main.java
java Main
```

## Run against the offline weave-mock fixture

```bash
go run ./cmd/weave-mock --spec api/openapi.yaml --addr :9090 &
WEAVE_BASE_URL=http://localhost:9090 java Main
```

## Environment

| Variable        | Default                  | Description                |
|-----------------|--------------------------|----------------------------|
| `WEAVE_BASE_URL`| `http://localhost:9117`  | Weave (or mock) base URL.  |
| `WEAVE_TOKEN`   | (unset)                  | Bearer token, if required. |

## Beyond the quickstart

For a fully-typed Java SDK with Object / Action / Function clients,
generate one from your ontology:

```bash
weave-cli sdk gen --lang java --ontology <api-name> -o sdk/
mvn -f sdk/pom.xml install
```

The Maven artifact group is `com.weave.sdk`; refer to the generated
`README.md` for usage.
