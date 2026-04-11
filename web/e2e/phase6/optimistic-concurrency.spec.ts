import { test, expect, type Page, type BrowserContext } from '@playwright/test';

/**
 * US-038 — Phase 6 gate Playwright spec for optimistic concurrency.
 *
 * Simulates two browser tabs loading the same Customer object and racing
 * an `updateCustomerContact` action through the Action Console. The first
 * tab's apply succeeds (version N -> N+1); the second tab is still
 * holding the pre-edit expectedVersion and must surface the StaleObject
 * 409 UX (banner + Reload button) instead of silently overwriting.
 *
 * Stack dependency: `scripts/e2e-setup.sh` must have run so that
 * 1. bin/weave is up on :9117
 * 2. Vite dev server is up on :5173 (proxying /api -> :9117)
 * 3. test/fixtures/e2e_seed.sh has seeded the northwind ontology,
 *    including the `updateCustomerContact` action type and the `ALFKI`
 *    customer (see test/fixtures/seed_northwind/schemas.go).
 *
 * The spec intentionally does NOT run the seed itself — the upstream
 * harness owns that, so reseed-on-every-spec doesn't blow up CI runtime.
 * The beforeAll only guards against running against a missing fixture.
 */

const ONTOLOGY_API_NAME = 'northwind';
const ACTION_API_NAME = 'updateCustomerContact';
const CUSTOMER_OBJECT_TYPE = 'customer';
const CUSTOMER_PRIMARY_KEY = 'ALFKI';

async function openActionConsole(
  context: BrowserContext,
): Promise<Page> {
  const page = await context.newPage();
  await page.goto(`/actions/${ONTOLOGY_API_NAME}`);
  await page.waitForLoadState('domcontentloaded');
  return page;
}

async function selectActionAndTarget(page: Page): Promise<void> {
  // Pick updateCustomerContact from the left-hand Action Types list.
  const actionItem = page.locator('button', { hasText: ACTION_API_NAME }).first();
  await expect(actionItem).toBeVisible();
  await actionItem.click();

  // Fill the target object (ontology object type + primary key). The
  // component keys useObjectVersion() off these two inputs so the
  // expectedVersion wire field only populates once both are set.
  await page.getByLabel(/target object type/i).fill(CUSTOMER_OBJECT_TYPE);
  await page.getByLabel(/target primary key/i).fill(CUSTOMER_PRIMARY_KEY);

  // Wait for the version chip to render — this is the signal that the
  // object_history lookup succeeded and the page now has a concrete
  // expectedVersion value queued for the apply payload.
  await expect(page.getByTestId('object-version')).toBeVisible({ timeout: 10_000 });
}

async function fillContactAndExecute(page: Page, newContact: string): Promise<void> {
  // ParameterForm renders inputs without htmlFor-linked labels, so we
  // select by the placeholder text the seed ships via each parameter's
  // `description`. See test/fixtures/seed_northwind/schemas.go.
  await page.getByPlaceholder('Customer primary key').fill(CUSTOMER_PRIMARY_KEY);
  await page.getByPlaceholder('New contact name').fill(newContact);
  await page.getByRole('button', { name: /execute action/i }).click();
}

test.describe('Phase 6 gate — optimistic concurrency (US-038)', () => {
  test.beforeAll(async ({ request }) => {
    // Preflight: the seed must already carry updateCustomerContact. If
    // scripts/e2e-setup.sh hasn't been run, bail loudly instead of
    // leaving the test to fail deep in the middle of a browser flow.
    const res = await request.get(
      `http://localhost:9117/api/v2/ontologies/${ONTOLOGY_API_NAME}/actionTypes`,
    );
    expect(res.ok(), 'northwind ontology must be seeded (run scripts/e2e-setup.sh)').toBe(true);
    const body = (await res.json()) as { data?: Array<{ apiName: string }> };
    const hasAction = (body.data ?? []).some((a) => a.apiName === ACTION_API_NAME);
    expect(
      hasAction,
      `${ACTION_API_NAME} action type missing from northwind seed — rerun e2e_seed.sh`,
    ).toBe(true);
  });

  test('tab B surfaces StaleObject banner + Reload recovers latest version', async ({
    browser,
  }) => {
    // Two isolated contexts = two independent browsers from the app's
    // perspective. Each gets its own cookie jar, its own React Query
    // cache, and most importantly its own useObjectVersion snapshot.
    const contextA = await browser.newContext();
    const contextB = await browser.newContext();
    try {
      const pageA = await openActionConsole(contextA);
      const pageB = await openActionConsole(contextB);

      await selectActionAndTarget(pageA);
      await selectActionAndTarget(pageB);

      // Snapshot the starting version both tabs see. Both must agree,
      // otherwise the race condition we want to exercise isn't actually
      // present (one tab would already be stale before Tab A commits).
      const startingVersionA = await pageA.getByTestId('object-version').textContent();
      const startingVersionB = await pageB.getByTestId('object-version').textContent();
      expect(startingVersionA?.trim()).not.toBe('');
      expect(startingVersionA).toBe(startingVersionB);

      // --- Tab A: happy path -----------------------------------------
      await fillContactAndExecute(pageA, 'Optimistic Alice');

      // Success surfaces as either an ActionResult panel rendering the
      // batch id or, at minimum, the absence of the stale-object banner.
      // We wait long enough for the NATS consumer to roll history
      // forward so that Tab B's apply below sees the drift.
      await expect(pageA.getByText(/This object was updated elsewhere/i)).toHaveCount(0);
      await pageA.waitForTimeout(750);

      // --- Tab B: stale write ----------------------------------------
      await fillContactAndExecute(pageB, 'Optimistic Bob');

      const staleBanner = pageB.getByRole('alert');
      await expect(staleBanner).toBeVisible({ timeout: 10_000 });
      await expect(staleBanner).toContainText(/This object was updated elsewhere/i);
      await expect(staleBanner.getByRole('button', { name: /reload/i })).toBeVisible();

      // --- Reload: Tab B picks up the current version ----------------
      const beforeReload = await pageB.getByTestId('object-version').textContent();
      await staleBanner.getByRole('button', { name: /reload/i }).click();

      // After Reload the banner clears and the version chip moves past
      // the originally-loaded snapshot. The object_history now carries
      // one extra row courtesy of Tab A's commit.
      await expect(pageB.getByRole('alert')).toHaveCount(0);
      await expect
        .poll(async () => (await pageB.getByTestId('object-version').textContent())?.trim(), {
          message: 'object version should advance after Reload',
          timeout: 10_000,
        })
        .not.toBe(beforeReload?.trim());
    } finally {
      await contextA.close();
      await contextB.close();
    }
  });
});
