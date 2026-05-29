"""TransactionsAPI — Python wrapper for the OntologyTransaction
preview surface (PRD-V2 Transaction (preview) round 60).

Round 59 added GET + DELETE on the server side; this module
wraps all three endpoints so Python callers don't have to
hand-build URLs with the mandatory ``?preview=true`` query.

Quickstart::

    from weave_client import Client

    client = Client("http://localhost:9117", access_token="...")
    edits = [
        {"type": "CREATE", "objectType": "User", "primaryKey": "u1",
         "properties": {"name": "alice"}},
        {"type": "MODIFY", "objectType": "User", "primaryKey": "u1",
         "properties": {"name": "alice2"}},
    ]
    appended = client.transactions.append_edits("nw", "tx-1", edits)
    print(appended.total_edits)

    tx = client.transactions.get("nw", "tx-1")
    for e in tx.edits:
        print(e["type"], e["primaryKey"])

    client.transactions.abort("nw", "tx-1")  # idempotent

The server gates every transactions endpoint behind ``?preview=
true``; this wrapper attaches it automatically so callers never
need to remember the flag.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Any, Dict, List

from ._http import quote_path

if TYPE_CHECKING:
    from .client import Client


@dataclass
class TransactionAppendResponse:
    """Wire shape returned by POST .../transactions/{id}/edits.

    The server returns ``{transactionId, appendedEdits, totalEdits}``;
    we expose them with snake_case Python names but preserve the
    semantic 1:1.
    """

    transaction_id: str
    appended_edits: int
    total_edits: int


@dataclass
class Transaction:
    """Wire shape returned by GET .../transactions/{id}.

    ``edits`` is the full append-ordered list as raw dicts (each
    matching the funnel.Edit shape: ``{type, objectType, primaryKey,
    properties?}``). We do not parse the edits into a typed model
    because the preview surface is intentionally schemaless — the
    server accepts ANY shape on append and round-trips it verbatim
    on get.
    """

    transaction_id: str
    total_edits: int
    edits: List[Dict[str, Any]] = field(default_factory=list)


# The ?preview=true gate is mandatory on every transactions endpoint;
# kept as a module-level constant so a future schema migration
# (preview → ga) only needs editing this one line.
_PREVIEW_QUERY = "?preview=true"


class TransactionsAPI:
    """Wrapper for the OntologyTransaction preview surface."""

    def __init__(self, client: "Client") -> None:
        self._client = client

    def append_edits(
        self,
        ontology: str,
        transaction_id: str,
        edits: List[Dict[str, Any]],
    ) -> TransactionAppendResponse:
        """POST .../transactions/{transactionId}/edits.

        Appends ``edits`` to the transaction's in-memory buffer
        (creates the transaction on first call). Returns counts so
        callers can correlate before/after sizes.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/transactions/{quote_path(transaction_id)}/edits"
            f"{_PREVIEW_QUERY}"
        )
        resp = self._client._request("POST", path, json_body={"edits": edits})
        return TransactionAppendResponse(
            transaction_id=str(resp.get("transactionId") or transaction_id),
            appended_edits=int(resp.get("appendedEdits") or 0),
            total_edits=int(resp.get("totalEdits") or 0),
        )

    def get(self, ontology: str, transaction_id: str) -> Transaction:
        """GET .../transactions/{transactionId}.

        Returns the full append-ordered edit buffer. Unknown
        transactions yield an empty list (auto-create-on-first-use
        semantic) so callers don't have to special-case 404.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/transactions/{quote_path(transaction_id)}"
            f"{_PREVIEW_QUERY}"
        )
        resp = self._client._request("GET", path)
        edits = resp.get("edits") or []
        return Transaction(
            transaction_id=str(resp.get("transactionId") or transaction_id),
            total_edits=int(resp.get("totalEdits") or 0),
            edits=list(edits),
        )

    def abort(self, ontology: str, transaction_id: str) -> None:
        """DELETE .../transactions/{transactionId}.

        Discards the transaction's edit buffer. Idempotent — calling
        on an unknown / already-aborted transaction is not an error
        so retries are safe.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/transactions/{quote_path(transaction_id)}"
            f"{_PREVIEW_QUERY}"
        )
        self._client._request("DELETE", path)
        return None
