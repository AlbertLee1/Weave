"""TDD tests for the FastAPI runtime app (VTX-049 BDD #1–#3).

The app routes ``POST /invoke`` → registry, translating registry
exceptions into the typed error envelopes the Go side
(``pkg/vertex/funcruntime``) decodes. Each test constructs an isolated
``FunctionRegistry`` so global state from example models doesn't leak.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from pydantic import BaseModel

from weave_runtime.app import create_app
from weave_runtime.external_http import (
    ForbiddenExternalCall,
    configure_allowed_domains,
    get_allowed_domains,
)
from weave_runtime.functions import FunctionRegistry
from weave_runtime.sandbox import (
    SandboxViolation,
    install_filesystem_sandbox,
    uninstall_filesystem_sandbox,
)


class DelayInput(BaseModel):
    distance_km: float
    weather: str = "clear"


class DelayOutput(BaseModel):
    delay_minutes: float
    category: str


@pytest.fixture(autouse=True)
def _sandbox_teardown():
    yield
    uninstall_filesystem_sandbox()
    # VTX-055: the app may configure the external-HTTP allowlist at
    # construction; reset between tests so module state doesn't leak.
    configure_allowed_domains([])


def _make_client(reg: FunctionRegistry, install_sandbox: bool = False) -> TestClient:
    app = create_app(registry=reg, install_sandbox=install_sandbox)
    return TestClient(app)


def test_invoke_returns_200_with_output_envelope():
    reg = FunctionRegistry()

    @reg.register("predict", input_model=DelayInput, output_model=DelayOutput)
    def predict(inputs):
        return DelayOutput(delay_minutes=12.5, category="minor")

    client = _make_client(reg)
    resp = client.post(
        "/invoke",
        json={"function": "predict", "inputs": {"distance_km": 1200.0, "weather": "stormy"}},
    )
    assert resp.status_code == 200, resp.text
    assert resp.json() == {"output": {"delay_minutes": 12.5, "category": "minor"}}


def test_invoke_unknown_function_returns_404():
    reg = FunctionRegistry()
    client = _make_client(reg)
    resp = client.post("/invoke", json={"function": "missing", "inputs": {}})
    assert resp.status_code == 404
    body = resp.json()
    assert "missing" in body["detail"]


def test_invoke_input_type_mismatch_returns_422_with_field_errors():
    reg = FunctionRegistry()

    @reg.register("predict", input_model=DelayInput, output_model=DelayOutput)
    def predict(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=1.0, category="x")

    client = _make_client(reg)
    resp = client.post(
        "/invoke",
        json={"function": "predict", "inputs": {"distance_km": "not-a-number"}},
    )
    assert resp.status_code == 422, resp.text
    body = resp.json()
    assert isinstance(body["detail"], list)
    assert len(body["detail"]) >= 1
    first = body["detail"][0]
    assert "loc" in first
    assert "msg" in first
    assert "distance_km" in first["loc"]


def test_invoke_missing_required_field_returns_422():
    reg = FunctionRegistry()

    class StrictInput(BaseModel):
        a: float
        b: float

    @reg.register("sum", input_model=StrictInput, output_model=DelayOutput)
    def s(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=0.0, category="ok")

    client = _make_client(reg)
    resp = client.post("/invoke", json={"function": "sum", "inputs": {"a": 1.0}})
    assert resp.status_code == 422
    detail = resp.json()["detail"]
    assert any("b" in entry.get("loc", []) for entry in detail)


def test_invoke_function_raising_sandbox_violation_returns_403():
    reg = FunctionRegistry()

    @reg.register("leak", input_model=DelayInput, output_model=DelayOutput)
    def leak(inputs):  # noqa: ARG001
        raise SandboxViolation("/etc/passwd", "path outside sandbox")

    client = _make_client(reg)
    resp = client.post("/invoke", json={"function": "leak", "inputs": {"distance_km": 1.0}})
    assert resp.status_code == 403
    body = resp.json()
    assert body["code"] == "ForbiddenFileAccess"
    assert "/etc/passwd" in body["detail"]


def test_invoke_function_raising_generic_exception_returns_500():
    reg = FunctionRegistry()

    @reg.register("boom", input_model=DelayInput, output_model=DelayOutput)
    def boom(inputs):  # noqa: ARG001
        raise ZeroDivisionError("divide by zero")

    client = _make_client(reg)
    resp = client.post("/invoke", json={"function": "boom", "inputs": {"distance_km": 1.0}})
    assert resp.status_code == 500
    body = resp.json()
    assert "divide by zero" in body["detail"]
    assert body["code"] == "ZeroDivisionError"


def test_invoke_request_missing_function_field_returns_422():
    # FastAPI's own request-body validator should fire here.
    reg = FunctionRegistry()
    client = _make_client(reg)
    resp = client.post("/invoke", json={"inputs": {}})
    assert resp.status_code == 422


def test_health_returns_function_names():
    reg = FunctionRegistry()

    @reg.register("alpha", input_model=DelayInput, output_model=DelayOutput)
    def alpha(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=0.0, category="ok")

    client = _make_client(reg)
    resp = client.get("/health")
    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok"
    assert body["functions"] == ["alpha"]


def test_sandbox_install_blocks_function_reading_etc_passwd(tmp_path):
    """End-to-end BDD #3: a function trying to read a denied path is
    rejected by the sandbox installed by ``create_app``.

    Uses ``tmp_path`` + an explicit ``try/finally`` around the sandbox
    so pytest's tmp_path cleanup runs after ``uninstall``; otherwise
    ``shutil.rmtree`` would hit the guard at session teardown and the
    suite would log noisy recursion errors."""

    import os

    secret = tmp_path / "secret.txt"
    secret.write_text("x")

    reg = FunctionRegistry()
    install_filesystem_sandbox(denylist=[str(tmp_path)])
    try:

        @reg.register("read_secret", input_model=DelayInput, output_model=DelayOutput)
        def read_secret(inputs):  # noqa: ARG001
            # SandboxViolation propagates up through invoke and the
            # FastAPI exception handler.
            with open(secret) as f:
                f.read()
            return DelayOutput(delay_minutes=0.0, category="never")

        client = _make_client(reg, install_sandbox=False)
        resp = client.post(
            "/invoke",
            json={"function": "read_secret", "inputs": {"distance_km": 1.0}},
        )
        assert resp.status_code == 403
        body = resp.json()
        assert body["code"] == "ForbiddenFileAccess"
        # Detail mentions either the temp dir prefix or the leaf file.
        assert str(tmp_path) in body["detail"] or "secret" in body["detail"]
    finally:
        uninstall_filesystem_sandbox()


def test_invoke_function_raising_forbidden_external_call_returns_403_with_typed_code():
    """VTX-055 BDD #2 — function code that tries to dial a host outside
    the allowlist surfaces as a typed 403 envelope. Distinct ``code``
    from the filesystem sandbox so the Go side can branch on it."""

    reg = FunctionRegistry()

    @reg.register("call_external", input_model=DelayInput, output_model=DelayOutput)
    def call_external(inputs):  # noqa: ARG001
        raise ForbiddenExternalCall("untrusted.example.com")

    client = _make_client(reg)
    resp = client.post(
        "/invoke",
        json={"function": "call_external", "inputs": {"distance_km": 1.0}},
    )
    assert resp.status_code == 403, resp.text
    body = resp.json()
    assert body["code"] == "ForbiddenExternalCall"
    assert "untrusted.example.com" in body["detail"]


def test_create_app_configures_external_http_allowlist_from_kwarg():
    """VTX-055 BDD #1 — operators inject ``config.allowedExternalDomains``
    at app construction; the module-level allowlist reflects it so any
    ``http_client.post`` call from inside a function sees the policy."""

    reg = FunctionRegistry()
    create_app(
        registry=reg,
        install_sandbox=False,
        allowed_external_domains=["external-api.test", "predict.ai"],
    )
    assert get_allowed_domains() == ("external-api.test", "predict.ai")


def test_invoke_output_validation_failure_returns_422():
    reg = FunctionRegistry()

    class TightOutput(BaseModel):
        score: int

    @reg.register("misbehaving", input_model=DelayInput, output_model=TightOutput)
    def misbehaving(inputs):  # noqa: ARG001
        # Returns wrong shape; output validation must reject it.
        return {"score": "not-an-int"}

    client = _make_client(reg)
    resp = client.post("/invoke", json={"function": "misbehaving", "inputs": {"distance_km": 1.0}})
    # Output mismatch is also a pydantic ValidationError → 422.
    assert resp.status_code == 422
    detail = resp.json()["detail"]
    assert any("score" in entry.get("loc", []) for entry in detail)
