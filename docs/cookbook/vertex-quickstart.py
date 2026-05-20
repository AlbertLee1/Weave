"""Runnable companion to docs/cookbook/vertex-quickstart.md.

Run against a local Weave instance:

    WEAVE_API_KEY=wvk_xxx python docs/cookbook/vertex-quickstart.py

The script exits 0 on the happy path and prints each step's result.
"""
from __future__ import annotations

import os
import sys

from weave_client.client import Client


def main() -> int:
    base_url = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")
    api_key = os.environ.get("WEAVE_API_KEY")
    if not api_key:
        print("WEAVE_API_KEY env var is required", file=sys.stderr)
        return 1

    client = Client(base_url, api_key=api_key)

    # Step 2 — ensure an ontology + case study exist. In a real install
    # both are created once and re-used; this snippet just hard-codes RIDs.
    ontology_rid = os.environ.get("ONTOLOGY_RID", "ri.weave.main.ontology.aviation")
    case_study_rid = os.environ.get("CASE_STUDY_RID", "ri.vertex.main.case-study.jfk-ops")

    # Step 3 — create a Scenario.
    scenario = client.vertex.scenarios.create(
        case_study_rid=case_study_rid,
        name="snowstorm",
        parent_ontology_commit="head",
    )
    print(f"created scenario {scenario['rid']}")

    # Step 4 — append an edit (handler shipped by VTX-044).
    client._request(
        "POST",
        f"/api/vertex/v1/scenarios/{scenario['rid']}/edits",
        json_body={
            "op": "modifyProperty",
            "objectType": "Airport",
            "objectId": "JFK",
            "property": "capacity",
            "newValue": 50,
        },
    )
    print("appended modifyProperty(capacity=50) to JFK")

    # Step 5 — read JFK with overlay.
    overlaid = client.objects.get("aviation", "Airport", "JFK", scenario_id=scenario["rid"])
    print(f"overlaid JFK.capacity = {overlaid.get('properties', {}).get('capacity')}")

    # Step 7 — start the scenario run and poll until a terminal record.
    run = client.vertex.scenarios.run(scenario["rid"])
    print(f"scenario run {run.get('rid')} ended with {run.get('status')}")

    # Step 8 — apply (commented by default; uncomment to merge into main).
    # client.vertex.scenarios.apply_to_main(scenario["rid"])

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
