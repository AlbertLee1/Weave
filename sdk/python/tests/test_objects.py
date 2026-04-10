"""Tests for the ObjectsAPI namespace."""
from __future__ import annotations

import unittest

from weave_client import Client

from tests.test_client import _StubServer


class ObjectsAPITests(unittest.TestCase):
    def test_list_returns_page_with_metadata(self):
        body = (
            '{"data":['
            '{"__rid":"ri.obj.1","__primaryKey":"ALFKI","__apiName":"Customer","customerId":"ALFKI","companyName":"Alfreds"},'
            '{"__rid":"ri.obj.2","__primaryKey":"ANATR","__apiName":"Customer","customerId":"ANATR","companyName":"Ana"}'
            '],"nextPageToken":"tok2","totalCount":"42"}'
        )
        with _StubServer({"GET /api/v2/ontologies/nw/objects/Customer": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            page = c.objects.list("nw", "Customer", page_size=25)
        self.assertEqual(len(page.data), 2)
        self.assertEqual(page.next_page_token, "tok2")
        self.assertEqual(page.total_count, "42")
        self.assertEqual(page.data[0]["customerId"], "ALFKI")

    def test_list_query_params_propagated(self):
        with _StubServer({"GET /api/v2/ontologies/nw/objects/Customer": (200, '{"data":[]}')}) as srv:
            c = Client(srv.url, access_token="t")
            c.objects.list("nw", "Customer", page_size=10, page_token="abc", order_by="customerId")
            req_path = srv.requests[0]["path"]
        self.assertIn("pageSize=10", req_path)
        self.assertIn("pageToken=abc", req_path)
        self.assertIn("orderBy=customerId", req_path)

    def test_get_returns_single_object(self):
        body = '{"__primaryKey":"ALFKI","customerId":"ALFKI","companyName":"Alfreds Futterkiste"}'
        with _StubServer({"GET /api/v2/ontologies/nw/objects/Customer/ALFKI": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            obj = c.objects.get("nw", "Customer", "ALFKI")
        self.assertEqual(obj["companyName"], "Alfreds Futterkiste")

    def test_search_posts_where_clause(self):
        body = '{"data":[{"__primaryKey":"ALFKI","customerId":"ALFKI"}],"totalCount":"1"}'
        with _StubServer({"POST /api/v2/ontologies/nw/objects/Customer/search": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            page = c.objects.search(
                "nw", "Customer",
                {"type": "eq", "field": "customerId", "value": "ALFKI"},
                select=["customerId", "companyName"],
            )
            req_body = srv.requests[0]["body"]
        self.assertEqual(len(page.data), 1)
        self.assertIn('"where"', req_body)
        self.assertIn('"ALFKI"', req_body)
        self.assertIn('"select"', req_body)
        self.assertIn('"customerId"', req_body)

    def test_iterate_pages_until_no_token(self):
        first = '{"data":[{"__primaryKey":"A"}],"nextPageToken":"p2","totalCount":"3"}'
        second = '{"data":[{"__primaryKey":"B"},{"__primaryKey":"C"}],"nextPageToken":"","totalCount":"3"}'
        # Use a counter to flip the response across calls.
        responses = [first, second]

        from http.server import BaseHTTPRequestHandler, HTTPServer
        import threading

        served: list[str] = []

        class _PageHandler(BaseHTTPRequestHandler):
            def log_message(self, format, *args):
                return

            def do_GET(self):
                served.append(self.path)
                payload = responses.pop(0)
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload.encode())

        srv = HTTPServer(("127.0.0.1", 0), _PageHandler)
        thread = threading.Thread(target=srv.serve_forever, daemon=True)
        thread.start()
        try:
            host, port = srv.server_address
            c = Client(f"http://{host}:{port}", access_token="t")
            primary_keys = []
            for obj in c.objects.iter_all("nw", "Customer", page_size=2):
                primary_keys.append(obj["__primaryKey"])
        finally:
            srv.shutdown()
            srv.server_close()

        self.assertEqual(primary_keys, ["A", "B", "C"])
        self.assertEqual(len(served), 2)
        self.assertIn("pageToken=p2", served[1])

    def test_get_url_escapes_primary_key(self):
        with _StubServer({"GET /api/v2/ontologies/nw/objects/Customer/ALF%20KI%2F1": (200, "{}")}) as srv:
            c = Client(srv.url, access_token="t")
            c.objects.get("nw", "Customer", "ALF KI/1")
            self.assertIn("ALF%20KI%2F1", srv.requests[0]["path"])


if __name__ == "__main__":
    unittest.main()
