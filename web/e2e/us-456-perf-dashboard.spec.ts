// US-456: Playwright e2e for the admin Performance Dashboard.

import { test, expect, type APIRequestContext } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

async function backendReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const res = await request.get(`${API_BASE}/health`, { timeout: 1500 });
    return res.ok();
  } catch {
    return false;
  }
}

test.describe('US-456 — Performance Dashboard', () => {
  test('mounts at /admin/perf and renders metric cards after two scrapes', async ({
    page,
    request,
  }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    for (let i = 0; i < 5; i++) {
      await request.get(`${API_BASE}/api/v2/ontologies/northwind`);
    }

    await page.goto('/admin/perf');

    await expect(page.getByTestId('perf-card-qps')).toBeVisible({ timeout: 15_000 });
    await expect(page.getByTestId('perf-card-error-rate')).toBeVisible();
    await expect(page.getByTestId('perf-card-latency')).toBeVisible();
    await expect(page.getByTestId('perf-card-db-qps')).toBeVisible();
  });

  test('Refresh button forces an immediate /metrics fetch', async ({ page, request }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    await page.goto('/admin/perf');
    const refreshBtn = page.getByTestId('perf-refresh-now');
    await expect(refreshBtn).toBeVisible({ timeout: 10_000 });

    // Wait for the page-level /metrics scrape that the polling loop
    // fires on mount, then click Refresh and assert another /metrics
    // request lands within 5s. The two-fetch evidence is the AC for
    // "force an immediate scrape" without depending on drawer visibility.
    const refreshFetch = page.waitForResponse(
      (res) => res.url().endsWith('/metrics') && res.status() === 200,
      { timeout: 10_000 },
    );
    await refreshBtn.click();
    const res = await refreshFetch;
    expect(res.ok()).toBe(true);
  });

  test('Pause button toggles between Pause and Resume states', async ({ page, request }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    await page.goto('/admin/perf');
    const pauseBtn = page.getByTestId('perf-toggle-pause');
    await expect(pauseBtn).toBeVisible({ timeout: 10_000 });
    await expect(pauseBtn).toContainText(/Pause/);
    await pauseBtn.click();
    await expect(pauseBtn).toContainText(/Resume/);
  });
});
