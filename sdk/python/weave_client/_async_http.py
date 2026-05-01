"""Async HTTP transport (US-355).

Mirrors :class:`weave_client._http.Transport` but built on
``httpx.AsyncClient``. Unlike the sync transport, there is no urllib
fallback — async is opt-in and httpx is its only backend.
"""
from __future__ import annotations

import json
from typing import Any, AsyncIterator, Dict, Optional, Tuple

import httpx

from ._http import HTTPResponse


class AsyncTransport:
    """Async sibling of :class:`weave_client._http.Transport`."""

    def __init__(self, timeout: float = 30.0):
        self.timeout = timeout
        self._client = httpx.AsyncClient(timeout=timeout)

    async def aclose(self) -> None:
        await self._client.aclose()

    async def request(
        self,
        method: str,
        url: str,
        *,
        headers: Optional[Dict[str, str]] = None,
        json_body: Any = None,
    ) -> HTTPResponse:
        headers = dict(headers or {})
        body_bytes: Optional[bytes] = None
        if json_body is not None:
            body_bytes = json.dumps(json_body).encode("utf-8")
            headers.setdefault("Content-Type", "application/json")
        headers.setdefault("Accept", "application/json")

        resp = await self._client.request(
            method.upper(), url, headers=headers, content=body_bytes
        )
        return HTTPResponse(resp.status_code, resp.text, dict(resp.headers))

    async def stream_lines(
        self,
        method: str,
        url: str,
        *,
        headers: Optional[Dict[str, str]] = None,
        json_body: Any = None,
    ) -> Tuple[int, Dict[str, str], AsyncIterator[str]]:
        """Issue a request and return (status, headers, async_line_iterator).

        Used by NDJSON-style streaming endpoints (US-219). The returned
        async iterator yields one decoded UTF-8 line per element with the
        trailing newline stripped, and the underlying response stays open
        until the iterator is exhausted.

        On HTTP error responses (4xx/5xx) the iterator yields the entire
        error body as a single line; callers should branch on the status
        code before attempting to parse stream entries.
        """
        headers = dict(headers or {})
        body_bytes: Optional[bytes] = None
        if json_body is not None:
            body_bytes = json.dumps(json_body).encode("utf-8")
            headers.setdefault("Content-Type", "application/json")
        headers.setdefault("Accept", "application/x-ndjson")

        req = self._client.build_request(
            method.upper(), url, headers=headers, content=body_bytes
        )
        resp = await self._client.send(req, stream=True)
        status = resp.status_code
        resp_headers = dict(resp.headers)

        if not (200 <= status < 300):
            body = await resp.aread()
            await resp.aclose()
            text = body.decode("utf-8") if body else ""

            async def _err_iter() -> AsyncIterator[str]:
                if text:
                    yield text

            return status, resp_headers, _err_iter()

        async def _line_iter() -> AsyncIterator[str]:
            try:
                async for line in resp.aiter_lines():
                    if line:
                        yield line
            finally:
                await resp.aclose()

        return status, resp_headers, _line_iter()


__all__ = ["AsyncTransport"]
