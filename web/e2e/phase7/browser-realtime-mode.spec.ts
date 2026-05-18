import { test, expect, type Page, type WebSocket as PlaywrightWebSocket } from '@playwright/test';
import { realtimePayloadMatchesPrimaryKey } from '../../src/lib/browserRealtimeHelpers';

const API_BASE = 'http://localhost:9117';
const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'customer';

/**
 * US-079: Playwright spec — browser realtime mode.
 *
 * Enables Browser Live mode, waits for the subscription stream to emit the
 * inserted object, then queries the Browser table deterministically for that
 * primary key.
 *
 * Stack dependency: `scripts/e2e-setup.sh` must have run so that
 * 1. bin/weave is up on :9117
 * 2. Vite dev server is up on :5173 (proxying /api -> :9117)
 * 3. test/fixtures/e2e_seed.sh has seeded the northwind ontology,
 *    including the `createCustomer` action type
 *    (see test/fixtures/seed_northwind/schemas.go).
 */

function frameText(payload: string | Buffer): string {
  return typeof payload === 'string' ? payload : payload.toString('utf8');
}

function waitForWebSocketFrame(
  page: Page,
  label: string,
  predicate: (payload: string | Buffer) => boolean,
  timeoutMs = 20_000,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const handlers = new Map<
      PlaywrightWebSocket,
      (event: { payload: string | Buffer }) => void
    >();
    let settled = false;

    const cleanup = () => {
      clearTimeout(timer);
      page.off('websocket', handleWebSocket);
      for (const [socket, handler] of handlers) {
        socket.off('framereceived', handler);
      }
      handlers.clear();
    };

    const finish = (error?: Error) => {
      if (settled) return;
      settled = true;
      cleanup();
      if (error) {
        reject(error);
        return;
      }
      resolve();
    };

    const handleWebSocket = (socket: PlaywrightWebSocket) => {
      const handleFrame = (event: { payload: string | Buffer }) => {
        if (predicate(event.payload)) finish();
      };
      handlers.set(socket, handleFrame);
      socket.on('framereceived', handleFrame);
    };

    const timer = setTimeout(() => {
      finish(new Error(`Timed out waiting for ${label}`));
    }, timeoutMs);

    page.on('websocket', handleWebSocket);
  });
}

function waitForRealtimeSubscribed(page: Page): Promise<void> {
  return waitForWebSocketFrame(page, 'Browser Live subscribed frame', (payload) => {
    try {
      return JSON.parse(frameText(payload)).type === 'subscribed';
    } catch {
      return false;
    }
  });
}

function waitForRealtimeObjectChange(
  page: Page,
  primaryKey: string,
): Promise<void> {
  return waitForWebSocketFrame(
    page,
    `Browser Live objectChanged frame for ${primaryKey}`,
    (payload) => realtimePayloadMatchesPrimaryKey(payload, primaryKey),
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

test.describe('Browser realtime mode (US-079)', () => {

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
    const uniqueId = `RT-${Date.now()}-${Math.random()
      .toString(36)
      .slice(2, 7)}`;

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
    const realtimeLabel = page.locator('label').filter({ hasText: 'Live' });
    await expect(realtimeLabel).toBeVisible();
    const subscriptionReady = waitForRealtimeSubscribed(page);
    const objectChanged = waitForRealtimeObjectChange(page, uniqueId);
    await realtimeLabel.click();

    // Wait for the green indicator dot and the WebSocket subscribed frame.
    const indicator = page.getByTestId('realtime-indicator');
    await expect(indicator).toBeVisible({ timeout: 10_000 });
    await subscriptionReady;

    // 3. POST a createCustomer action via the backend API to insert a
    //    new customer row. The subscription stream should push an event to
    //    the browser, which triggers query invalidation and a refetch.
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

    // 4. Wait for the realtime event before querying for the row. Searching
    //    by the primary key keeps the assertion deterministic even when
    //    earlier specs have pushed the customer table beyond the first page.
    await objectChanged;
    await searchForCustomerById(page, uniqueId);

    await expect(
      table.getByRole('cell', { name: uniqueId, exact: true }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('realtime indicator disappears when toggled off', async ({ page }) => {
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    const realtimeLabel = page.locator('label').filter({ hasText: 'Live' });

    // Turn on
    await realtimeLabel.click();
    const indicator = page.getByTestId('realtime-indicator');
    await expect(indicator).toBeVisible({ timeout: 10_000 });

    // Turn off
    await realtimeLabel.click();
    await expect(indicator).not.toBeVisible();
  });
});
