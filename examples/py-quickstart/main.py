"""Weave Python SDK — 5 minute hello-world.

Run a local Weave server (``make dev`` from repo root), then::

    pip install -e ../../sdk/python
    python main.py

The example uses ``AUTH_MODE=dev`` (no token required). When pointing at a
deployment with ``AUTH_MODE=token`` set ``WEAVE_TOKEN`` first.
"""
from __future__ import annotations

import os
import sys
from typing import Optional

from weave_client import Client, WeaveError


DEFAULT_BASE_URL = os.environ.get("WEAVE_BASE_URL", "http://localhost:9117")


def make_client(token: Optional[str]) -> Client:
    return Client(DEFAULT_BASE_URL, access_token=token) if token else Client(DEFAULT_BASE_URL)


def list_ontologies(client: Client) -> None:
    print("=== Ontologies ===")
    for ontology in client.ontologies.list():
        print(f"- {ontology.api_name}\t{ontology.display_name}")


def first_ontology_api_name(client: Client) -> Optional[str]:
    ontologies = list(client.ontologies.list())
    return ontologies[0].api_name if ontologies else None


def list_object_types(client: Client, ontology: str) -> None:
    print(f"=== Object types in {ontology} ===")
    types = client.ontologies.list_object_types(ontology)
    for ot in types:
        print(f"- {ot.api_name}\t{ot.display_name}")


def list_first_objects(client: Client, ontology: str, object_type: str, limit: int = 5) -> None:
    print(f"=== First {limit} {object_type} ===")
    page = client.objects.list(ontology, object_type, page_size=limit)
    for row in page.data:
        pk = row.get("__primaryKey", "?")
        print(f"- {pk}\t{row}")


def main() -> int:
    token = os.environ.get("WEAVE_TOKEN")
    client = make_client(token)
    try:
        list_ontologies(client)
        api_name = first_ontology_api_name(client)
        if not api_name:
            print("(no ontologies — load a fixture e.g. testdata/northwind to see more)")
            return 0
        list_object_types(client, api_name)
        types = client.ontologies.list_object_types(api_name)
        if types:
            list_first_objects(client, api_name, types[0].api_name)
    except WeaveError as err:
        print(f"Weave API error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
