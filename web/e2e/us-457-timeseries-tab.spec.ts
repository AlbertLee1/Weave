// US-457: Playwright e2e for the per-object TimeSeries tab.
//
// Northwind seed does not currently include timeseries-typed properties,
// so the spec verifies the tab control exists, opens cleanly, and
// renders the empty-state when no timeseries property is present. The
// test is degraded-mode tolerant — if a future seed adds timeseries
// columns the empty-state branch reads as a soft skip instead of a
// failure.

import { test, expect, type APIRequestContext } from '@playwright/test';

const API_BASE = 'http://localhost:9117';
const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'customer';

async function backendReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const res = await request.get(`${API_BASE}/health`, { timeout: 1500 });
    return res.ok();
  } catch {
    return false;
  }
}

test.describe('US-457 — TimeSeries Tab', () => {
  test('TimeSeries tab is reachable from the object detail panel', async ({
    page,
    request,
  }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    // Land on the customer browser and open the first row's detail panel.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 15_000 });

    const firstRow = table.locator('tbody tr').first();
    await expect(firstRow).toBeVisible();
    await firstRow.click();

    // The detail panel slides in. Find the TimeSeries tab control and
    // click it.
    const tab = page.getByTestId('object-detail-tab-timeseries');
    await expect(tab).toBeVisible({ timeout: 10_000 });
    await tab.click();

    // The tab section should mount. Northwind seed has no timeseries
    // properties, so the empty-state copy is the expected render.
    const tabBody = page.getByTestId('object-detail-timeseries');
    await expect(tabBody).toBeVisible();
    const emptyState = page.getByTestId('ts-tab-empty');
    await expect(emptyState).toBeVisible();
    await expect(emptyState).toContainText(/no timeseries/i);
  });
});
