"""Weave Vertex Python function runtime (VTX-049).

A FastAPI + pydantic + sklearn sidecar that owns function execution
and the filesystem sandbox boundary. The Go side
(``pkg/vertex/funcruntime``) forwards invocations here over HTTP — see
that package for the wire contract.

The default deployment runs ``uvicorn weave_runtime.app:app`` against
the module-level ``registry``. Operators register functions in their
own module that imports ``register_function`` from here, then ensure
that module is imported before uvicorn boots (usually by listing it
under ``app.on_event('startup')`` in a wrapper, or by importing it at
the top of a custom ``run.py``).
"""

from .app import app, create_app
from .external_http import (
    ForbiddenExternalCall,
    HttpClient,
    configure_allowed_domains,
    get_allowed_domains,
    http_client,
    is_domain_allowed,
)
from .functions import (
    FunctionRegistry,
    FunctionSpec,
    UnknownFunctionError,
    register_function,
    registry,
)
from .sandbox import (
    SandboxViolation,
    install_filesystem_sandbox,
    is_path_denied,
    uninstall_filesystem_sandbox,
)

__all__ = [
    "FunctionRegistry",
    "FunctionSpec",
    "UnknownFunctionError",
    "register_function",
    "registry",
    "SandboxViolation",
    "install_filesystem_sandbox",
    "uninstall_filesystem_sandbox",
    "is_path_denied",
    "ForbiddenExternalCall",
    "HttpClient",
    "configure_allowed_domains",
    "get_allowed_domains",
    "http_client",
    "is_domain_allowed",
    "app",
    "create_app",
]
