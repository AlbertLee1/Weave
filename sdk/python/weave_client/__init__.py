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
from .rid import InvalidRidError, Rid, format_rid, parse_rid
from .sessions import SessionsAPI
from .types import (
    ActionCheckResponse,
    ActionResults,
    ActionType,
    ApplyActionResponse,
    Attachment,
    BatchApplyActionResponse,
    CountResponse,
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
    QueryType,
    RevokeOthersResponse,
    Session,
    TimeSeriesPoint,
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
    "ObjectCheckResponse",
    "ObjectCheckBatchEntry",
    "ObjectCheckBatchResponse",
]

__version__ = "0.1.0"
