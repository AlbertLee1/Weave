# Python SDK

The `weave-client` package is a thin, typed Python wrapper around the
Weave REST API. It covers the MVP surface (auth, ontology metadata, object
retrieval, search, action apply) and is hand-written rather than auto-generated
from the OpenAPI spec, so it has no Java/openapi-generator dependency.

The full SDK source lives at [`sdk/python/`](../sdk/python/).

## Installation

The SDK targets Python 3.9 or newer.

```bash
# From a clone of the repository (editable install)
cd sdk/python
pip install -e .

# With the test extras
pip install -e .[test]
```

Runtime dependencies: `httpx>=0.25`, `pydantic>=2.0`. The SDK can also fall
back to `urllib.request` when `httpx` is not installed — convenient for tests,
CI without network egress, or constrained runtimes.

## Quickstart

```python
from weave_client import Client

weave = Client("http://localhost:9117", access_token="eyJhbGciOi…")

# Ontologies and types
for ont in weave.ontologies.list():
    print(ont.api_name, ont.display_name)

types = weave.ontologies.list_object_types("northwind")
customer = weave.ontologies.get_object_type("northwind", "Customer")

# Object retrieval
page = weave.objects.list("northwind", "Customer", page_size=25)
print(len(page.data), page.total_count, page.next_page_token)

for obj in weave.objects.iter_all("northwind", "Customer", page_size=100):
    print(obj["__primaryKey"])

alfki = weave.objects.get("northwind", "Customer", "ALFKI")

# Where-clause search
hits = weave.objects.search("northwind", "Customer", {
    "type": "eq", "field": "country", "value": "Germany",
})

# Action execution
result = weave.actions.apply("northwind", "createCustomer", {
    "customerId": "WEAVE", "companyName": "Weave Co",
})
print(result.action_rid, len(result.edits))
```

## API reference

### Client

```python
Client(
    base_url: str,
    *,
    access_token: str | None = None,
    api_key:      str | None = None,
    timeout:      float = 30.0,
)
```

| Property        | Type             | Notes                                         |
|-----------------|------------------|-----------------------------------------------|
| `base_url`      | `str`            | trailing slash stripped automatically         |
| `token`         | `str` (read-only)| `access_token` if set, else `api_key`         |
| `ontologies`    | `OntologiesAPI`  | metadata namespace                            |
| `objects`       | `ObjectsAPI`     | object retrieval and search                   |
| `actions`       | `ActionsAPI`     | action execution                              |
| `login(email, password)`  | `LoginResponse` | refreshes `access_token` in-place |
| `logout(refresh_token="")`| `None`          | clears the in-memory token        |

### OntologiesAPI

| Method | Endpoint | Returns |
|---|---|---|
| `list()` | `GET /api/v2/ontologies` | `List[Ontology]` |
| `get(api_name)` | `GET /api/v2/ontologies/{name}` | `Ontology` |
| `list_object_types(ontology)` | `GET /api/v2/ontologies/{name}/objectTypes` | `List[ObjectType]` |
| `get_object_type(ontology, object_type)` | `GET .../objectTypes/{type}` | `ObjectType` |

### ObjectsAPI

| Method | Endpoint | Returns |
|---|---|---|
| `list(ontology, object_type, page_size=100, page_token="", order_by="")` | `GET .../objects/{type}` | `ObjectPage` |
| `iter_all(ontology, object_type, page_size=100, order_by="")` | follows `nextPageToken` | `Iterator[WireObject]` |
| `get(ontology, object_type, primary_key)` | `GET .../objects/{type}/{pk}` | `WireObject` (`dict`) |
| `search(ontology, object_type, where, page_size=None, page_token=None)` | `POST .../objects/{type}/search` | `ObjectPage` |

`WireObject` is a plain `dict[str, Any]` with the V2 wire format
(properties flattened, plus `__rid`, `__primaryKey`, `__apiName`).

### ActionsAPI

| Method | Endpoint | Returns |
|---|---|---|
| `apply(ontology, action_type, parameters)` | `POST .../actions/apply` | `ApplyActionResponse` |

### Errors

| Exception | Status codes | Notes |
|---|---|---|
| `WeaveError` | any non-2xx | base class |
| `WeaveAuthError` | 401, 403 | missing/invalid credentials |
| `WeaveNotFoundError` | 404 | resource not found |

All exceptions carry `status_code`, `error_code`, `error_name`,
`error_instance_id`, `parameters`, and `raw_body`.

## Testing

The default test suite uses Python stdlib `unittest` with a tiny in-process
HTTP stub server, so it works without `pip install` or network access:

```bash
cd sdk/python
PYTHONPATH=. python -m unittest discover -s tests -p 'test_*.py' -v
```

After installing the test extras you can also use pytest:

```bash
pip install -e .[test]
pytest
```

The repository root provides a `make python-test` shortcut that runs the
former (no extra deps required).
