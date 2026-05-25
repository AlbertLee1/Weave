"""Round-118 SDK BDD for WeaveVersionedLookupError — typed
exception that mirrors round-117 backend 501 VersionedLookupNotSupported.

When the SPA/SDK accidentally passes a versioned RID (e.g.
``ri.ontology.main.object-type.{uuid}@v3``) to a Get endpoint
that doesn't yet support snapshots, the backend returns 501 with
errorName=VersionedLookupNotSupported (round 117). The SDK
translates that into a typed exception so callers can catch it
specifically — e.g. retry without the @vN suffix, or surface a
'version pin not yet supported' UI banner.

Contract under test:
- Sync Client._handle dispatches 501+VersionedLookupNotSupported
  to WeaveVersionedLookupError (sibling of WeaveNotFoundError,
  WeaveAuthError)
- Async _handle dispatches the same way (parity)
- Other 501s (different errorName) fall back to plain WeaveError
  so future unrelated 501 surfaces aren't auto-typed
- The typed exception exposes .version convenience property
  extracted from parameters
"""
from __future__ import annotations

import json
import unittest

from weave_client import (
    Client,
    WeaveAsyncClient,
    WeaveError,
    WeaveVersionedLookupError,
)

from tests.test_client import _StubServer


_VERSIONED_BODY = json.dumps({
    "errorCode": "UNIMPLEMENTED",
    "errorName": "VersionedLookupNotSupported",
    "errorInstanceId": "abc",
    "parameters": {
        "ontologyApiName": "northwind",
        "objectTypeApiName": "ri.ontology.main.object-type.x%40v3",
        "version": "3",
        "reason": "snapshot lookups not yet implemented",
    },
})


class SyncVersionedLookupTests(unittest.TestCase):

    def test_501_versioned_raises_typed_exception(self):
        routes = {"GET /api/v2/ontologies/nw/objectTypes/ri.ontology.main.object-type.x%40v3":
                  (501, _VERSIONED_BODY)}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveVersionedLookupError) as ctx:
                # Caller passes literal @v3; wrapper url-quotes to %40v3
                # so the route key (with %40) matches the encoded request.
                c.ontologies.get_object_type(
                    "nw", "ri.ontology.main.object-type.x@v3")
        e = ctx.exception
        self.assertEqual(e.status_code, 501)
        self.assertEqual(e.error_name, "VersionedLookupNotSupported")
        self.assertEqual(e.error_code, "UNIMPLEMENTED")
        # WeaveVersionedLookupError must be a WeaveError subclass so
        # callers that catch the base type still see the error.
        self.assertIsInstance(e, WeaveError)

    def test_version_property_extracts_from_parameters(self):
        # Convenience accessor so callers don't need to dig through
        # the parameters dict for the most useful field.
        routes = {"GET /api/v2/ontologies/nw/objectTypes/x%40v7":
                  (501, json.dumps({
                      "errorCode": "UNIMPLEMENTED",
                      "errorName": "VersionedLookupNotSupported",
                      "errorInstanceId": "x",
                      "parameters": {"version": "7"},
                  }))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveVersionedLookupError) as ctx:
                c.ontologies.get_object_type("nw", "x@v7")
        self.assertEqual(ctx.exception.version, "7")

    def test_version_property_empty_when_parameters_missing_version(self):
        # Defensive default — if a future backend omits the version
        # field for some reason, the accessor returns "" rather than
        # raising KeyError on the caller path.
        routes = {"GET /api/v2/ontologies/nw/objectTypes/x%40v7":
                  (501, json.dumps({
                      "errorCode": "UNIMPLEMENTED",
                      "errorName": "VersionedLookupNotSupported",
                      "errorInstanceId": "x",
                      "parameters": {},
                  }))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveVersionedLookupError) as ctx:
                c.ontologies.get_object_type("nw", "x@v7")
        self.assertEqual(ctx.exception.version, "")

    def test_other_501_falls_back_to_plain_WeaveError(self):
        # Round-117 only narrowly typed the VersionedLookupNotSupported
        # variant. Any future unrelated 501 must still raise plain
        # WeaveError so the catch-all path stays valid.
        routes = {"GET /api/v2/ontologies/nw/objectTypes/x":
                  (501, json.dumps({
                      "errorCode": "UNIMPLEMENTED",
                      "errorName": "FeaturePending",
                      "errorInstanceId": "y",
                      "parameters": {},
                  }))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            with self.assertRaises(WeaveError) as ctx:
                c.ontologies.get_object_type("nw", "x")
        # Specifically NOT WeaveVersionedLookupError — different
        # errorName means caller can't treat it as the version case.
        self.assertNotIsInstance(ctx.exception, WeaveVersionedLookupError)
        self.assertEqual(ctx.exception.error_name, "FeaturePending")


class AsyncVersionedLookupTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_501_versioned_raises_typed_exception(self):
        # Parity guard — async transport must dispatch the SAME
        # typed exception so callers catch it identically across
        # sync and async code paths.
        routes = {"GET /api/v2/ontologies/nw/objectTypes/ri.ontology.main.object-type.x%40v3":
                  (501, _VERSIONED_BODY)}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                with self.assertRaises(WeaveVersionedLookupError) as ctx:
                    await c.ontologies.get_object_type(
                        "nw", "ri.ontology.main.object-type.x@v3")
        self.assertEqual(ctx.exception.version, "3")
        self.assertIsInstance(ctx.exception, WeaveError)


if __name__ == "__main__":
    unittest.main()
