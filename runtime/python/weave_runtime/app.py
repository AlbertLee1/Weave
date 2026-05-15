"""FastAPI app for the Vertex Python function runtime (VTX-049).

The HTTP surface is intentionally narrow — see ``pkg/vertex/funcruntime``
for the Go-side wire contract:

    POST /invoke
        Body:  {"function": "...", "inputs": {...}}
        200 →  {"output": <jsonable>}
        404 →  {"detail": "..."}                        (unknown function)
        422 →  {"detail": [{"loc": [...], "msg": "...", "type": "..."}]}
        403 →  {"detail": "...", "code": "ForbiddenFileAccess"}      (sandbox)
        403 →  {"detail": "...", "code": "ForbiddenExternalCall"}    (allowlist)
        5xx →  {"detail": "...", "code": "<ExceptionClass>"}

    GET /health  →  200 {"status": "ok", "functions": [...]}

Each invocation goes through the ``FunctionRegistry``: input is
validated against ``input_model``, the function runs inside the
process-wide filesystem sandbox installed at app boot, output is
validated against ``output_model``. Failures at any step are mapped
to typed envelopes so the Go client can decode them without re-parsing
JSON ad hoc.
"""

from __future__ import annotations

import os
from typing import Any, Dict, Iterable, List, Optional

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field, ValidationError

from .external_http import (
    ForbiddenExternalCall,
    configure_allowed_domains,
)
from .functions import FunctionRegistry, UnknownFunctionError, registry as default_registry
from .llm import clear_llm_config, configure_llm
from .sandbox import SandboxViolation, install_filesystem_sandbox


class InvokeRequest(BaseModel):
    """Wire body for ``POST /invoke``.

    ``inputs`` defaults to ``{}`` so callers that pass ``{"function":
    "f"}`` with no inputs land in the same code path as those that
    pass an empty object — both forms are common in clients that
    serialise from a non-optional map type.
    """

    function: str = Field(..., min_length=1)
    inputs: Dict[str, Any] = Field(default_factory=dict)


def _format_validation_errors(err: ValidationError) -> List[Dict[str, Any]]:
    """Map a pydantic ``ValidationError`` to the wire ``detail`` array.

    Loc tuples are flattened to ``["body", "inputs", <field>, ...]`` so
    the shape matches what FastAPI emits when its own request-body
    validator fires — keeping the Go client's parser uniform across
    "bad envelope" vs. "bad inputs/outputs" error sources.
    """

    details: List[Dict[str, Any]] = []
    for entry in err.errors():
        loc = list(entry.get("loc", ()))
        loc_strings = [str(part) for part in loc]
        details.append(
            {
                "loc": ["body", "inputs", *loc_strings],
                "msg": str(entry.get("msg", "")),
                "type": str(entry.get("type", "")),
            }
        )
    return details


def _resolve_allowed_external_domains(
    explicit: Optional[Iterable[str]],
) -> List[str]:
    """Pick the active allowlist source for ``create_app``.

    Precedence: explicit kwarg > ``WEAVE_ALLOWED_EXTERNAL_DOMAINS``
    env var (comma-separated) > empty (deny everything). Centralised
    here so tests can assert on either source without duplicating the
    parse logic.
    """

    if explicit is not None:
        return [d for d in explicit if d and d.strip()]
    raw = os.environ.get("WEAVE_ALLOWED_EXTERNAL_DOMAINS", "")
    return [piece.strip() for piece in raw.split(",") if piece.strip()]


def _resolve_llm_api_key(explicit: Optional[str]) -> Optional[str]:
    """Pick the active LLM API key source for ``create_app``.

    Precedence: explicit kwarg > ``WEAVE_LLM_API_KEY`` env var >
    ``ANTHROPIC_API_KEY`` env var > ``None``. Two env vars are honoured
    so operators who already export ``ANTHROPIC_API_KEY`` for other
    tooling don't have to duplicate the value under a Weave-specific
    name; the Weave-specific name wins when both are set so a
    deployment-level override is unambiguous.
    """

    if explicit is not None and str(explicit).strip():
        return str(explicit).strip()
    for env in ("WEAVE_LLM_API_KEY", "ANTHROPIC_API_KEY"):
        raw = os.environ.get(env, "")
        if raw and raw.strip():
            return raw.strip()
    return None


def create_app(
    *,
    registry: Optional[FunctionRegistry] = None,
    install_sandbox: bool = True,
    allowed_external_domains: Optional[Iterable[str]] = None,
    llm_api_key: Optional[str] = None,
) -> FastAPI:
    """Construct a FastAPI app bound to ``registry``.

    ``install_sandbox=True`` (default) installs the process-wide
    filesystem sandbox at app construction. Tests that want to skip
    the patch pass ``install_sandbox=False`` and either install the
    sandbox manually with a narrow denylist or rely on the BDD #3
    end-to-end test for coverage.

    ``allowed_external_domains`` configures the VTX-055 outbound-HTTP
    allowlist. ``None`` (default) falls back to
    ``WEAVE_ALLOWED_EXTERNAL_DOMAINS``; an empty iterable explicitly
    denies every external host.

    ``llm_api_key`` configures the VTX-056 LLM SDK. ``None`` (default)
    falls back to ``WEAVE_LLM_API_KEY`` / ``ANTHROPIC_API_KEY`` env
    vars; nothing configured means ``invoke_llm`` raises ``ConfigError``
    until the operator sets the key. Stored on app state for diagnostic
    introspection.
    """

    reg = registry if registry is not None else default_registry
    if install_sandbox:
        install_filesystem_sandbox()
    configure_allowed_domains(_resolve_allowed_external_domains(allowed_external_domains))
    resolved_llm_key = _resolve_llm_api_key(llm_api_key)
    if resolved_llm_key:
        configure_llm(api_key=resolved_llm_key)
    else:
        clear_llm_config()

    app = FastAPI(title="Weave Vertex Function Runtime", version="0.1.0")
    app.state.registry = reg

    @app.exception_handler(RequestValidationError)
    async def _handle_request_validation(_: Request, exc: RequestValidationError):
        # FastAPI emits ``loc`` as a tuple starting with ``"body"``;
        # preserve it as-is so the Go side sees a consistent shape.
        details = []
        for entry in exc.errors():
            details.append(
                {
                    "loc": [str(part) for part in entry.get("loc", ())],
                    "msg": str(entry.get("msg", "")),
                    "type": str(entry.get("type", "")),
                }
            )
        return JSONResponse(status_code=422, content={"detail": details})

    @app.post("/invoke")
    async def invoke(body: InvokeRequest):
        try:
            output = reg.invoke(body.function, body.inputs)
        except UnknownFunctionError as e:
            return JSONResponse(status_code=404, content={"detail": str(e)})
        except ValidationError as e:
            return JSONResponse(
                status_code=422,
                content={"detail": _format_validation_errors(e)},
            )
        except SandboxViolation as e:
            return JSONResponse(
                status_code=403,
                content={"detail": str(e), "code": "ForbiddenFileAccess"},
            )
        except ForbiddenExternalCall as e:
            return JSONResponse(
                status_code=403,
                content={"detail": str(e), "code": "ForbiddenExternalCall"},
            )
        except Exception as e:  # noqa: BLE001 - blanket on purpose; see below
            # Any other exception inside the function body becomes a
            # 5xx with the class name as ``code`` so an operator
            # grepping logs has a typed handle on the failure. The
            # message is the exception's ``str()`` — function authors
            # are responsible for not leaking secrets into it.
            return JSONResponse(
                status_code=500,
                content={"detail": str(e), "code": type(e).__name__},
            )
        return {"output": output}

    @app.get("/health")
    async def health():
        return {"status": "ok", "functions": list(reg.names())}

    return app


# Module-level app used by ``uvicorn weave_runtime.app:app`` in the
# default deployment. It binds to the global ``registry`` so any
# ``register_function`` decorators evaluated at import time wire
# through to this app's dispatcher.
app = create_app()


__all__ = ["create_app", "app", "InvokeRequest"]
