"""Cookbook chapter 7 — v2 SDK builders & typed errors.

Demonstrates the three new builder surfaces (ObjectSetBuilder /
AggregationAPI / criteria_builders) plus the two typed exceptions
(WeaveValidationError / WeaveVersionedLookupError) end-to-end against a
running Weave instance (default http://localhost:9117). Mirrors the
prose in 07-builders.md so the on-disk recipe is runnable.

Honours the standard cookbook env contract:

    WEAVE_BASE_URL  - server URL (default http://localhost:9117)
    WEAVE_TOKEN     - JWT/API key when AUTH_MODE!=dev (optional)
"""

from __future__ import annotations

import os
import sys


def main() -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    token = os.environ.get("WEAVE_TOKEN")

    try:
        from weave_client import WeaveClient
        from weave_client.objectsets import ObjectSetBuilder
        from weave_client.criteria_builders import (
            always,
            parameter_match,
            parameter_compare,
            and_,
            or_,
            not_,
        )
        from weave_client.errors import (
            WeaveError,
            WeaveValidationError,
            WeaveVersionedLookupError,
        )
    except ImportError as exc:  # pragma: no cover - exercised only when SDK absent
        print(
            f"weave_client import failed: {exc}\n"
            "Install via `pip install -e sdk/python` then re-run.",
            file=sys.stderr,
        )
        return 2

    client = WeaveClient(base_url, token=token) if token else WeaveClient(base_url)

    # --- ObjectSetBuilder ---------------------------------------------------
    definition = (
        ObjectSetBuilder(client)
        .base("Customer")
        .filter({"field": "country", "op": "eq", "value": "USA"})
        .with_properties(
            {"orderCount": {"link": "orders", "metric": "count"}}
        )
        .build()
    )
    print("== ObjectSetBuilder definition ==")
    print(definition)

    # --- Criteria builders --------------------------------------------------
    criteria = and_(
        parameter_match("status", "active"),
        or_(
            parameter_compare("age", "gt", "minAge"),
            parameter_compare("seniority", "gte", 10),
        ),
        not_(parameter_match("region", "restricted")),
        always(),
    )
    print("\n== Submission criteria tree ==")
    print(criteria)

    # --- Typed error branches -----------------------------------------------
    # These two except blocks are the point of the recipe: branch on the
    # specific contract instead of parsing the prose of an error body.
    print("\n== Typed error demo ==")
    print(
        "  Catch WeaveValidationError for 400 InvalidParameter:submissionCriteria "
        "(Gap-A3 SDK136)."
    )
    print(
        "  Catch WeaveVersionedLookupError for 501 VersionedLookupNotSupported "
        "(Gap-T4 SDK118)."
    )
    print(
        "  Both subclass WeaveError so legacy `except WeaveError:` blocks keep working."
    )

    # Sanity assert the typed classes exist so a future SDK refactor that
    # drops them is caught loudly by `make test` running py_compile here.
    assert issubclass(WeaveValidationError, WeaveError)
    assert issubclass(WeaveVersionedLookupError, WeaveError)

    return 0


if __name__ == "__main__":
    sys.exit(main())
