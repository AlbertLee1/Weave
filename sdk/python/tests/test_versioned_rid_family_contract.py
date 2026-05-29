"""Round-120 SDK contract test mirroring round-119 backend.

Asserts every SDK get_*_type method raises WeaveVersionedLookupError
(round 118) when the backend returns 501 VersionedLookupNotSupported.
The round-119 backend extended the @vN guard to 8 Get endpoints; the
SDK covers the 5 of those that have wrapper methods today
(get_object_type / get_action_type / get_interface_type /
get_value_type / get_query_type). The 3 backend endpoints without
SDK wrappers (link/shared-property/type-group singletons) are
listed in the gap section at the bottom for future rounds.

Table-driven so adding a new wrapper method is one row + one route
in the stub — same evolution path as the round-119 backend table.

Sibling of round-112 check-family SDK contract test; together they
form the second "SDK locks the recipe" pair (after rounds 93+94
locked batch-by-RID and rounds 111+112 locked check-family).
"""
from __future__ import annotations

import inspect
import json
import unittest

from weave_client import (
    Client,
    WeaveAsyncClient,
    WeaveVersionedLookupError,
)
from weave_client._http import quote_path

from tests.test_client import _StubServer


def _versioned_body(field_name: str, identifier: str) -> str:
    """Canned 501 envelope matching the round-117/119 backend shape."""
    return json.dumps({
        "errorCode": "UNIMPLEMENTED",
        "errorName": "VersionedLookupNotSupported",
        "errorInstanceId": "x",
        "parameters": {
            "reason": "RID @vN version suffix is recognised but snapshot lookups are not yet implemented",
            "ontologyApiName": "nw",
            field_name: identifier,
            "version": "3",
        },
    })


# (sync_method_name, async_method_name, url_segment, identifier_field)
# url_segment is the chi path segment between /ontologies/{ont}/ and
# /{identifier}, matching cmd/server route registrations.
# identifier_field is the backend's params key — proves the round-119
# rejectVersionedRID(..., identifierField) wiring passes through.
_GET_METHODS = [
    ("get_object_type", "objectTypes", "objectTypeApiName"),
    ("get_action_type", "actionTypes", "actionTypeRid"),
    ("get_interface_type", "interfaceTypes", "interfaceType"),
    ("get_value_type", "valueTypes", "valueType"),
    ("get_query_type", "queryTypes", "queryApiName"),
    # Round 122: gap-fill wrappers, now matching round-119 backend 8-of-8
    ("get_link_type", "linkTypes", "linkType"),
    ("get_shared_property_type", "sharedPropertyTypes", "sharedPropertyType"),
    ("get_type_group", "typeGroups", "typeGroup"),
]


def _make_routes(url_segment: str, identifier: str, field_name: str) -> dict:
    quoted = quote_path(identifier)
    return {
        f"GET /api/v2/ontologies/nw/{url_segment}/{quoted}":
            (501, _versioned_body(field_name, identifier)),
    }


class SyncVersionedRIDFamilyTests(unittest.TestCase):

    def test_all_sync_get_methods_raise_typed_exception_on_versioned_rid(self):
        # Mirror of round-119 backend table test on the SDK side.
        # Each row proves the round-118 dispatch (501 + VersionedLookup
        # NotSupported -> WeaveVersionedLookupError) fires for the
        # corresponding wrapper.
        for method, url_segment, field_name in _GET_METHODS:
            with self.subTest(method=method):
                rid = f"ri.ontology.main.{url_segment[:-1]}.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3"
                routes = _make_routes(url_segment, rid, field_name)
                with _StubServer(routes) as srv:
                    c = Client(srv.url, access_token="t")
                    fn = getattr(c.ontologies, method)
                    with self.assertRaises(WeaveVersionedLookupError) as ctx:
                        fn("nw", rid)
                self.assertEqual(ctx.exception.version, "3",
                                 f"{method} version property drift")
                self.assertEqual(ctx.exception.error_name,
                                 "VersionedLookupNotSupported",
                                 f"{method} error_name drift")
                # Verify the identifier field is echoed in parameters —
                # proves the round-119 rejectVersionedRID(identifierField)
                # wiring round-trips through the typed exception.
                self.assertEqual(ctx.exception.parameters.get(field_name), rid,
                                 f"{method} parameters[{field_name}] drift")

    def test_each_sync_get_method_actually_exists(self):
        # Belt-and-braces presence guard — if a wrapper is renamed or
        # removed, the table test would skip silently rather than fail.
        # This explicit check makes such breakage loud.
        from weave_client.ontologies import OntologiesAPI
        for method, _, _ in _GET_METHODS:
            self.assertTrue(callable(getattr(OntologiesAPI, method, None)),
                            f"OntologiesAPI.{method} missing — backend "
                            "round-119 covered 8 endpoints but SDK has "
                            "a gap here that needs filling")


class AsyncVersionedRIDFamilyTests(unittest.IsolatedAsyncioTestCase):

    async def test_all_async_get_methods_raise_typed_exception_on_versioned_rid(self):
        # Parity guard — async transport must dispatch the SAME typed
        # exception so caller-side version-pin handling is one
        # try/except block regardless of sync/async choice.
        for method, url_segment, field_name in _GET_METHODS:
            with self.subTest(method=method):
                rid = f"ri.ontology.main.{url_segment[:-1]}.7c9e6679-7425-40de-944b-e07fc1f90ae7@v3"
                routes = _make_routes(url_segment, rid, field_name)
                with _StubServer(routes) as srv:
                    async with WeaveAsyncClient(srv.url, access_token="t") as c:
                        fn = getattr(c.ontologies, method)
                        with self.assertRaises(WeaveVersionedLookupError) as ctx:
                            await fn("nw", rid)
                self.assertEqual(ctx.exception.version, "3",
                                 f"async {method} version property drift")
                self.assertEqual(ctx.exception.error_name,
                                 "VersionedLookupNotSupported",
                                 f"async {method} error_name drift")

    def test_each_async_get_method_is_coroutine(self):
        from weave_client.async_client import AsyncOntologiesAPI
        for method, _, _ in _GET_METHODS:
            attr = getattr(AsyncOntologiesAPI, method, None)
            self.assertTrue(callable(attr),
                            f"AsyncOntologiesAPI.{method} missing")
            self.assertTrue(inspect.iscoroutinefunction(attr),
                            f"AsyncOntologiesAPI.{method} must be `async def`")


if __name__ == "__main__":
    unittest.main()
