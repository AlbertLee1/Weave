# Weave Vertex Python Function Runtime (VTX-049)

A FastAPI + pydantic + sklearn sidecar that executes Vertex Functions
inside a filesystem sandbox. The Go side
([`pkg/vertex/funcruntime`](../../pkg/vertex/funcruntime)) forwards
invocations over HTTP — this package owns execution and the security
boundary.

## Wire contract

```
POST /invoke
    Body:  {"function": "...", "inputs": {...}}
    200 →  {"output": <jsonable>}
    404 →  {"detail": "..."}                                  (unknown function)
    422 →  {"detail": [{"loc":[...], "msg":"...", "type":"..."}]}
    403 →  {"detail": "...", "code": "ForbiddenFileAccess"}    (sandbox)
    5xx →  {"detail": "...", "code": "<ExceptionClass>"}       (runtime error)

GET  /health  →  200 {"status": "ok", "functions": ["a", "b", ...]}
```

The Go client at `pkg/vertex/funcruntime` decodes these envelopes into
typed errors (`ValidationError`, `SandboxViolationError`,
`NotFoundError`, `RuntimeError`, `TransportError`).

## Local install

```bash
cd runtime/python
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

## Running

```bash
# Default — binds the example sklearn model registered by
# weave_runtime.example_functions to the global registry.
python3 -c "import weave_runtime.example_functions" \
    && uvicorn weave_runtime.app:app --host 0.0.0.0 --port 9118
```

Then point Weave at it:

```bash
FUNCTION_RUNTIME_URL=http://localhost:9118 ./bin/weave
```

## Registering your own functions

```python
from pydantic import BaseModel
from weave_runtime import register_function


class Input(BaseModel):
    distance_km: float


class Output(BaseModel):
    delay_minutes: float


@register_function("my_predict", input_model=Input, output_model=Output)
def my_predict(inputs: Input) -> Output:
    return Output(delay_minutes=inputs.distance_km * 0.01)
```

Import the module that defines the function before booting uvicorn —
either set `PYTHONPATH=...` and `import` it at the top of a wrapper
script, or list it under `--app` in a Procfile / docker entrypoint.

## Filesystem sandbox

The runtime installs a process-wide guard on `builtins.open` and
`os.open` at app startup. Paths under
`/etc`, `/root`, `/proc`, `/sys`, `/var/log`, `/var/run`, `/Users`,
`/home`, `~/.ssh`, `~/.aws`, `~/.config` (the default denylist) are
blocked outright. A function that calls `open("/etc/passwd")` raises
`SandboxViolation`, which the FastAPI handler maps to `403
{"detail": "...", "code": "ForbiddenFileAccess"}`.

The sandbox is *defensive*, not a hardened container boundary — for
that, operators should still run this sidecar in its own user /
namespace / VM. See `weave_runtime/sandbox.py` for the threat model.

## Testing

```bash
cd runtime/python
python3 -m pytest tests/
```
