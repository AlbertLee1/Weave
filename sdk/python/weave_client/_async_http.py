"""Async HTTP transport (US-355).

Mirrors :class:`weave_client._http.Transport` but built on
``httpx.AsyncClient``. Unlike the sync transport, there is no urllib
fallback — async is opt-in and httpx is its only backend.
"""
from __future__ import annotations

import asyncio
import json
import time
from typing import Any, AsyncIterator, Dict, Optional, Tuple

import httpx

from ._http import HTTPResponse
from ._retry import RetryPolicy, header_get_ci, parse_retry_after


class AsyncTransport:
    """Async sibling of :class:`weave_client._http.Transport`."""

    def __init__(self, timeout: float = 30.0, retry: Optional[RetryPolicy] = None):
        self.timeout = timeout
        self.retry = retry
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
        method_upper = method.upper()
        headers = dict(headers or {})
        body_bytes: Optional[bytes] = None
        if json_body is not None:
            body_bytes = json.dumps(json_body).encode("utf-8")
            headers.setdefault("Content-Type", "application/json")
        headers.setdefault("Accept", "application/json")

        policy = self.retry
        if policy is None or not policy.is_retriable_method(method_upper):
            return await self._send_once(method_upper, url, headers, body_bytes)

        last_err: Optional[BaseException] = None
        last_resp: Optional[HTTPResponse] = None
        attempts = policy.attempts()
        for attempt in range(attempts):
            try:
                resp = await self._send_once(method_upper, url, headers, body_bytes)
            except (httpx.TransportError, ConnectionError, TimeoutError) as e:
                if attempt == attempts - 1:
                    raise
                last_err = e
                last_resp = None
            else:
                if not policy.is_retriable_status(resp.status_code) or attempt == attempts - 1:
                    return resp
                last_resp = resp
                last_err = None
            delay = policy.backoff(attempt)
            if last_resp is not None:
                ra = parse_retry_after(
                    header_get_ci(last_resp.headers, "Retry-After"), time.time()
                )
                if ra is not None:
                    delay = min(policy.max_delay, ra)
            if policy.sleep is not None:
                policy.sleep(delay)
            else:
                await asyncio.sleep(delay)
        if last_resp is not None:
            return last_resp
        if last_err is not None:
            raise last_err
        raise RuntimeError("retry policy exhausted with no result")

    async def _send_once(
        self,
        method: str,
        url: str,
        headers: Dict[str, str],
        body_bytes: Optional[bytes],
    ) -> HTTPResponse:
        resp = await self._client.request(
            method, url, headers=headers, content=body_bytes
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
