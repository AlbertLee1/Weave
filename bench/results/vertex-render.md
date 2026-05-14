# Vertex System Graph Render Benchmark (VTX-099)

`web/e2e/vtx-099-system-graph-render.spec.ts` is the regression gate for
System Graph render perf. It drives a real browser against the dev server
and measures two budgets from PRD VTX-099:

| Load                          | Metric        | Budget |
| ----------------------------- | ------------- | -----: |
| 5 000 nodes + 15 000 edges    | First paint   | ≤ 2 s  |
| 10 000 nodes                  | Sustained fps | ≥ 24   |

## Gate semantics

The spec calls `test.skip(...)` when either:

1. The Weave backend on `:9117` is unreachable (no `/health`), or
2. The `/api/vertex/v1/graphs` System Graph endpoint is not yet wired up
   (returns non-2xx).

This is deliberate: VTX-099 depends on VTX-018 (System Graph page + API),
which is owned by a different replication stream. The contract test stays
in-tree so it activates automatically the moment VTX-018 lands. Skips show
up explicitly in the Playwright report — they are never silently passed.

## First paint methodology

`measureFCP` navigates to `/vertex/<rid>`, waits for the
`[data-testid="vertex-graph-canvas"]` element to mount with non-zero width,
and reports wall time from `page.goto` start. The test bundles its own seed
fixture under deterministic RIDs (`perf-5k`, `perf-10k`) so reruns do not
fight for snapshots.

## fps methodology

`measureFPS` uses `requestAnimationFrame` for a 2 s window after the canvas
becomes visible. It deliberately includes layout / pan idle frames — the
budget is a sustained fps floor, not a single-frame peak.

## How to re-run

```bash
# Full Playwright suite (auto-starts dev server via playwright.config.ts)
cd web && npx playwright test vtx-099-system-graph-render

# Headed for debugging
cd web && npx playwright test vtx-099-system-graph-render --headed
```

## Latest run

> Pending VTX-018. The skip path is exercised on every CI run; budgets stay
> aspirational until the System Graph page ships. Once it does, append a
> "Latest run" block here with browser + machine info and the observed
> p99 first-paint ms and median fps.
