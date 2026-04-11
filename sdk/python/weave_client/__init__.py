"""weave-client - Python SDK for the Weave ontology engine.

Quickstart::

    from weave_client import Client

    weave = Client("http://localhost:9117", access_token="...")
    for ontology in weave.ontologies.list():
        print(ontology.api_name)

The SDK is intentionally thin: it returns Pydantic models for metadata
endpoints and plain ``dict`` payloads for object endpoints, where the schema
varies per object type.
"""
from .client import Client
from .exceptions import (
    WeaveAuthError,
    WeaveError,
    WeaveNotFoundError,
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
    "WeaveError",
    "WeaveAuthError",
    "WeaveNotFoundError",
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
