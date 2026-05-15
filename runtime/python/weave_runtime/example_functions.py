"""Reference functions shipped with the Vertex Python runtime.

The Weave Vertex PRD (VTX-049) requires a working sklearn example in
``runtime/python/`` so operators can smoke-test the sandbox end-to-end
without writing their own model. ``predict_delay`` is intentionally
trivial — a fitted ``LinearRegression`` over two features — so the
acceptance test stays deterministic without a frozen pickle on disk.

Importing this module registers the example functions on the
module-level ``registry`` from ``weave_runtime.functions``. ``app.py``
imports it lazily on first ``/invoke`` request through the registry
binding, so tests that build their own ``FunctionRegistry`` stay
isolated.
"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field
from sklearn.linear_model import LinearRegression

from .functions import register_function

# Categories the example model surfaces. Kept as a Literal so the
# output_model both documents the allowed values and gets pydantic to
# reject any future drift if a function author edits the math.
DelayCategory = Literal["none", "minor", "major"]


class DelayPredictionInput(BaseModel):
    """Inputs accepted by ``predict_delay``.

    ``distance_km`` is the leg length; ``weather_severity`` is a
    0..1 scalar where 0 = clear, 1 = severe storm. Both fields are
    constrained so a misconfigured Scenario surfaces a pydantic 422
    rather than an undefined sklearn prediction.
    """

    distance_km: float = Field(ge=0.0, le=20_000.0)
    weather_severity: float = Field(ge=0.0, le=1.0, default=0.0)


class DelayPredictionOutput(BaseModel):
    """Result returned by ``predict_delay``.

    ``delay_minutes`` is the predicted scalar; ``category`` is a
    quantised bucket derived from it. The Go side (and any caller
    that wires the output to a property override) gets both fields
    so it can pick the granularity it needs.
    """

    delay_minutes: float
    category: DelayCategory


# Train once at import time so each /invoke call doesn't repay the
# fit cost. The training set is hand-picked synthetic data — enough
# rows for the model to produce a non-trivial slope, but small
# enough that the test suite imports in milliseconds.
_TRAIN_X = [
    # (distance_km, weather_severity)
    [200.0, 0.0],
    [800.0, 0.0],
    [1500.0, 0.0],
    [200.0, 0.5],
    [1500.0, 0.5],
    [200.0, 1.0],
    [800.0, 1.0],
    [1500.0, 1.0],
]
_TRAIN_Y = [
    # delay_minutes
    2.0,
    5.0,
    9.0,
    8.0,
    21.0,
    18.0,
    32.0,
    45.0,
]


def _train_demo_model() -> LinearRegression:
    model = LinearRegression()
    model.fit(_TRAIN_X, _TRAIN_Y)
    return model


_demo_model: LinearRegression = _train_demo_model()


def _bucket(delay_minutes: float) -> DelayCategory:
    """Quantise a continuous prediction to a Vertex-friendly bucket.

    Boundaries are an editorial choice; the test suite locks them in
    so future tuning is conscious."""

    if delay_minutes < 5.0:
        return "none"
    if delay_minutes < 30.0:
        return "minor"
    return "major"


@register_function(
    "predict_delay",
    input_model=DelayPredictionInput,
    output_model=DelayPredictionOutput,
)
def predict_delay(inputs: DelayPredictionInput) -> DelayPredictionOutput:
    """Reference sklearn-backed Vertex function.

    Takes a flight-leg length + weather severity and returns a
    predicted delay in minutes plus a categorical bucket. The model
    is a trivial ``LinearRegression`` trained at import time — good
    enough to demonstrate the wire path without smuggling a 10 MB
    pickle into the repo.
    """

    features = [[inputs.distance_km, inputs.weather_severity]]
    delay = float(_demo_model.predict(features)[0])
    # Clamp at zero — the regression line can dip negative for
    # short, clear-weather legs and a negative delay would be
    # nonsense at the Scenario layer.
    delay = max(delay, 0.0)
    return DelayPredictionOutput(delay_minutes=delay, category=_bucket(delay))


__all__ = [
    "DelayCategory",
    "DelayPredictionInput",
    "DelayPredictionOutput",
    "predict_delay",
]
