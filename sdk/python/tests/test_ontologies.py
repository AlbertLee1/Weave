"""Tests for the OntologiesAPI namespace."""
from __future__ import annotations

import unittest

from weave_client import Client

from tests.test_client import _StubServer


class OntologiesAPITests(unittest.TestCase):
    def test_list_returns_models(self):
        body = (
            '{"data":['
            '{"rid":"ri.ontology.main.ontology.northwind","apiName":"northwind","displayName":"Northwind","currentVersion":3},'
            '{"rid":"ri.ontology.main.ontology.chinook","apiName":"chinook","displayName":"Chinook","currentVersion":1}'
            ']}'
        )
        with _StubServer({"GET /api/v2/ontologies": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            ontologies = c.ontologies.list()
        self.assertEqual(len(ontologies), 2)
        self.assertEqual(ontologies[0].api_name, "northwind")
        self.assertEqual(ontologies[0].display_name, "Northwind")
        self.assertEqual(ontologies[0].current_version, 3)

    def test_get_returns_single_model(self):
        body = (
            '{"rid":"ri.ontology.main.ontology.northwind",'
            '"apiName":"northwind","displayName":"Northwind","currentVersion":7}'
        )
        with _StubServer({"GET /api/v2/ontologies/northwind": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            o = c.ontologies.get("northwind")
        self.assertEqual(o.api_name, "northwind")
        self.assertEqual(o.current_version, 7)

    def test_list_object_types_returns_models(self):
        body = (
            '{"data":['
            '{"rid":"ri.ontology.main.objecttype.customer","apiName":"Customer","displayName":"Customer","primaryKey":"customerId","status":"ACTIVE","visibility":"NORMAL"},'
            '{"rid":"ri.ontology.main.objecttype.order","apiName":"Order","displayName":"Order","primaryKey":"orderId","status":"ACTIVE","visibility":"NORMAL"}'
            ']}'
        )
        with _StubServer({"GET /api/v2/ontologies/northwind/objectTypes": (200, body)}) as srv:
            c = Client(srv.url, access_token="t")
            types = c.ontologies.list_object_types("northwind")
        self.assertEqual([t.api_name for t in types], ["Customer", "Order"])
        self.assertEqual(types[0].primary_key, "customerId")

    def test_list_object_types_handles_empty_data(self):
        with _StubServer({"GET /api/v2/ontologies/empty/objectTypes": (200, '{"data":[]}')}) as srv:
            c = Client(srv.url, access_token="t")
            types = c.ontologies.list_object_types("empty")
        self.assertEqual(types, [])

    def test_path_escapes_special_characters(self):
        with _StubServer({"GET /api/v2/ontologies/has%20space": (200, '{"rid":"r","apiName":"has space","displayName":"X","currentVersion":1}')}) as srv:
            c = Client(srv.url, access_token="t")
            o = c.ontologies.get("has space")
        self.assertEqual(o.api_name, "has space")


if __name__ == "__main__":
    unittest.main()
