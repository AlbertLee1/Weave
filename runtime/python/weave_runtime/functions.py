"""Function registry for the Vertex Python runtime (VTX-049 BDD #1, #2).

A ``FunctionRegistry`` owns a name → ``FunctionSpec`` map. Each spec
ties a callable to a pair of pydantic models (``input_model`` /
``output_model``) so the runtime can validate both directions at the
sandbox boundary without trusting function authors to handle bad input.

The module also exposes a default ``registry`` singleton plus a
``register_function`` decorator. Example functions and ad-hoc tests
import these directly; ``FunctionRegistry`` exists as a class so the
FastAPI test client can spin up a fresh runtime per test without
polluting the global state.
"""

from __future__ import annotations

import threading
from dataclasses import dataclass
from typing import Any, Callable, Dict, Iterator, Mapping, Type

from pydantic import BaseModel


class UnknownFunctionError(KeyError):
    """Raised by ``FunctionRegistry.invoke`` when ``name`` isn't registered.

    Inherits ``KeyError`` so callers that already catch ``KeyError``
    still see this — but the FastAPI handler in ``app.py`` uses
    ``isinstance`` to map it to a typed 404 envelope.
    """

    def __init__(self, name: str):
        self.name = name
        super().__init__(f"function {name!r} is not registered")


@dataclass(frozen=True)
class FunctionSpec:
    """Immutable description of a registered function.

    ``fn`` receives the validated ``input_model`` instance and returns
    either a validated ``output_model`` instance or a plain dict the
    ``output_model`` can coerce. The registry — not the function —
    owns I/O validation, so a misbehaving function still surfaces
    typed pydantic errors instead of arbitrary tracebacks.
    """

    name: str
    fn: Callable[[BaseModel], Any]
    input_model: Type[BaseModel]
    output_model: Type[BaseModel]


class FunctionRegistry:
    """Thread-safe name → ``FunctionSpec`` map."""

    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._specs: Dict[str, FunctionSpec] = {}

    def register(
        self,
        name: str,
        *,
        input_model: Type[BaseModel],
        output_model: Type[BaseModel],
    ) -> Callable[[Callable[[BaseModel], Any]], Callable[[BaseModel], Any]]:
        """Decorator that stores the wrapped function under ``name``.

        Returns the original function unchanged so callers can still
        call it directly (handy in tests). Duplicate registrations and
        blank names raise ``ValueError`` at decoration time — failing
        loudly at app boot beats a confusing dispatch miss later.
        """

        trimmed = (name or "").strip()
        if not trimmed:
            raise ValueError("function name must be non-blank")

        def decorator(fn: Callable[[BaseModel], Any]) -> Callable[[BaseModel], Any]:
            with self._lock:
                if trimmed in self._specs:
                    raise ValueError(f"function {trimmed!r} already registered")
                self._specs[trimmed] = FunctionSpec(
                    name=trimmed,
                    fn=fn,
                    input_model=input_model,
                    output_model=output_model,
                )
            return fn

        return decorator

    def unregister(self, name: str) -> None:
        """Remove ``name`` if present; silent when absent.

        Test fixtures use this to scrub the module-level ``registry``
        after a test that registered against it. Silent on absence so
        teardown can call it unconditionally.
        """

        with self._lock:
            self._specs.pop(name, None)

    def has(self, name: str) -> bool:
        with self._lock:
            return name in self._specs

    def get(self, name: str) -> FunctionSpec:
        with self._lock:
            if name not in self._specs:
                raise UnknownFunctionError(name)
            return self._specs[name]

    def names(self) -> Iterator[str]:
        with self._lock:
            # Return a sorted snapshot so /health output is stable
            # across runs.
            return iter(sorted(self._specs.keys()))

    def invoke(self, name: str, inputs: Mapping[str, Any]) -> Dict[str, Any]:
        """Validate, dispatch, validate, return.

        - Looks up ``name`` (raises ``UnknownFunctionError`` if missing).
        - Validates ``inputs`` against the spec's ``input_model``
          (raises ``pydantic.ValidationError`` on type mismatch).
        - Calls ``spec.fn(validated_instance)``.
        - Validates the result against ``output_model``: passes
          ``BaseModel`` instances through, coerces dicts and other
          mappings, anything else raises ``pydantic.ValidationError``.
        - Returns the output dump (``model_dump()``) so the FastAPI
          handler can put it under ``{"output": ...}`` without
          double-serialising.
        """

        spec = self.get(name)
        validated_input = spec.input_model.model_validate(dict(inputs))
        raw_output = spec.fn(validated_input)

        if isinstance(raw_output, spec.output_model):
            validated_output = raw_output
        elif isinstance(raw_output, BaseModel):
            # Function returned a different pydantic model — coerce
            # via dump so the output_model can validate.
            validated_output = spec.output_model.model_validate(
                raw_output.model_dump()
            )
        else:
            validated_output = spec.output_model.model_validate(raw_output)

        return validated_output.model_dump()


# Module-level default registry. Example models / scripts use this via
# the ``register_function`` shim; tests that need isolation construct
# their own ``FunctionRegistry()``.
registry = FunctionRegistry()


def register_function(
    name: str,
    *,
    input_model: Type[BaseModel],
    output_model: Type[BaseModel],
) -> Callable[[Callable[[BaseModel], Any]], Callable[[BaseModel], Any]]:
    """Decorator shim that registers against the module-level ``registry``."""

    return registry.register(name, input_model=input_model, output_model=output_model)


__all__ = [
    "FunctionRegistry",
    "FunctionSpec",
    "UnknownFunctionError",
    "register_function",
    "registry",
]
