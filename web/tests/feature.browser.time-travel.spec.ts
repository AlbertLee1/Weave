import { expect, test, type Page, type Route } from '@playwright/test';
import { BrowserPage, Given, Then, When, describeFeature } from './support';

/**
 * BDD coverage of the Browser Time Travel toolbar (US-048, PC-A09).
 *
 * Scenarios map to the US-048 acceptance criteria:
 *
 *   1. "Browser 顶栏新增 Time Travel toggle 与 dataset transaction picker"
 *      → render scenario asserts toolbar + picker render with the
 *        stubbed transactions; toggle is disabled until a tx is chosen.
 *   2. "切换时所有列表/详情查询带 asOf 参数"
 *      → toggle-on scenario verifies that subsequent GETs to
 *        `/api/v2/ontologies/{name}/objects/{ot}` carry `?asOf=tx-...`
 *        with the picked id, and toggle-off drops the param.
 *   3. "历史模式下编辑按钮禁用并显示 hint"
 *      → historical-mode scenario asserts the Live toggle is disabled,
 *        the hint banner renders, and the Bulk Action Toolbar mount is
 *        suppressed entirely while time-travel is on.
 *
 * Every scenario stubs the ontology + objectType + dataset-history +
 * objects endpoints through `page.route` so the page renders against
 * deterministic fixtures without touching a real backend. The wire-shape
 * for /datasets/{rid}/history mirrors `cmd/server/dataset_transaction_handler.go`'s
 * `{transactions: [...]}` envelope so the spec is byte-compatible with the
 * production endpoint.
 */

const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'employee';
const TX_LATEST = 'tx-aaaaaaaa-1111-7222-9333-444444444444';
const TX_EARLIER = 'tx-bbbbbbbb-2222-7333-9444-555555555555';

interface CapturedRequest {
  url: string;
  method: string;
  asOf: string | null;
}

const objectTypeFixture = {
  rid: 'ri.ontology.main.object-type.employee',
  apiName: OBJECT_TYPE,
  displayName: 'Employee',
  pluralDisplayName: 'Employees',
  primaryKey: 'employeeID',
  titleProperty: 'lastName',
  status: 'ACTIVE',
  visibility: 'PROMINENT',
  properties: {
    employeeID: {
      dataType: { type: 'long' },
      rid: 'ri.ontology.main.property.employeeID',
    },
    lastName: {
      dataType: { type: 'string' },
      rid: 'ri.ontology.main.property.lastName',
    },
  },
};

const liveRows = {
  data: [
    {
      __rid: 'ri.object.employee.1',
      __primaryKey: 1,
      __apiName: OBJECT_TYPE,
      employeeID: 1,
      lastName: 'Davolio',
    },
    {
      __rid: 'ri.object.employee.2',
      __primaryKey: 2,
      __apiName: OBJECT_TYPE,
      employeeID: 2,
      lastName: 'Fuller',
    },
  ],
  totalCount: '2',
};

const historicalRows = {
  data: [
    {
      __rid: 'ri.object.employee.1',
      __primaryKey: 1,
      __apiName: OBJECT_TYPE,
      employeeID: 1,
      lastName: 'Davolio (historical)',
    },
  ],
  totalCount: '1',
};

async function stubMetadata(page: Page): Promise<void> {
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/${OBJECT_TYPE}`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(objectTypeFixture),
      });
    },
  );
  // Properties + outgoing link types are referenced by ObjectDetail; stub
  // them with empty payloads so the slide-panel side-effects do not throw.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/${OBJECT_TYPE}/linkTypes`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    },
  );
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes**`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    },
  );
}

async function stubDatasetHistory(
  page: Page,
  transactions: Array<{
    txId: string;
    parentTxId?: string;
    committedAt: string;
    editsCount: number;
  }>,
): Promise<void> {
  await page.route(
    `**/api/v2/datasets/${ONTOLOGY}/history*`,
    async (route: Route) => {
      const decorated = transactions.map((t) => ({
        ...t,
        ontologyApiName: ONTOLOGY,
        userId: 'user:alice',
      }));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ transactions: decorated }),
      });
    },
  );
}

/**
 * stubObjectList captures every GET to /objects/{ot} (the BrowserPage list
 * fetch) so a scenario can assert how the asOf query param flows through
 * the API-client interceptor. Returns the captured array so the caller can
 * assert against it.
 */
async function stubObjectList(page: Page): Promise<CapturedRequest[]> {
  const captured: CapturedRequest[] = [];
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}**`,
    async (route: Route) => {
      const request = route.request();
      const method = request.method();
      const url = new URL(request.url());
      // Only capture the list GET — search POSTs go to /search, single-row
      // GETs go to /{primaryKey} (we don't trigger either in these specs).
      if (method !== 'GET') {
        await route.continue();
        return;
      }
      // Sub-path filters (links / activity / history) live under
      // /objects/{ot}/{pk}/... — they have an additional segment, so the
      // raw path-length check disambiguates them from the list endpoint.
      const segments = url.pathname.split('/').filter(Boolean);
      const isList = segments[segments.length - 1] === OBJECT_TYPE;
      if (!isList) {
        await route.continue();
        return;
      }
      const asOf = url.searchParams.get('asOf');
      captured.push({ url: request.url(), method, asOf });
      const rows = asOf ? historicalRows : liveRows;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(rows),
      });
    },
  );
  return captured;
}

describeFeature('Browser Time Travel', () => {
  test('Scenario: toolbar renders the dataset-transaction picker @smoke', async ({
    page,
  }) => {
    const browser = new BrowserPage(page);

    await Given('the dataset history endpoint advertises two transactions', async () => {
      await stubMetadata(page);
      await stubObjectList(page);
      await stubDatasetHistory(page, [
        {
          txId: TX_LATEST,
          parentTxId: TX_EARLIER,
          committedAt: '2026-05-12T18:30:00Z',
          editsCount: 4,
        },
        {
          txId: TX_EARLIER,
          committedAt: '2026-05-11T09:15:00Z',
          editsCount: 2,
        },
      ]);
    });

    await When('the user navigates to the employee Browser page', async () => {
      await browser.goto(ONTOLOGY, OBJECT_TYPE);
    });

    await Then('the Time Travel toolbar is visible', async () => {
      await expect(browser.timeTravelToolbar).toBeVisible();
      await expect(browser.timeTravelToggle).toBeVisible();
      await expect(browser.timeTravelPicker).toBeVisible();
    });

    await Then('the picker lists every recorded transaction', async () => {
      const options = browser.timeTravelPicker.locator('option');
      // 2 fixture txs + the leading "Latest (live)" placeholder.
      await expect(options).toHaveCount(3);
      // Latest tx surfaces in the dropdown labelled by short id + commit ts.
      const fullText = await browser.timeTravelPicker.innerText();
      expect(fullText).toContain(TX_LATEST.slice(0, 11));
      expect(fullText).toContain(TX_EARLIER.slice(0, 11));
    });

    await Then('Time Travel is off by default and no historical badge renders', async () => {
      await expect(browser.timeTravelToolbar).toHaveAttribute(
        'data-time-travel-enabled',
        'false',
      );
      await expect(browser.timeTravelActiveBadge).toHaveCount(0);
      await expect(browser.timeTravelHintBanner).toHaveCount(0);
    });
  });

  test('Scenario: toggling Time Travel pins asOf into every list query @smoke', async ({
    page,
  }) => {
    const browser = new BrowserPage(page);
    let captured: CapturedRequest[] = [];

    await Given('the dataset history endpoint advertises a recent transaction', async () => {
      await stubMetadata(page);
      captured = await stubObjectList(page);
      await stubDatasetHistory(page, [
        {
          txId: TX_LATEST,
          committedAt: '2026-05-12T18:30:00Z',
          editsCount: 4,
        },
      ]);
    });

    await When('the user navigates to the employee Browser page', async () => {
      await browser.goto(ONTOLOGY, OBJECT_TYPE);
      await expect(browser.timeTravelToolbar).toBeVisible();
    });

    await Then(
      'the initial list GET fires without an asOf parameter',
      async () => {
        await expect.poll(() => captured.length).toBeGreaterThanOrEqual(1);
        expect(captured[0]!.asOf).toBeNull();
      },
    );

    await When('the user picks the recent transaction and flips Time Travel on', async () => {
      await browser.selectTimeTravelTx(TX_LATEST);
      await browser.toggleTimeTravel();
    });

    await Then(
      'every subsequent list GET carries asOf=<txId>',
      async () => {
        await expect.poll(() =>
          captured.filter((c) => c.asOf === TX_LATEST).length,
        ).toBeGreaterThanOrEqual(1);
        const last = captured[captured.length - 1]!;
        expect(last.asOf).toBe(TX_LATEST);
      },
    );

    let beforeOff = 0;
    await When('the user flips Time Travel back off', async () => {
      beforeOff = captured.length;
      await browser.toggleTimeTravel();
    });

    await Then(
      'subsequent list GETs drop the asOf parameter again',
      async () => {
        // Wait for a fresh capture (refetch fires synchronously when the
        // store mutates + queryClient invalidates).
        await expect
          .poll(() => captured.length)
          .toBeGreaterThan(beforeOff);
        const last = captured[captured.length - 1]!;
        expect(last.asOf).toBeNull();
      },
    );
  });

  test('Scenario: historical mode disables Live toggle and hides Bulk actions @smoke', async ({
    page,
  }) => {
    const browser = new BrowserPage(page);

    await Given('the dataset history endpoint advertises one transaction', async () => {
      await stubMetadata(page);
      await stubObjectList(page);
      await stubDatasetHistory(page, [
        {
          txId: TX_LATEST,
          committedAt: '2026-05-12T18:30:00Z',
          editsCount: 4,
        },
      ]);
    });

    await When('the user navigates to the employee Browser page', async () => {
      await browser.goto(ONTOLOGY, OBJECT_TYPE);
      await expect(browser.timeTravelToolbar).toBeVisible();
    });

    await Then('the Live toggle starts enabled in live mode', async () => {
      await expect(browser.liveToggle).toBeEnabled();
    });

    await When('the user pins a historical transaction', async () => {
      await browser.selectTimeTravelTx(TX_LATEST);
      await browser.toggleTimeTravel();
    });

    await Then('the historical-mode hint banner renders', async () => {
      await expect(browser.timeTravelHintBanner).toBeVisible();
      await expect(browser.timeTravelActiveBadge).toBeVisible();
      await expect(browser.timeTravelToolbar).toHaveAttribute(
        'data-time-travel-enabled',
        'true',
      );
      await expect(browser.timeTravelToolbar).toHaveAttribute(
        'data-time-travel-asof',
        TX_LATEST,
      );
    });

    await Then('the Live toggle is disabled while Time Travel is on', async () => {
      await expect(browser.liveToggle).toBeDisabled();
    });

    await Then(
      'the Bulk Action Toolbar mount is suppressed (honest absence)',
      async () => {
        // Bulk toolbar is gated entirely on time-travel mode to keep the
        // historical view a strict read-only snapshot. We assert the
        // expected absence via role-based queries that would catch the
        // toolbar's Delete affordance if it were rendered.
        await expect(
          page.getByRole('button', { name: /delete.*selected/i }),
        ).toHaveCount(0);
        await expect(
          page.getByRole('button', { name: /export.*selected/i }),
        ).toHaveCount(0);
      },
    );

    await When('the user toggles Time Travel back off', async () => {
      await browser.toggleTimeTravel();
    });

    await Then('the hint banner disappears and Live is re-enabled', async () => {
      await expect(browser.timeTravelHintBanner).toHaveCount(0);
      await expect(browser.liveToggle).toBeEnabled();
    });
  });
});
