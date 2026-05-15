"""TDD tests for the function registry (VTX-049 BDD #1 & #2).

Covers the contract advertised by ``weave_runtime/__init__.py``:
``FunctionSpec`` / ``register_function`` / ``registry``. The FastAPI
layer (``test_app.py``) builds on this by translating registry errors
to the wire envelopes the Go client expects.
"""

from __future__ import annotations

import pytest
from pydantic import BaseModel, ValidationError

from weave_runtime.functions import (
    FunctionRegistry,
    FunctionSpec,
    UnknownFunctionError,
    register_function,
    registry,
)


class DelayInput(BaseModel):
    distance_km: float
    weather: str = "clear"


class DelayOutput(BaseModel):
    delay_minutes: float
    category: str


def test_register_function_stores_spec_in_registry():
    reg = FunctionRegistry()

    @reg.register("noop", input_model=DelayInput, output_model=DelayOutput)
    def noop(inputs: DelayInput) -> DelayOutput:
        return DelayOutput(delay_minutes=0.0, category="none")

    assert reg.has("noop")
    spec = reg.get("noop")
    assert isinstance(spec, FunctionSpec)
    assert spec.name == "noop"
    assert spec.input_model is DelayInput
    assert spec.output_model is DelayOutput
    assert spec.fn is noop


def test_register_duplicate_name_raises():
    reg = FunctionRegistry()

    @reg.register("dup", input_model=DelayInput, output_model=DelayOutput)
    def first(inputs):  # noqa: ARG001 - signature for registry
        return DelayOutput(delay_minutes=0.0, category="none")

    with pytest.raises(ValueError) as ei:

        @reg.register("dup", input_model=DelayInput, output_model=DelayOutput)
        def second(inputs):  # noqa: ARG001
            return DelayOutput(delay_minutes=0.0, category="none")

    assert "already registered" in str(ei.value)


def test_register_blank_name_raises():
    reg = FunctionRegistry()
    with pytest.raises(ValueError):

        @reg.register("   ", input_model=DelayInput, output_model=DelayOutput)
        def f(inputs):  # noqa: ARG001
            return DelayOutput(delay_minutes=0.0, category="none")


def test_names_returns_sorted_iterable():
    reg = FunctionRegistry()

    @reg.register("b", input_model=DelayInput, output_model=DelayOutput)
    def b(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=0.0, category="none")

    @reg.register("a", input_model=DelayInput, output_model=DelayOutput)
    def a(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=0.0, category="none")

    assert list(reg.names()) == ["a", "b"]


def test_invoke_validates_input_and_returns_output_dict():
    reg = FunctionRegistry()

    @reg.register("predict", input_model=DelayInput, output_model=DelayOutput)
    def predict(inputs: DelayInput) -> DelayOutput:
        # Inputs is the validated pydantic instance.
        assert inputs.distance_km == 1200.0
        assert inputs.weather == "stormy"
        return DelayOutput(delay_minutes=12.5, category="minor")

    out = reg.invoke("predict", {"distance_km": 1200.0, "weather": "stormy"})
    assert out == {"delay_minutes": 12.5, "category": "minor"}


def test_invoke_input_validation_error_propagates():
    reg = FunctionRegistry()

    @reg.register("predict", input_model=DelayInput, output_model=DelayOutput)
    def predict(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=1.0, category="x")

    with pytest.raises(ValidationError):
        reg.invoke("predict", {"distance_km": "not-a-number"})


def test_invoke_unknown_function_raises():
    reg = FunctionRegistry()
    with pytest.raises(UnknownFunctionError) as ei:
        reg.invoke("missing", {})
    assert "missing" in str(ei.value)


def test_invoke_output_validation_error_propagates():
    reg = FunctionRegistry()

    class TightOutput(BaseModel):
        score: int

    @reg.register("loose", input_model=DelayInput, output_model=TightOutput)
    def loose(inputs):  # noqa: ARG001
        # Function returned a value the output_model can't coerce.
        return {"score": "not-an-int"}

    with pytest.raises(ValidationError):
        reg.invoke("loose", {"distance_km": 1.0})


def test_invoke_accepts_basemodel_or_dict_returns():
    """Functions may return either the pydantic instance or a plain dict."""

    reg = FunctionRegistry()

    @reg.register("returns_model", input_model=DelayInput, output_model=DelayOutput)
    def fn_model(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=3.0, category="ok")

    @reg.register("returns_dict", input_model=DelayInput, output_model=DelayOutput)
    def fn_dict(inputs):  # noqa: ARG001
        return {"delay_minutes": 4.0, "category": "ok"}

    assert reg.invoke("returns_model", {"distance_km": 1.0})["delay_minutes"] == 3.0
    assert reg.invoke("returns_dict", {"distance_km": 1.0})["delay_minutes"] == 4.0


def test_unregister_removes_entry():
    reg = FunctionRegistry()

    @reg.register("tmp", input_model=DelayInput, output_model=DelayOutput)
    def f(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=1.0, category="x")

    reg.unregister("tmp")
    assert not reg.has("tmp")
    with pytest.raises(UnknownFunctionError):
        reg.invoke("tmp", {"distance_km": 1.0})


def test_global_register_function_uses_module_registry():
    @register_function("global_one", input_model=DelayInput, output_model=DelayOutput)
    def f(inputs):  # noqa: ARG001
        return DelayOutput(delay_minutes=2.0, category="ok")

    try:
        assert registry.has("global_one")
        out = registry.invoke("global_one", {"distance_km": 1.0})
        assert out == {"delay_minutes": 2.0, "category": "ok"}
    finally:
        registry.unregister("global_one")
