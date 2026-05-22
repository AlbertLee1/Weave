import { readFile } from 'node:fs/promises';
import { expect, test, type Page, type Route } from '@playwright/test';
import {
  AggregationPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/aggregation/:ontology/:objectType` — the Aggregation
 * page rendered by `src/components/aggregation/AggregationPage.tsx`.
 *
 * Scenarios map the PRD AC for US-033 (upgrade us444/03-aggregate.spec
 * from a pure-API smoke into a full UI BDD suite):
 *
 *   AC: "至少 6 scenarios：选 group-by、metric、filter、
 *        切换图表类型（bar/line/pie）、导出 CSV、空集"
 *
 * Honest mapping (mirroring US-025/026/028/030):
 *   - "选 group-by" + "选 metric" → two happy-path scenarios drive the
 *     `groupby-add` + `groupby-{i}-field` + `metric-add` + `metric-{i}-*`
 *     controls then click `aggregation-execute`. The bucket-tree depth
 *     is read off `data-groupby-depth` and the named metric is asserted
 *     via the table's `<th>` column header (key already produced as
 *     `<field>.<aggType>` by the wire normalizer for unnamed metrics,
 *     so we use an explicit `name` alias to lock the friendly path).
 *   - "filter" → AggregationPage renders a where-clause row using the
 *     same field/operator/value contract as the object browser filter
 *     builder; the scenario captures the POST body and asserts the
 *     optional AggregationRequest.where key is included only when the
 *     user has added a filter.
 *   - "切换图表类型 (bar/line/pie)" → SimpleChart hard-codes a bar
 *     chart and there is no chart-type switcher today. Happy-path
 *     scenario asserts the chart wrapper carries `data-chart-type="bar"`
 *     + an `<svg>` with bar elements is rendered; absence scenario locks
 *     "no line/pie/area toggle exists" via role+name triple absence and
 *     pre-emptively prevents accidental partial swaps.
 *   - "导出 CSV" → CSV export is available once buckets exist. The
 *     scenario captures the browser download and asserts group columns
 *     precede metric columns with RFC4180 escaping.
 *   - "空集" → backend returns `{data: []}`; ResultTable renders the
 *     `aggregation-empty-results` placeholder. Locks the empty-array
 *     branch which `aggResult.data.length === 0` short-circuits the
 *     chart wrapper from rendering at all.
 *
 * Every scenario stubs the two endpoints AggregationPage actually hits:
 *   - GET /api/v2/ontologies/{ont}/objectTypes/{apiName} for
 *     `useObjectType` (drives `availableFields` from properties).
 *   - POST /api/v2/ontologies/{ont}/objects/{ot}/aggregate for
 *     `useAggregation` (returns wire-shape `{data:[{group, metrics:[]}],
 *     accuracy}`); the spec captures the request body so we can lock
 *     "the front-end sends a wire-correct aggregation spec".
 */

const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'order';

interface AggRequestBody {
  aggregation?: Array<{ type: string; field?: string; name?: string }>;
  groupBy?: Array<{ field: string; type: string }>;
  where?: unknown;
  [k: string]: unknown;
}

interface CapturedAgg {
  body: AggRequestBody;
}

function orderObjectType(): {
  rid: string;
  apiName: string;
  displayName: string;
  primaryKey: string;
  status: string;
  visibility: string;
  properties: Record<string, { dataType: { type: string }; rid: string }>;
} {
  return {
    rid: 'ri.ontology.main.object-type.order',
    apiName: OBJECT_TYPE,
    displayName: 'Order',
    primaryKey: 'orderID',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    properties: {
      orderID: { dataType: { type: 'string' }, rid: 'ri.p.order.id' },
      shipCountry: { dataType: { type: 'string' }, rid: 'ri.p.order.country' },
      customerID: { dataType: { type: 'string' }, rid: 'ri.p.order.cust' },
      freight: { dataType: { type: 'double' }, rid: 'ri.p.order.freight' },
    },
  };
}

interface AggRow {
  group?: Record<string, unknown>;
  metrics: Array<{ name: string; value: number }>;
}

interface AggResponseBody {
  data: AggRow[];
  accuracy?: string;
  excludedItems?: number;
}

async function stubAggregationEndpoints(
  page: Page,
  captured: CapturedAgg[],
  responseForCall: (
    ordinal: number,
    body: AggRequestBody,
  ) => AggResponseBody,
): Promise<void> {
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/*`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = new URL(route.request().url());
      const last = url.pathname.split('/').pop() ?? '';
      if (last !== OBJECT_TYPE) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'NotFound',
            errorInstanceId: 'spec',
          }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(orderObjectType()),
      });
    },
  );

  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}/aggregate`,
    async (route: Route) => {
      if (route.request().method() !== 'POST') {
        await route.continue();
        return;
      }
      const body = route.request().postDataJSON() as AggRequestBody;
      const ordinal = captured.length;
      captured.push({ body });
      const response = responseForCall(ordinal, body);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(response),
      });
    },
  );
}

describeFeature('Aggregation page', () => {
  test('Scenario: choose group-by + metric, execute, bucket tree renders nested columns @smoke', async ({
    page,
    request,
  }) => {
    // Locks the AC "选 group-by + metric" happy path: user adds one
    // groupBy clause and switches the default count metric to a named
    // sum on `freight`, clicks Execute, and the bucket tree picks up
    // the resolved column headers + `data-groupby-depth=1`. We also
    // capture the POST body so future PRs that drop fields from the
    // wire shape break this scenario before the bucket tree breaks.
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the aggregate endpoint returns two buckets', async () => {
      await stubAggregationEndpoints(page, captured, () => ({
        data: [
          {
            group: { shipCountry: 'USA' },
            metrics: [{ name: 'totalFreight', value: 12345 }],
          },
          {
            group: { shipCountry: 'Germany' },
            metrics: [{ name: 'totalFreight', value: 6789 }],
          },
        ],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user opens /aggregation/northwind/order', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await expect(agg.root).toBeVisible();
    });

    await Then('the page starts in the no-results-yet empty state', async () => {
      await expect(agg.emptyState).toBeVisible();
      await expect(agg.results).toHaveCount(0);
      expect(captured.length).toBe(0);
    });

    await When('the user adds a group-by on shipCountry', async () => {
      await agg.groupByAddBtn.click();
      await agg.groupByFieldSelect(0).selectOption('shipCountry');
    });

    await When('the user switches the first metric to sum freight with alias totalFreight', async () => {
      await agg.metricTypeSelect(0).selectOption('sum');
      await agg.metricFieldSelect(0).selectOption('freight');
      await agg.metricNameInput(0).fill('totalFreight');
    });

    await When('the user clicks Execute', async () => {
      await agg.executeBtn.click();
    });

    await Then('the results wrapper supersedes the empty-state branch', async () => {
      await expect(agg.results).toBeVisible();
      await expect(agg.emptyState).toHaveCount(0);
      await expect(agg.results).toHaveAttribute('data-bucket-count', '2');
    });

    await Then('the bucket tree advertises depth 1 with the right headers', async () => {
      await expect(agg.bucketTree).toBeVisible();
      await expect(agg.bucketTree).toHaveAttribute('data-groupby-depth', '1');
      const headers = agg.bucketTree.locator('thead th');
      await expect(headers.filter({ hasText: 'shipCountry' })).toHaveCount(1);
      await expect(headers.filter({ hasText: 'totalFreight' })).toHaveCount(1);
    });

    await Then('two rows render with the country labels and freight totals', async () => {
      const rows = agg.bucketTree.locator('tbody tr');
      await expect(rows).toHaveCount(2);
      await expect(agg.bucketTree).toContainText('USA');
      await expect(agg.bucketTree).toContainText('Germany');
      await expect(agg.bucketTree).toContainText('12,345');
      await expect(agg.bucketTree).toContainText('6,789');
    });

    await Then('the captured POST body carries the metric + groupBy spec', async () => {
      await expect.poll(() => captured.length).toBe(1);
      const body = captured[0]!.body;
      expect(body.aggregation).toEqual([
        { type: 'sum', field: 'freight', name: 'totalFreight' },
      ]);
      expect(body.groupBy).toEqual([{ field: 'shipCountry', type: 'exact' }]);
    });

    await Then('the accuracy badge reflects the backend marker', async () => {
      await expect(agg.accuracyBadge).toBeVisible();
      await expect(agg.accuracyBadge).toHaveAttribute('data-accuracy', 'ACCURATE');
    });
  });

  test('Scenario: chart renders as a bar SVG when groupBy is populated @smoke', async ({
    page,
  }) => {
    // Locks the "切换图表类型 (bar/line/pie)" AC half that *is*
    // implemented: today SimpleChart hard-codes a bar SVG and is the
    // only chart type. We assert the chart wrapper carries the
    // `data-chart-type="bar"` semantic marker and at least one
    // `[data-bar]` SVG rect renders, so a future PR that breaks the
    // chart altogether (or silently swaps in line/pie) is rejected.
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the aggregate endpoint returns three buckets', async () => {
      await stubAggregationEndpoints(page, captured, () => ({
        data: [
          { group: { shipCountry: 'USA' }, metrics: [{ name: 'count', value: 30 }] },
          { group: { shipCountry: 'France' }, metrics: [{ name: 'count', value: 12 }] },
          { group: { shipCountry: 'Brazil' }, metrics: [{ name: 'count', value: 5 }] },
        ],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user configures groupBy=shipCountry and executes', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await agg.groupByAddBtn.click();
      await agg.groupByFieldSelect(0).selectOption('shipCountry');
      // default count metric is fine; rename it to 'count' so the named
      // wire metric `count` matches the chartMetricKey detection.
      await agg.metricNameInput(0).fill('count');
      await agg.executeBtn.click();
    });

    await Then('the chart wrapper renders with chart-type="bar"', async () => {
      await expect(agg.chart).toBeVisible();
      await expect(agg.chart).toHaveAttribute('data-chart-type', 'bar');
    });

    await Then('the SVG inside the wrapper contains three bar marks', async () => {
      const svg = agg.chart.locator('svg');
      await expect(svg).toBeVisible();
      const bars = svg.locator('[data-bar]');
      await expect(bars).toHaveCount(3);
    });
  });

  test('Scenario: filter / where clause submits the configured where body', async ({
    page,
  }) => {
    // Locks AC "filter": the AggregationRequest type carries an
    // optional `where`, and the UI must now populate it from a visible
    // field/operator/value affordance instead of silently omitting it.
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the aggregate endpoint returns one bucket', async () => {
      await stubAggregationEndpoints(page, captured, () => ({
        data: [{ metrics: [{ name: 'count', value: 99 }] }],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user opens the page and configures shipCountry = USA', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await agg.filterFieldSelect().selectOption('shipCountry');
      await agg.filterOperatorSelect().selectOption('eq');
      await agg.filterValueInput().fill('USA');
      await agg.filterAddBtn.click();
    });

    await Then('the filter chip is visible in the config panel', async () => {
      await expect(agg.activeFilters).toContainText('shipCountry =');
      await expect(agg.activeFilters).toContainText('USA');
    });

    await When('the user clicks Execute', async () => {
      await agg.executeBtn.click();
      await expect(agg.results).toBeVisible();
    });

    await Then('the POST body carries the configured where clause', async () => {
      await expect.poll(() => captured.length).toBe(1);
      const body = captured[0]!.body;
      expect(body.where).toEqual({ type: 'eq', field: 'shipCountry', value: 'USA' });
    });
  });

  test('Scenario: chart type switcher (line/pie/area) is absent today', async ({
    page,
  }) => {
    // Honest mapping for the second half of AC "切换图表类型":
    // SimpleChart hard-codes a bar SVG; there is no toggle to switch
    // to line/pie/area. We lock the gap with role+name triple
    // absence so a partial swap (e.g. someone replaces bar with pie
    // but forgets the user-facing toggle) is rejected.
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the aggregate endpoint returns three buckets', async () => {
      await stubAggregationEndpoints(page, captured, () => ({
        data: [
          { group: { shipCountry: 'USA' }, metrics: [{ name: 'count', value: 30 }] },
          { group: { shipCountry: 'France' }, metrics: [{ name: 'count', value: 12 }] },
          { group: { shipCountry: 'Brazil' }, metrics: [{ name: 'count', value: 5 }] },
        ],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user reaches the post-Execute chart state', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await agg.groupByAddBtn.click();
      await agg.groupByFieldSelect(0).selectOption('shipCountry');
      await agg.metricNameInput(0).fill('count');
      await agg.executeBtn.click();
      await expect(agg.chart).toBeVisible();
    });

    await Then('no chart-type toggle is rendered alongside the chart', async () => {
      await expect(
        page.getByRole('button', { name: /^(line|pie|area)\b|chart\s*type|switch\s*chart/i }),
      ).toHaveCount(0);
      await expect(
        page.getByRole('tab', { name: /^(bar|line|pie|area)\b/i }),
      ).toHaveCount(0);
      await expect(
        page.getByRole('radio', { name: /^(bar|line|pie|area)\b/i }),
      ).toHaveCount(0);
    });

    await Then('the chart wrapper still locks chart-type=bar exclusively', async () => {
      await expect(agg.chart).toHaveAttribute('data-chart-type', 'bar');
    });
  });

  test('Scenario: CSV export downloads grouped buckets', async ({ page }) => {
    // Locks AC "导出 CSV": after an aggregate returns buckets, the
    // operator can download the current bucket table as a CSV with
    // group columns first, metric columns second, and quoted cells for
    // commas / embedded quotes.
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the aggregate endpoint returns two buckets', async () => {
      await stubAggregationEndpoints(page, captured, () => ({
        data: [
          {
            group: { shipCountry: 'USA, East', segment: 'Quoted "Direct"' },
            metrics: [{ name: 'count', value: 30 }],
          },
          { group: { shipCountry: 'France' }, metrics: [{ name: 'count', value: 12 }] },
        ],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user reaches the post-Execute results state', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await agg.groupByAddBtn.click();
      await agg.groupByFieldSelect(0).selectOption('shipCountry');
      await agg.executeBtn.click();
      await expect(agg.results).toBeVisible();
    });

    await Then('the CSV export button is available', async () => {
      await expect(agg.exportCsvBtn).toBeVisible();
    });

    await When('the user exports the CSV', async () => {
      const downloadPromise = page.waitForEvent('download');
      await agg.exportCsvBtn.click();
      const download = await downloadPromise;
      expect(download.suggestedFilename()).toBe('northwind-order-aggregation.csv');
      const path = await download.path();
      if (!path) throw new Error('download path was not available');
      const csv = await readFile(path, 'utf8');
      expect(csv).toBe(
        [
          'shipCountry,segment,count',
          '"USA, East","Quoted ""Direct""",30',
          'France,,12',
          '',
        ].join('\n'),
      );
    });
  });

  test('Scenario: empty result set surfaces the explicit no-results placeholder @smoke', async ({
    page,
  }) => {
    // Locks AC "空集": when the backend honestly returns `{data: []}`
    // (legitimate empty bucket set), the page swaps from the
    // pre-Execute empty-state wrapper into a results wrapper that
    // contains the `aggregation-empty-results` placeholder. The
    // chart MUST NOT render (length check short-circuits it).
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the aggregate endpoint returns an empty bucket array', async () => {
      await stubAggregationEndpoints(page, captured, () => ({
        data: [],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user configures groupBy and executes', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await agg.groupByAddBtn.click();
      await agg.groupByFieldSelect(0).selectOption('shipCountry');
      await agg.executeBtn.click();
    });

    await Then('the results wrapper renders with bucket-count=0', async () => {
      await expect(agg.results).toBeVisible();
      await expect(agg.results).toHaveAttribute('data-bucket-count', '0');
      await expect(agg.emptyState).toHaveCount(0);
    });

    await Then('the explicit empty-results placeholder is visible', async () => {
      await expect(agg.emptyResults).toBeVisible();
      await expect(agg.emptyResults).toContainText('No aggregation results');
    });

    await Then('the bucket tree is not rendered (length < 1)', async () => {
      await expect(agg.bucketTree).toHaveCount(0);
    });

    await Then('the chart is not rendered for an empty bucket set', async () => {
      await expect(agg.chart).toHaveCount(0);
    });
  });

  test('Scenario: metric add/remove keeps the row count and POST body in sync', async ({
    page,
  }) => {
    // Bonus scenario locking the "metric add/remove" lifecycle: the
    // default metric is `count` (no field needed); the user adds a
    // second `avg` metric on `freight` with alias `avgFreight`, then
    // removes the first row, leaving a single `avg` metric whose POST
    // body sends only that metric. This locks the wire-mapping
    // between MetricSelector array state and the AggregationRequest
    // `aggregation` array — a region the existing vitest doesn't
    // cover with multi-row mutation.
    const agg = new AggregationPage(page);
    const captured: CapturedAgg[] = [];

    await Given('the aggregate endpoint echoes one bucket per call', async () => {
      await stubAggregationEndpoints(page, captured, (ordinal) => ({
        data: [
          {
            metrics: [{ name: `m${ordinal}`, value: 100 + ordinal }],
          },
        ],
        accuracy: 'ACCURATE',
      }));
    });

    await When('the user adds a second metric (avg on freight)', async () => {
      await agg.goto(ONTOLOGY, OBJECT_TYPE);
      await expect(agg.metricRow(0)).toBeVisible();
      await agg.metricAddBtn.click();
      await expect(agg.metricRow(1)).toBeVisible();
      await agg.metricTypeSelect(1).selectOption('avg');
      await agg.metricFieldSelect(1).selectOption('freight');
      await agg.metricNameInput(1).fill('avgFreight');
    });

    await When('the user removes the first (count) metric', async () => {
      await agg.metricRemoveBtn(0).click();
    });

    await Then('only one metric row remains and it carries avg type', async () => {
      await expect(agg.metricRow(0)).toBeVisible();
      await expect(agg.metricRow(1)).toHaveCount(0);
      await expect(agg.metricRow(0)).toHaveAttribute('data-metric-type', 'avg');
    });

    await When('the user clicks Execute', async () => {
      await agg.executeBtn.click();
    });

    await Then('the POST body carries the single avg metric only', async () => {
      await expect.poll(() => captured.length).toBe(1);
      const body = captured[0]!.body;
      expect(body.aggregation).toEqual([
        { type: 'avg', field: 'freight', name: 'avgFreight' },
      ]);
      // No groupBy was added — the wire body must omit the key
      // (handleExecute uses `groupBy.length > 0 ? groupBy : undefined`).
      expect(body.groupBy).toBeUndefined();
    });
  });
});
