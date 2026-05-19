import { test, expect, type Page } from '@playwright/test';

/**
 * US-039 — Phase 6 gate Playwright spec for multi-layer groupBy.
 *
 * Drives the AggregationPage through a 3-layer groupBy (three exact groupBy
 * clauses over distinct fields), runs the query against the real Northwind
 * seed, and asserts that the resulting bucket tree renders three groupBy
 * columns alongside an accuracy badge wired from the backend
 * `AggregationResponse.accuracy` marker.
 *
 * The narrative AC in `tasks/prd-v2-deep-parity.md` calls out a conceptual
 * country × priceBucket × quarter split — the minimal Playwright seed
 * (`test/fixtures/seed_northwind/schemas.go`) does not ship a timestamp
 * column, so the spec instead exercises three distinct exact groupBys
 * (shipCountry × customerID × orderID) which is still a 3-level nested
 * recursive groupBy through the same code path on the backend.
 *
 * Stack dependency: `scripts/e2e-setup.sh` must have run so that
 * 1. bin/weave is up on :9117
 * 2. Vite dev server is up on :5173 (proxying /api -> :9117)
 * 3. test/fixtures/e2e_seed.sh has seeded the northwind ontology with the
 *    `order` ObjectType and at least a handful of Orders indexed in Bleve.
 */

const ONTOLOGY_API_NAME = 'northwind';
const OBJECT_TYPE = 'order';

async function addGroupByRow(page: Page): Promise<void> {
  await page.getByTestId('groupby-add').click();
}

async function selectGroupByField(
  page: Page,
  index: number,
  field: string,
): Promise<void> {
  await page.getByTestId(`groupby-${index}-field`).selectOption(field);
}

test.describe('Phase 6 gate — aggregation multi-groupBy (US-039)', () => {
  test.beforeAll(async ({ request }) => {
    // Preflight: the Northwind seed must carry the `order` ObjectType.
    // Bail loudly if scripts/e2e-setup.sh hasn't been run.
    const res = await request.get(
      `http://localhost:9117/api/v2/ontologies/${ONTOLOGY_API_NAME}/objectTypes`,
    );
    expect(res.ok(), 'northwind ontology must be seeded (run scripts/e2e-setup.sh)').toBe(true);
    const body = (await res.json()) as { data?: Array<{ apiName: string }> };
    expect(
      Array.isArray(body.data),
      'objectTypes response must include a data array',
    ).toBe(true);
    const hasOrder = (body.data ?? []).some((ot) => ot.apiName === OBJECT_TYPE);
    expect(
      hasOrder,
      `${OBJECT_TYPE} object type missing from northwind seed — rerun e2e_seed.sh`,
    ).toBe(true);
  });

  test('3-layer groupBy renders nested bucket tree and accuracy badge', async ({
    page,
  }) => {
    await page.goto(`/aggregation/${ONTOLOGY_API_NAME}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    // Queue three groupBy rows. GroupByBuilder starts empty for order.
    await addGroupByRow(page);
    await addGroupByRow(page);
    await addGroupByRow(page);

    // Three distinct exact groupBys drive the recursive multi-groupBy path
    // on the backend (pkg/oss/aggregation/engine.go recursiveGroupBy).
    // Each picks a field that exists on the Northwind order seed.
    await selectGroupByField(page, 0, 'shipCountry');
    await selectGroupByField(page, 1, 'customerID');
    await selectGroupByField(page, 2, 'orderID');

    // MetricSelector defaults to count which is already enough to exercise
    // the 3-layer path. Execute the query.
    await page.getByTestId('aggregation-execute').click();

    // Bucket tree renders with 3 groupBy columns (depth = 3).
    const tree = page.getByTestId('aggregation-bucket-tree');
    await expect(tree).toBeVisible({ timeout: 10_000 });
    await expect(tree).toHaveAttribute('data-groupby-depth', '3');

    // All three groupBy fields appear as column headers.
    const headers = tree.locator('thead th');
    await expect(headers.filter({ hasText: 'shipCountry' })).toHaveCount(1);
    await expect(headers.filter({ hasText: 'customerID' })).toHaveCount(1);
    await expect(headers.filter({ hasText: 'orderID' })).toHaveCount(1);

    // At least one result row rendered — without this the tree is empty
    // and the depth assertion is meaningless.
    const bodyRows = tree.locator('tbody tr');
    await expect
      .poll(async () => bodyRows.count(), {
        message: 'bucket tree should render at least one row',
        timeout: 10_000,
      })
      .toBeGreaterThan(0);

    // Accuracy badge is wired from AggregationResponse.accuracy. Northwind
    // seed is small enough that the backend should return ACCURATE (the
    // truncation threshold in computeMetrics kicks in around 10k+ docs).
    const badge = page.getByTestId('aggregation-accuracy-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText(/ACCURATE|APPROXIMATE/);
  });
});
