"""Tests for the FunctionsAPI namespace (US-216 / US-219).

Covers both the regular ``execute()`` JSON path and the ``execute_stream()``
NDJSON iterator. The streaming path uses a separate stub server that emits
``Content-Type: application/x-ndjson`` line-by-line so the SDK iterator
contract is exercised end-to-end against a real socket.
"""
from __future__ import annotations

import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict, List, Tuple

from weave_client import Client
from weave_client.exceptions import WeaveError, WeaveNotFoundError

from tests.test_client import _StubServer


class FunctionsExecuteJSONTests(unittest.TestCase):
    """The non-streaming ``execute()`` returns the deserialized result."""

    def test_execute_returns_result_payload(self):
        body = json.dumps({
            "functionRid": "ri.ontology.main.function.add",
            "result": 7,
        })
        routes = {"POST /api/v2/ontologies/nw/functions/add/execute": (200, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.functions.execute("nw", "add", {"a": 3, "b": 4})
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(result, 7)
        self.assertEqual(sent["parameters"], {"a": 3, "b": 4})

    def test_execute_accepts_versioned_ref(self):
        body = json.dumps({"result": "ok"})
        routes = {
            "POST /api/v2/ontologies/nw/functions/topProducts%401.2.3/execute": (200, body),
        }
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            self.assertEqual(c.functions.execute("nw", "topProducts@1.2.3", {}), "ok")

    def test_execute_validation_error_raises(self):
        body = (
            '{"errorCode":"INVALID_ARGUMENT","errorName":"InvalidParameter:a",'
            '"errorInstanceId":"x","parameters":{"parameter":"a","code":"missing_required",'
            '"reason":"a is required"}}'
        )
        routes = {"POST /api/v2/ontologies/nw/functions/add/execute": (400, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError) as ctx:
                c.functions.execute("nw", "add", {})
        self.assertEqual(ctx.exception.error_name, "InvalidParameter:a")
        self.assertEqual(ctx.exception.parameters["code"], "missing_required")

    def test_execute_not_found_raises(self):
        body = (
            '{"errorCode":"NOT_FOUND","errorName":"FunctionNotFound",'
            '"errorInstanceId":"x","parameters":{}}'
        )
        routes = {"POST /api/v2/ontologies/nw/functions/missing/execute": (404, body)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveNotFoundError):
                c.functions.execute("nw", "missing", {})


# --- streaming stub server ----------------------------------------------------


class _NDJSONHandler(BaseHTTPRequestHandler):
    """HTTP stub that emits NDJSON when the path matches a streaming route."""

    routes: Dict[str, Tuple[int, str, List[str]]] = {}
    requests: List[Dict[str, Any]] = []

    def log_message(self, format, *args):  # silence test output
        return

    def _record(self, body: bytes) -> None:
        type(self).requests.append(
            {
                "method": self.command,
                "path": self.path,
                "body": body.decode() if body else "",
            }
        )

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(length) if length else b""
        self._record(body)
        key = f"{self.command} {self.path.split('?', 1)[0]}"
        if key in type(self).routes:
            status, content_type, lines = type(self).routes[key]
            payload = "".join(line + "\n" for line in lines).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        msg = b'{"errorCode":"NOT_FOUND","errorName":"NoStub","errorInstanceId":"x","parameters":{}}'
        self.send_response(404)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(msg)))
        self.end_headers()
        self.wfile.write(msg)


class _NDJSONServer:
    def __init__(self, routes: Dict[str, Tuple[int, str, List[str]]]):
        _NDJSONHandler.routes = routes
        _NDJSONHandler.requests = []
        self.server = HTTPServer(("127.0.0.1", 0), _NDJSONHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "_NDJSONServer":
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
        return _NDJSONHandler.requests


class FunctionsExecuteStreamTests(unittest.TestCase):
    def test_stream_yields_each_item_in_order(self):
        lines = [
            json.dumps({"item": {"id": "a"}}),
            json.dumps({"item": {"id": "b"}}),
            json.dumps({"item": {"id": "c"}}),
        ]
        routes = {
            "POST /api/v2/ontologies/nw/functions/topProducts/execute": (
                200,
                "application/x-ndjson",
                lines,
            ),
        }
        with _NDJSONServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            collected = list(c.functions.execute_stream("nw", "topProducts", {"limit": 100}))
            sent_path = srv.requests[0]["path"]
        self.assertEqual([row["id"] for row in collected], ["a", "b", "c"])
        self.assertIn("stream=1", sent_path)

    def test_stream_terminal_error_raises_weave_error(self):
        lines = [
            json.dumps({"item": {"id": "first"}}),
            json.dumps({"error": {"code": "FunctionExecutionTimeout", "reason": "ran too long"}}),
        ]
        routes = {
            "POST /api/v2/ontologies/nw/functions/slow/execute": (
                200,
                "application/x-ndjson",
                lines,
            ),
        }
        with _NDJSONServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            it = c.functions.execute_stream("nw", "slow", {})
            first = next(it)
            self.assertEqual(first, {"id": "first"})
            with self.assertRaises(WeaveError) as ctx:
                next(it)
        self.assertEqual(ctx.exception.error_name, "FunctionExecutionTimeout")
        self.assertEqual(ctx.exception.parameters["reason"], "ran too long")

    def test_stream_empty_result_yields_nothing(self):
        routes = {
            "POST /api/v2/ontologies/nw/functions/empty/execute": (
                200,
                "application/x-ndjson",
                [],
            ),
        }
        with _NDJSONServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            self.assertEqual(list(c.functions.execute_stream("nw", "empty", {})), [])

    def test_stream_pre_execution_error_raises_before_first_yield(self):
        # Validation 400 must surface as WeaveError raised by the
        # generator-returning call (not from inside the loop).
        body = (
            '{"errorCode":"INVALID_ARGUMENT","errorName":"InvalidParameter:a",'
            '"errorInstanceId":"x","parameters":{"parameter":"a","code":"missing_required"}}'
        )
        routes = {
            "POST /api/v2/ontologies/nw/functions/add/execute": (
                400,
                "application/json",
                [body],
            ),
        }
        with _NDJSONServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError) as ctx:
                c.functions.execute_stream("nw", "add", {})
        self.assertEqual(ctx.exception.status_code, 400)
        self.assertEqual(ctx.exception.error_name, "InvalidParameter:a")

    def test_stream_scalar_item_is_yielded_as_value(self):
        lines = [json.dumps({"item": 42})]
        routes = {
            "POST /api/v2/ontologies/nw/functions/scalar/execute": (
                200,
                "application/x-ndjson",
                lines,
            ),
        }
        with _NDJSONServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            self.assertEqual(list(c.functions.execute_stream("nw", "scalar", {})), [42])


if __name__ == "__main__":
    unittest.main()
