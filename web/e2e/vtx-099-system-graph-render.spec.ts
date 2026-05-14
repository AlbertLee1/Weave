// VTX-099: System Graph render performance gates.
//
// PRD budgets (bench/results/vertex-render.md):
//   - 5 000 nodes + 15 000 edges: first contentful paint ≤ 2 s
//   - 10 000 nodes: sustained ≥ 24 fps over a 2 s sample window
//
// The actual System Graph page is owned by VTX-018 (other stream). Until that
// lands the spec auto-skips with a clear message — that keeps CI green while
// the contract test stays in-tree as the regression gate the moment the page
// is reachable.

import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

async function backendReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const res = await request.get(`${API_BASE}/health`, { timeout: 1500 });
    return res.ok();
  } catch {
    return false;
  }
}

async function systemGraphReachable(page: Page): Promise<boolean> {
  const res = await page.request.get(`${API_BASE}/api/vertex/v1/graphs`, { timeout: 1500 });
  return res.ok();
}

async function measureFCP(page: Page, url: string): Promise<number> {
  const start = Date.now();
  await page.goto(url, { waitUntil: 'domcontentloaded' });
  await page.waitForFunction(
    () => {
      const c = document.querySelector('[data-testid="vertex-graph-canvas"]');
      return c instanceof HTMLElement && c.getBoundingClientRect().width > 0;
    },
    { timeout: 10_000 },
  );
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
    test.skip(!(await systemGraphReachable(page)), 'VTX-018 System Graph page not yet wired up');

    const fcpMs = await measureFCP(page, '/vertex/perf-5k');
    expect(fcpMs).toBeLessThanOrEqual(2_000);
  });

  test('10000 nodes: sustained ≥ 24 fps', async ({ page, request }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');
    test.skip(!(await systemGraphReachable(page)), 'VTX-018 System Graph page not yet wired up');

    await page.goto('/vertex/perf-10k', { waitUntil: 'domcontentloaded' });
    await expect(page.getByTestId('vertex-graph-canvas')).toBeVisible();
    const fps = await measureFPS(page, 2_000);
    expect(fps).toBeGreaterThanOrEqual(24);
  });
});
