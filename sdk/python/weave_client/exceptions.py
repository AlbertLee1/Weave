"""Typed exceptions raised by weave_client.

The hierarchy mirrors the HTTP status family:

- WeaveError: base class for any non-2xx response.
- WeaveAuthError: 401/403 — credentials missing or insufficient.
- WeaveNotFoundError: 404 — resource not found.

All exceptions carry the structured error envelope (``error_code``,
``error_name``, ``error_instance_id``, ``parameters``) when the server returns
JSON.  When the body is non-JSON the message falls back to the raw text.
"""
from __future__ import annotations

from typing import Any, Dict, Optional


class WeaveError(Exception):
    """Base class for all Weave SDK errors."""

    def __init__(
        self,
        status_code: int,
        *,
        error_code: str = "",
        error_name: str = "",
        error_instance_id: str = "",
        parameters: Optional[Dict[str, Any]] = None,
        raw_body: str = "",
    ):
        self.status_code = status_code
        self.error_code = error_code
        self.error_name = error_name
        self.error_instance_id = error_instance_id
        self.parameters = parameters or {}
        self.raw_body = raw_body
        message = (
            f"weave: {status_code} {error_code}/{error_name}"
            if error_name
            else f"weave: {status_code} {raw_body}"
        )
        super().__init__(message)


class WeaveAuthError(WeaveError):
    """Raised on HTTP 401/403 responses."""


class WeaveNotFoundError(WeaveError):
    """Raised on HTTP 404 responses."""


class WeaveVersionedLookupError(WeaveError):
    """Raised on HTTP 501 + errorName=VersionedLookupNotSupported.

    Round 118 SDK mirror of round-117 backend pilot. The backend
    returns this when the caller passes a versioned RID (e.g.
    ``ri.ontology.main.object-type.{uuid}@v3``) to a Get endpoint
    that doesn't yet support snapshot lookups. Catching this
    specifically lets callers retry without the @vN suffix or
    surface a 'version pin not yet supported' UI banner — rather
    than treating it as a generic 501.

    Other 501 responses (different errorName) still raise plain
    WeaveError so this narrow typed branch doesn't capture
    unrelated 501s the backend may add later.
    """

    @property
    def version(self) -> str:
        """The version digits parsed by the backend rid.Parse,
        extracted from ``parameters['version']``. Returns ``""`` when
        the backend omits the field (defensive default — callers
        never see a KeyError)."""
        v = self.parameters.get("version", "")
        return v if isinstance(v, str) else ""
