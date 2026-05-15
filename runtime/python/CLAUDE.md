# runtime/python — Weave Vertex Function Runtime

Python sidecar that owns Vertex Function execution. Boots as
`uvicorn weave_runtime.app:app`. The Go client at `pkg/vertex/funcruntime`
forwards `POST /invoke` over HTTP and decodes typed error envelopes from
the response — see that package's docstring for the canonical wire
contract.

## Module layout

- `weave_runtime/functions.py` — `FunctionRegistry` (thread-safe
  name→`FunctionSpec` map), `@register_function(name, *, input_model,
  output_model)` decorator, module-level `registry` singleton,
  `UnknownFunctionError`.
- `weave_runtime/app.py` — `create_app(*, registry=None,
  install_sandbox=True, allowed_external_domains=None,
  llm_api_key=None)` FastAPI factory + module-level `app` for uvicorn.
  Maps registry exceptions to wire envelopes.
  `allowed_external_domains=None` (default) falls back to the
  `WEAVE_ALLOWED_EXTERNAL_DOMAINS` env var (comma-separated).
  `llm_api_key=None` (default) falls back to `WEAVE_LLM_API_KEY`,
  then `ANTHROPIC_API_KEY`; if none resolved the LLM config is cleared
  so `invoke_llm` raises `ConfigError` until the operator sets a key.
- `weave_runtime/sandbox.py` — process-wide monkey-patch on
  `builtins.open` / `os.open`. Default denylist covers `/etc`,
  `/root`, `/proc`, `/sys`, `/var/log`, `/var/run`, `/Users`, `/home`,
  `~/.ssh`, `~/.aws`, `~/.config`.
- `weave_runtime/external_http.py` (VTX-055) — `http_client` SDK
  singleton + `configure_allowed_domains` allowlist + `ForbiddenExternalCall`
  exception. Functions import `http_client` and call `.get(url)` /
  `.post(url, json=...)`; calls to hosts outside the allowlist raise
  `ForbiddenExternalCall` before the transport runs. Subdomain matching
  is NOT automatic — every host must be listed exactly.
- `weave_runtime/llm.py` (VTX-056) — `llm_client` SDK singleton +
  `configure_llm(api_key=...)` + `ConfigError` / `ModelOutputError`
  exceptions. Functions call `invoke_llm(model="claude-haiku-4-5",
  prompt="...")` for free-text replies or `invoke_llm_json(...)` for
  structured output (strips leading ```json ... ``` fences; JSON parse
  failures raise `ModelOutputError` with `raw_text` preserved). Missing
  API key raises `ConfigError` BEFORE the transport runs. Both errors
  fall into the generic 5xx envelope so the Go side sees
  `code="ConfigError"` / `code="ModelOutputError"` and can branch via
  `*RuntimeError.Code` — no dedicated wire code is added.
- `weave_runtime/example_functions.py` — reference `predict_delay`
  function backed by a trivially-trained `LinearRegression`.

## Gotcha: sandbox + temp dirs

`install_filesystem_sandbox(denylist=[tmp_path])` makes any `os.open`
under `tmp_path` raise `SandboxViolation`. `tempfile.TemporaryDirectory`
exits its `with` block by calling `shutil.rmtree` which calls
`os.open` — hitting the guard. `rmtree`'s `onerror` callback retries
`rmtree`, which fails again, and Python eventually trips the recursion
limit.

**Always** wrap sandbox installs in an explicit `try/finally` and call
`uninstall_filesystem_sandbox()` before any temp dir cleanup. Use
pytest's `tmp_path` fixture (cleaned up at session end, after the test
function returns) rather than `with tempfile.TemporaryDirectory()`
inside the sandboxed scope. The autouse safety-net fixture in
`tests/test_sandbox.py` is a backstop, not a substitute.

## Test discovery without `pip install -e .`

`tests/conftest.py` prepends `runtime/python/` to `sys.path` so
`from weave_runtime import ...` resolves. Run pytest from
`runtime/python/`:

```bash
cd runtime/python && python3 -m pytest tests/
```

## Wire contract anchoring

The shape of every error envelope is dictated by what the Go client
(`pkg/vertex/funcruntime/client.go`) decodes. When changing
`app.py` exception handlers, keep the contract in lockstep:

- 200 → `{"output": <jsonable>}`
- 404 → `{"detail": "..."}`
- 422 → `{"detail": [{"loc": [...], "msg": "...", "type": "..."}, ...]}`
- 403 → `{"detail": "...", "code": "ForbiddenFileAccess"}` (sandbox)
- 403 → `{"detail": "...", "code": "ForbiddenExternalCall"}` (allowlist; VTX-055)
- 5xx → `{"detail": "...", "code": "<ExceptionClass>"}`

When you add a new 403 sub-type, the Go client's `parseForbiddenError`
in `pkg/vertex/funcruntime/client.go` must grow a matching `code ==
"..."` branch so callers can `errors.As` to a distinct Go type.

The Go side's `client_test.go` exercises every branch; run both
suites whenever you touch envelope encoding:

```bash
cd runtime/python && python3 -m pytest tests/
cd ../.. && go test ./pkg/vertex/funcruntime/...
```

## Adding a function

```python
from pydantic import BaseModel
from weave_runtime import register_function


class MyInput(BaseModel):
    distance_km: float


class MyOutput(BaseModel):
    delay_minutes: float


@register_function("my_predict", input_model=MyInput, output_model=MyOutput)
def my_predict(inputs: MyInput) -> MyOutput:
    return MyOutput(delay_minutes=inputs.distance_km * 0.01)
```

Functions may return either the `output_model` instance, a different
`BaseModel` (registry dumps and re-validates), or any dict-like the
`output_model` can coerce. Output validation failures surface as 422
with `loc` rooted in the output field name.
