# Chapter 2 — Retry

The Python SDK ships an off-by-default retry layer. Pass a `RetryPolicy`
to either `Client` or `WeaveAsyncClient` and every transient failure
(transport error, 5xx, 408, 425, 429) is retried with full-jitter
exponential backoff — but **only for idempotent methods**.

## Defaults

`RetryPolicy()` means:

- `max_attempts=3` (1 original + 2 retries)
- `base_delay=0.1`s, `max_delay=2.0`s, `multiplier=2.0`
- Jitter: `uniform(0, cap)` — no thundering herd on rebooting servers
- `retry_methods=(GET, HEAD, OPTIONS, PUT, DELETE)`
- `retry_statuses=(408, 425, 429, 500, 502, 503, 504)`

```python
from weave_client import Client, RetryPolicy

policy = RetryPolicy(max_attempts=5, base_delay=0.25, max_delay=10.0)
client = Client("http://localhost:9117", retry=policy)
```

## Why POST and PATCH never retry

The HTTP method gate is intentional: POST / PATCH may have already taken
effect on the server even when the client saw a 5xx. Replaying them
silently risks double-writes. If you need at-least-once delivery for an
Action, use [Chapter 3 — Batching](03-batching.md): the server is the one
holding the idempotency contract via Action RIDs.

## `Retry-After` always wins

When the server emits a `Retry-After: 30` (delta-seconds) or RFC-1123
date header, the SDK uses that value instead of its computed backoff —
clamped to `max_delay` so a misbehaving server can't park your client
for ten minutes.

```python
RetryPolicy(max_delay=60.0)   # cap server-supplied Retry-After at 60s
```

## Disabling retries entirely

`max_attempts=1` means "one attempt, no retries". There is no separate
`enabled` flag — the integer carries the disable knob.

```python
client = Client(url, retry=RetryPolicy(max_attempts=1))
```

The SDK is also off-by-default: omitting the `retry=` kwarg disables
retries entirely for that client.

## Test-friendly: deterministic backoff

`RetryPolicy.sleep` and `RetryPolicy.rand` are injectable so unit tests
don't have to wait for real wall-clock time:

```python
import random

policy = RetryPolicy(
    max_attempts=4,
    rand=random.Random(42),     # deterministic jitter
    sleep=lambda secs: None,    # collapse the waits
)
```

The async transport applies the same hooks, swapping `asyncio.sleep` for
the supplied callable when one is configured.

## Custom retriable status sets

If your deployment fronts Weave behind an LB that returns 599 on transient
upstream issues, extend the set:

```python
policy = RetryPolicy(
    retry_statuses=(408, 425, 429, 500, 502, 503, 504, 599),
)
```

Don't add 4xx classes (other than 408/425/429) — they encode caller bugs
that a retry won't fix.

## Common pitfalls

- **Body re-reads.** If you supply your own `httpx.Request` to a custom
  transport, set `req.aread()`-style buffered bodies — a once-consumed
  stream silently sends an empty body on retry. The SDK's built-in JSON
  encoder is already buffered and safe.
- **Compound timeouts.** Total time = `timeout × max_attempts +
  Σ backoff`. With defaults that's ~3 × 30s + ~3s ≈ 93s worst case. Tune
  `timeout` and `max_delay` together.
- **Don't retry forever.** Above 5 attempts the marginal recovery
  probability collapses — surface the failure and let the caller decide.

See [`02-retry.py`](02-retry.py) for an end-to-end example with a
deterministic clock.
