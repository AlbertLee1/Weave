"""weave-client - Python SDK for the Weave ontology engine.

Quickstart::

    from weave_client import Client

    weave = Client("http://localhost:9117", access_token="...")
    for ontology in weave.ontologies.list():
        print(ontology.api_name)

Async siblings (US-355)::

    import asyncio
    from weave_client import WeaveAsyncClient

    async def main():
        async with WeaveAsyncClient("http://localhost:9117", access_token="...") as c:
            for ontology in await c.ontologies.list():
                print(ontology.api_name)

    asyncio.run(main())

The SDK is intentionally thin: it returns Pydantic models for metadata
endpoints and plain ``dict`` payloads for object endpoints, where the schema
varies per object type.
"""
from ._retry import RetryPolicy
from .async_client import WeaveAsyncClient
from .builders import (
    ObjectSetBuilder,
    avg,
    count,
    max_,
    min_,
    sum_,
)
from .client import Client
from .exceptions import (
    WeaveAuthError,
    WeaveError,
    WeaveNotFoundError,
)
from .subscriptions import (
    ChangeEvent,
    Subscription,
    WeaveOutOfDate,
    WebSocketTransport,
    WebsocketsTransport,
)
from .types import (
    ActionResults,
    ActionType,
    ApplyActionResponse,
    BatchApplyActionResponse,
    CountResponse,
    Edit,
    InterfaceType,
    LinkType,
    LoginResponse,
    ObjectPage,
    ObjectType,
    Ontology,
    QueryType,
    ValueType,
    WireObject,
)

__all__ = [
    "Client",
    "ObjectSetBuilder",
    "RetryPolicy",
    "WeaveAsyncClient",
    "avg",
    "count",
    "max_",
    "min_",
    "sum_",
    "WeaveError",
    "WeaveAuthError",
    "WeaveNotFoundError",
    "ChangeEvent",
    "Subscription",
    "WeaveOutOfDate",
    "WebSocketTransport",
    "WebsocketsTransport",
    "Ontology",
    "ObjectType",
    "WireObject",
    "ObjectPage",
    "ApplyActionResponse",
    "BatchApplyActionResponse",
    "Edit",
    "LoginResponse",
    "ActionResults",
    "ActionType",
    "LinkType",
    "InterfaceType",
    "ValueType",
    "QueryType",
    "CountResponse",
]

__version__ = "0.1.0"
