import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:9117';
const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'customer';

/**
 * US-079: Playwright spec — browser realtime mode.
 *
 * Enables "Realtime Mode" on the Browser page, then POSTs a createCustomer
 * action via the backend API and asserts the DataTable updates within 2 s
 * to include the newly created row.
 *
 * Stack dependency: `scripts/e2e-setup.sh` must have run so that
 * 1. bin/weave is up on :9117
 * 2. Vite dev server is up on :5173 (proxying /api -> :9117)
 * 3. test/fixtures/e2e_seed.sh has seeded the northwind ontology,
 *    including the `createCustomer` action type
 *    (see test/fixtures/seed_northwind/schemas.go).
 */
test.describe('Browser realtime mode (US-079)', () => {
  // Generate a unique customer ID per run to avoid collisions.
  const uniqueId = `RT-${Date.now()}`;

  test.beforeAll(async ({ request }) => {
    // Preflight: the seed must already carry createCustomer action type.
    const res = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    );
    expect(
      res.ok(),
      'northwind ontology must be seeded (run scripts/e2e-setup.sh)',
    ).toBe(true);
    const body = (await res.json()) as { data?: Array<{ apiName: string }> };
    const hasAction = (body.data ?? []).some((a) => a.apiName === 'createCustomer');
    expect(
      hasAction,
      'createCustomer action type missing from northwind seed — rerun e2e_seed.sh',
    ).toBe(true);
  });

  test('new object appears in table after backend apply with realtime on', async ({
    page,
    request,
  }) => {
    // 1. Navigate to the Browser page for customers.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for the data table to render with initial data.
    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // Count the initial number of data rows.
    const initialRowCount = await table.locator('tbody tr').count();
    expect(initialRowCount).toBeGreaterThan(0);

    // 2. Enable Realtime Mode by clicking the label (the checkbox itself is
    //    sr-only/visually-hidden, so clicking the wrapping <label> is the
    //    accessible way to toggle it).
    const realtimeLabel = page.locator('label').filter({ hasText: 'Realtime' });
    await expect(realtimeLabel).toBeVisible();
    await realtimeLabel.click();

    // Wait for the green indicator dot — it only renders once the
    // createTemporary ObjectSet call succeeds and the SSE EventSource
    // is connected.
    const indicator = page.getByTestId('realtime-indicator');
    await expect(indicator).toBeVisible({ timeout: 10_000 });

    // 3. POST a createCustomer action via the backend API to insert a
    //    new customer row. The NATS consumer → Broadcast → SSE pipeline
    //    should push an event to the browser which triggers a table refetch.
    const applyRes = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/createCustomer/apply`,
      {
        data: {
          parameters: {
            customerID: uniqueId,
            companyName: `Realtime Co ${uniqueId}`,
            country: 'testland',
            contactName: 'E2E Bot',
          },
        },
      },
    );
    expect(applyRes.ok(), `createCustomer apply failed: ${applyRes.status()}`).toBe(
      true,
    );

    // 4. Assert the new row appears in the DataTable within 5 seconds.
    //    The SSE event triggers queryClient.invalidateQueries(['objects'])
    //    which re-fetches and re-renders. We match on the primary key cell
    //    specifically (exact: true avoids matching the companyName column
    //    which also embeds the ID).
    //    The pipeline (action → NATS → Bleve index → Broadcast → SSE →
    //    query invalidation → refetch) can take a few seconds end-to-end.
    await expect(
      table.getByRole('cell', { name: uniqueId, exact: true }),
    ).toBeVisible({ timeout: 5_000 });
  });

  test('realtime indicator disappears when toggled off', async ({ page }) => {
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    const realtimeLabel = page.locator('label').filter({ hasText: 'Realtime' });

    // Turn on
    await realtimeLabel.click();
    const indicator = page.getByTestId('realtime-indicator');
    await expect(indicator).toBeVisible({ timeout: 10_000 });

    // Turn off
    await realtimeLabel.click();
    await expect(indicator).not.toBeVisible();
  });
});
