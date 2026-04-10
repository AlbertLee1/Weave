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
from .exceptions import WeaveAuthError, WeaveError, WeaveNotFoundError
from .types import LoginResponse


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
    ):
        self.base_url = base_url.rstrip("/")
        self.access_token = access_token
        self.api_key = api_key
        self._transport = transport or Transport(timeout=timeout)

        # Lazy import to avoid circular references at module-import time.
        from .actions import ActionsAPI
        from .objects import ObjectsAPI
        from .objectsets import ObjectSetsAPI
        from .ontologies import OntologiesAPI

        self.ontologies = OntologiesAPI(self)
        self.objects = ObjectsAPI(self)
        self.actions = ActionsAPI(self)
        self.objectsets = ObjectSetsAPI(self)

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
    ) -> Any:
        url = self.base_url + path
        resp = self._transport.request(
            method,
            url,
            headers=self._headers(anonymous=anonymous),
            json_body=json_body,
        )
        return self._handle(resp)

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
