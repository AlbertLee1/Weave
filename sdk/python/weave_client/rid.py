"""Resource Identifier helpers — round 92 mirror of pkg/rid.

Foundry SDKs ship ergonomic RID parsers so callers can construct
version-pinned RIDs without hand-rolling string concatenation. This
module mirrors the Go parser added in round 91 (pkg/rid/rid.go) so
the Python and backend sides agree byte-for-byte on what is a
valid RID.

Canonical form:

    ri.{service}.{realm}.{resourceType}.{uuid}
    ri.{service}.{realm}.{resourceType}.{uuid}@v{N}

UUID must be lowercase canonical (RFC 4122 textual form). The
optional @vN suffix carries the snapshot version — empty when the
RID points at "latest", otherwise positive decimal digits with no
leading zero.

The strict rejection of @v0 / @v0123 / uppercase UUIDs keeps
persisted RIDs byte-identical across reads — callers compare RIDs
by string equality everywhere on both sides of the wire.
"""
from __future__ import annotations

import re
from dataclasses import dataclass


_UUID_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
_VERSION_RE = re.compile(r"^[1-9][0-9]*$")


class InvalidRidError(ValueError):
    """Raised when a RID string fails canonical-form validation."""


@dataclass(frozen=True)
class Rid:
    """Parsed Resource Identifier. Frozen so it can be hashed / used
    as a dict key; ``__eq__`` from the dataclass covers the
    field-by-field comparison (including ``version``) that the Go
    pkg/rid.RID.Equal helper implements."""

    service: str
    realm: str
    resource_type: str
    id: str
    version: str = ""


def _validate_segment(s: str, label: str, source: str) -> None:
    if not s:
        raise InvalidRidError(f"invalid RID {label} segment: {source!r}")
    for ch in s:
        # Mirror Go validSegment: reject control chars and the
        # segment separator '.'.
        if ord(ch) < 0x20 or ord(ch) == 0x7f or ch == ".":
            raise InvalidRidError(f"invalid RID {label} segment: {source!r}")


def _split_version_suffix(rid: str) -> tuple[str, str]:
    """Peel optional @vN suffix off the input. Returns (base, version);
    version is "" when no suffix. Raises InvalidRidError when the
    suffix is malformed so callers never persist garbage."""
    at = rid.find("@")
    if at < 0:
        return rid, ""
    if rid.rfind("@") != at:
        raise InvalidRidError(
            f"invalid RID version suffix (multiple @): {rid!r}")
    suffix = rid[at + 1:]
    if not suffix.startswith("v"):
        raise InvalidRidError(
            f"invalid RID version suffix (expected @vN): {rid!r}")
    v = suffix[1:]
    if not _VERSION_RE.match(v):
        raise InvalidRidError(
            "invalid RID version suffix "
            f"(expected positive decimal, no leading zero): {rid!r}")
    return rid[:at], v


def parse_rid(s: str) -> Rid:
    """Parse a canonical RID string into a :class:`Rid` dataclass.

    Raises :class:`InvalidRidError` on any deviation from the canonical
    form. Backwards-compatible: un-versioned RIDs parse with
    ``version=""``.
    """
    base, version = _split_version_suffix(s)
    parts = base.split(".", 4)
    if len(parts) != 5 or parts[0] != "ri":
        raise InvalidRidError(f"invalid RID format: {s!r}")
    _validate_segment(parts[1], "service", s)
    _validate_segment(parts[2], "realm", s)
    _validate_segment(parts[3], "resource-type", s)
    if not _UUID_RE.match(parts[4]):
        raise InvalidRidError(
            "invalid RID id segment "
            f"(expected lowercase canonical UUID): {s!r}")
    return Rid(
        service=parts[1],
        realm=parts[2],
        resource_type=parts[3],
        id=parts[4],
        version=version,
    )


def format_rid(r: Rid) -> str:
    """Format a :class:`Rid` back to its canonical string form,
    validating every component so the write path cannot produce
    something :func:`parse_rid` would reject."""
    _validate_segment(r.service, "service", _describe(r))
    _validate_segment(r.realm, "realm", _describe(r))
    _validate_segment(r.resource_type, "resource-type", _describe(r))
    if not _UUID_RE.match(r.id):
        raise InvalidRidError(
            "invalid RID id segment "
            f"(expected lowercase canonical UUID): {_describe(r)!r}")
    if r.version != "" and not _VERSION_RE.match(r.version):
        raise InvalidRidError(
            "invalid RID version "
            f"(expected positive decimal, no leading zero): {_describe(r)!r}")
    base = f"ri.{r.service}.{r.realm}.{r.resource_type}.{r.id}"
    if r.version:
        return base + "@v" + r.version
    return base


def _describe(r: Rid) -> str:
    # Helper for error messages — gives users context without leaking
    # internal repr formatting that varies across Python versions.
    return (f"Rid(service={r.service!r}, realm={r.realm!r}, "
            f"resource_type={r.resource_type!r}, id={r.id!r}, "
            f"version={r.version!r})")


__all__ = ["Rid", "InvalidRidError", "parse_rid", "format_rid"]
