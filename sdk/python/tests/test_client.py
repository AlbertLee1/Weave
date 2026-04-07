"""Tests for the top-level weave_client.Client object.

These tests use a tiny stdlib http.server stub instead of httpx-respx so the
suite can run on machines that haven't installed the optional `[test]` extras.
The Client class is implemented on top of httpx, so the tests still exercise
the real network path against a loopback server.
"""
from __future__ import annotations

import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict, List, Tuple

from weave_client import Client
from weave_client.exceptions import (
    WeaveAuthError,
    WeaveError,
    WeaveNotFoundError,
)


class _StubHandler(BaseHTTPRequestHandler):
    routes: Dict[str, Tuple[int, str]] = {}
    requests: List[Dict[str, Any]] = []

    def log_message(self, format, *args):  # silence test output
        return

    def _record(self, body: bytes) -> None:
        type(self).requests.append(
            {
                "method": self.command,
                "path": self.path,
                "auth": self.headers.get("Authorization", ""),
                "body": body.decode() if body else "",
            }
        )

    def _serve(self) -> None:
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(length) if length else b""
        self._record(body)
        key = f"{self.command} {self.path.split('?', 1)[0]}"
        if key in type(self).routes:
            status, payload = type(self).routes[key]
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload.encode())
            return
        self.send_response(404)
        self.send_header("Content-Type", "application/json")
        msg = '{"errorCode":"NOT_FOUND","errorName":"NoStub","errorInstanceId":"x","parameters":{}}'
        self.send_header("Content-Length", str(len(msg)))
        self.end_headers()
        self.wfile.write(msg.encode())

    do_GET = _serve
    do_POST = _serve
    do_PUT = _serve
    do_DELETE = _serve


class _StubServer:
    def __init__(self, routes: Dict[str, Tuple[int, str]]):
        _StubHandler.routes = routes
        _StubHandler.requests = []
        self.server = HTTPServer(("127.0.0.1", 0), _StubHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "_StubServer":
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
    def requests(self) -> List[Dict[str, Any]]:
        return _StubHandler.requests


class ClientTests(unittest.TestCase):
    def test_client_strips_trailing_slash_and_stores_token(self):
        c = Client("http://example.test/", access_token="abc")
        self.assertEqual(c.base_url, "http://example.test")
        self.assertEqual(c.token, "abc")

    def test_client_prefers_explicit_access_token(self):
        c = Client("http://x", access_token="acc", api_key="wvk_zzz")
        self.assertEqual(c.token, "acc")

    def test_client_falls_back_to_api_key(self):
        c = Client("http://x", api_key="wvk_secret")
        self.assertEqual(c.token, "wvk_secret")

    def test_client_sends_bearer_header(self):
        with _StubServer({"GET /api/v2/ontologies": (200, '{"data":[]}')}) as srv:
            c = Client(srv.url, access_token="t")
            c.ontologies.list()
            self.assertEqual(srv.requests[0]["auth"], "Bearer t")

    def test_client_skips_bearer_header_when_no_token(self):
        with _StubServer({"GET /api/v2/ontologies": (200, '{"data":[]}')}) as srv:
            c = Client(srv.url)
            c.ontologies.list()
            self.assertEqual(srv.requests[0]["auth"], "")

    def test_unauthorized_response_maps_to_auth_error(self):
        body = (
            '{"errorCode":"UNAUTHORIZED","errorName":"InvalidCredentials",'
            '"errorInstanceId":"x","parameters":{}}'
        )
        with _StubServer({"GET /api/v2/ontologies": (401, body)}) as srv:
            c = Client(srv.url, access_token="bad")
            with self.assertRaises(WeaveAuthError) as ctx:
                c.ontologies.list()
            self.assertEqual(ctx.exception.status_code, 401)
            self.assertEqual(ctx.exception.error_name, "InvalidCredentials")

    def test_not_found_response_maps_to_not_found_error(self):
        body = (
            '{"errorCode":"NOT_FOUND","errorName":"OntologyNotFound",'
            '"errorInstanceId":"x","parameters":{}}'
        )
        with _StubServer({"GET /api/v2/ontologies/missing": (404, body)}) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveNotFoundError):
                c.ontologies.get("missing")

    def test_server_error_maps_to_generic_weave_error(self):
        with _StubServer({"GET /api/v2/ontologies": (500, "boom")}) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError) as ctx:
                c.ontologies.list()
            self.assertEqual(ctx.exception.status_code, 500)


if __name__ == "__main__":
    unittest.main()
