"""Round-122 SDK BDD for 3 missing get_*_type wrappers.

Closes the gap noted in round-120's _GET_METHODS docstring: backend
r119 covers 8 Get endpoints with @vN guards, SDK had wrappers for
only 5. This round adds get_link_type, get_shared_property_type,
get_type_group on both sync and async OntologiesAPI so the round-120
contract table grows from 5 to 8 rows matching the backend.

Each wrapper:
- GET /api/v2/ontologies/{ont}/{segment}/{name}
- Returns the typed model (LinkType existed; SharedPropertyType +
  TypeGroup added in this round to types.py)
- Url-quotes both path segments
- Raises WeaveVersionedLookupError on @vN input via the existing
  round-118 _handle dispatch (no per-wrapper code needed)
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    LinkType,
    SharedPropertyType,
    TypeGroup,
    WeaveAsyncClient,
)

from tests.test_client import _StubServer


_LINK_PAYLOAD = {
    "rid": "ri.lt.1",
    "apiName": "ownedBy",
    "displayName": "Owned By",
    "objectTypeApiName": "Customer",
    "linkedObjectTypeApiName": "Employee",
    "cardinality": "MANY_TO_ONE",
    "required": False,
}

_SPT_PAYLOAD = {
    "rid": "ri.spt.1",
    "apiName": "lastTouchedAt",
    "displayName": "Last Touched At",
    "description": "Timestamp set on every save",
    "baseType": "timestamp",
    "isArray": False,
}

_TG_PAYLOAD = {
    "rid": "ri.tg.1",
    "apiName": "people",
    "displayName": "People",
    "description": "Person-shaped object types",
    "color": "#3b82f6",
}


class SyncGet3Tests(unittest.TestCase):

    def test_get_link_type_returns_typed_model(self):
        routes = {"GET /api/v2/ontologies/nw/linkTypes/ownedBy":
                  (200, json.dumps(_LINK_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            lt = c.ontologies.get_link_type("nw", "ownedBy")
        self.assertIsInstance(lt, LinkType)
        self.assertEqual(lt.rid, "ri.lt.1")
        self.assertEqual(lt.api_name, "ownedBy")
        self.assertEqual(lt.cardinality, "MANY_TO_ONE")

    def test_get_shared_property_type_returns_typed_model(self):
        routes = {"GET /api/v2/ontologies/nw/sharedPropertyTypes/lastTouchedAt":
                  (200, json.dumps(_SPT_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            spt = c.ontologies.get_shared_property_type("nw", "lastTouchedAt")
        self.assertIsInstance(spt, SharedPropertyType)
        self.assertEqual(spt.rid, "ri.spt.1")
        self.assertEqual(spt.api_name, "lastTouchedAt")
        self.assertEqual(spt.base_type, "timestamp")
        self.assertFalse(spt.is_array)

    def test_get_type_group_returns_typed_model(self):
        routes = {"GET /api/v2/ontologies/nw/typeGroups/people":
                  (200, json.dumps(_TG_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            tg = c.ontologies.get_type_group("nw", "people")
        self.assertIsInstance(tg, TypeGroup)
        self.assertEqual(tg.rid, "ri.tg.1")
        self.assertEqual(tg.api_name, "people")
        self.assertEqual(tg.color, "#3b82f6")

    def test_all_3_url_quote_both_path_segments(self):
        # Defense-in-depth — neither ontology nor identifier should
        # be left unencoded. A slash in either would otherwise
        # produce wrong-host or wrong-path requests.
        cases = [
            ("get_link_type", "linkTypes", _LINK_PAYLOAD),
            ("get_shared_property_type", "sharedPropertyTypes", _SPT_PAYLOAD),
            ("get_type_group", "typeGroups", _TG_PAYLOAD),
        ]
        for method, segment, payload in cases:
            with self.subTest(method=method):
                routes = {
                    f"GET /api/v2/ontologies/nw%2Fchild/{segment}/x%2Fy":
                        (200, json.dumps(payload)),
                }
                with _StubServer(routes) as srv:
                    c = Client(srv.url, access_token="t")
                    fn = getattr(c.ontologies, method)
                    fn("nw/child", "x/y")
                self.assertIn(f"/{segment}/x%2Fy", srv.requests[0]["path"],
                              f"{method} url-quoting drift")


class AsyncGet3Tests(unittest.IsolatedAsyncioTestCase):

    async def test_async_get_link_type(self):
        routes = {"GET /api/v2/ontologies/nw/linkTypes/ownedBy":
                  (200, json.dumps(_LINK_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                lt = await c.ontologies.get_link_type("nw", "ownedBy")
        self.assertIsInstance(lt, LinkType)
        self.assertEqual(lt.api_name, "ownedBy")

    async def test_async_get_shared_property_type(self):
        routes = {"GET /api/v2/ontologies/nw/sharedPropertyTypes/lastTouchedAt":
                  (200, json.dumps(_SPT_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                spt = await c.ontologies.get_shared_property_type(
                    "nw", "lastTouchedAt")
        self.assertIsInstance(spt, SharedPropertyType)
        self.assertEqual(spt.base_type, "timestamp")

    async def test_async_get_type_group(self):
        routes = {"GET /api/v2/ontologies/nw/typeGroups/people":
                  (200, json.dumps(_TG_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                tg = await c.ontologies.get_type_group("nw", "people")
        self.assertIsInstance(tg, TypeGroup)
        self.assertEqual(tg.color, "#3b82f6")


if __name__ == "__main__":
    unittest.main()
