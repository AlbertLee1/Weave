"""TimeSeriesAPI — read/write TimeSeriesProperty data via Foundry's
/api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/* surface.

PRD-V2 Gap-D2 round 44: the Go server has comprehensive
TimeSeriesProperty endpoints (firstPoint, lastPoint, latestValue,
streamPoints, streamValues, append-point, transform) but the
Python SDK exposed none of them. Callers had to drop down to raw
HTTP via the internal Transport. This module wraps the entire
read+write surface and returns Pydantic models so type-checkers
and IDE completion see structured data.

Quickstart::

    from weave_client import Client

    client = Client("http://localhost:9117", access_token="...")
    first = client.timeseries.get_first_point(
        "northwind", "Reading", "sensor-001", "temperature",
    )
    print(first.time, first.value)

    points = client.timeseries.stream_points(
        "northwind", "Reading", "sensor-001", "temperature",
    )

    client.timeseries.append_point(
        "northwind", "Reading", "sensor-001", "temperature",
        time="2026-04-01T00:00:00Z", value=42.5,
    )
"""
from __future__ import annotations

from datetime import datetime
from typing import TYPE_CHECKING, Any, List, Optional, Union

from ._http import quote_path
from .types import TimeSeriesPoint

if TYPE_CHECKING:
    from .client import Client


def _validate_point(payload: Any) -> TimeSeriesPoint:
    """Round-trip a server payload into TimeSeriesPoint.

    The server omits empty-state series with a 404; callers should
    catch WeaveNotFoundError. A successful response always has both
    ``time`` and ``value`` populated, so the model validator never
    sees None on a 2xx path.
    """
    if hasattr(TimeSeriesPoint, "model_validate"):
        return TimeSeriesPoint.model_validate(payload or {})
    return TimeSeriesPoint(**(payload or {}))


def _isoformat(t: Union[str, datetime]) -> str:
    """Coerce a time argument to RFC3339Nano. Accepts either a
    pre-formatted string (passed through) or a naive/aware
    datetime (formatted to ISO 8601 with timezone preserved)."""
    if isinstance(t, str):
        return t
    if t.tzinfo is None:
        # Naive datetimes are interpreted as UTC. The server's
        # time.Parse(RFC3339Nano) rejects naive ISO strings so we
        # always emit a Z-suffixed instant.
        return t.isoformat() + "Z"
    return t.isoformat()


class TimeSeriesAPI:
    """Wraps ``/api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/*``.

    All read methods return a fully-validated ``TimeSeriesPoint``
    (or list thereof). All write methods return ``None`` on
    success — the server returns 204 No Content for append.
    """

    def __init__(self, client: "Client"):
        self._client = client

    # ------------------------------------------------------------------
    # Read endpoints — TimeSeriesProperty
    # ------------------------------------------------------------------

    def get_first_point(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
    ) -> TimeSeriesPoint:
        """Earliest point in the series for the given object property."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/timeseries/{quote_path(property)}/firstPoint"
        )
        return _validate_point(self._client._request("GET", path))

    def get_last_point(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
    ) -> TimeSeriesPoint:
        """Latest point in the series for the given object property."""
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/timeseries/{quote_path(property)}/lastPoint"
        )
        return _validate_point(self._client._request("GET", path))

    def stream_points(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
    ) -> List[TimeSeriesPoint]:
        """Every point in the series, time-ordered.

        Wraps ``POST .../timeseries/{property}/streamPoints``. The
        ?format= query parameter is fixed to JSON — the server
        rejects ARROW with HTTP 400 UnsupportedFormat. The
        underlying server stores the entire series in memory for
        a single response; pagination is not yet supported.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/timeseries/{quote_path(property)}/streamPoints"
        )
        resp = self._client._request("POST", path)
        return [_validate_point(p) for p in (resp or [])]

    # ------------------------------------------------------------------
    # Read endpoints — TimeSeriesValueBankProperty (US-038)
    # ------------------------------------------------------------------

    def get_latest_value(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property_name: str,
    ) -> TimeSeriesPoint:
        """Most recent point in a TimeSeriesValueBankProperty series.

        Equivalent to ``get_last_point`` for TimeSeriesProperty but
        bound to the ``/latestValue`` endpoint Foundry exposes for
        the ValueBank variant. The path parameter is named
        ``{propertyName}`` (not ``{property}``) on the wire — kept
        as a kwarg here for clarity.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/timeseries/{quote_path(property_name)}/latestValue"
        )
        return _validate_point(self._client._request("GET", path))

    def stream_values(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
    ) -> List[TimeSeriesPoint]:
        """Every point in a TimeSeriesValueBankProperty series.

        Wire-equivalent to ``stream_points`` but bound to
        ``/streamValues`` — Foundry splits the two endpoints by
        property kind, the wire shape is identical.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/timeseries/{quote_path(property)}/streamValues"
        )
        resp = self._client._request("POST", path)
        return [_validate_point(p) for p in (resp or [])]

    # ------------------------------------------------------------------
    # Write endpoint — US-400 AppendPoint
    # ------------------------------------------------------------------

    def append_point(
        self,
        ontology: str,
        object_type: str,
        primary_key: str,
        property: str,
        *,
        time: Union[str, datetime],
        value: Any,
    ) -> None:
        """Append one point to the series.

        ``time`` accepts a pre-formatted RFC3339Nano string or a
        ``datetime`` (naive instances are interpreted as UTC).
        ``value`` is forwarded as-is — the VictoriaMetrics backend
        coerces to float64 and returns 400
        TimeSeriesNonNumericValue on unsupported types; the
        memory + PG backends accept any JSON value.
        """
        path = (
            f"/api/v2/ontologies/{quote_path(ontology)}"
            f"/objects/{quote_path(object_type)}/{quote_path(primary_key)}"
            f"/timeseries/{quote_path(property)}/points"
        )
        body = {"time": _isoformat(time), "value": value}
        self._client._request("POST", path, json_body=body)
        return None

    # ------------------------------------------------------------------
    # Transform endpoint — US-402
    # ------------------------------------------------------------------

    def transform(
        self,
        ontology: str,
        *,
        source: Optional[dict] = None,
        points: Optional[List[dict]] = None,
        steps: List[dict],
    ) -> List[TimeSeriesPoint]:
        """Run a chain of {op, params} transforms over a series.

        Exactly one of ``source`` (server-resolved series via
        {object_type, primary_key, property}) OR ``points``
        (caller-supplied inline points) must be supplied; both or
        neither raises ValueError before the HTTP call so callers
        get a clear local error instead of an opaque 400.

        Returns the transformed series as a list of TimeSeriesPoint.
        """
        if (source is None) == (points is None):
            raise ValueError(
                "transform: exactly one of 'source' or 'points' must be supplied",
            )
        body: dict = {"steps": steps}
        if source is not None:
            body["source"] = source
        if points is not None:
            body["points"] = points
        path = f"/api/v2/ontologies/{quote_path(ontology)}/timeseries/transform"
        resp = self._client._request("POST", path, json_body=body)
        return [_validate_point(p) for p in (resp or [])]


__all__ = ["TimeSeriesAPI"]
