"""Pydantic models that mirror the Weave OpenAPI schemas.

The wire protocol uses camelCase, the Python API uses snake_case. Models
configure ``populate_by_name`` so callers can pass either spelling and access
either spelling.
"""
from __future__ import annotations

from datetime import datetime
from typing import Any, Dict, List, Optional

try:
    from pydantic import BaseModel, ConfigDict, Field
except Exception:  # pragma: no cover - pydantic v1 fallback
    from pydantic import BaseModel, Field  # type: ignore

    ConfigDict = None  # type: ignore


def _model_config():
    if ConfigDict is not None:
        return ConfigDict(populate_by_name=True, extra="allow")
    return None


class _CamelModel(BaseModel):
    """Base model that accepts both camelCase and snake_case fields."""

    if ConfigDict is not None:
        model_config = _model_config()
    else:  # pragma: no cover - pydantic v1
        class Config:
            allow_population_by_field_name = True
            extra = "allow"


class Ontology(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    description: Optional[str] = None
    current_version: int = Field(alias="currentVersion")


class ObjectType(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    plural_display_name: Optional[str] = Field(default=None, alias="pluralDisplayName")
    description: Optional[str] = None
    primary_key: str = Field(alias="primaryKey")
    title_property: Optional[str] = Field(default=None, alias="titleProperty")
    status: str
    visibility: str
    properties: Optional[Dict[str, Any]] = None


class LinkType(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    object_type_api_name: str = Field(alias="objectTypeApiName")
    linked_object_type_api_name: str = Field(alias="linkedObjectTypeApiName")
    cardinality: str
    required: bool = False


class ActionType(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    description: Optional[str] = None
    status: str
    parameters: Optional[Dict[str, Any]] = None


# WireObject is intentionally a plain dict alias rather than a model — object
# payloads are unbounded, schema-driven, and cheap to keep raw.
WireObject = Dict[str, Any]


class ObjectPage(_CamelModel):
    data: List[WireObject] = Field(default_factory=list)
    next_page_token: Optional[str] = Field(default=None, alias="nextPageToken")
    total_count: Optional[str] = Field(default=None, alias="totalCount")


class Edit(_CamelModel):
    type: str
    object_type: Optional[str] = Field(default=None, alias="objectType")


class ActionResults(_CamelModel):
    """Foundry OSv2 edit summary (counts, not individual edits)."""
    type: str = "edits"
    added_object_count: int = Field(default=0, alias="addedObjectCount")
    modified_object_count: int = Field(default=0, alias="modifiedObjectCount")
    deleted_object_count: int = Field(default=0, alias="deletedObjectCount")
    added_links_count: int = Field(default=0, alias="addedLinksCount")
    deleted_links_count: int = Field(default=0, alias="deletedLinksCount")


class ApplyActionResponse(_CamelModel):
    """SyncApplyActionResponseV2 — Foundry OSv2 response for single apply."""
    operation_id: Optional[str] = Field(default=None, alias="operationId")
    validation: Optional[Dict[str, Any]] = None
    edits: Optional[ActionResults] = None


class BatchApplyActionResponse(_CamelModel):
    """Response envelope for applyBatch."""
    edits: Optional[ActionResults] = None


class CountResponse(_CamelModel):
    """Response for object count requests."""
    count: int = 0


class InterfaceType(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    extends_rid: Optional[str] = Field(default=None, alias="extendsRid")
    shared_properties: Optional[Dict[str, Any]] = Field(default=None, alias="sharedProperties")


class ValueType(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    base_type: str = Field(alias="baseType")
    constraints: Optional[Dict[str, Any]] = None
    version: int = 0


class QueryType(_CamelModel):
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    description: Optional[str] = None
    parameters: Optional[Dict[str, Any]] = None
    output: Optional[Dict[str, Any]] = None
    query: Optional[Dict[str, Any]] = None
    status: str = ""


class LoginUser(_CamelModel):
    id: str
    email: str
    name: Optional[str] = ""
    roles: List[str] = Field(default_factory=list)
    ontology_roles: Dict[str, str] = Field(default_factory=dict, alias="ontologyRoles")


class LoginResponse(_CamelModel):
    access_token: str
    refresh_token: str
    token_type: str
    expires_in: int
    user: LoginUser


class TimeSeriesPoint(_CamelModel):
    """One sample on a TimeSeriesProperty / TimeSeriesValueBank.

    The server emits ``{"time": "<RFC3339>", "value": <any>}``. Pydantic
    parses ``time`` into a ``datetime`` (UTC) so callers can use it
    directly in arithmetic without an extra parse step. ``value`` stays
    ``Any`` because the schema varies per series (float for sensor
    readings, string for state codes, bool for flags, etc.).
    """
    time: datetime
    value: Any


class Attachment(_CamelModel):
    """Metadata for one blob in the attachment store (round 45).

    Server emits camelCase; the SDK exposes both snake_case (via
    Pydantic field alias) and the wire names. ``linked`` flips to
    True the first time the blob is referenced from an attachment-
    property value on a persisted object — a GC pass can safely
    delete orphan blobs without losing user uploads that just
    haven't been wired to an object yet.
    """
    rid: str
    filename: str = ""
    size_bytes: int = Field(default=0, alias="sizeBytes")
    media_type: str = Field(default="", alias="mediaType")
    created_at: Optional[datetime] = Field(default=None, alias="createdAt")
    linked: bool = False


class ObjectCheckResponse(_CamelModel):
    """Round-106 SDK mirror of round-105 backend ObjectCheckResponse.

    Two-axis read/write matrix for the SPA's per-object-type UI
    gating: can_read controls row visibility, can_write controls
    edit-pencil visibility. Always carries the five fields so the
    SPA can rely on each without nil-checks.
    """
    ontology_api_name: str = Field(alias="ontologyApiName")
    object_type_api_name: str = Field(alias="objectTypeApiName")
    object_type_rid: str = Field(alias="objectTypeRid")
    can_read: bool = Field(alias="canRead")
    can_write: bool = Field(alias="canWrite")


class ObjectCheckBatchEntry(_CamelModel):
    """One row in the round-108 bulk OT check response.

    ``found`` is the key discriminator (round-107 backend contract):
    True when the object type exists, False when it has been
    removed/renamed in config. found=False entries always carry
    can_read=can_write=False regardless of caller perms, so the
    SPA never accidentally shows UI for a missing type.
    """
    object_type_api_name: str = Field(alias="objectTypeApiName")
    found: bool = False
    object_type_rid: str = Field(default="", alias="objectTypeRid")
    can_read: bool = Field(default=False, alias="canRead")
    can_write: bool = Field(default=False, alias="canWrite")


class ObjectCheckBatchResponse(_CamelModel):
    """Round-108 SDK mirror of round-107 backend
    ObjectCheckBatchResponse. results preserves input order so
    callers can correlate row N → row N without a name→row map.
    """
    ontology_api_name: str = Field(alias="ontologyApiName")
    results: List[ObjectCheckBatchEntry] = Field(default_factory=list)


class ActionCheckResponse(_CamelModel):
    """Round-104 SDK mirror of round-103 backend ActionCheckResponse.

    Wire field can_apply uses snake_case via Pydantic alias to match
    the backend's canApply camelCase (Foundry-parity external name).
    Always carries the four fields: caller-side gating logic can rely
    on every field being present without nil-checks.
    """
    ontology_api_name: str = Field(alias="ontologyApiName")
    action_api_name: str = Field(alias="actionApiName")
    action_rid: str = Field(alias="actionRid")
    can_apply: bool = Field(alias="canApply")


class Session(_CamelModel):
    """One active session row from GET /api/auth/sessions (US-254).

    Wire-format uses snake_case for created_at / last_seen /
    user_agent (the Go SessionView struct preserves the Foundry
    json:"..." spellings). _CamelModel's populate-by-name lets
    callers access either spelling via the dataclass.
    """
    id: str
    ip: str = ""
    user_agent: str = Field(default="", alias="user_agent")
    created_at: Optional[datetime] = Field(default=None, alias="created_at")
    last_seen: Optional[datetime] = Field(default=None, alias="last_seen")
    current: bool = False


class RevokeOthersResponse(_CamelModel):
    """Response for POST /api/auth/sessions/revoke-others — round 101.

    revoked is the count of sessions destroyed; current_session_id is
    the session anchor preserved (empty when caller had no anchor,
    in which case revoked covers ALL of the caller's sessions).
    """
    revoked: int = 0
    current_session_id: str = Field(default="", alias="currentSessionId")


class MeOntologiesEntry(_CamelModel):
    """One row from GET /api/v2/me/ontologies — round-100 SDK mirror
    of round-99 backend MeOntologiesResponse. Always carries a
    non-empty role (the backend filters entries where role=='').
    """
    rid: str
    api_name: str = Field(alias="apiName")
    display_name: str = Field(alias="displayName")
    role: str


class PermissionsCheckResponse(_CamelModel):
    """Granted/denied partition of an input permission set —
    round-98 SDK mirror of round-97 backend POST
    /api/v2/me/permissions/check.

    The two list fields always exactly partition the input
    permissions (no overlap, no missing entries). Default-empty
    lists mean callers can do ``for p in resp.granted`` without
    nil-checks even when the server returns ``{"granted":[]}``.
    """
    granted: List[str] = Field(default_factory=list)
    denied: List[str] = Field(default_factory=list)


class OntologyMe(_CamelModel):
    """Caller's scope on ONE specific ontology — round 96 mirror of
    round-95 backend ``GET /api/v2/ontologies/{ontologyApiName}/me``.

    Narrower than :class:`weave_client.types`'s general user shape
    (the global ``/api/v2/me`` returns every per-ontology role at
    once); this model carries just the resolved role + effective
    permissions for ONE ontology, which is the shape the SPA needs
    once it has scoped itself to a single ontology. ``role`` is an
    empty string when the caller has no scoped role on this
    ontology (they still get global-role permissions).
    """
    ontology_rid: str = Field(alias="ontologyRid")
    ontology_api_name: str = Field(alias="ontologyApiName")
    role: str = ""
    permissions: List[str] = Field(default_factory=list)
    markings: List[str] = Field(default_factory=list)
