"""Top-level Client object.

The Client owns one Transport and exposes API namespaces:

- ``client.ontologies``  -> :class:`weave_client.ontologies.OntologiesAPI`
- ``client.objects``     -> :class:`weave_client.objects.ObjectsAPI`
- ``client.actions``     -> :class:`weave_client.actions.ActionsAPI`
- ``client.objectsets``  -> :class:`weave_client.objectsets.ObjectSetsAPI`

Authentication is configured at construction time. Either supply a JWT
``access_token`` (typically obtained from ``POST /api/auth/login``) or an API
key starting with ``wvk_``. If both are passed, the access token wins.
"""
from __future__ import annotations

from typing import Any, Optional

from ._http import HTTPResponse, Transport
from ._retry import RetryPolicy
from .exceptions import (
    WeaveAuthError,
    WeaveError,
    WeaveNotFoundError,
    WeaveVersionedLookupError,
)
from typing import List

from .types import BuildInfo, Dependency, Feature, LoginResponse


class Client:
    """A configured Weave client."""

    def __init__(
        self,
        base_url: str,
        *,
        access_token: Optional[str] = None,
        api_key: Optional[str] = None,
        timeout: float = 30.0,
        transport: Optional[Transport] = None,
        retry: Optional[RetryPolicy] = None,
    ):
        self.base_url = base_url.rstrip("/")
        self.access_token = access_token
        self.api_key = api_key
        if transport is None:
            transport = Transport(timeout=timeout, retry=retry)
        elif retry is not None:
            transport.retry = retry
        self._transport = transport

        # Lazy import to avoid circular references at module-import time.
        from .actions import ActionsAPI
        from .attachments import AttachmentsAPI
        from .dashboards import DashboardsAPI
        from .functions import FunctionsAPI
        from .objects import ObjectsAPI
        from .objectsets import ObjectSetsAPI
        from .ontologies import OntologiesAPI
        from .notifications import NotificationsAPI
        from .permissions import PermissionsAPI
        from .permissionrequests import PermissionRequestsAPI
        from .queries import QueriesAPI
        from .reactions import ReactionsAPI
        from .sessions import SessionsAPI
        from .timeseries import TimeSeriesAPI
        from .transactions import TransactionsAPI
        from .vertex import VertexAPI

        self.ontologies = OntologiesAPI(self)
        self.objects = ObjectsAPI(self)
        self.actions = ActionsAPI(self)
        self.objectsets = ObjectSetsAPI(self)
        self.functions = FunctionsAPI(self)
        self.timeseries = TimeSeriesAPI(self)
        self.attachments = AttachmentsAPI(self)
        self.transactions = TransactionsAPI(self)
        self.reactions = ReactionsAPI(self)
        self.notifications = NotificationsAPI(self)
        self.dashboards = DashboardsAPI(self)
        self.permissionrequests = PermissionRequestsAPI(self)
        self.permissions = PermissionsAPI(self)
        self.queries = QueriesAPI(self)
        self.sessions = SessionsAPI(self)
        self.vertex = VertexAPI(self)

    @property
    def token(self) -> str:
        """Return the bearer token sent on each request (may be empty)."""
        if self.access_token:
            return self.access_token
        if self.api_key:
            return self.api_key
        return ""

    def close(self) -> None:
        self._transport.close()

    def __enter__(self) -> "Client":
        return self

    def __exit__(self, *exc):
        self.close()

    # ---- internal helpers --------------------------------------------------

    def _headers(self, *, anonymous: bool = False) -> dict:
        h: dict = {}
        if not anonymous and self.token:
            h["Authorization"] = f"Bearer {self.token}"
        return h

    def _request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        anonymous: bool = False,
        extra_headers: Optional[dict] = None,
    ) -> Any:
        url = self.base_url + path
        headers = self._headers(anonymous=anonymous)
        if extra_headers:
            # Caller-supplied headers win — used by VertexAPI for X-Scenario-Id
            # and by the objects.get(scenario_id=...) overlay path. Auth and
            # Accept defaults from _headers() stay intact for any key the
            # caller does not set.
            headers = {**headers, **extra_headers}
        resp = self._transport.request(
            method,
            url,
            headers=headers,
            json_body=json_body,
        )
        return self._handle(resp)

    def _upload_binary(
        self,
        method: str,
        path: str,
        *,
        body: bytes,
        content_type: str,
        extra_headers: Optional[dict] = None,
    ) -> Any:
        """Send a raw-bytes request body and decode the JSON response.

        Used by round-45 attachment uploads — the server's
        ``/attachments/upload`` endpoint streams the request body
        verbatim into the BlobStore. ``content_type`` is mandatory
        because the server uses it to populate ``Attachment.media_type``
        on the persisted blob.
        """
        url = self.base_url + path
        headers = self._headers()
        headers["Content-Type"] = content_type
        if extra_headers:
            headers = {**headers, **extra_headers}
        resp = self._transport.request(
            method, url, headers=headers, raw_body=body,
        )
        return self._handle(resp)

    def _download_binary(
        self,
        method: str,
        path: str,
        *,
        extra_headers: Optional[dict] = None,
    ) -> bytes:
        """Fetch a binary response body.

        Returns the raw bytes; the caller is responsible for
        interpreting them (e.g. image / archive payloads). On a
        non-2xx the standard JSON error envelope path runs unchanged —
        exceptions surface via ``_handle`` as usual.
        """
        url = self.base_url + path
        headers = self._headers()
        if extra_headers:
            headers = {**headers, **extra_headers}
        resp = self._transport.request(
            method, url, headers=headers, accept_binary=True,
        )
        if 200 <= resp.status_code < 300:
            return resp.body_bytes or b""
        # Reuse the standard error-handling path on non-2xx.
        self._handle(resp)
        return b""  # unreachable; _handle raises

    @staticmethod
    def _handle(resp: HTTPResponse) -> Any:
        if 200 <= resp.status_code < 300:
            if not resp.text:
                return None
            try:
                return resp.json()
            except ValueError as e:  # pragma: no cover - bad server response
                raise WeaveError(resp.status_code, raw_body=resp.text) from e

        envelope: dict = {}
        try:
            parsed = resp.json()
            if isinstance(parsed, dict):
                envelope = parsed
        except ValueError:
            pass

        kwargs = dict(
            status_code=resp.status_code,
            error_code=envelope.get("errorCode", "") or "",
            error_name=envelope.get("errorName", "") or "",
            error_instance_id=envelope.get("errorInstanceId", "") or "",
            parameters=envelope.get("parameters") or {},
            raw_body=resp.text,
        )
        if resp.status_code in (401, 403):
            raise WeaveAuthError(**kwargs)
        if resp.status_code == 404:
            raise WeaveNotFoundError(**kwargs)
        # Round 118: narrow-typed 501 dispatch for the round-117
        # backend pilot. Only the VersionedLookupNotSupported
        # variant gets the typed exception; other 501s fall through
        # to plain WeaveError so future unrelated 501 surfaces
        # aren't auto-captured by this branch.
        if (resp.status_code == 501 and
                kwargs["error_name"] == "VersionedLookupNotSupported"):
            raise WeaveVersionedLookupError(**kwargs)
        raise WeaveError(**kwargs)

    # ---- top-level convenience --------------------------------------------

    def login(self, email: str, password: str) -> LoginResponse:
        """Exchange credentials for an access/refresh pair.

        On success the new ``access_token`` is automatically attached to this
        client so subsequent calls are authenticated. Callers may also persist
        ``response.access_token`` and rebuild a fresh ``Client`` later.
        """
        body = self._request(
            "POST",
            "/api/auth/login",
            json_body={"email": email, "password": password},
            anonymous=True,
        )
        resp = LoginResponse.model_validate(body) if hasattr(LoginResponse, "model_validate") else LoginResponse(**body)
        self.access_token = resp.access_token
        return resp

    def logout(self, refresh_token: str = "") -> None:
        """Revoke the supplied (or last-issued) refresh token. Idempotent."""
        body = {"refresh_token": refresh_token} if refresh_token else None
        self._request("POST", "/api/auth/logout", json_body=body, anonymous=True)
        self.access_token = None

    def build_info(self) -> BuildInfo:
        """Fetch the server's build metadata (round 124).

        Mirrors round-123 backend GET /api/v2/build-info. Returns a
        typed BuildInfo (version / commit / go_version / build_time).
        Endpoint is public — no Authorization header is attached
        even when the client has credentials, mirroring the
        backend's security:[] declaration.
        """
        body = self._request("GET", "/api/v2/build-info", anonymous=True)
        if hasattr(BuildInfo, "model_validate"):
            return BuildInfo.model_validate(body or {})
        return BuildInfo(**(body or {}))

    def build_info_dependencies(self) -> List[Dependency]:
        """Fetch the server's Go module dependency inventory (round 126).

        Mirrors round-125 backend GET /api/v2/build-info/dependencies.
        Returns typed Dependency entries (path / version / sum /
        replace). Empty list when the backend has no embedded build
        info (defensive: backend guarantees `[]` not null). Same
        anonymous public access as ``build_info()``.
        """
        body = self._request(
            "GET", "/api/v2/build-info/dependencies", anonymous=True)
        items = (body or {}).get("dependencies", []) if isinstance(body, dict) else []
        if hasattr(Dependency, "model_validate"):
            return [Dependency.model_validate(d) for d in items]
        return [Dependency(**d) for d in items]

    def build_info_features(self) -> List[Feature]:
        """Fetch the server's capability feature manifest (round 128).

        Mirrors round-127 backend GET /api/v2/build-info/features.
        Returns typed Feature entries (name / enabled / description
        / reason). SPA reads this at page-load to decide which UI
        affordances to render without poking endpoints for 404s.
        Same anonymous public access as ``build_info()``.
        """
        body = self._request(
            "GET", "/api/v2/build-info/features", anonymous=True)
        items = (body or {}).get("features", []) if isinstance(body, dict) else []
        if hasattr(Feature, "model_validate"):
            return [Feature.model_validate(f) for f in items]
        return [Feature(**f) for f in items]
