"""TDD tests for the LLM SDK shim (VTX-056).

The Vertex LLM Model BDD spec requires Functions to invoke Anthropic
Claude models through a runtime-managed SDK so the API key never
leaves the sidecar process. Tests cover the helper surface (config
mutation, API-key resolution, request shape) and the error semantics
(``ConfigError`` when the key is missing, ``ModelOutputError`` when
JSON parsing the model's text reply fails).

Mirrors the layout of ``test_external_http.py``: a fake transport
records the request the SDK would have sent, an autouse fixture scrubs
module-level state between tests so leakage doesn't silently flip
allow/deny outcomes downstream.
"""

from __future__ import annotations

import json
from typing import Any, Dict, List, Optional

import pytest

from weave_runtime.llm import (
    ConfigError,
    DEFAULT_ANTHROPIC_BASE_URL,
    DEFAULT_ANTHROPIC_VERSION,
    LLMClient,
    ModelOutputError,
    clear_llm_config,
    configure_llm,
    get_llm_config,
    invoke_llm,
    invoke_llm_json,
)


@pytest.fixture(autouse=True)
def _reset_llm_config():
    """Every test starts and ends with the LLM config cleared.

    Module-level state means a leaky test would carry an API key (or a
    custom base URL) into the next case, masking the ConfigError path
    or silently routing requests to a stale endpoint."""

    clear_llm_config()
    yield
    clear_llm_config()


# ---------------------------------------------------------------------------
# configure_llm + get_llm_config
# ---------------------------------------------------------------------------


def test_configure_llm_records_api_key_and_defaults_base_url_and_version():
    configure_llm(api_key="sk-test-123")
    cfg = get_llm_config()
    assert cfg["api_key"] == "sk-test-123"
    assert cfg["base_url"] == DEFAULT_ANTHROPIC_BASE_URL
    assert cfg["anthropic_version"] == DEFAULT_ANTHROPIC_VERSION


def test_configure_llm_allows_custom_base_url_and_version():
    configure_llm(
        api_key="sk-test-456",
        base_url="https://proxy.example/anthropic",
        anthropic_version="2024-12-01",
    )
    cfg = get_llm_config()
    assert cfg["base_url"] == "https://proxy.example/anthropic"
    assert cfg["anthropic_version"] == "2024-12-01"


def test_configure_llm_replaces_previous_values_atomically():
    configure_llm(api_key="sk-a", base_url="https://a.example")
    configure_llm(api_key="sk-b")
    cfg = get_llm_config()
    assert cfg["api_key"] == "sk-b"
    # Reset to default base_url when caller drops the kwarg — config is
    # atomic replace, not partial update, so an operator rotation does
    # not silently keep the prior endpoint.
    assert cfg["base_url"] == DEFAULT_ANTHROPIC_BASE_URL


def test_configure_llm_blank_api_key_is_treated_as_unset():
    """Whitespace-only keys would slip past a ``if api_key:`` guard at
    invoke time; canonicalise to None at configure time so the
    ``ConfigError`` branch fires deterministically."""

    configure_llm(api_key="   ")
    assert get_llm_config()["api_key"] is None


def test_clear_llm_config_returns_to_empty_state():
    configure_llm(api_key="sk-x", base_url="https://x.example")
    clear_llm_config()
    cfg = get_llm_config()
    assert cfg["api_key"] is None
    assert cfg["base_url"] == DEFAULT_ANTHROPIC_BASE_URL
    assert cfg["anthropic_version"] == DEFAULT_ANTHROPIC_VERSION


# ---------------------------------------------------------------------------
# LLMClient.invoke — request shape + happy path
# ---------------------------------------------------------------------------


class _RecordingTransport:
    """Records the request the SDK would have sent.

    The real transport hits the Anthropic API; tests inject this so a
    passing case asserts on the wire shape without a live API key, and
    a failing case (missing config) never reaches the transport at all.
    """

    def __init__(self, response: Optional[Dict[str, Any]] = None):
        self.calls: List[Dict[str, Any]] = []
        self._response = response if response is not None else {
            "content": [{"type": "text", "text": "ok"}],
        }

    def __call__(
        self,
        *,
        method: str,
        url: str,
        headers: Dict[str, str],
        body: Optional[bytes],
        timeout: float,
    ) -> Any:
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


def test_invoke_with_api_key_forwards_to_anthropic_messages_endpoint():
    """BDD #1 — ``invokeLLM`` posts to the Anthropic Messages API.

    Verifies URL, method, body shape, headers (auth, version,
    content-type)."""

    configure_llm(api_key="sk-test")
    transport = _RecordingTransport(response={
        "content": [{"type": "text", "text": "hello"}],
    })
    client = LLMClient(transport=transport)

    out = client.invoke(model="claude-haiku-4-5", prompt="say hello")

    assert out == "hello"
    assert len(transport.calls) == 1
    call = transport.calls[0]
    assert call["method"] == "POST"
    assert call["url"] == DEFAULT_ANTHROPIC_BASE_URL + "/v1/messages"
    assert call["headers"]["x-api-key"] == "sk-test"
    assert call["headers"]["anthropic-version"] == DEFAULT_ANTHROPIC_VERSION
    assert call["headers"]["content-type"] == "application/json"
    body = json.loads(call["body"].decode("utf-8"))
    assert body["model"] == "claude-haiku-4-5"
    assert body["messages"] == [{"role": "user", "content": "say hello"}]
    assert isinstance(body.get("max_tokens"), int)


def test_invoke_uses_custom_base_url_when_configured():
    configure_llm(api_key="sk-test", base_url="https://proxy.example/anthropic")
    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    client.invoke(model="claude-haiku-4-5", prompt="x")

    assert transport.calls[0]["url"] == "https://proxy.example/anthropic/v1/messages"


def test_invoke_includes_system_prompt_when_provided():
    configure_llm(api_key="sk-test")
    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    client.invoke(model="claude-haiku-4-5", prompt="hi", system="be terse")

    body = json.loads(transport.calls[0]["body"].decode("utf-8"))
    assert body["system"] == "be terse"


def test_invoke_omits_system_field_when_not_provided():
    configure_llm(api_key="sk-test")
    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    client.invoke(model="claude-haiku-4-5", prompt="hi")

    body = json.loads(transport.calls[0]["body"].decode("utf-8"))
    assert "system" not in body


def test_invoke_honours_max_tokens_override():
    configure_llm(api_key="sk-test")
    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    client.invoke(model="claude-haiku-4-5", prompt="hi", max_tokens=42)

    body = json.loads(transport.calls[0]["body"].decode("utf-8"))
    assert body["max_tokens"] == 42


def test_invoke_concatenates_multiple_text_blocks_in_response():
    """The Anthropic Messages API returns ``content`` as a list of
    blocks; the SDK joins consecutive ``text`` blocks so callers get a
    single string back instead of an array they need to flatten."""

    configure_llm(api_key="sk-test")
    transport = _RecordingTransport(response={
        "content": [
            {"type": "text", "text": "hello "},
            {"type": "text", "text": "world"},
        ],
    })
    client = LLMClient(transport=transport)

    out = client.invoke(model="claude-haiku-4-5", prompt="x")

    assert out == "hello world"


def test_invoke_ignores_non_text_content_blocks_in_response():
    """Tool-use blocks (or any non-text type) must not leak into the
    flattened reply — Functions that opted out of tool use shouldn't
    have to filter on the consumer side."""

    configure_llm(api_key="sk-test")
    transport = _RecordingTransport(response={
        "content": [
            {"type": "text", "text": "answer"},
            {"type": "tool_use", "id": "x", "name": "foo", "input": {}},
        ],
    })
    client = LLMClient(transport=transport)

    out = client.invoke(model="claude-haiku-4-5", prompt="x")

    assert out == "answer"


# ---------------------------------------------------------------------------
# Config errors
# ---------------------------------------------------------------------------


def test_invoke_without_api_key_raises_config_error_without_calling_transport():
    """BDD #2 — missing API key surfaces as ``ConfigError`` BEFORE the
    transport runs (so a misconfigured runtime doesn't leak a request
    to the upstream)."""

    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    with pytest.raises(ConfigError) as ei:
        client.invoke(model="claude-haiku-4-5", prompt="hi")

    assert "api key" in str(ei.value).lower()
    assert transport.calls == []


def test_invoke_with_blank_model_raises_value_error():
    """A blank model would silently coerce to ``"None"`` in the JSON
    body; refuse at the boundary."""

    configure_llm(api_key="sk-test")
    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    with pytest.raises(ValueError):
        client.invoke(model="  ", prompt="hi")
    assert transport.calls == []


def test_invoke_with_blank_prompt_raises_value_error():
    configure_llm(api_key="sk-test")
    transport = _RecordingTransport()
    client = LLMClient(transport=transport)

    with pytest.raises(ValueError):
        client.invoke(model="claude-haiku-4-5", prompt="")
    assert transport.calls == []


# ---------------------------------------------------------------------------
# Module-level shim + late configure
# ---------------------------------------------------------------------------


def test_invoke_llm_shim_uses_module_singleton_so_late_configure_still_applies():
    """The singleton ``invoke_llm`` re-reads the live config on every
    call — calling ``configure_llm`` *after* a previous denied call
    must still let the next call succeed."""

    transport = _RecordingTransport(response={
        "content": [{"type": "text", "text": "ok"}],
    })

    # Before configure: ConfigError.
    with pytest.raises(ConfigError):
        invoke_llm(model="claude-haiku-4-5", prompt="hi", transport=transport)

    configure_llm(api_key="sk-test")
    out = invoke_llm(model="claude-haiku-4-5", prompt="hi", transport=transport)
    assert out == "ok"


# ---------------------------------------------------------------------------
# invoke_llm_json — structured-output parsing
# ---------------------------------------------------------------------------


def test_invoke_llm_json_parses_structured_reply_into_dict():
    configure_llm(api_key="sk-test")
    transport = _RecordingTransport(response={
        "content": [{"type": "text", "text": '{"answer": 42, "tag": "ok"}'}],
    })

    out = invoke_llm_json(model="claude-haiku-4-5", prompt="x", transport=transport)

    assert out == {"answer": 42, "tag": "ok"}


def test_invoke_llm_json_raises_model_output_error_on_unstructured_text():
    """BDD #3 — when the model emits free text the function's parser
    should propagate a ``ModelOutputError`` rather than a raw
    ``json.JSONDecodeError``. Functions catching ``ModelOutputError``
    can branch on retry / fallback without depending on stdlib internals."""

    configure_llm(api_key="sk-test")
    transport = _RecordingTransport(response={
        "content": [{"type": "text", "text": "I'm sorry, I cannot comply."}],
    })

    with pytest.raises(ModelOutputError) as ei:
        invoke_llm_json(model="claude-haiku-4-5", prompt="x", transport=transport)
    # The raw text is preserved on the exception so a Function (or an
    # operator combing logs) can see what the model actually said.
    assert "cannot comply" in ei.value.raw_text


def test_invoke_llm_json_strips_markdown_fence_before_parsing():
    """Anthropic sometimes wraps JSON in ```json ... ``` fences when
    asked for structured output. The helper tolerates a single leading
    fenced block so Functions don't have to teach every model the same
    'no fence' instruction."""

    configure_llm(api_key="sk-test")
    transport = _RecordingTransport(response={
        "content": [{"type": "text", "text": "```json\n{\"x\": 1}\n```"}],
    })

    out = invoke_llm_json(model="claude-haiku-4-5", prompt="x", transport=transport)

    assert out == {"x": 1}


def test_model_output_error_is_value_error_subclass():
    """A 5xx-shaped envelope at the HTTP boundary depends on the runtime
    treating ModelOutputError as a generic failure; anchor the class
    hierarchy so a future refactor doesn't accidentally make it a
    ``PermissionError`` (which would change the wire status)."""

    assert issubclass(ModelOutputError, ValueError)


def test_config_error_is_value_error_subclass():
    assert issubclass(ConfigError, ValueError)
