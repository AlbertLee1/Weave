# Weave Python SDK — Quickstart

A 5-minute hello-world that lists ontologies and a few objects via
[`weave-client`](../../sdk/python).

## Prerequisites

- Python 3.9+
- A Weave server reachable at `http://localhost:9117`. From the repo root:

  ```bash
  make dev
  ```

## Run

```bash
cd examples/py-quickstart
pip install -e ../../sdk/python
python main.py
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
- ALFKI         {'__primaryKey': 'ALFKI', '__apiName': 'Customer', 'companyName': 'Alfreds Futterkiste', ...}
```

## Configuration

| Env var | Default | Notes |
|---|---|---|
| `WEAVE_BASE_URL` | `http://localhost:9117` | Server URL |
| `WEAVE_TOKEN` | _(unset)_ | JWT or API key (`wvk_…`); only required when `AUTH_MODE=token` |

## What's next

- Iterate every object: `for row in client.objects.iter_all(ontology, type, page_size=100): ...`
- Search with a filter: `client.objects.search(ontology, type, {"type": "eq", "field": "country", "value": "Germany"})`
- Apply an action: `client.actions.apply(ontology, "createCustomer", {...})`
- Async siblings: `from weave_client import WeaveAsyncClient`

See [../../sdk/python/README.md](../../sdk/python/README.md) for the full SDK
surface and [../../docs](../../docs) for end-to-end guides.
