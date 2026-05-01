"""Tests for WeaveAsyncClient (US-355).

Mirrors the synchronous client/objects/actions/objectsets/functions test
suite using ``unittest.IsolatedAsyncioTestCase`` so the suite runs under
the stdlib runner without pytest. The same loopback ``_StubServer`` /
``_NDJSONServer`` from the sync test files back the assertions — this
exercises the real ``httpx.AsyncClient`` socket path against an actual
HTTP server, not a mocked transport.
"""
from __future__ import annotations

import json
import unittest

from weave_client import WeaveAsyncClient
from weave_client.exceptions import (
    WeaveAuthError,
    WeaveError,
    WeaveNotFoundError,
)

from tests.test_client import _StubServer
from tests.test_functions import _NDJSONServer


class AsyncClientCoreTests(unittest.IsolatedAsyncioTestCase):
    def test_async_client_strips_trailing_slash_and_stores_token(self):
        c = WeaveAsyncClient("http://example.test/", access_token="abc")
        self.assertEqual(c.base_url, "http://example.test")
        self.assertEqual(c.token, "abc")

    def test_async_client_prefers_explicit_access_token(self):
        c = WeaveAsyncClient("http://x", access_token="acc", api_key="wvk_zzz")
        self.assertEqual(c.token, "acc")

    def test_async_client_falls_back_to_api_key(self):
        c = WeaveAsyncClient("http://x", api_key="wvk_secret")
        self.assertEqual(c.token, "wvk_secret")

    async def test_async_client_sends_bearer_header(self):
        with _StubServer({"GET /api/v2/ontologies": (200, '{"data":[]}')}) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.ontologies.list()
            self.assertEqual(srv.requests[0]["auth"], "Bearer t")

    async def test_async_client_skips_bearer_header_when_no_token(self):
        with _StubServer({"GET /api/v2/ontologies": (200, '{"data":[]}')}) as srv:
            async with WeaveAsyncClient(srv.url) as c:
                await c.ontologies.list()
            self.assertEqual(srv.requests[0]["auth"], "")

    async def test_unauthorized_response_maps_to_auth_error(self):
        body = (
            '{"errorCode":"UNAUTHORIZED","errorName":"InvalidCredentials",'
            '"errorInstanceId":"x","parameters":{}}'
        )
        with _StubServer({"GET /api/v2/ontologies": (401, body)}) as srv:
            async with WeaveAsyncClient(srv.url, access_token="bad") as c:
                with self.assertRaises(WeaveAuthError) as ctx:
                    await c.ontologies.list()
        self.assertEqual(ctx.exception.status_code, 401)
        self.assertEqual(ctx.exception.error_name, "InvalidCredentials")

    async def test_not_found_response_maps_to_not_found_error(self):
        body = (
            '{"errorCode":"NOT_FOUND","errorName":"OntologyNotFound",'
            '"errorInstanceId":"x","parameters":{}}'
        )
        with _StubServer({"GET /api/v2/ontologies/missing": (404, body)}) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                with self.assertRaises(WeaveNotFoundError):
                    await c.ontologies.get("missing")

    async def test_server_error_maps_to_generic_weave_error(self):
        with _StubServer({"GET /api/v2/ontologies": (500, "boom")}) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                with self.assertRaises(WeaveError) as ctx:
                    await c.ontologies.list()
        self.assertEqual(ctx.exception.status_code, 500)

    async def test_login_attaches_access_token(self):
        body = json.dumps({
            "access_token": "new-token",
            "refresh_token": "refresh",
            "token_type": "Bearer",
            "expires_in": 3600,
            "user": {"id": "user:a@b.c", "email": "a@b.c"},
        })
        with _StubServer({"POST /api/auth/login": (200, body)}) as srv:
            async with WeaveAsyncClient(srv.url) as c:
                resp = await c.login("a@b.c", "pw")
                self.assertEqual(resp.access_token, "new-token")
                self.assertEqual(c.access_token, "new-token")
                self.assertEqual(c.token, "new-token")

    async def test_logout_clears_access_token(self):
        with _StubServer({"POST /api/auth/logout": (200, "")}) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.logout()
                self.assertIsNone(c.access_token)


class AsyncObjectsTests(unittest.IsolatedAsyncioTestCase):
    async def test_objects_list_decodes_page(self):
        body = json.dumps({
            "data": [{"__primaryKey": "ALFKI", "companyName": "Alf"}],
            "nextPageToken": "next",
        })
        routes = {"GET /api/v2/ontologies/nw/objects/Customer": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                page = await c.objects.list("nw", "Customer", page_size=25)
                self.assertEqual(page.next_page_token, "next")
                self.assertEqual(page.data[0]["__primaryKey"], "ALFKI")
                self.assertIn("pageSize=25", srv.requests[0]["path"])

    async def test_objects_iter_all_walks_next_page_token(self):
        page1 = json.dumps({
            "data": [{"__primaryKey": "A"}, {"__primaryKey": "B"}],
            "nextPageToken": "p2",
        })
        page2 = json.dumps({
            "data": [{"__primaryKey": "C"}],
            "nextPageToken": "",
        })

        # Single-route stub re-orders by call: rely on the path's pageToken
        # to disambiguate which fixture to return.
        from http.server import BaseHTTPRequestHandler, HTTPServer
        import threading

        class _Handler(BaseHTTPRequestHandler):
            def log_message(self, format, *args):
                return

            def do_GET(self):
                payload = page2 if "pageToken=p2" in self.path else page1
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload.encode())

        server = HTTPServer(("127.0.0.1", 0), _Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            host, port = server.server_address
            url = f"http://{host}:{port}"
            collected = []
            async with WeaveAsyncClient(url, access_token="t") as c:
                async for row in c.objects.iter_all("nw", "Customer", page_size=2):
                    collected.append(row["__primaryKey"])
            self.assertEqual(collected, ["A", "B", "C"])
        finally:
            server.shutdown()
            server.server_close()

    async def test_objects_search_posts_where_clause(self):
        body = json.dumps({"data": [{"__primaryKey": "A"}], "nextPageToken": ""})
        routes = {"POST /api/v2/ontologies/nw/objects/Customer/search": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                page = await c.objects.search(
                    "nw",
                    "Customer",
                    {"type": "eq", "field": "country", "value": "DE"},
                    select=["companyName"],
                )
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(page.data[0]["__primaryKey"], "A")
        self.assertEqual(sent["where"]["value"], "DE")
        self.assertEqual(sent["select"], ["companyName"])

    async def test_objects_count_returns_int(self):
        body = json.dumps({"count": 17})
        routes = {"POST /api/v2/ontologies/nw/objects/Customer/count": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                self.assertEqual(await c.objects.count("nw", "Customer"), 17)


class AsyncActionsTests(unittest.IsolatedAsyncioTestCase):
    async def test_apply_returns_response_envelope(self):
        body = json.dumps({
            "operationId": "op-1",
            "edits": {
                "type": "edits",
                "addedObjectCount": 1,
                "modifiedObjectCount": 0,
                "deletedObjectCount": 0,
            },
        })
        routes = {"POST /api/v2/ontologies/nw/actions/createCustomer/apply": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.actions.apply(
                    "nw", "createCustomer", {"id": "WEAVE", "name": "Co"}
                )
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(resp.operation_id, "op-1")
        self.assertEqual(sent["parameters"]["id"], "WEAVE")


class AsyncObjectSetsTests(unittest.IsolatedAsyncioTestCase):
    async def test_load_objects_posts_object_set(self):
        body = json.dumps({
            "data": [{"__primaryKey": "1"}],
            "nextPageToken": "",
        })
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadObjects": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                page = await c.objectsets.load_objects(
                    "nw",
                    {"type": "base", "objectType": "Customer"},
                    ["companyName"],
                )
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(page.data[0]["__primaryKey"], "1")
        self.assertEqual(sent["objectSet"]["objectType"], "Customer")
        self.assertEqual(sent["select"], ["companyName"])

    async def test_aggregate_includes_group_by(self):
        body = json.dumps({"buckets": [{"key": "DE", "value": 5}]})
        routes = {"POST /api/v2/ontologies/nw/objectSets/aggregate": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                resp = await c.objectsets.aggregate(
                    "nw",
                    {"type": "base", "objectType": "Customer"},
                    [{"name": "n", "metric": "count"}],
                    group_by=[{"field": "country"}],
                )
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(resp["buckets"][0]["key"], "DE")
        self.assertEqual(sent["groupBy"][0]["field"], "country")


class AsyncFunctionsExecuteJSONTests(unittest.IsolatedAsyncioTestCase):
    async def test_execute_returns_result_payload(self):
        body = json.dumps({"functionRid": "ri.x.x.f.add", "result": 7})
        routes = {"POST /api/v2/ontologies/nw/functions/add/execute": (200, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                result = await c.functions.execute("nw", "add", {"a": 3, "b": 4})
                sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(result, 7)
        self.assertEqual(sent["parameters"], {"a": 3, "b": 4})

    async def test_execute_validation_error_raises(self):
        body = (
            '{"errorCode":"INVALID_ARGUMENT","errorName":"InvalidParameter:a",'
            '"errorInstanceId":"x","parameters":{"parameter":"a","code":"missing_required"}}'
        )
        routes = {"POST /api/v2/ontologies/nw/functions/add/execute": (400, body)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                with self.assertRaises(WeaveError) as ctx:
                    await c.functions.execute("nw", "add", {})
        self.assertEqual(ctx.exception.error_name, "InvalidParameter:a")
        self.assertEqual(ctx.exception.parameters["code"], "missing_required")


class AsyncFunctionsExecuteStreamTests(unittest.IsolatedAsyncioTestCase):
    async def test_stream_yields_each_item_in_order(self):
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
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                it = await c.functions.execute_stream("nw", "topProducts", {"limit": 100})
                collected = [row async for row in it]
                sent_path = srv.requests[0]["path"]
        self.assertEqual([row["id"] for row in collected], ["a", "b", "c"])
        self.assertIn("stream=1", sent_path)

    async def test_stream_terminal_error_raises_weave_error(self):
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
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                it = await c.functions.execute_stream("nw", "slow", {})
                first = await it.__anext__()
                self.assertEqual(first, {"id": "first"})
                with self.assertRaises(WeaveError) as ctx:
                    await it.__anext__()
        self.assertEqual(ctx.exception.error_name, "FunctionExecutionTimeout")
        self.assertEqual(ctx.exception.parameters["reason"], "ran too long")

    async def test_stream_pre_execution_error_raises_before_first_yield(self):
        # 4xx response must surface BEFORE the async-for begins iterating.
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
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                with self.assertRaises(WeaveError) as ctx:
                    await c.functions.execute_stream("nw", "add", {})
        self.assertEqual(ctx.exception.status_code, 400)
        self.assertEqual(ctx.exception.error_name, "InvalidParameter:a")

    async def test_stream_empty_result_yields_nothing(self):
        routes = {
            "POST /api/v2/ontologies/nw/functions/empty/execute": (
                200,
                "application/x-ndjson",
                [],
            ),
        }
        with _NDJSONServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                it = await c.functions.execute_stream("nw", "empty", {})
                collected = [row async for row in it]
        self.assertEqual(collected, [])


if __name__ == "__main__":
    unittest.main()
