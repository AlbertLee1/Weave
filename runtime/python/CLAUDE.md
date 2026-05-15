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
  install_sandbox=True)` FastAPI factory + module-level `app` for
  uvicorn. Maps registry exceptions to wire envelopes.
- `weave_runtime/sandbox.py` — process-wide monkey-patch on
  `builtins.open` / `os.open`. Default denylist covers `/etc`,
  `/root`, `/proc`, `/sys`, `/var/log`, `/var/run`, `/Users`, `/home`,
  `~/.ssh`, `~/.aws`, `~/.config`.
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
- 403 → `{"detail": "...", "code": "ForbiddenFileAccess"}`
- 5xx → `{"detail": "...", "code": "<ExceptionClass>"}`

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
