// VTX-099: System Graph render performance gates.
//
// PRD budgets (bench/results/vertex-render.md):
//   - 5 000 nodes + 15 000 edges: first contentful paint ≤ 2 s
//   - 10 000 nodes: sustained ≥ 24 fps over a 2 s sample window
//
// Once the backend is reachable, missing benchmark graph records or empty
// render payloads are product failures. The only portability skip below is
// for a local/dev CI run that has no weave backend listening on :9117.

import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

const API_BASE = 'http://localhost:9117';
const PERF_5K_RID = 'perf-5k';
const PERF_10K_RID = 'perf-10k';

interface GraphResponse {
  rid: string;
  name?: string;
  payload?: unknown;
}

async function backendReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const res = await request.get(`${API_BASE}/health`, { timeout: 1500 });
    return res.ok();
  } catch {
    return false;
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function arrayProp(parent: Record<string, unknown> | null, key: string): unknown[] {
  if (parent === null) return [];
  const value = parent[key];
  return Array.isArray(value) ? value : [];
}

function countPayloadNodes(payload: unknown): number {
  const layers = arrayProp(asRecord(payload), 'layers');
  const nodeIds = new Set<string>();
  for (const layer of layers) {
    for (const object of arrayProp(asRecord(layer), 'objects')) {
      const objectRid = asRecord(object)?.objectRid;
      if (typeof objectRid === 'string' && objectRid !== '') nodeIds.add(objectRid);
    }
  }
  return nodeIds.size;
}

function countPayloadEdges(payload: unknown): number {
  return arrayProp(asRecord(payload), 'edges').length;
}

async function fetchRequiredGraph(
  request: APIRequestContext,
  rid: string,
  minNodes: number,
  minEdges = 0,
): Promise<GraphResponse> {
  const res = await request.get(
    `${API_BASE}/api/vertex/v1/graphs/${encodeURIComponent(rid)}`,
    { timeout: 5_000 },
  );
  expect(
    res.ok(),
    `system graph ${rid} endpoint must be wired; status ${res.status()}`,
  ).toBe(true);

  const graph = (await res.json()) as GraphResponse;
  expect(graph.rid, `system graph ${rid} response must include a graph RID`).toBeTruthy();
  expect(
    countPayloadNodes(graph.payload),
    `${rid} system graph payload must include nodes`,
  ).toBeGreaterThanOrEqual(minNodes);
  if (minEdges > 0) {
    expect(
      countPayloadEdges(graph.payload),
      `${rid} system graph payload must include edges`,
    ).toBeGreaterThanOrEqual(minEdges);
  }
  return graph;
}

async function waitForRenderedGraph(page: Page, graph: GraphResponse): Promise<void> {
  await page.goto(`/vertex/${encodeURIComponent(graph.rid)}`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('vertex-workspace')).toBeVisible();
  await expect(page.getByTestId('vertex-canvas-host')).toBeVisible();
  await expect(page.getByTestId('vertex-canvas-loading')).toHaveCount(0);
  await expect(page.getByTestId('vertex-canvas-error')).toHaveCount(0);
  await expect(page.getByTestId('vertex-not-found')).toHaveCount(0);
  await expect(page.getByTestId('vertex-topbar-graph-name')).toContainText(
    graph.name ?? graph.rid,
  );
  await page.waitForFunction(
    () => {
      const host = document.querySelector('[data-testid="vertex-canvas-host"]');
      if (!(host instanceof HTMLElement)) return false;
      const hostRect = host.getBoundingClientRect();
      if (hostRect.width <= 0 || hostRect.height <= 0) return false;
      const canvas = host.querySelector('canvas');
      if (!(canvas instanceof HTMLCanvasElement)) return false;
      const canvasRect = canvas.getBoundingClientRect();
      return canvasRect.width > 0 && canvasRect.height > 0;
    },
    null,
    { timeout: 10_000 },
  );
}

async function measureFCP(page: Page, graph: GraphResponse): Promise<number> {
  const start = Date.now();
  await waitForRenderedGraph(page, graph);
  return Date.now() - start;
}

async function measureFPS(page: Page, sampleMs: number): Promise<number> {
  return page.evaluate(
    (ms) =>
      new Promise<number>((resolve) => {
        let frames = 0;
        const start = performance.now();
        const tick = () => {
          frames += 1;
          if (performance.now() - start < ms) {
            requestAnimationFrame(tick);
          } else {
            resolve((frames * 1000) / (performance.now() - start));
          }
        };
        requestAnimationFrame(tick);
      }),
    sampleMs,
  );
}

test.describe('VTX-099 — System Graph render perf', () => {
  test('5000 nodes + 15000 edges: first paint ≤ 2 s', async ({ page, request }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    const graph = await fetchRequiredGraph(request, PERF_5K_RID, 5_000, 15_000);
    const fcpMs = await measureFCP(page, graph);
    expect(fcpMs).toBeLessThanOrEqual(2_000);
  });

  test('10000 nodes: sustained ≥ 24 fps', async ({ page, request }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    const graph = await fetchRequiredGraph(request, PERF_10K_RID, 10_000);
    await waitForRenderedGraph(page, graph);
    const fps = await measureFPS(page, 2_000);
    expect(fps).toBeGreaterThanOrEqual(24);
  });
});
