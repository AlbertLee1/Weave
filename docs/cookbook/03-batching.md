# Chapter 3 — Batching

`ActionsAPI.apply_batch` submits N action invocations in one round-trip
and is the right answer whenever you have hundreds or thousands of
edits to commit. The server applies the whole batch atomically: every
invocation succeeds or none do.

## Single round-trip vs N×apply

```python
# Slow — one HTTP round-trip per row
for row in customers:
    client.actions.apply("northwind", "createCustomer", row)

# Fast — one round-trip, server-side atomic commit
client.actions.apply_batch(
    "northwind",
    "createCustomer",
    [{"parameters": row} for row in customers],
)
```

Each list element is a `{"parameters": {...}}` envelope, mirroring the
single-apply body. Optional `options` (mode / returnEdits) attach at the
batch level via the `return_edits` keyword:

```python
client.actions.apply_batch(
    "northwind", "createOrder", invocations,
    return_edits="NONE",   # save bandwidth for huge batches
)
```

## Choosing a chunk size

The server's batch limit is configurable but defaults to **500
invocations per batch**. For larger ingests, chunk client-side:

```python
def chunk(seq, size):
    for i in range(0, len(seq), size):
        yield seq[i : i + size]

for invocations in chunk(payloads, 250):
    resp = client.actions.apply_batch(
        "northwind", "createCustomer",
        [{"parameters": p} for p in invocations],
        return_edits="NONE",
    )
    edits = resp.edits
    if edits is not None:
        print(
            f"committed +{edits.added_object_count} "
            f"~{edits.modified_object_count} -{edits.deleted_object_count}"
        )
```

Sweet spot: 100–500 invocations per batch. Smaller batches waste round-
trips; larger ones risk hitting parameter-size limits and stretch the
server-side transaction window. Profile against your real shape.

## Atomicity

`applyBatch` is **all-or-nothing**. If any invocation fails validation
or its rule body raises, the entire transaction is rolled back and the
server returns a `BatchError` describing which invocation broke and
why. No partial state is ever published to NATS or the Bleve indexes.

```python
try:
    resp = client.actions.apply_batch(...)
except WeaveError as err:
    if err.error_name == "BatchError":
        # err.parameters carries {"index": <int>, "phase": "validate"|"prepare"|"apply"|"publish", "cause": "..."}
        bad = err.parameters.get("index")
        print(f"batch failed at row {bad}: {err.parameters.get('cause')}")
        raise
```

The `phase` discriminator is the part to log. `validate` / `prepare`
mean "nothing committed, fix and replay"; `publish` is the rare
post-commit failure where PG state stuck but the edit didn't reach
NATS — the server retries the publish on its own, but you should
flag it.

## Idempotency

Action RIDs are server-generated UUIDs, so re-submitting the same batch
after a network failure creates duplicate edits unless you carry your
own idempotency key in the parameters and gate at the rule level. For
true exactly-once semantics, model the dedup key as a unique-indexed
property on the target ObjectType so the second insert hits a 409.

## Streaming progress

`apply_batch` is fully buffered — there is no progress callback while
the server is committing. For very large pipelines, chunk client-side
(one batch ≈ one progress tick) and report after each chunk completes.

## Combining with retry

Batches are POST, so the SDK's retry layer **does not** retry them
automatically (see [Chapter 2](02-retry.md)). If you need at-least-once
delivery, retry at the application layer:

```python
import time

def submit_with_backoff(client, invocations, attempts=3):
    delay = 1.0
    for attempt in range(attempts):
        try:
            return client.actions.apply_batch("northwind", "createCustomer", invocations)
        except WeaveError as err:
            if err.status_code in (502, 503, 504) and attempt + 1 < attempts:
                time.sleep(delay)
                delay *= 2
                continue
            raise
```

Pair this with the idempotency note above so retries don't double-write.

See [`03-batching.py`](03-batching.py) for an end-to-end example with
chunking and `return_edits="NONE"` tuning.
