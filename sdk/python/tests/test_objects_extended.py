"""Tests for extended ObjectsAPI methods: count, list_linked_objects, get_linked_object."""
from __future__ import annotations

import json
import unittest

from weave_client import Client

from tests.test_client import _StubServer


class ObjectsCountTests(unittest.TestCase):
    def test_count_returns_integer(self):
        resp = json.dumps({"count": 42})
        routes = {"POST /api/v2/ontologies/nw/objects/Customer/count": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.objects.count("nw", "Customer")
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(result, 42)
        self.assertEqual(sent, {})
        self.assertEqual(srv.requests[0]["method"], "POST")

    def test_count_defaults_to_zero_when_missing(self):
        routes = {"POST /api/v2/ontologies/nw/objects/Customer/count": (200, "{}")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.objects.count("nw", "Customer")
        self.assertEqual(result, 0)


class LinkedObjectsTests(unittest.TestCase):
    def test_list_linked_objects_returns_page(self):
        resp = json.dumps({
            "data": [
                {"__primaryKey": "10248", "orderId": "10248", "customerId": "ALFKI"},
                {"__primaryKey": "10249", "orderId": "10249", "customerId": "ALFKI"},
            ],
            "nextPageToken": "tok2",
            "totalCount": "5",
        })
        routes = {"GET /api/v2/ontologies/nw/objects/Customer/ALFKI/links/orders": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            page = c.objects.list_linked_objects("nw", "Customer", "ALFKI", "orders")
        self.assertEqual(len(page.data), 2)
        self.assertEqual(page.next_page_token, "tok2")
        self.assertEqual(page.data[0]["orderId"], "10248")

    def test_list_linked_objects_query_params(self):
        routes = {"GET /api/v2/ontologies/nw/objects/Customer/ALFKI/links/orders": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objects.list_linked_objects("nw", "Customer", "ALFKI", "orders", page_size=25, page_token="abc")
            req_path = srv.requests[0]["path"]
        self.assertIn("pageSize=25", req_path)
        self.assertIn("pageToken=abc", req_path)

    def test_list_linked_objects_url_encodes_pk(self):
        routes = {"GET /api/v2/ontologies/nw/objects/Customer/ALF%20KI/links/orders": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objects.list_linked_objects("nw", "Customer", "ALF KI", "orders")
            self.assertIn("ALF%20KI", srv.requests[0]["path"])

    def test_get_linked_object(self):
        resp = json.dumps({"__primaryKey": "10248", "orderId": "10248", "freight": 32.38})
        routes = {"GET /api/v2/ontologies/nw/objects/Customer/ALFKI/links/orders/10248": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            obj = c.objects.get_linked_object("nw", "Customer", "ALFKI", "orders", "10248")
        self.assertEqual(obj["orderId"], "10248")
        self.assertEqual(obj["freight"], 32.38)

    def test_get_linked_object_returns_empty_dict_on_null(self):
        routes = {"GET /api/v2/ontologies/nw/objects/Customer/ALFKI/links/orders/99999": (200, "")}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            obj = c.objects.get_linked_object("nw", "Customer", "ALFKI", "orders", "99999")
        self.assertEqual(obj, {})


if __name__ == "__main__":
    unittest.main()
