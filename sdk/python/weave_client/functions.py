"""FunctionsAPI - execute ontology functions.

Wraps ``POST /api/v2/ontologies/{ontology}/functions/{function_ref}/execute``.
The ``function_ref`` segment may be a Function RID, a bare ``name``, or
``name@version`` (US-217 semver pinning).

US-219 streaming: ``execute_stream()`` returns a generator that yields each
NDJSON ``item`` as it arrives. A terminal ``error`` line raised by the
server surfaces as a :class:`weave_client.exceptions.WeaveError` with the
``code`` mapped to ``error_name``. Callers consume the iterator with a
plain ``for`` loop:

    for item in client.functions.execute_stream("nw", "topProducts", {"limit": 100}):
        print(item)
"""
from __future__ import annotations

import json
from typing import TYPE_CHECKING, Any, Dict, Iterator, Optional

from ._http import quote_path
from .exceptions import WeaveError

if TYPE_CHECKING:
    from .client import Client


class FunctionsAPI:
    """Wraps ``/api/v2/ontologies/{ontology}/functions/...`` execute paths."""

    def __init__(self, client: "Client"):
        self._client = client

    def execute(
        self,
        ontology: str,
        function_ref: str,
        parameters: Optional[Dict[str, Any]] = None,
    ) -> Any:
        """Execute the function and return the deserialized ``result`` payload.

        Returns ``None`` when the executor itself returned no value.
        """
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/functions/{quote_path(function_ref)}/execute"
        )
        resp = self._client._request("POST", path, json_body=body)
        if not isinstance(resp, dict):
            return resp
        return resp.get("result")

    def execute_stream(
        self,
        ontology: str,
        function_ref: str,
        parameters: Optional[Dict[str, Any]] = None,
    ) -> Iterator[Any]:
        """Execute with ``?stream=1`` and yield each NDJSON item.

        The returned generator yields one element per ``{"item": ...}`` line
        in the response. If the server emits a terminal ``{"error": ...}``
        line (CPU timeout, memory ceiling, or executor failure), the
        generator raises :class:`WeaveError` with ``error_name`` set to the
        server-supplied ``code`` and ``parameters`` carrying the full error
        envelope.

        Pre-execution failures (validation, 404, quota, no-executor) still
        come back as regular HTTP errors and surface as :class:`WeaveError`
        / :class:`WeaveAuthError` / :class:`WeaveNotFoundError` raised
        BEFORE the generator yields its first item.
        """
        body = {"parameters": parameters or {}}
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/functions/{quote_path(function_ref)}/execute?stream=1"
        )
        url = self._client.base_url + path
        status, headers, lines = self._client._transport.stream_lines(
            "POST",
            url,
            headers=self._client._headers(),
            json_body=body,
        )
        if not (200 <= status < 300):
            payload = "\n".join(lines)
            envelope: Dict[str, Any] = {}
            try:
                parsed = json.loads(payload) if payload else {}
                if isinstance(parsed, dict):
                    envelope = parsed
            except ValueError:
                pass
            raise WeaveError(
                status_code=status,
                error_code=envelope.get("errorCode", "") or "",
                error_name=envelope.get("errorName", "") or "",
                error_instance_id=envelope.get("errorInstanceId", "") or "",
                parameters=envelope.get("parameters") or {},
                raw_body=payload,
            )

        return _consume_ndjson(status, lines)


def _consume_ndjson(status: int, lines: Iterator[str]) -> Iterator[Any]:
    """Yield items / raise on terminal error from a NDJSON line stream."""
    for line in lines:
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except ValueError as e:
            raise WeaveError(
                status_code=status,
                error_code="STREAM_DECODE",
                error_name="InvalidStreamLine",
                error_instance_id="",
                parameters={"line": line},
                raw_body=str(e),
            ) from e
        if not isinstance(obj, dict):
            yield obj
            continue
        if "error" in obj:
            err = obj["error"] if isinstance(obj["error"], dict) else {}
            raise WeaveError(
                status_code=status,
                error_code=str(err.get("code", "FunctionExecutionFailed")),
                error_name=str(err.get("code", "FunctionExecutionFailed")),
                error_instance_id="",
                parameters=err,
                raw_body=line,
            )
        if "item" in obj:
            yield obj["item"]
            continue
        yield obj
