"""Smoke tests for the reference sklearn function shipped with the
runtime (VTX-049 acceptance: "FastAPI + sklearn + 示例 model").

The model is trivial on purpose — these tests assert it round-trips
through the FastAPI handler, validates against the declared pydantic
schema, and produces monotonically larger predictions as the inputs
grow. They explicitly do *not* lock in numeric predictions: that would
make future model retraining a churn-y diff.
"""

from __future__ import annotations

from fastapi.testclient import TestClient

from weave_runtime.app import create_app

# Importing the module registers ``predict_delay`` on the global
# ``registry`` via the ``@register_function`` decorator side effect.
from weave_runtime import example_functions  # noqa: F401 - registration side effect
from weave_runtime.functions import registry


def _client() -> TestClient:
    # ``install_sandbox=False`` because pytest tmp dirs use the same
    # ``/var/folders`` tree the default denylist excludes on macOS;
    # the sandbox isn't relevant to this acceptance.
    app = create_app(registry=registry, install_sandbox=False)
    return TestClient(app)


def test_example_function_is_registered():
    assert registry.has("predict_delay")


def test_predict_delay_round_trips_through_invoke():
    client = _client()
    resp = client.post(
        "/invoke",
        json={
            "function": "predict_delay",
            "inputs": {"distance_km": 1500.0, "weather_severity": 1.0},
        },
    )
    assert resp.status_code == 200, resp.text
    body = resp.json()
    out = body["output"]
    assert isinstance(out["delay_minutes"], float)
    assert out["delay_minutes"] >= 0.0
    assert out["category"] in {"none", "minor", "major"}


def test_predict_delay_rejects_out_of_range_severity():
    client = _client()
    resp = client.post(
        "/invoke",
        json={
            "function": "predict_delay",
            "inputs": {"distance_km": 800.0, "weather_severity": 5.0},
        },
    )
    assert resp.status_code == 422
    detail = resp.json()["detail"]
    assert any("weather_severity" in entry.get("loc", []) for entry in detail)


def test_predict_delay_rejects_negative_distance():
    client = _client()
    resp = client.post(
        "/invoke",
        json={
            "function": "predict_delay",
            "inputs": {"distance_km": -10.0},
        },
    )
    assert resp.status_code == 422


def test_predict_delay_monotonic_in_severity():
    """Higher weather severity should not *decrease* the predicted delay.

    We avoid asserting exact values so retraining the demo data
    doesn't force a test rewrite, but the model would have to be
    catastrophically miscalibrated to fail this monotonicity check."""

    client = _client()

    def predict(severity: float) -> float:
        resp = client.post(
            "/invoke",
            json={
                "function": "predict_delay",
                "inputs": {"distance_km": 1000.0, "weather_severity": severity},
            },
        )
        assert resp.status_code == 200
        return resp.json()["output"]["delay_minutes"]

    clear = predict(0.0)
    stormy = predict(1.0)
    assert stormy >= clear
