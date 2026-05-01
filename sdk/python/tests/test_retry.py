"""Tests for the SDK's retry policy (US-358).

Covers both the sync :class:`weave_client.Client` (built on
``urllib.request`` in this loopback path) and the async
:class:`weave_client.WeaveAsyncClient` (built on ``httpx.AsyncClient``).
"""
from __future__ import annotations

import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict, List, Tuple

from weave_client import Client, RetryPolicy, WeaveAsyncClient, WeaveError
from weave_client._retry import parse_retry_after


class _FlakyHandler(BaseHTTPRequestHandler):
    # Sequence of (status, body, extra_headers) tuples consumed in order. The
    # tail tuple is repeated for any extra requests so tests stay deterministic
    # even if the SDK over-retries.
    plan: List[Tuple[int, str, Dict[str, str]]] = []
    request_count = 0
    methods: List[str] = []

    def log_message(self, format, *args):
        return

    def _serve(self) -> None:
        cls = type(self)
        idx = min(cls.request_count, len(cls.plan) - 1)
        cls.request_count += 1
        cls.methods.append(self.command)
        status, body, extra = cls.plan[idx]
        body_bytes = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body_bytes)))
        for k, v in extra.items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body_bytes)

    do_GET = _serve
    do_POST = _serve
    do_PUT = _serve
    do_DELETE = _serve


class _FlakyServer:
    def __init__(self, plan: List[Tuple[int, str, Dict[str, str]]]):
        _FlakyHandler.plan = list(plan)
        _FlakyHandler.request_count = 0
        _FlakyHandler.methods = []
        self.server = HTTPServer(("127.0.0.1", 0), _FlakyHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "_FlakyServer":
        self.thread.start()
        return self

    def __exit__(self, *exc):
        self.server.shutdown()
        self.server.server_close()

    @property
    def url(self) -> str:
        host, port = self.server.server_address
        return f"http://{host}:{port}"

    @property
    def request_count(self) -> int:
        return _FlakyHandler.request_count

    @property
    def methods(self) -> List[str]:
        return _FlakyHandler.methods


class _NoSleepCapture:
    """Captures sleep calls without actually delaying the test."""

    def __init__(self) -> None:
        self.delays: List[float] = []

    def __call__(self, seconds: float) -> None:
        self.delays.append(seconds)


class RetryPolicyUnitTests(unittest.TestCase):
    def test_default_policy_attempts_is_three(self):
        self.assertEqual(RetryPolicy().attempts(), 3)

    def test_max_attempts_clamped_to_one(self):
        self.assertEqual(RetryPolicy(max_attempts=0).attempts(), 1)
        self.assertEqual(RetryPolicy(max_attempts=-5).attempts(), 1)

    def test_idempotent_methods_default_set(self):
        p = RetryPolicy()
        for m in ("GET", "head", "OPTIONS", "PUT", "DELETE"):
            self.assertTrue(p.is_retriable_method(m), m)
        for m in ("POST", "PATCH"):
            self.assertFalse(p.is_retriable_method(m), m)

    def test_retriable_status_default_set(self):
        p = RetryPolicy()
        for s in (408, 425, 429, 500, 502, 503, 504):
            self.assertTrue(p.is_retriable_status(s), s)
        for s in (200, 400, 401, 403, 404):
            self.assertFalse(p.is_retriable_status(s), s)

    def test_backoff_full_jitter_within_cap(self):
        p = RetryPolicy(base_delay=0.5, max_delay=4.0, multiplier=2.0)
        # Repeat to ensure no draw escapes the cap; fixed seed avoids flakes.
        import random

        p.rand = random.Random(0)
        for attempt in range(0, 5):
            cap = min(p.max_delay, p.base_delay * (p.multiplier ** attempt))
            for _ in range(50):
                d = p.backoff(attempt)
                self.assertGreaterEqual(d, 0.0)
                self.assertLessEqual(d, cap + 1e-9)


class RetryAfterParseTests(unittest.TestCase):
    def test_delta_seconds(self):
        self.assertEqual(parse_retry_after("5", 0), 5.0)
        self.assertEqual(parse_retry_after(" 0.5 ", 0), 0.5)

    def test_negative_delta_returns_none(self):
        self.assertIsNone(parse_retry_after("-1", 0))

    def test_http_date(self):
        # 5 seconds in the future per the supplied "now".
        import datetime

        target = datetime.datetime(2026, 5, 1, 0, 0, 5, tzinfo=datetime.timezone.utc)
        now = target.timestamp() - 5.0
        d = parse_retry_after("Fri, 01 May 2026 00:00:05 GMT", now)
        self.assertIsNotNone(d)
        self.assertAlmostEqual(d, 5.0, places=3)

    def test_http_date_in_past_returns_zero(self):
        import datetime

        target = datetime.datetime(2026, 5, 1, 0, 0, 0, tzinfo=datetime.timezone.utc)
        now = target.timestamp() + 5.0
        d = parse_retry_after("Fri, 01 May 2026 00:00:00 GMT", now)
        self.assertEqual(d, 0.0)

    def test_garbage_returns_none(self):
        self.assertIsNone(parse_retry_after("not a date", 0))

    def test_empty_returns_none(self):
        self.assertIsNone(parse_retry_after("", 0))
        self.assertIsNone(parse_retry_after(None, 0))


class SyncClientRetryTests(unittest.TestCase):
    def test_retries_503_then_succeeds_on_get(self):
        capture = _NoSleepCapture()
        plan = [
            (503, '{"errorCode":"BUSY"}', {}),
            (503, '{"errorCode":"BUSY"}', {}),
            (200, '{"data":[]}', {}),
        ]
        with _FlakyServer(plan) as srv:
            c = Client(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            c.ontologies.list()
        self.assertEqual(srv.request_count, 3)
        # Two backoff sleeps, no third (success on the last attempt).
        self.assertEqual(len(capture.delays), 2)

    def test_does_not_retry_post_even_on_503(self):
        capture = _NoSleepCapture()
        plan = [
            (503, '{"errorCode":"BUSY"}', {}),
            (200, '{"data":[]}', {}),
        ]
        with _FlakyServer(plan) as srv:
            c = Client(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            with self.assertRaises(WeaveError) as ctx:
                # ObjectsAPI.search uses POST.
                c.objects.search(
                    "ont", "Customer", {"type": "eq", "field": "x", "value": 1}, select=["x"]
                )
        self.assertEqual(ctx.exception.status_code, 503)
        self.assertEqual(srv.request_count, 1)
        self.assertEqual(len(capture.delays), 0)

    def test_does_not_retry_4xx_other_than_429_408_425(self):
        capture = _NoSleepCapture()
        plan = [
            (404, '{"errorCode":"NOT_FOUND","errorName":"Missing"}', {}),
        ]
        with _FlakyServer(plan) as srv:
            c = Client(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            with self.assertRaises(WeaveError):
                c.ontologies.get("nope")
        self.assertEqual(srv.request_count, 1)
        self.assertEqual(len(capture.delays), 0)

    def test_exhausting_retries_raises_last_response_error(self):
        capture = _NoSleepCapture()
        plan = [
            (502, '{"errorCode":"GATEWAY"}', {}),
            (502, '{"errorCode":"GATEWAY"}', {}),
            (502, '{"errorCode":"GATEWAY"}', {}),
        ]
        with _FlakyServer(plan) as srv:
            c = Client(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            with self.assertRaises(WeaveError) as ctx:
                c.ontologies.list()
        self.assertEqual(ctx.exception.status_code, 502)
        self.assertEqual(srv.request_count, 3)
        self.assertEqual(len(capture.delays), 2)

    def test_max_attempts_one_disables_retries(self):
        capture = _NoSleepCapture()
        plan = [(503, '{"errorCode":"BUSY"}', {})]
        with _FlakyServer(plan) as srv:
            c = Client(
                srv.url,
                retry=RetryPolicy(max_attempts=1, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            with self.assertRaises(WeaveError):
                c.ontologies.list()
        self.assertEqual(srv.request_count, 1)
        self.assertEqual(len(capture.delays), 0)

    def test_retry_after_header_is_honoured(self):
        capture = _NoSleepCapture()
        plan = [
            (429, '{"errorCode":"TOO_MANY_REQUESTS"}', {"Retry-After": "1"}),
            (200, '{"data":[]}', {}),
        ]
        with _FlakyServer(plan) as srv:
            c = Client(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=10.0, max_delay=10.0, sleep=capture),
            )
            c.ontologies.list()
        # Retry-After=1 wins over the 10s computed backoff.
        self.assertEqual(len(capture.delays), 1)
        self.assertAlmostEqual(capture.delays[0], 1.0, places=3)


class AsyncClientRetryTests(unittest.IsolatedAsyncioTestCase):
    async def test_async_retries_503_then_succeeds_on_get(self):
        capture = _NoSleepCapture()
        plan = [
            (503, '{"errorCode":"BUSY"}', {}),
            (200, '{"data":[]}', {}),
        ]
        with _FlakyServer(plan) as srv:
            c = WeaveAsyncClient(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            try:
                await c.ontologies.list()
            finally:
                await c.aclose()
        self.assertEqual(srv.request_count, 2)
        self.assertEqual(len(capture.delays), 1)

    async def test_async_does_not_retry_post(self):
        capture = _NoSleepCapture()
        plan = [(503, '{"errorCode":"BUSY"}', {})]
        with _FlakyServer(plan) as srv:
            c = WeaveAsyncClient(
                srv.url,
                retry=RetryPolicy(max_attempts=3, base_delay=0.0, max_delay=0.0, sleep=capture),
            )
            try:
                with self.assertRaises(WeaveError):
                    await c.actions.apply("ont", "noop", {})
            finally:
                await c.aclose()
        self.assertEqual(srv.request_count, 1)
        self.assertEqual(len(capture.delays), 0)


if __name__ == "__main__":
    unittest.main()
