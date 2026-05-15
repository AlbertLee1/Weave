"""LLM SDK shim for the Vertex Python function runtime (VTX-056).

Functions in the Vertex runtime may call out to a managed language
model — Palantir Foundry's Vertex exposes Anthropic-style Claude models
through ``invokeLLM`` in the function SDK. This module is the Python
equivalent: a ``LLMClient`` plus module-level singleton ``llm_client``
that Functions consume directly, with config (API key, base URL,
Anthropic version) injected at app boot through ``configure_llm``.

The module mirrors the layout of ``external_http.py`` for two reasons:

  1. Functions discover the SDK the same way they discovered
     ``http_client`` — ``from weave_runtime import invoke_llm``.
  2. State management (module-level ``_state`` + atomic replace via
     ``configure_llm``) is identical, so the test-fixture pattern that
     resets external_http state can be reused for llm state.

Error semantics:

  * ``ConfigError`` (``ValueError`` subclass) — no API key configured.
    Raised BEFORE the transport runs so a misconfigured runtime doesn't
    leak a request to Anthropic. Propagates through the registry as a
    5xx envelope with ``code="ConfigError"`` so the Go client can
    branch via ``errors.As`` on a ``*RuntimeError``.
  * ``ModelOutputError`` (``ValueError`` subclass) — the model's text
    reply cannot be parsed as the structured shape the Function asked
    for (only relevant to ``invoke_llm_json``). Functions catch this
    when they want to retry with a different prompt or fall back to a
    deterministic default.

Threat model anchor: this is *accidental key leakage* protection (the
key stays inside the sidecar; Function authors never see it), not a
hardened rate-limit boundary. Operators should run the runtime behind
an egress proxy if they need quota enforcement beyond Anthropic's own.
"""

from __future__ import annotations

import json as _json
import re
from typing import Any, Callable, Dict, List, Mapping, Optional, Tuple


DEFAULT_ANTHROPIC_BASE_URL = "https://api.anthropic.com"
DEFAULT_ANTHROPIC_VERSION = "2023-06-01"
DEFAULT_MAX_TOKENS = 1024
DEFAULT_TIMEOUT_SECONDS = 30.0


class ConfigError(ValueError):
    """Raised when ``invoke_llm`` is called without a configured API key.

    Inherits ``ValueError`` so Functions catching a broad value-level
    failure still see it; the FastAPI handler in ``app.py`` maps any
    uncaught exception to a 5xx envelope with ``code="ConfigError"``,
    giving the Go side a typed handle via ``parseRuntimeError``.
    """


class ModelOutputError(ValueError):
    """Raised when the model's text reply cannot be coerced into the
    structured shape ``invoke_llm_json`` promised its caller.

    ``raw_text`` carries the unparsed reply so an operator combing logs
    can see what the model actually said without re-running the call.
    """

    def __init__(self, message: str, *, raw_text: str = ""):
        self.raw_text = raw_text
        super().__init__(message)


# Module-level mutable state. Mirrors ``external_http._state`` so the
# singleton ``llm_client`` below sees mutations from ``configure_llm``
# without re-construction.
_state: Dict[str, Any] = {
    "api_key": None,
    "base_url": DEFAULT_ANTHROPIC_BASE_URL,
    "anthropic_version": DEFAULT_ANTHROPIC_VERSION,
}


def configure_llm(
    *,
    api_key: Optional[str] = None,
    base_url: str = DEFAULT_ANTHROPIC_BASE_URL,
    anthropic_version: str = DEFAULT_ANTHROPIC_VERSION,
) -> None:
    """Replace the active LLM config atomically.

    ``api_key`` is canonicalised: blank or whitespace-only inputs are
    stored as ``None`` so the ``ConfigError`` branch in ``LLMClient``
    fires deterministically. ``base_url`` and ``anthropic_version`` use
    their module-level defaults when the caller drops the kwarg —
    config is an atomic replace, not a partial update, so an operator
    rotating the key doesn't silently keep a custom endpoint from an
    earlier ``configure_llm`` call.
    """

    canonical_key: Optional[str] = None
    if api_key is not None and str(api_key).strip():
        canonical_key = str(api_key).strip()
    _state["api_key"] = canonical_key
    _state["base_url"] = base_url
    _state["anthropic_version"] = anthropic_version


def clear_llm_config() -> None:
    """Reset module state to factory defaults.

    Used by test fixtures and by ``create_app`` when the operator
    explicitly passes ``llm_api_key=None``. Idempotent."""

    _state["api_key"] = None
    _state["base_url"] = DEFAULT_ANTHROPIC_BASE_URL
    _state["anthropic_version"] = DEFAULT_ANTHROPIC_VERSION


def get_llm_config() -> Dict[str, Any]:
    """Return a shallow copy of the live config.

    Exposed for diagnostic logging, ``/health`` introspection, and the
    app test that asserts ``create_app`` wired the kwarg through.
    Callers must not rely on the returned dict to track future
    mutations — re-call ``get_llm_config`` after a ``configure_llm``."""

    return dict(_state)


# Type alias for a transport callable. Kept narrow on purpose: tests
# inject a recorder, production wires ``_default_transport`` which
# delegates to ``urllib.request.urlopen``.
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
    matching the design choice in ``external_http``. Anthropic returns
    JSON, so the response body is always JSON-decoded; a non-JSON
    response (e.g. a 5xx HTML error page from a misconfigured proxy)
    surfaces as a ``json.JSONDecodeError`` which propagates up as a
    generic runtime failure."""

    import urllib.request  # local import keeps the hot-path import cost off ``configure_llm``

    request = urllib.request.Request(url, data=body, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=timeout) as response:  # noqa: S310 - URL is fixed to configured base
        raw = response.read()
    return _json.loads(raw.decode("utf-8"))


_FENCED_JSON = re.compile(r"^\s*```(?:json)?\s*\n?(.*?)\n?\s*```\s*$", re.DOTALL)


def _strip_markdown_fence(text: str) -> str:
    """Tolerate a single leading ``` or ```json ... ``` fence.

    Anthropic models sometimes wrap structured output in a fenced block
    when asked for "respond as JSON". Stripping at the SDK boundary
    means a Function asking for JSON output doesn't have to teach every
    model the same anti-fence prompt."""

    match = _FENCED_JSON.match(text)
    if match:
        return match.group(1)
    return text


def _flatten_text_blocks(content: Any) -> str:
    """Concatenate ``text`` blocks from an Anthropic Messages response.

    The Messages API returns ``content`` as a list of blocks
    (``text`` / ``tool_use`` / ...). We join consecutive ``text``
    blocks so the caller gets a single string back instead of an array
    they need to flatten; non-text blocks are dropped because Functions
    that opted out of tool use shouldn't have to filter on the consumer
    side."""

    if not isinstance(content, list):
        return ""
    pieces: List[str] = []
    for block in content:
        if not isinstance(block, dict):
            continue
        if block.get("type") != "text":
            continue
        text = block.get("text")
        if isinstance(text, str):
            pieces.append(text)
    return "".join(pieces)


class LLMClient:
    """SDK shim Functions use to call Anthropic Claude models.

    Every ``invoke`` call reads the live config so a late
    ``configure_llm`` (e.g. operator rotates the key) takes effect
    immediately without rebinding the singleton. Tests inject a custom
    transport to assert on the wire shape without dialling out to
    Anthropic.

    The class deliberately exposes only ``invoke``: the BDD spec
    ("invoke a Claude model with a prompt") is the only call shape
    Functions need today, and a narrow surface means there are fewer
    ways for a Function author to accidentally bypass the config check.
    """

    def __init__(self, transport: Optional[TransportCallable] = None):
        self._transport: TransportCallable = transport or _default_transport

    def invoke(
        self,
        *,
        model: str,
        prompt: str,
        system: Optional[str] = None,
        max_tokens: int = DEFAULT_MAX_TOKENS,
        headers: Optional[Mapping[str, str]] = None,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
    ) -> str:
        """Send a single-turn prompt to ``model`` and return the
        concatenated text reply.

        Raises ``ConfigError`` when the API key isn't configured (the
        transport is NOT called), and ``ValueError`` for blank model or
        prompt (same reason — refuse at the boundary so the upstream
        sees nothing nonsensical)."""

        if not isinstance(model, str) or not model.strip():
            raise ValueError("model must be a non-blank string")
        if not isinstance(prompt, str) or not prompt:
            raise ValueError("prompt must be a non-empty string")

        api_key, base_url, anthropic_version = self._resolve_config()
        body, merged_headers = self._build_request(
            model=model.strip(),
            prompt=prompt,
            system=system,
            max_tokens=max_tokens,
            api_key=api_key,
            anthropic_version=anthropic_version,
            extra_headers=headers,
        )
        response = self._transport(
            method="POST",
            url=base_url.rstrip("/") + "/v1/messages",
            headers=merged_headers,
            body=body,
            timeout=timeout,
        )
        if isinstance(response, dict):
            return _flatten_text_blocks(response.get("content"))
        return ""

    @staticmethod
    def _resolve_config() -> Tuple[str, str, str]:
        """Snapshot the active config and enforce the API-key invariant.

        Raises ``ConfigError`` with a message that intentionally names
        the env var operators set (``WEAVE_LLM_API_KEY``) so the failure
        is actionable without digging through wiring code."""

        api_key = _state["api_key"]
        if not api_key:
            raise ConfigError(
                "LLM API key not configured (set WEAVE_LLM_API_KEY or pass "
                "llm_api_key= to create_app before invoking the model)"
            )
        return api_key, _state["base_url"], _state["anthropic_version"]

    @staticmethod
    def _build_request(
        *,
        model: str,
        prompt: str,
        system: Optional[str],
        max_tokens: int,
        api_key: str,
        anthropic_version: str,
        extra_headers: Optional[Mapping[str, str]],
    ) -> Tuple[bytes, Dict[str, str]]:
        payload: Dict[str, Any] = {
            "model": model,
            "max_tokens": int(max_tokens),
            "messages": [{"role": "user", "content": prompt}],
        }
        if system:
            payload["system"] = system
        body = _json.dumps(payload).encode("utf-8")
        headers: Dict[str, str] = {
            "x-api-key": api_key,
            "anthropic-version": anthropic_version,
            "content-type": "application/json",
        }
        if extra_headers:
            for k, v in extra_headers.items():
                headers[k] = v
        return body, headers


# Module-level singleton. Function code imports ``invoke_llm`` from
# ``weave_runtime`` and calls it directly — the config is read at call
# time, so app boot order (``configure_llm`` runs in ``create_app``)
# doesn't matter.
llm_client = LLMClient()


def invoke_llm(
    *,
    model: str,
    prompt: str,
    system: Optional[str] = None,
    max_tokens: int = DEFAULT_MAX_TOKENS,
    headers: Optional[Mapping[str, str]] = None,
    timeout: float = DEFAULT_TIMEOUT_SECONDS,
    transport: Optional[TransportCallable] = None,
) -> str:
    """Module-level shim around ``llm_client.invoke``.

    Function authors import this directly: ``from weave_runtime import
    invoke_llm``. The ``transport`` kwarg exists for tests — production
    callers should leave it ``None`` so the module's singleton (and the
    live config) drives the call."""

    if transport is None:
        return llm_client.invoke(
            model=model,
            prompt=prompt,
            system=system,
            max_tokens=max_tokens,
            headers=headers,
            timeout=timeout,
        )
    ephemeral = LLMClient(transport=transport)
    return ephemeral.invoke(
        model=model,
        prompt=prompt,
        system=system,
        max_tokens=max_tokens,
        headers=headers,
        timeout=timeout,
    )


def invoke_llm_json(
    *,
    model: str,
    prompt: str,
    system: Optional[str] = None,
    max_tokens: int = DEFAULT_MAX_TOKENS,
    headers: Optional[Mapping[str, str]] = None,
    timeout: float = DEFAULT_TIMEOUT_SECONDS,
    transport: Optional[TransportCallable] = None,
) -> Any:
    """Invoke the model, parse the reply as JSON, return the value.

    Markdown ```json ... ``` fences are stripped before parsing because
    Anthropic models sometimes wrap structured output in them. Any
    parse failure raises ``ModelOutputError`` carrying the raw reply on
    ``.raw_text`` — that's the wire contract the VTX-056 BDD spec
    asserts on ('error 传播为 ModelOutputError').
    """

    raw = invoke_llm(
        model=model,
        prompt=prompt,
        system=system,
        max_tokens=max_tokens,
        headers=headers,
        timeout=timeout,
        transport=transport,
    )
    stripped = _strip_markdown_fence(raw)
    try:
        return _json.loads(stripped)
    except (ValueError, TypeError) as exc:
        raise ModelOutputError(
            f"failed to parse model output as JSON: {exc}",
            raw_text=raw,
        ) from exc


__all__ = [
    "ConfigError",
    "DEFAULT_ANTHROPIC_BASE_URL",
    "DEFAULT_ANTHROPIC_VERSION",
    "DEFAULT_MAX_TOKENS",
    "LLMClient",
    "ModelOutputError",
    "clear_llm_config",
    "configure_llm",
    "get_llm_config",
    "invoke_llm",
    "invoke_llm_json",
    "llm_client",
]
