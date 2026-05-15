"""External-HTTP allowlist + ``http_client`` SDK shim (VTX-055).

Vertex Functions may need to call out to external REST APIs (e.g. a
managed prediction service). Letting arbitrary outbound traffic
through would defeat the sandbox boundary the filesystem guard in
``sandbox.py`` already establishes — so operators declare a
``config.allowedExternalDomains`` list at app boot and every outbound
call must match an exact host on that list. Disallowed hosts raise
``ForbiddenExternalCall`` *before* the transport runs, so a denied
call doesn't even trigger a DNS lookup.

The module is structured the same way as ``sandbox.py``: module-level
state holds the active allowlist; ``configure_allowed_domains``
replaces it atomically; ``HttpClient`` is a thin, transport-injectable
SDK that Functions consume directly. Tests inject a fake transport so
allow/deny coverage doesn't depend on a live network.

Threat model anchor — same wording as the sandbox: this is *accidental
exfiltration* protection, not a hardened container boundary. A
Function author who bypasses ``http_client`` and goes straight to
``socket`` can still dial out; operators should run the runtime in
its own network namespace if that matters.
"""

from __future__ import annotations

import json as _json
import urllib.parse
import urllib.request
from typing import Any, Callable, Dict, Iterable, Mapping, Optional, Tuple


class ForbiddenExternalCall(PermissionError):
    """Raised when sandboxed code tries to call a host outside the
    allowlist. Inherits ``PermissionError`` for the same reason
    ``SandboxViolation`` does — any handler already catching that
    base class will see the violation; the FastAPI handler in
    ``app.py`` maps it to a 403 envelope with
    ``code="ForbiddenExternalCall"``.
    """

    def __init__(self, host: str, reason: str = "domain not in allowlist"):
        self.host = host
        self.reason = reason
        super().__init__(f"forbidden external call: {reason}: {host}")


# Module-level mutable state. Mirrors ``sandbox._state`` so the
# singleton ``http_client`` below sees mutations from
# ``configure_allowed_domains`` without re-construction. ``allowed`` is
# a tuple (immutable, hashable) of lowercase exact-match hostnames.
_state: Dict[str, Tuple[str, ...]] = {"allowed": ()}


def _canonicalise_host(host: str) -> str:
    """Lowercase + strip whitespace. Module keeps everything in this
    canonical form so ``is_domain_allowed`` does a single set-style
    compare without per-call normalisation."""

    return (host or "").strip().lower()


def configure_allowed_domains(domains: Iterable[str]) -> None:
    """Replace the active allowlist atomically.

    Blank/whitespace-only entries are dropped. Each entry is
    canonicalised to lowercase. Idempotent: calling with the same
    sequence is a no-op. Calling with an empty iterable clears the
    allowlist (denies everything) — useful in tests' teardown and as
    the default at app boot when ``WEAVE_ALLOWED_EXTERNAL_DOMAINS`` is
    unset.
    """

    canonical: Tuple[str, ...] = tuple(
        _canonicalise_host(d) for d in domains if d and _canonicalise_host(d)
    )
    _state["allowed"] = canonical


def get_allowed_domains() -> Tuple[str, ...]:
    """Return the active allowlist as an immutable tuple.

    Exposed for diagnostic logging, ``/health`` introspection, and the
    app test that asserts ``create_app`` wired the kwarg through.
    """

    return _state["allowed"]


def _extract_host(url_or_host: str) -> Optional[str]:
    """Return the host component of a URL, or the input itself when it
    looks like a bare hostname. Returns ``None`` for empty / malformed
    inputs so ``is_domain_allowed`` can default-deny rather than crash.
    """

    if not url_or_host or not isinstance(url_or_host, str):
        return None
    candidate = url_or_host.strip()
    if not candidate:
        return None
    # Bare hostname heuristic: no scheme, no path separator.
    if "://" not in candidate and "/" not in candidate and ":" not in candidate:
        return candidate
    try:
        parsed = urllib.parse.urlparse(candidate)
    except ValueError:
        return None
    if not parsed.netloc:
        return None
    host = parsed.hostname
    if not host:
        return None
    return host


def is_domain_allowed(url_or_host: str) -> bool:
    """Return ``True`` when ``url_or_host``'s host matches the
    allowlist exactly (case-insensitive). Subdomains are NOT
    auto-allowed — operators must spell out every trusted host so a
    typo doesn't widen the network egress surface.
    """

    host = _extract_host(url_or_host)
    if host is None:
        return False
    return _canonicalise_host(host) in _state["allowed"]


# Type alias for a transport callable. Kept narrow on purpose: tests
# inject a recorder, production wires ``_default_transport`` which
# delegates to ``urllib.request.urlopen`` — Functions never need to
# customise it directly.
TransportCallable = Callable[..., Any]


def _default_transport(
    *,
    method: str,
    url: str,
    headers: Dict[str, str],
    body: Optional[bytes],
    timeout: float,
) -> Any:
    """Real-network transport used when no fake is injected.

    Builds a ``urllib.request.Request`` so we stay in the stdlib —
    pulling in ``requests`` for a single call inside a sidecar is
    overkill, and ``urllib`` gives us enough to round-trip JSON.

    The response body is JSON-decoded when the response advertises
    ``application/json``; otherwise the raw bytes are returned. Status
    codes are not specially mapped — a 5xx from the upstream surfaces
    as a ``urllib.error.HTTPError`` and propagates as the function's
    own failure (the runtime's generic exception handler converts it
    to a 500 envelope).
    """

    request = urllib.request.Request(url, data=body, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=timeout) as response:  # noqa: S310 - allowlist enforced upstream
        raw = response.read()
        content_type = response.headers.get("Content-Type", "")
    if "application/json" in content_type and raw:
        return _json.loads(raw.decode("utf-8"))
    return raw


class HttpClient:
    """SDK shim Functions use to make outbound HTTP calls.

    Every method checks the URL against the live allowlist before
    handing off to the transport — so the singleton instance below
    picks up ``configure_allowed_domains`` calls without rebinding.
    Tests inject a custom transport to assert on the request shape
    without actually dialling out.

    The class deliberately exposes only ``get`` and ``post``: those
    cover the BDD spec ("call out to external REST"), and a narrow
    surface means there are fewer ways for a Function author to
    accidentally bypass the allowlist check.
    """

    def __init__(self, transport: Optional[TransportCallable] = None):
        self._transport: TransportCallable = transport or _default_transport

    def get(
        self,
        url: str,
        *,
        headers: Optional[Mapping[str, str]] = None,
        timeout: float = 10.0,
    ) -> Any:
        """Issue a GET to ``url``. Body is always ``None``."""

        return self._request("GET", url, headers=headers, body=None, timeout=timeout)

    def post(
        self,
        url: str,
        *,
        json: Any = None,
        headers: Optional[Mapping[str, str]] = None,
        timeout: float = 10.0,
    ) -> Any:
        """Issue a POST to ``url``.

        When ``json`` is not ``None`` it is JSON-encoded as the request
        body and a ``Content-Type: application/json`` header is added
        (caller-provided headers win on collision so a Function can
        override it intentionally).
        """

        body: Optional[bytes] = None
        merged_headers: Dict[str, str] = {"Content-Type": "application/json"} if json is not None else {}
        if headers:
            for k, v in headers.items():
                merged_headers[k] = v
        if json is not None:
            body = _json.dumps(json).encode("utf-8")
        return self._request("POST", url, headers=merged_headers, body=body, timeout=timeout)

    def _request(
        self,
        method: str,
        url: str,
        *,
        headers: Optional[Mapping[str, str]],
        body: Optional[bytes],
        timeout: float,
    ) -> Any:
        if not is_domain_allowed(url):
            host = _extract_host(url) or url
            raise ForbiddenExternalCall(host)
        return self._transport(
            method=method,
            url=url,
            headers=dict(headers or {}),
            body=body,
            timeout=timeout,
        )


# Module-level singleton. Function code imports ``http_client`` from
# ``weave_runtime`` and calls ``http_client.post(...)`` directly — the
# allowlist is read at call time, so app boot order
# (``configure_allowed_domains`` runs in ``create_app``) doesn't matter.
http_client = HttpClient()


__all__ = [
    "ForbiddenExternalCall",
    "HttpClient",
    "configure_allowed_domains",
    "get_allowed_domains",
    "http_client",
    "is_domain_allowed",
]
