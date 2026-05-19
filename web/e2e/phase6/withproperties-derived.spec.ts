import { test, expect } from '@playwright/test';

/**
 * US-040 — Phase 6 gate Playwright spec for ObjectSetComposer withProperties.
 *
 * Drives the ObjectSetPage through a single-hop derived property: starting
 * from a `customer` base set the spec adds `withProperties` with an
 * `orderCount` derived metric computed as `customerOrders.count()` — the
 * "pivotTo(orders).count()" shape from the narrative AC translated to the
 * Northwind seed link name (`customerOrders`).
 *
 * After Execute, the DataTable must surface an extra `orderCount` column
 * populated with non-null numeric values merged onto each row by the
 * withProperties executor (pkg/oss/objectset/executor.go executeWithProperties
 * + handler.go DerivedValues merge).
 *
 * Stack dependency: `scripts/e2e-setup.sh` must have run so that
 * 1. bin/weave is up on :9117
 * 2. Vite dev server is up on :5173 (proxying /api -> :9117)
 * 3. test/fixtures/e2e_seed.sh has seeded the northwind ontology with the
 *    `customer` + `order` ObjectTypes and the `customerOrders` link.
 */

const ONTOLOGY_API_NAME = 'northwind';
const BASE_OBJECT_TYPE = 'customer';
const LINK_API_NAME = 'customerOrders';
const DERIVED_NAME = 'orderCount';

test.describe('Phase 6 gate — withProperties derived column (US-040)', () => {
  test.beforeAll(async ({ request }) => {
    // Preflight: northwind seed must carry customer + customerOrders link.
    const otRes = await request.get(
      `http://localhost:9117/api/v2/ontologies/${ONTOLOGY_API_NAME}/objectTypes`,
    );
    expect(
      otRes.ok(),
      'northwind ontology must be seeded (run scripts/e2e-setup.sh)',
    ).toBe(true);
    const otBody = (await otRes.json()) as {
      data?: Array<{ apiName: string }>;
    };
    expect(
      Array.isArray(otBody.data),
      'objectTypes response must include a data array',
    ).toBe(true);
    const hasCustomer = (otBody.data ?? []).some(
      (ot) => ot.apiName === BASE_OBJECT_TYPE,
    );
    expect(
      hasCustomer,
      `${BASE_OBJECT_TYPE} object type missing from northwind seed — rerun e2e_seed.sh`,
    ).toBe(true);

    const ltRes = await request.post(
      `http://localhost:9117/api/v2/ontologies/${ONTOLOGY_API_NAME}/metadata`,
      { data: { linkTypes: {} } },
    );
    expect(ltRes.ok(), 'metadata endpoint must respond').toBe(true);
    const ltBody = (await ltRes.json()) as {
      linkTypes?: Array<{ apiName: string; foreignKeyConfig?: unknown }>;
    };
    expect(
      Array.isArray(ltBody.linkTypes),
      'metadata response must include a linkTypes array',
    ).toBe(true);
    const link = (ltBody.linkTypes ?? []).find(
      (lt) => lt.apiName === LINK_API_NAME,
    );
    expect(
      link,
      `${LINK_API_NAME} link type missing from northwind seed — rerun e2e_seed.sh`,
    ).toBeTruthy();
    expect(
      link?.foreignKeyConfig,
      `${LINK_API_NAME} link type must carry foreignKeyConfig so FK resolution works`,
    ).toBeTruthy();
  });

  test('withProperties adds derived orderCount column with numeric values', async ({
    page,
  }) => {
    await page.goto(`/objectsets/${ONTOLOGY_API_NAME}`);
    await page.waitForLoadState('domcontentloaded');

    // The composer starts with a base node. Switch it to withProperties,
    // which auto-seeds a nested base inner + a default derived row.
    const rootTypeSelect = page.getByLabel('objectset type').first();
    await expect(rootTypeSelect).toBeVisible();
    await rootTypeSelect.selectOption('withProperties');

    // The inner base select is the SECOND 'objectset type' select because
    // ObjectSetBuilder renders nested children recursively. Default the inner
    // base object type to customer.
    await page
      .getByLabel('object type')
      .first()
      .selectOption(BASE_OBJECT_TYPE);

    // Configure the single default derived property row.
    const derivedRow = page.getByTestId('derived-property-row').first();
    await expect(derivedRow).toBeVisible();
    await derivedRow.getByTestId('derived-name').fill(DERIVED_NAME);
    await derivedRow.getByTestId('derived-link').fill(LINK_API_NAME);
    await derivedRow.getByTestId('derived-direction').selectOption('forward');
    await derivedRow.getByTestId('derived-metric').selectOption('count');

    // Execute the composed object set. The derived column must render.
    await page.getByRole('button', { name: /execute/i }).click();

    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 15_000 });

    // Column header for the derived property appears in the table head.
    const headers = table.locator('thead th');
    await expect(headers.filter({ hasText: DERIVED_NAME })).toHaveCount(1);

    // At least one rendered derived cell carries a non-null numeric value.
    const derivedCells = page.locator(`[data-derived-column="${DERIVED_NAME}"]`);
    await expect
      .poll(async () => derivedCells.count(), {
        message: 'derived column should render at least one cell',
        timeout: 15_000,
      })
      .toBeGreaterThan(0);

    const total = await derivedCells.count();
    let sawNumeric = false;
    for (let i = 0; i < total; i++) {
      const text = (await derivedCells.nth(i).innerText()).trim();
      if (text === '') continue;
      const n = Number(text);
      expect(
        Number.isFinite(n),
        `derived cell ${i} should be numeric, got "${text}"`,
      ).toBe(true);
      if (Number.isFinite(n)) sawNumeric = true;
    }
    expect(
      sawNumeric,
      'at least one customer must have a numeric orderCount derived value',
    ).toBe(true);
  });
});
