"""Round-126 SDK BDD for c.build_info_dependencies — sync + async
mirror of round-125 backend GET /api/v2/build-info/dependencies.

Contract under test:
- ``c.build_info_dependencies() -> List[Dependency]``
- ``await c.build_info_dependencies() -> List[Dependency]``
- Anonymous request (no Authorization header)
- Each Dependency carries path/version + optional sum/replace
- Empty server response yields [] not None — defensive contract
  matches round-125 backend's empty-not-null guarantee
"""
from __future__ import annotations

import json
import unittest

from weave_client import Client, Dependency, WeaveAsyncClient

from tests.test_client import _StubServer


_PAYLOAD = {
    "dependencies": [
        {"path": "github.com/go-chi/chi/v5", "version": "v5.0.10",
         "sum": "h1:chiabc"},
        {"path": "github.com/jackc/pgx/v5", "version": "v5.4.3",
         "sum": "h1:pgxabc",
         "replace": "github.com/jackc/pgx/v5"},
        {"path": "github.com/liyang/weave", "version": "(devel)"},
    ]
}


class SyncDependenciesTests(unittest.TestCase):

    def test_returns_typed_dependency_list_in_order(self):
        routes = {"GET /api/v2/build-info/dependencies":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            deps = c.build_info_dependencies()
        self.assertEqual(len(deps), 3)
        self.assertIsInstance(deps[0], Dependency)
        self.assertEqual(deps[0].path, "github.com/go-chi/chi/v5")
        self.assertEqual(deps[0].version, "v5.0.10")
        self.assertEqual(deps[0].sum, "h1:chiabc")
        # Replace empty by default — only second entry has one.
        self.assertEqual(deps[0].replace, "")
        self.assertEqual(deps[1].replace, "github.com/jackc/pgx/v5")
        # (devel) version is a valid string the backend may emit
        # for local-path replaces; the SDK passes it through.
        self.assertEqual(deps[2].version, "(devel)")

    def test_sends_no_auth_header(self):
        # Mirror of r124 — endpoint is public; SDK must NOT attach
        # Authorization even when the client has credentials.
        routes = {"GET /api/v2/build-info/dependencies":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            c.build_info_dependencies()
        req = srv.requests[0]
        self.assertEqual(req["auth"], "",
                         "Authorization header should be absent for public endpoint")
        self.assertEqual(req["method"], "GET")
        self.assertEqual(req["body"], "")

    def test_handles_empty_dependencies_array(self):
        # Round-125 backend defensive contract: returns [] not null
        # when runtime/debug.ReadBuildInfo() is empty. SDK must
        # surface that as an empty list, not raise.
        routes = {"GET /api/v2/build-info/dependencies":
                  (200, '{"dependencies":[]}')}
        with _StubServer(routes) as srv:
            c = Client(srv.url, access_token="t")
            deps = c.build_info_dependencies()
        self.assertEqual(deps, [])
        # Iteration safety — empty list iterates without error.
        for _ in deps:
            self.fail("empty deps should not iterate")

    def test_works_with_no_credentials(self):
        # No-token client still reaches the public endpoint.
        routes = {"GET /api/v2/build-info/dependencies":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            c = Client(srv.url)  # no access_token, no api_key
            deps = c.build_info_dependencies()
        self.assertEqual(len(deps), 3)


class AsyncDependenciesTests(unittest.IsolatedAsyncioTestCase):

    async def test_async_returns_typed_dependency_list(self):
        routes = {"GET /api/v2/build-info/dependencies":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                deps = await c.build_info_dependencies()
        self.assertEqual(len(deps), 3)
        self.assertIsInstance(deps[0], Dependency)
        self.assertEqual(deps[1].replace, "github.com/jackc/pgx/v5")

    async def test_async_sends_no_auth_header(self):
        routes = {"GET /api/v2/build-info/dependencies":
                  (200, json.dumps(_PAYLOAD))}
        with _StubServer(routes) as srv:
            async with WeaveAsyncClient(srv.url, access_token="t") as c:
                await c.build_info_dependencies()
        self.assertEqual(srv.requests[0]["auth"], "")


if __name__ == "__main__":
    unittest.main()
