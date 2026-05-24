"""Tests for the extended OntologiesAPI methods (Phase 1 endpoints).

Covers: load_metadata, get_full_metadata, get_object_type_full_metadata,
get_object_types_by_rid_batch, list/get action types, list/get interface types,
list/get value types, list/get query types.
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client

from tests.test_client import _StubServer


class OntologiesMetadataTests(unittest.TestCase):
    """Tests for ontology metadata endpoints."""

    def test_load_metadata_posts_subsets(self):
        resp = json.dumps({"objectTypes": [{"apiName": "Customer"}], "linkTypes": []})
        routes = {"POST /api/v2/ontologies/nw/metadata": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.load_metadata("nw", {"objectTypes": True, "linkTypes": True})
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"objectTypes": True, "linkTypes": True})
        self.assertEqual(result["objectTypes"][0]["apiName"], "Customer")

    def test_get_full_metadata(self):
        resp = json.dumps({"ontology": {"apiName": "nw"}, "objectTypes": []})
        routes = {"GET /api/v2/ontologies/nw/fullMetadata": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_full_metadata("nw")
        self.assertEqual(result["ontology"]["apiName"], "nw")

    def test_get_object_type_full_metadata(self):
        resp = json.dumps({"objectType": {"apiName": "Customer"}, "properties": {}})
        routes = {"GET /api/v2/ontologies/nw/objectTypes/Customer/fullMetadata": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_object_type_full_metadata("nw", "Customer")
        self.assertEqual(result["objectType"]["apiName"], "Customer")

    def test_get_object_types_by_rid_batch(self):
        resp = json.dumps({"data": [
            {"rid": "ri.ot.1", "apiName": "Customer", "displayName": "Customer",
             "primaryKey": "id", "status": "ACTIVE", "visibility": "NORMAL"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/objectTypes/getByRidBatch": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_object_types_by_rid_batch("nw", ["ri.ot.1"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["ri.ot.1"]})
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["apiName"], "Customer")


class ActionTypesTests(unittest.TestCase):
    """Tests for action type endpoints on OntologiesAPI."""

    _ACTION_JSON = {
        "rid": "ri.at.1",
        "apiName": "createCustomer",
        "displayName": "Create Customer",
        "description": "Creates a customer",
        "status": "ACTIVE",
        "parameters": {},
    }

    def test_list_action_types(self):
        resp = json.dumps({"data": [self._ACTION_JSON]})
        routes = {"GET /api/v2/ontologies/nw/actionTypes": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_action_types("nw")
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].api_name, "createCustomer")
        self.assertEqual(result[0].display_name, "Create Customer")

    def test_get_action_type(self):
        resp = json.dumps(self._ACTION_JSON)
        routes = {"GET /api/v2/ontologies/nw/actionTypes/createCustomer": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_action_type("nw", "createCustomer")
        self.assertEqual(result.api_name, "createCustomer")
        self.assertEqual(result.rid, "ri.at.1")

    def test_get_action_type_by_rid(self):
        resp = json.dumps(self._ACTION_JSON)
        routes = {"GET /api/v2/ontologies/nw/actionTypes/byRid/ri.at.1": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_action_type_by_rid("nw", "ri.at.1")
        self.assertEqual(result.api_name, "createCustomer")

    def test_get_action_types_by_rid_batch(self):
        resp = json.dumps({"data": [self._ACTION_JSON]})
        routes = {"POST /api/v2/ontologies/nw/actionTypes/getByRidBatch": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_action_types_by_rid_batch("nw", ["ri.at.1"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["ri.at.1"]})
        self.assertEqual(len(result), 1)

    def test_get_action_type_full_metadata(self):
        resp = json.dumps({"actionType": self._ACTION_JSON, "rules": []})
        routes = {"GET /api/v2/ontologies/nw/actionTypes/createCustomer/fullMetadata": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_action_type_full_metadata("nw", "createCustomer")
        self.assertIn("actionType", result)
        self.assertEqual(result["actionType"]["apiName"], "createCustomer")

    def test_list_action_types_full_metadata(self):
        resp = json.dumps({"data": [{"actionType": self._ACTION_JSON, "rules": []}]})
        routes = {"GET /api/v2/ontologies/nw/actionTypesFullMetadata": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_action_types_full_metadata("nw")
        self.assertEqual(len(result), 1)


class InterfaceTypesTests(unittest.TestCase):
    """Tests for interface type endpoints."""

    _IFACE_JSON = {
        "rid": "ri.iface.1",
        "apiName": "GeoLocatable",
        "displayName": "Geo Locatable",
        "extendsRid": None,
        "sharedProperties": {"latitude": {"baseType": "DOUBLE"}},
    }

    def test_list_interface_types(self):
        resp = json.dumps({"data": [self._IFACE_JSON]})
        routes = {"GET /api/v2/ontologies/nw/interfaceTypes": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_interface_types("nw")
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].api_name, "GeoLocatable")
        self.assertEqual(result[0].display_name, "Geo Locatable")
        self.assertIsNone(result[0].extends_rid)
        self.assertIn("latitude", result[0].shared_properties)

    def test_get_interface_type(self):
        resp = json.dumps(self._IFACE_JSON)
        routes = {"GET /api/v2/ontologies/nw/interfaceTypes/GeoLocatable": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_interface_type("nw", "GeoLocatable")
        self.assertEqual(result.api_name, "GeoLocatable")
        self.assertEqual(result.rid, "ri.iface.1")


class ValueTypesTests(unittest.TestCase):
    """Tests for value type endpoints."""

    _VT_JSON = {
        "rid": "ri.vt.1",
        "apiName": "Currency",
        "displayName": "Currency",
        "baseType": "DOUBLE",
        "constraints": {"min": 0},
        "version": 2,
    }

    def test_list_value_types(self):
        resp = json.dumps({"data": [self._VT_JSON]})
        routes = {"GET /api/v2/ontologies/nw/valueTypes": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_value_types("nw")
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].api_name, "Currency")
        self.assertEqual(result[0].base_type, "DOUBLE")
        self.assertEqual(result[0].version, 2)

    def test_get_value_type(self):
        resp = json.dumps(self._VT_JSON)
        routes = {"GET /api/v2/ontologies/nw/valueTypes/Currency": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_value_type("nw", "Currency")
        self.assertEqual(result.api_name, "Currency")
        self.assertEqual(result.constraints, {"min": 0})


class QueryTypesTests(unittest.TestCase):
    """Tests for query type endpoints."""

    _QT_JSON = {
        "rid": "ri.qt.1",
        "apiName": "topCustomers",
        "displayName": "Top Customers",
        "description": "Returns top N customers",
        "parameters": {"limit": {"baseType": "INTEGER"}},
        "output": {"type": "objectSet"},
        "status": "ACTIVE",
    }

    def test_list_query_types(self):
        resp = json.dumps({"data": [self._QT_JSON]})
        routes = {"GET /api/v2/ontologies/nw/queryTypes": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_query_types("nw")
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0].api_name, "topCustomers")
        self.assertEqual(result[0].description, "Returns top N customers")
        self.assertEqual(result[0].status, "ACTIVE")

    def test_get_query_type(self):
        resp = json.dumps(self._QT_JSON)
        routes = {"GET /api/v2/ontologies/nw/queryTypes/topCustomers": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_query_type("nw", "topCustomers")
        self.assertEqual(result.api_name, "topCustomers")
        self.assertIn("limit", result.parameters)

    def test_list_action_types_handles_empty(self):
        routes = {"GET /api/v2/ontologies/nw/actionTypes": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_action_types("nw")
        self.assertEqual(result, [])

    def test_list_interface_types_handles_empty(self):
        routes = {"GET /api/v2/ontologies/nw/interfaceTypes": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_interface_types("nw")
        self.assertEqual(result, [])

    def test_list_value_types_handles_empty(self):
        routes = {"GET /api/v2/ontologies/nw/valueTypes": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_value_types("nw")
        self.assertEqual(result, [])

    def test_list_query_types_handles_empty(self):
        routes = {"GET /api/v2/ontologies/nw/queryTypes": (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.list_query_types("nw")
        self.assertEqual(result, [])


class BatchByRidRound84Tests(unittest.TestCase):
    """Round 84 — Python SDK helpers for the 3 backend batch endpoints
    added in rounds 79 (linkTypes), 81 (interfaceTypes), 83 (valueTypes).

    Matches the existing get_object_types_by_rid_batch /
    get_action_types_by_rid_batch surface: POST {rids: [...]},
    returns List[Dict[str, Any]] from response.data, preserves the
    server's missing-RIDs-silently-skipped semantics.
    """

    def test_get_link_types_by_rid_batch_posts_rids(self):
        resp = json.dumps({"data": [
            {"rid": "lt-1", "apiName": "owns", "sourceObjectType": "ri.ot.Cust",
             "targetObjectType": "ri.ot.Ord", "cardinality": "ONE_TO_MANY"},
            {"rid": "lt-2", "apiName": "billedTo", "sourceObjectType": "ri.ot.Ord",
             "targetObjectType": "ri.ot.Cust", "cardinality": "MANY_TO_ONE"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/linkTypes/getByRidBatch": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_link_types_by_rid_batch("nw", ["lt-1", "lt-2"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["lt-1", "lt-2"]})
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0]["apiName"], "owns")
        self.assertEqual(result[1]["apiName"], "billedTo")

    def test_get_link_types_by_rid_batch_partial_resolve(self):
        # Server contract: unresolved RIDs drop silently. The wrapper
        # surfaces what came back, no synthesised None placeholders.
        resp = json.dumps({"data": [{"rid": "lt-1", "apiName": "owns"}]})
        routes = {"POST /api/v2/ontologies/nw/linkTypes/getByRidBatch": (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_link_types_by_rid_batch(
                "nw", ["lt-1", "ghost-99"])
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["rid"], "lt-1")

    def test_get_link_types_by_rid_batch_empty_data(self):
        routes = {"POST /api/v2/ontologies/nw/linkTypes/getByRidBatch":
                  (200, '{"data":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_link_types_by_rid_batch("nw", [])
        self.assertEqual(result, [])

    def test_get_interface_types_by_rid_batch_posts_rids(self):
        resp = json.dumps({"data": [
            {"rid": "if-1", "apiName": "HasOwner", "displayName": "Has Owner"},
            {"rid": "if-2", "apiName": "Searchable", "displayName": "Searchable"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/interfaceTypes/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_interface_types_by_rid_batch(
                "nw", ["if-1", "if-2"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["if-1", "if-2"]})
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0]["apiName"], "HasOwner")

    def test_get_interface_types_by_rid_batch_partial_resolve(self):
        resp = json.dumps({"data": [{"rid": "if-1", "apiName": "HasOwner"}]})
        routes = {"POST /api/v2/ontologies/nw/interfaceTypes/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_interface_types_by_rid_batch(
                "nw", ["if-1", "ghost-99"])
        self.assertEqual(len(result), 1)

    def test_get_value_types_by_rid_batch_posts_rids(self):
        resp = json.dumps({"data": [
            {"rid": "vt-1", "apiName": "Currency", "baseType": "DOUBLE",
             "displayName": "Currency"},
            {"rid": "vt-2", "apiName": "EmailAddress", "baseType": "STRING",
             "displayName": "Email"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/valueTypes/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_value_types_by_rid_batch(
                "nw", ["vt-1", "vt-2"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["vt-1", "vt-2"]})
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0]["apiName"], "Currency")
        self.assertEqual(result[1]["baseType"], "STRING")

    def test_get_value_types_by_rid_batch_partial_resolve(self):
        resp = json.dumps({"data": [
            {"rid": "vt-1", "apiName": "Currency", "baseType": "DOUBLE"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/valueTypes/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_value_types_by_rid_batch(
                "nw", ["vt-1", "ghost-99"])
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["rid"], "vt-1")

    # ---- round 86 — sharedPropertyTypes batch wrapper -----------------

    def test_get_shared_property_types_by_rid_batch_posts_rids(self):
        # Round 86 mirrors round-85 backend on the sync SDK surface,
        # closing the 6-of-6 batch helper symmetry on the sync side.
        resp = json.dumps({"data": [
            {"rid": "sp-1", "apiName": "email", "baseType": "string",
             "displayName": "Email"},
            {"rid": "sp-2", "apiName": "phone", "baseType": "string",
             "displayName": "Phone"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/sharedPropertyTypes/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_shared_property_types_by_rid_batch(
                "nw", ["sp-1", "sp-2"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["sp-1", "sp-2"]})
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0]["apiName"], "email")
        self.assertEqual(result[1]["baseType"], "string")

    def test_get_shared_property_types_by_rid_batch_partial_resolve(self):
        resp = json.dumps({"data": [
            {"rid": "sp-1", "apiName": "email", "baseType": "string"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/sharedPropertyTypes/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_shared_property_types_by_rid_batch(
                "nw", ["sp-1", "ghost-99"])
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["rid"], "sp-1")

    # ---- round 88 — typeGroups batch wrapper --------------------------

    def test_get_type_groups_by_rid_batch_posts_rids(self):
        # Round 88 mirrors round-87 backend on the sync SDK surface,
        # 7-of-7 sync helpers in lockstep with 7-of-7 backend.
        resp = json.dumps({"data": [
            {"rid": "tg-1", "apiName": "people", "displayName": "People",
             "color": "#3b82f6"},
            {"rid": "tg-2", "apiName": "places", "displayName": "Places",
             "color": "#10b981"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/typeGroups/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_type_groups_by_rid_batch(
                "nw", ["tg-1", "tg-2"])
            sent = json.loads(srv.requests[0]["body"])
        self.assertEqual(sent, {"rids": ["tg-1", "tg-2"]})
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0]["apiName"], "people")
        self.assertEqual(result[1]["color"], "#10b981")

    def test_get_type_groups_by_rid_batch_partial_resolve(self):
        resp = json.dumps({"data": [
            {"rid": "tg-1", "apiName": "people", "displayName": "People"},
        ]})
        routes = {"POST /api/v2/ontologies/nw/typeGroups/getByRidBatch":
                  (200, resp)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            result = c.ontologies.get_type_groups_by_rid_batch(
                "nw", ["tg-1", "ghost-99"])
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["rid"], "tg-1")


if __name__ == "__main__":
    unittest.main()
