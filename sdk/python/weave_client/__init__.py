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
    AggregationResponse,
    AggregationRow,
    ObjectSetBuilder,
    approx_distinct,
    approx_percentile,
    approx_percentiles,
    avg,
    collect_list,
    count,
    duration_group,
    exact_distinct,
    exact_group,
    fixed_width_group,
    max_,
    min_,
    parse_aggregation_response,
    range_group,
    stddev,
    sum_,
    variance,
)
from .client import Client
from .exceptions import WeaveVersionedLookupError  # noqa: F401  re-export
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
from .dashboards import (
    Dashboard,
    DashboardsAPI,
)
from .notifications import (
    Notification,
    NotificationsAPI,
)
from .permissionrequests import (
    PermissionRequest,
    PermissionRequestList,
    PermissionRequestsAPI,
    STATUS_APPROVED,
    STATUS_CANCELLED,
    STATUS_PENDING,
    STATUS_REJECTED,
)
from .reactions import (
    EmojiCount,
    Reaction,
    ReactionsAPI,
    ReactionSummary,
)
from .transactions import (
    Transaction,
    TransactionAppendResponse,
    TransactionsAPI,
)
from .permissions import PermissionsAPI
from .queries import QueriesAPI
from .rid import InvalidRidError, Rid, format_rid, parse_rid
from .sessions import SessionsAPI
from .types import (
    ActionCheckBatchEntry,
    ActionCheckBatchResponse,
    ActionCheckResponse,
    ActionResults,
    ActionType,
    ApplyActionResponse,
    Attachment,
    BatchApplyActionResponse,
    BuildInfo,
    CountResponse,
    Dependency,
    Edit,
    InterfaceType,
    LinkType,
    LoginResponse,
    MeOntologiesEntry,
    ObjectCheckBatchEntry,
    ObjectCheckBatchResponse,
    ObjectCheckResponse,
    ObjectPage,
    ObjectType,
    Ontology,
    OntologyMe,
    PermissionsCheckResponse,
    QueryCheckBatchEntry,
    QueryCheckBatchResponse,
    QueryCheckResponse,
    QueryType,
    RevokeOthersResponse,
    Session,
    SharedPropertyType,
    TimeSeriesPoint,
    TypeGroup,
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
    "approx_distinct",
    "exact_distinct",
    "stddev",
    "variance",
    "collect_list",
    "approx_percentile",
    "approx_percentiles",
    "exact_group",
    "fixed_width_group",
    "range_group",
    "duration_group",
    "AggregationResponse",
    "AggregationRow",
    "parse_aggregation_response",
    "WeaveError",
    "WeaveAuthError",
    "WeaveNotFoundError",
    "WeaveVersionedLookupError",
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
    "TimeSeriesPoint",
    "Attachment",
    "Transaction",
    "TransactionAppendResponse",
    "TransactionsAPI",
    "EmojiCount",
    "Reaction",
    "ReactionSummary",
    "ReactionsAPI",
    "Notification",
    "NotificationsAPI",
    "Dashboard",
    "DashboardsAPI",
    "PermissionRequest",
    "PermissionRequestList",
    "PermissionRequestsAPI",
    "STATUS_PENDING",
    "STATUS_APPROVED",
    "STATUS_REJECTED",
    "STATUS_CANCELLED",
    "Rid",
    "InvalidRidError",
    "parse_rid",
    "format_rid",
    "OntologyMe",
    "PermissionsCheckResponse",
    "PermissionsAPI",
    "MeOntologiesEntry",
    "Session",
    "RevokeOthersResponse",
    "SessionsAPI",
    "ActionCheckResponse",
    "ActionCheckBatchEntry",
    "ActionCheckBatchResponse",
    "ObjectCheckResponse",
    "ObjectCheckBatchEntry",
    "ObjectCheckBatchResponse",
    "QueryCheckResponse",
    "QueryCheckBatchEntry",
    "QueryCheckBatchResponse",
    "QueriesAPI",
    "SharedPropertyType",
    "TypeGroup",
    "BuildInfo",
    "Dependency",
]

__version__ = "0.1.0"
