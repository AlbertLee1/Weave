"""Internal HTTP transport.

Tries to use httpx if it's installed (the recommended dependency); otherwise
falls back to urllib so the SDK still works in minimal environments. The
fallback is intentionally feature-poor — no connection pooling, no
HTTP/2 — but it makes the test suite runnable without `pip install`.
"""
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, Iterator, Optional, Tuple

from ._retry import RetryPolicy, header_get_ci, parse_retry_after

try:  # pragma: no cover - exercised only when httpx is installed
    import httpx  # type: ignore

    _HAS_HTTPX = True
except Exception:  # pragma: no cover
    _HAS_HTTPX = False


class HTTPResponse:
    """Subset of an httpx Response sufficient for the SDK's needs."""

    __slots__ = ("status_code", "text", "headers")

    def __init__(self, status_code: int, text: str, headers: Optional[Dict[str, str]] = None):
        self.status_code = status_code
        self.text = text
        self.headers = headers or {}

    def json(self) -> Any:
        return json.loads(self.text) if self.text else None


class Transport:
    """Tiny request shim. Subclassed only to swap in test transports."""

    def __init__(self, timeout: float = 30.0, retry: Optional[RetryPolicy] = None):
        self.timeout = timeout
        self.retry = retry
        if _HAS_HTTPX:
            self._httpx_client = httpx.Client(timeout=timeout)
        else:
            self._httpx_client = None

    def close(self) -> None:
        if self._httpx_client is not None:
            self._httpx_client.close()

    def request(
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
            return self._send_once(method_upper, url, headers, body_bytes)

        last_err: Optional[BaseException] = None
        last_resp: Optional[HTTPResponse] = None
        attempts = policy.attempts()
        sleep_fn = policy.sleep or time.sleep
        for attempt in range(attempts):
            try:
                resp = self._send_once(method_upper, url, headers, body_bytes)
            except (
                urllib.error.URLError,
                ConnectionError,
                TimeoutError,
            ) as e:
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
            sleep_fn(delay)
        if last_resp is not None:
            return last_resp
        if last_err is not None:
            raise last_err
        raise RuntimeError("retry policy exhausted with no result")

    def _send_once(
        self,
        method: str,
        url: str,
        headers: Dict[str, str],
        body_bytes: Optional[bytes],
    ) -> HTTPResponse:
        if self._httpx_client is not None:  # pragma: no cover - httpx path
            resp = self._httpx_client.request(
                method, url, headers=headers, content=body_bytes
            )
            return HTTPResponse(resp.status_code, resp.text, dict(resp.headers))

        req = urllib.request.Request(url=url, data=body_bytes, method=method)
        for k, v in headers.items():
            req.add_header(k, v)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as r:
                body = r.read().decode("utf-8")
                return HTTPResponse(r.status, body, dict(r.getheaders()))
        except urllib.error.HTTPError as e:
            body = e.read().decode("utf-8") if e.fp is not None else ""
            return HTTPResponse(e.code, body, dict(e.headers or {}))

    def stream_lines(
        self,
        method: str,
        url: str,
        *,
        headers: Optional[Dict[str, str]] = None,
        json_body: Any = None,
    ) -> Tuple[int, Dict[str, str], Iterator[str]]:
        """Issue a request and return (status, headers, line_iterator).

        Used by NDJSON-style streaming endpoints (US-219). Callers consume
        the iterator lazily — each yielded value is one decoded UTF-8 line
        with the trailing newline stripped. The transport keeps the
        underlying socket / response alive until the iterator is exhausted
        or garbage-collected.

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

        if self._httpx_client is not None:  # pragma: no cover - httpx path
            req = self._httpx_client.build_request(
                method.upper(), url, headers=headers, content=body_bytes
            )
            resp = self._httpx_client.send(req, stream=True)
            status = resp.status_code
            resp_headers = dict(resp.headers)

            def _httpx_iter() -> Iterator[str]:
                try:
                    for line in resp.iter_lines():
                        if line:
                            yield line if isinstance(line, str) else line.decode("utf-8")
                finally:
                    resp.close()

            return status, resp_headers, _httpx_iter()

        req = urllib.request.Request(url=url, data=body_bytes, method=method.upper())
        for k, v in headers.items():
            req.add_header(k, v)
        try:
            r = urllib.request.urlopen(req, timeout=self.timeout)
            status = r.status
            resp_headers = dict(r.getheaders())

            def _urllib_iter() -> Iterator[str]:
                try:
                    for raw in r:
                        line = raw.decode("utf-8").rstrip("\r\n")
                        if line:
                            yield line
                finally:
                    r.close()

            return status, resp_headers, _urllib_iter()
        except urllib.error.HTTPError as e:
            body = e.read().decode("utf-8") if e.fp is not None else ""
            return e.code, dict(e.headers or {}), iter([body] if body else [])


def build_query_string(params: Dict[str, Any]) -> str:
    """Render a query string, dropping None and empty values."""
    cleaned: Dict[str, Any] = {}
    for k, v in params.items():
        if v is None:
            continue
        if isinstance(v, str) and v == "":
            continue
        cleaned[k] = v
    if not cleaned:
        return ""
    return "?" + urllib.parse.urlencode(cleaned)


def quote_path(s: str) -> str:
    """Percent-escape a path segment.

    ``urllib.parse.quote`` defaults to ``safe='/'`` which would let path
    separators through; we want them encoded so a primary key like
    ``"ALF KI/1"`` becomes ``"ALF%20KI%2F1"``.
    """
    return urllib.parse.quote(s, safe="")


__all__ = ["Transport", "HTTPResponse", "build_query_string", "quote_path"]
