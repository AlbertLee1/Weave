"""Tests for the ObjectSetsAPI namespace."""
from __future__ import annotations

import json
import unittest

from weave_client import Client

from tests.test_client import _StubServer


class ObjectSetsLoadObjectsTests(unittest.TestCase):
    def test_load_objects_posts_body(self):
        resp = json.dumps({
            "data": [{"__primaryKey": "ALFKI", "customerId": "ALFKI"}],
            "nextPageToken": "tok2",
            "totalCount": "1",
        })
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadObjects": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            page = c.objectsets.load_objects(
                "nw",
                {"type": "base", "objectType": "Customer"},
                ["customerId", "companyName"],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(len(page.data), 1)
        self.assertEqual(page.next_page_token, "tok2")
        self.assertEqual(sent["objectSet"]["type"], "base")
        self.assertEqual(sent["select"], ["customerId", "companyName"])

    def test_load_objects_with_pagination(self):
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadObjects": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objectsets.load_objects(
                "nw",
                {"type": "base", "objectType": "Customer"},
                ["customerId"],
                page_size=50,
                page_token="abc",
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["pageSize"], 50)
        self.assertEqual(sent["pageToken"], "abc")

    def test_load_objects_omits_pagination_when_none(self):
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadObjects": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objectsets.load_objects(
                "nw",
                {"type": "base", "objectType": "Customer"},
                ["customerId"],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertNotIn("pageSize", sent)
        self.assertNotIn("pageToken", sent)


class ObjectSetsLoadLinksTests(unittest.TestCase):
    def test_load_links_posts_body(self):
        resp = json.dumps({
            "data": [{"__primaryKey": "10248", "orderId": "10248"}],
        })
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadLinks": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            page = c.objectsets.load_links(
                "nw",
                {"type": "base", "objectType": "Customer"},
                "orders",
                ["orderId"],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(len(page.data), 1)
        self.assertEqual(sent["objectSet"]["type"], "base")
        self.assertEqual(sent["linkType"], "orders")
        self.assertEqual(sent["select"], ["orderId"])

    def test_load_links_with_pagination(self):
        routes = {"POST /api/v2/ontologies/nw/objectSets/loadLinks": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objectsets.load_links(
                "nw",
                {"type": "base", "objectType": "Customer"},
                "orders",
                ["orderId"],
                page_size=10,
                page_token="p2",
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["pageSize"], 10)
        self.assertEqual(sent["pageToken"], "p2")


class ObjectSetsAggregateTests(unittest.TestCase):
    def test_aggregate_posts_body(self):
        resp = json.dumps({
            "data": [{"group": {}, "metrics": [{"value": 42}]}],
        })
        routes = {"POST /api/v2/ontologies/nw/objectSets/aggregate": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.objectsets.aggregate(
                "nw",
                {"type": "base", "objectType": "Customer"},
                [{"type": "count"}],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["objectSet"]["type"], "base")
        self.assertEqual(sent["aggregation"], [{"type": "count"}])
        self.assertNotIn("groupBy", sent)
        self.assertEqual(result["data"][0]["metrics"][0]["value"], 42)

    def test_aggregate_with_group_by(self):
        routes = {"POST /api/v2/ontologies/nw/objectSets/aggregate": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.objectsets.aggregate(
                "nw",
                {"type": "base", "objectType": "Customer"},
                [{"type": "count"}],
                group_by=[{"field": "country", "type": "exact"}],
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["groupBy"], [{"field": "country", "type": "exact"}])


class ObjectSetsCreateTemporaryTests(unittest.TestCase):
    def test_create_temporary(self):
        resp = json.dumps({"objectSetRid": "ri.os.tmp.123", "objectSet": {"type": "base"}})
        routes = {"POST /api/v2/ontologies/nw/objectSets/createTemporary": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.objectsets.create_temporary(
                "nw",
                {"type": "base", "objectType": "Customer"},
            )
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent["objectSet"]["type"], "base")
        self.assertEqual(result["objectSetRid"], "ri.os.tmp.123")


class ObjectSetsGetTests(unittest.TestCase):
    def test_get_objectset_by_rid(self):
        resp = json.dumps({"objectSetRid": "ri.os.tmp.123", "objectSet": {"type": "base", "objectType": "Customer"}})
        routes = {"GET /api/v2/ontologies/nw/objectSets/ri.os.tmp.123": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.objectsets.get("nw", "ri.os.tmp.123")
        self.assertEqual(result["objectSetRid"], "ri.os.tmp.123")
        self.assertEqual(result["objectSet"]["objectType"], "Customer")


if __name__ == "__main__":
    unittest.main()
