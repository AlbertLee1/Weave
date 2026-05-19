import { test, expect, type Page } from '@playwright/test';

const API_BASE = 'http://localhost:9117';
const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'customer';

/**
 * US-080: Playwright spec — SSE reconnect.
 *
 * Simulates offline/online transitions and verifies that SSE reconnect
 * delivers buffered events (via SubscribeWithReplay) after the browser
 * comes back online.
 *
 * Flow:
 * 1. Enable realtime mode (opens the live transport)
 * 2. Set browser context offline (transport drops → hook backoff reconnect)
 * 3. POST 3 createCustomer actions via backend API while browser offline
 * 4. Set browser context online (hook reconnects, SubscribeWithReplay
 *    replays buffered events → queryClient.invalidateQueries → table refetch)
 * 5. Search by each inserted primary key and assert the rows appear in the DataTable
 */
async function waitForLiveConnected(page: Page, timeout = 10_000): Promise<void> {
  const liveStatus = page.getByTestId('live-status');
  await expect(liveStatus).toHaveText(/Connected/, { timeout });
  await expect(liveStatus).toHaveAttribute(
    'aria-label',
    /Live updates connected over/i,
  );
}

async function searchForCustomerById(page: Page, customerId: string): Promise<void> {
  const searchResponse = page.waitForResponse((response) => {
    return (
      response.request().method() === 'POST' &&
      response.url().includes(
        `/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}/search`,
      ) &&
      response.ok()
    );
  });

  const searchInput = page.getByTestId('search-input');
  await searchInput.fill(customerId);
  await searchInput.press('Enter');
  await searchResponse;
}

test.describe('SSE reconnect (US-080)', () => {
  test.beforeAll(async ({ request }) => {
    // Preflight: the seed must carry createCustomer action type.
    const res = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    );
    expect(
      res.ok(),
      'northwind ontology must be seeded (run scripts/e2e-setup.sh)',
    ).toBe(true);
    const body = (await res.json()) as { data?: Array<{ apiName: string }> };
    expect(
      Array.isArray(body.data),
      'actionTypes response must include data array',
    ).toBe(true);
    const actionTypes = body.data ?? [];
    const hasAction = actionTypes.some(
      (a) => a.apiName === 'createCustomer',
    );
    expect(
      hasAction,
      'createCustomer action type missing from northwind seed',
    ).toBe(true);
  });

  test(
    'offline → online → buffered events delivered after reconnect',
    async ({ page, request }) => {
    // 1. Navigate to the Browser page for customers.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // 2. Enable Realtime Mode.
    const realtimeLabel = page
      .locator('label')
      .filter({ hasText: 'Live' });
    await expect(realtimeLabel).toBeVisible();
    await realtimeLabel.click();

    // Wait for the toolbar status — confirms a live transport is connected.
    await waitForLiveConnected(page);

    // 3. Simulate going offline. The EventSource will error out and the
    //    hook enters exponential backoff reconnection.
    await page.context().setOffline(true);

    // Give the browser time to detect the disconnect and fire onerror.
    await page.waitForTimeout(2_000);

    // 4. While the browser is offline, POST 3 createCustomer actions.
    //    These events are buffered in the Broadcast ring buffer.
    //    The `request` fixture is independent of the page's network state.
    const ids: string[] = [];
    for (let i = 0; i < 3; i++) {
      const uniqueId = `SSE-RC-${Date.now()}-${i}`;
      ids.push(uniqueId);
      const applyRes = await request.post(
        `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/createCustomer/apply`,
        {
          data: {
            parameters: {
              customerID: uniqueId,
              companyName: `Reconnect Co ${uniqueId}`,
              country: 'testland',
              contactName: 'E2E Bot',
            },
          },
        },
      );
      expect(
        applyRes.ok(),
        `createCustomer apply failed for ${uniqueId}: ${applyRes.status()}`,
      ).toBe(true);
    }

    // Small delay to ensure events are published to the ring buffer.
    await page.waitForTimeout(500);

    // 5. Restore connectivity. The hook's next backoff timer fires a new
    //    EventSource → connects → SubscribeWithReplay replays buffered
    //    events → onEvent triggers queryClient.invalidateQueries → refetch.
    await page.context().setOffline(false);

    // 6. Assert all 3 new rows appear in the DataTable. Searching by
    //    primary key keeps this deterministic even when earlier specs have
    //    pushed the customer table beyond the first page.
    for (const id of ids) {
      await searchForCustomerById(page, id);
      await expect(
        table.getByRole('cell', { name: id, exact: true }),
      ).toBeVisible({ timeout: 15_000 });
    }

    // Verify the live transport is back after reconnect.
    await waitForLiveConnected(page);
    },
  );
});
