"""BDD acceptance tests for the round-92 SDK RID helper.

Round 92 mirrors round-91 backend (pkg/rid Go parser @vN suffix
support) on the Python SDK side. Foundry SDKs ship ergonomic RID
helpers so users can construct version-pinned RIDs without hand-
rolling string concatenation; this module provides the same shape:
parse_rid(s) -> Rid dataclass, and format_rid(...) -> str.

Wire-format contract is byte-identical with round 91:

    ri.{service}.{realm}.{resourceType}.{uuid}        -> Version=""
    ri.{service}.{realm}.{resourceType}.{uuid}@v3     -> Version="3"
    ri.{service}.{realm}.{resourceType}.{uuid}@v123   -> Version="123"

Strict validation mirrors the Go parser:
    - 5 dot-separated segments after "ri."
    - lowercase canonical UUID in the id segment
    - optional @vN suffix where N is positive decimal, no leading zero
    - rejects @vabc, @x3, bare @, double @v3@v4, @v0, @v0123
"""
from __future__ import annotations

import unittest

from weave_client.rid import Rid, format_rid, parse_rid, InvalidRidError


BASE_UUID = "550e8400-e29b-41d4-a716-446655440000"
BASE = f"ri.ontology.main.object-type.{BASE_UUID}"


class ParseRidTests(unittest.TestCase):

    def test_unversioned_parses_with_empty_version(self):
        r = parse_rid(BASE)
        self.assertEqual(r.service, "ontology")
        self.assertEqual(r.realm, "main")
        self.assertEqual(r.resource_type, "object-type")
        self.assertEqual(r.id, BASE_UUID)
        self.assertEqual(r.version, "")

    def test_at_v_suffix_populates_version(self):
        for suffix, expected in [("@v3", "3"), ("@v1", "1"), ("@v123", "123")]:
            r = parse_rid(BASE + suffix)
            self.assertEqual(r.version, expected, f"suffix={suffix}")
            self.assertEqual(r.id, BASE_UUID,
                             "suffix must not bleed into id segment")

    def test_format_roundtrips_with_and_without_version(self):
        for s in [BASE, BASE + "@v1", BASE + "@v42"]:
            r = parse_rid(s)
            self.assertEqual(format_rid(r), s,
                             f"round-trip broken: parse_rid->format_rid for {s!r}")

    def test_rid_equality_compares_version(self):
        a = parse_rid(BASE + "@v3")
        b = parse_rid(BASE + "@v3")
        c = parse_rid(BASE + "@v4")
        d = parse_rid(BASE)
        self.assertEqual(a, b, "identical @v3 RIDs should be equal")
        self.assertNotEqual(a, c, "@v3 != @v4 different versions")
        self.assertNotEqual(a, d,
                            "@v3 != unversioned (explicit vs latest)")

    def test_malformed_version_suffix_raises(self):
        bad = [
            BASE + "@",
            BASE + "@v",
            BASE + "@vabc",
            BASE + "@v0",
            BASE + "@v0123",
            BASE + "@v3@v4",
            BASE + "@x3",
        ]
        for s in bad:
            with self.assertRaises(InvalidRidError, msg=f"expected raise for {s!r}"):
                parse_rid(s)

    def test_invalid_base_rid_errors_on_base(self):
        # Bad base shouldn't be hidden by a syntactically-ok suffix.
        with self.assertRaises(InvalidRidError) as ctx:
            parse_rid("not.a.rid.format@v3")
        self.assertIn("invalid rid", str(ctx.exception).lower())

    def test_invalid_uuid_rejected(self):
        # Mirror Go's lowercase canonical UUID rejection.
        with self.assertRaises(InvalidRidError):
            parse_rid("ri.ontology.main.object-type.NOT-A-UUID")
        with self.assertRaises(InvalidRidError):
            # uppercase UUID rejected (byte-identical persistence rationale).
            parse_rid("ri.ontology.main.object-type." + BASE_UUID.upper())

    def test_format_rid_builds_canonical_string(self):
        # The dataclass-first construction path: build then format.
        r = Rid(service="ontology", realm="main",
                resource_type="object-type", id=BASE_UUID, version="")
        self.assertEqual(format_rid(r), BASE)
        r2 = Rid(service="ontology", realm="main",
                 resource_type="object-type", id=BASE_UUID, version="7")
        self.assertEqual(format_rid(r2), BASE + "@v7")

    def test_format_rid_validates_inputs(self):
        # format_rid is a write-path equivalent of parse_rid — same
        # invariants enforced so callers cannot produce a string that
        # parse_rid would reject.
        with self.assertRaises(InvalidRidError):
            format_rid(Rid("ontology", "main", "object-type",
                           "not-a-uuid", ""))
        with self.assertRaises(InvalidRidError):
            format_rid(Rid("ontology", "main", "object-type",
                           BASE_UUID, "0"))  # leading-zero/zero rejected
        with self.assertRaises(InvalidRidError):
            format_rid(Rid("ontology", "main", "bad.segment",
                           BASE_UUID, ""))  # dot in segment


if __name__ == "__main__":
    unittest.main()
