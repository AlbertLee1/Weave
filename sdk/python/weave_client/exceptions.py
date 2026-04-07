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
