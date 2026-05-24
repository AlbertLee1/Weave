"""ReactionsAPI — Python wrapper for the /api/v2/reactions surface
(PRD-V2 上层体验 row, round 71).

The Go server exposes four endpoints:

    GET    /api/v2/reactions?targetRid=…           (Aggregate)
    POST   /api/v2/reactions                        (Create)
    DELETE /api/v2/reactions?targetRid=…&emoji=…   (Delete)
    POST   /api/v2/reactions/batch                  (AggregateBatch, round 67)

This module wraps them so callers don't have to hand-build URLs
with the right query params or remember the wire shape of the
batch envelope.

Quickstart::

    from weave_client import Client

    client = Client("http://localhost:9117", access_token="…")

    summary = client.reactions.aggregate("ri.objects.main.Customer.1")
    for bucket in summary.emojis:
        print(bucket.emoji, bucket.count, bucket.mine)

    client.reactions.create("ri.objects.main.Customer.1", "👍")
    client.reactions.delete("ri.objects.main.Customer.1", "👍")

    # Bulk-render an ObjectList row-reactions bar in one round-trip:
    summaries = client.reactions.aggregate_batch([
        "ri.objects.main.Customer.1",
        "ri.objects.main.Customer.2",
        "ri.objects.main.Customer.3",
    ])

Empty input on aggregate_batch short-circuits without hitting the
server — the Foundry "no rows visible" navbar poll stays free.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import TYPE_CHECKING, Any, Dict, List, Mapping, Optional
from urllib.parse import quote

from ._http import build_query_string

if TYPE_CHECKING:
    from .client import Client


@dataclass
class EmojiCount:
    """One bucket of an aggregated reactions Summary."""

    emoji: str
    count: int
    mine: bool


@dataclass
class ReactionSummary:
    """Wire shape returned by GET /api/v2/reactions and per-target
    entries of POST /api/v2/reactions/batch.

    `emojis` is sorted server-side by descending count then ascending
    emoji for deterministic display; the wrapper preserves order
    verbatim.
    """

    target_rid: str
    emojis: List[EmojiCount] = field(default_factory=list)


@dataclass
class Reaction:
    """Wire shape returned by POST /api/v2/reactions."""

    id: str
    user_id: str
    target_rid: str
    emoji: str
    created_at: Optional[datetime] = None


def _parse_emoji_count(raw: Mapping[str, Any]) -> EmojiCount:
    return EmojiCount(
        emoji=str(raw.get("emoji") or ""),
        count=int(raw.get("count") or 0),
        mine=bool(raw.get("mine")),
    )


def _parse_summary(raw: Mapping[str, Any]) -> ReactionSummary:
    emojis_raw = raw.get("emojis") or []
    return ReactionSummary(
        target_rid=str(raw.get("targetRid") or ""),
        emojis=[_parse_emoji_count(b) for b in emojis_raw],
    )


def _parse_reaction(raw: Mapping[str, Any]) -> Reaction:
    created_at: Optional[datetime] = None
    if ca := raw.get("createdAt"):
        # The server emits RFC3339 with Z suffix; replace for fromisoformat
        # which doesn't accept "Z" until Python 3.11.
        try:
            created_at = datetime.fromisoformat(str(ca).replace("Z", "+00:00"))
        except ValueError:
            created_at = None
    return Reaction(
        id=str(raw.get("id") or ""),
        user_id=str(raw.get("userId") or ""),
        target_rid=str(raw.get("targetRid") or ""),
        emoji=str(raw.get("emoji") or ""),
        created_at=created_at,
    )


class ReactionsAPI:
    """Wrapper for the /api/v2/reactions surface."""

    def __init__(self, client: "Client") -> None:
        self._client = client

    def aggregate(self, target_rid: str) -> ReactionSummary:
        """GET /api/v2/reactions?targetRid=… — count + mine flag per emoji."""
        path = "/api/v2/reactions" + build_query_string({"targetRid": target_rid})
        resp = self._client._request("GET", path)
        return _parse_summary(resp or {})

    def create(self, target_rid: str, emoji: str) -> Reaction:
        """POST /api/v2/reactions — idempotently records one reaction."""
        resp = self._client._request(
            "POST", "/api/v2/reactions",
            json_body={"targetRid": target_rid, "emoji": emoji},
        )
        return _parse_reaction(resp or {})

    def delete(self, target_rid: str, emoji: str) -> None:
        """DELETE /api/v2/reactions?targetRid=…&emoji=… — remove caller's
        reaction. Server is keyed on (user, target, emoji); 404 if no
        row matches (so SPA can disambiguate already-unreacted)."""
        # Hand-build the query so the emoji gets URL-encoded (the
        # generic build_query_string used above also encodes properly;
        # using it here keeps the two paths consistent).
        path = "/api/v2/reactions" + build_query_string({
            "targetRid": target_rid,
            "emoji": emoji,
        })
        self._client._request("DELETE", path)
        return None

    def aggregate_batch(self, target_rids: List[str]) -> List[ReactionSummary]:
        """POST /api/v2/reactions/batch — one round-trip for many targets.

        Empty input short-circuits without hitting the server so the
        Foundry "no rows visible" navbar poll stays free.

        Order preservation: summaries[i] always corresponds to
        target_rids[i] — the server guarantees input-order alignment
        (round 67 BDD asserts it) and the wrapper passes the response
        through.
        """
        if not target_rids:
            return []
        resp = self._client._request(
            "POST", "/api/v2/reactions/batch",
            json_body={"targetRids": list(target_rids)},
        )
        raw = (resp or {}).get("summaries") or []
        return [_parse_summary(s) for s in raw]


# Internal-quote shim so `from .reactions import quote` is stable
# even if we later swap urllib for httpx's quote helper.
_ = quote
_ = Dict  # keep typing import for future signature evolution
