"""TDD tests for the external-HTTP allowlist (VTX-055).

The Vertex External Model BDD spec requires Functions to make
outbound HTTP through a process-wide allowlist (``config
.allowedExternalDomains``). Disallowed hosts must raise
``ForbiddenExternalCall`` instead of dialling out. Tests cover the
helper surface (allowlist mutation, host extraction, deny semantics)
and the ``http_client`` SDK shim Functions consume directly.

Each test installs a narrow allowlist and uses the autouse fixture
to scrub state — the module-level state mirrors the sandbox pattern
so leakage between tests would silently break later cases.
"""

from __future__ import annotations

from typing import Any, Dict, List, Optional, Tuple

import pytest

from weave_runtime.external_http import (
    ForbiddenExternalCall,
    HttpClient,
    configure_allowed_domains,
    get_allowed_domains,
    is_domain_allowed,
)


@pytest.fixture(autouse=True)
def _reset_allowlist():
    """Each test runs with an empty allowlist; teardown restores empty.

    The module keeps state at module scope (so the singleton
    ``http_client`` sees mutations from ``configure_allowed_domains``
    without re-construction). Without this fixture a leaky test would
    bleed into the next case's allow/deny outcome.
    """

    configure_allowed_domains([])
    yield
    configure_allowed_domains([])


# ---------------------------------------------------------------------------
# Allowlist configuration + host extraction
# ---------------------------------------------------------------------------


def test_configure_allowed_domains_then_get_returns_canonical_lowercase_tuple():
    configure_allowed_domains(["Example.com", "  Predict.AI  ", ""])
    assert get_allowed_domains() == ("example.com", "predict.ai")


def test_configure_allowed_domains_is_replaced_not_appended():
    configure_allowed_domains(["example.com"])
    configure_allowed_domains(["other.com"])
    assert get_allowed_domains() == ("other.com",)


def test_is_domain_allowed_matches_exact_host_in_url():
    configure_allowed_domains(["external-api.test"])
    assert is_domain_allowed("https://external-api.test/predict")
    assert is_domain_allowed("http://external-api.test:8080/v1")


def test_is_domain_allowed_strips_port_and_compares_case_insensitive():
    configure_allowed_domains(["external-api.test"])
    assert is_domain_allowed("https://External-Api.Test:443/x")


def test_is_domain_allowed_does_not_auto_match_subdomains():
    """``api.example.com`` must not auto-allow when only ``example.com``
    is listed — operators must spell out every host they trust."""

    configure_allowed_domains(["example.com"])
    assert not is_domain_allowed("https://api.example.com/v1")


def test_is_domain_allowed_rejects_when_allowlist_empty():
    configure_allowed_domains([])
    assert not is_domain_allowed("https://example.com/")


def test_is_domain_allowed_rejects_blank_or_malformed_url():
    configure_allowed_domains(["example.com"])
    assert not is_domain_allowed("")
    assert not is_domain_allowed("not-a-url")
    assert not is_domain_allowed("://broken")


def test_is_domain_allowed_accepts_bare_host_string():
    """Helpful for callers that already split the host out (e.g. logs)."""

    configure_allowed_domains(["external-api.test"])
    assert is_domain_allowed("external-api.test")


# ---------------------------------------------------------------------------
# HttpClient: post / get
# ---------------------------------------------------------------------------


class _RecordingTransport:
    """Test double that records calls and returns a canned response.

    The real transport hits the network; tests inject this so a passing
    case asserts on the request shape without depending on DNS, while
    a failing case (denied domain) never reaches the transport at all.
    """

    def __init__(self, response: Optional[Dict[str, Any]] = None):
        self.calls: List[Dict[str, Any]] = []
        self._response = response if response is not None else {"ok": True}

    def __call__(self, *, method: str, url: str, headers: Dict[str, str], body: Optional[bytes], timeout: float):
        self.calls.append(
            {
                "method": method,
                "url": url,
                "headers": dict(headers),
                "body": body,
                "timeout": timeout,
            }
        )
        return self._response


def test_post_to_allowed_domain_calls_transport_and_returns_response():
    configure_allowed_domains(["external-api.test"])
    transport = _RecordingTransport(response={"score": 0.7})
    client = HttpClient(transport=transport)

    out = client.post("https://external-api.test/predict", json={"x": 1})

    assert out == {"score": 0.7}
    assert len(transport.calls) == 1
    call = transport.calls[0]
    assert call["method"] == "POST"
    assert call["url"] == "https://external-api.test/predict"
    assert call["headers"].get("Content-Type") == "application/json"
    # Body is JSON-encoded UTF-8 bytes.
    assert call["body"] == b'{"x": 1}'


def test_post_to_disallowed_domain_raises_forbidden_external_call_without_calling_transport():
    """BDD #2 — domain not in allowlist raises ForbiddenExternalCall.

    The transport must NOT be invoked, otherwise a DNS lookup could
    still leak the existence of the target host."""

    configure_allowed_domains(["external-api.test"])
    transport = _RecordingTransport()
    client = HttpClient(transport=transport)

    with pytest.raises(ForbiddenExternalCall) as ei:
        client.post("https://untrusted.example.com/steal", json={"x": 1})

    assert ei.value.host == "untrusted.example.com"
    assert transport.calls == []


def test_get_to_allowed_domain_returns_response():
    configure_allowed_domains(["external-api.test"])
    transport = _RecordingTransport(response={"hello": "world"})
    client = HttpClient(transport=transport)

    out = client.get("https://external-api.test/data?id=42")

    assert out == {"hello": "world"}
    assert transport.calls[0]["method"] == "GET"
    # GET carries no body even if caller supplied none.
    assert transport.calls[0]["body"] is None


def test_get_to_disallowed_domain_raises_forbidden_external_call():
    configure_allowed_domains(["external-api.test"])
    transport = _RecordingTransport()
    client = HttpClient(transport=transport)

    with pytest.raises(ForbiddenExternalCall):
        client.get("https://untrusted.example.com/")
    assert transport.calls == []


def test_post_includes_caller_headers_alongside_content_type():
    configure_allowed_domains(["external-api.test"])
    transport = _RecordingTransport()
    client = HttpClient(transport=transport)

    client.post(
        "https://external-api.test/predict",
        json={"x": 1},
        headers={"X-Trace": "abc"},
    )
    headers = transport.calls[0]["headers"]
    assert headers["X-Trace"] == "abc"
    assert headers["Content-Type"] == "application/json"


def test_post_without_json_body_still_routes_through_allowlist_check():
    """Allowlist must guard even when the body is empty — callers
    shouldn't be able to ping a denied host with a header-only POST."""

    configure_allowed_domains(["external-api.test"])
    transport = _RecordingTransport()
    client = HttpClient(transport=transport)

    with pytest.raises(ForbiddenExternalCall):
        client.post("https://untrusted.example.com/", json=None)

    assert transport.calls == []


def test_forbidden_external_call_is_permission_error_subclass():
    """A 403-shaped envelope at the HTTP boundary depends on
    ``ForbiddenExternalCall`` being a ``PermissionError`` (so callers
    that already catch the base get notified). Anchor that here."""

    assert issubclass(ForbiddenExternalCall, PermissionError)


def test_http_client_uses_module_state_so_late_configure_still_applies():
    """The singleton ``http_client`` reads the live allowlist on every
    call — registering an allowlist *after* constructing the client
    must still take effect."""

    transport = _RecordingTransport()
    client = HttpClient(transport=transport)

    # Before configure_allowed_domains: denied.
    with pytest.raises(ForbiddenExternalCall):
        client.get("https://external-api.test/x")

    configure_allowed_domains(["external-api.test"])
    out = client.get("https://external-api.test/x")
    assert out == {"ok": True}
