import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  QueryTypesSandboxPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/queries/:ontology` — the QueryTypes sandbox rendered by
 * `src/components/queries/QueryTypesSandboxPage.tsx` (US-055 / PC-A14).
 *
 * AC mapping → scenario:
 *   list page                        → list-renders-rows-with-status-badges
 *   parameter form per schema        → parameter-form-auto-generated-from-schema
 *   result table + JSON dual-view    → execute-and-render-result-in-table-and-json
 *   empty list state                 → empty-state-when-no-querytypes
 *
 * Stubs `page.route` so the page renders deterministic fixtures without
 * touching the real backend — same convention as US-051 ValueType admin /
 * US-054 Marketplace specs.
 */

const ONTOLOGY = 'northwind';

interface MockQueryType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  status: string;
  parameters: Array<{
    id: string;
    type: string;
    required?: boolean;
    description?: string;
  }>;
  output: Record<string, unknown>;
  query: Record<string, unknown>;
}

const topCustomers: MockQueryType = {
  rid: 'ri.ontology.main.querytype.top-customers',
  apiName: 'topCustomers',
  displayName: 'Top Customers',
  description: 'Customers ordered by total revenue',
  status: 'ACTIVE',
  parameters: [
    {
      id: 'limit',
      type: 'integer',
      required: true,
      description: 'Number of customers to return',
    },
    {
      id: 'region',
      type: 'string',
      required: false,
      description: 'Optional region filter',
    },
  ],
  output: {},
  query: {},
};

const recentOrders: MockQueryType = {
  rid: 'ri.ontology.main.querytype.recent-orders',
  apiName: 'recentOrders',
  displayName: 'Recent Orders',
  status: 'EXPERIMENTAL',
  parameters: [],
  output: {},
  query: {},
};

interface ExecuteCall {
  queryApiName: string;
  body: Record<string, unknown>;
}

interface StubState {
  rows: MockQueryType[];
  listCalls: number;
  executeCalls: ExecuteCall[];
  nextResult: Record<string, unknown> | null;
}

function makeState(initial: MockQueryType[]): StubState {
  return {
    rows: initial.map((r) => structuredClone(r)),
    listCalls: 0,
    executeCalls: [],
    nextResult: null,
  };
}

async function stubQueryTypeApi(page: Page, state: StubState): Promise<void> {
  // List endpoint — returns the canonical {data} envelope.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/queryTypes*`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'GET') {
        await route.continue();
        return;
      }
      state.listCalls += 1;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: state.rows }),
      });
    },
  );

  // Execute endpoint — capture the body and return the latched fixture.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/queries/*/execute*`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'POST') {
        await route.continue();
        return;
      }
      const url = new URL(req.url());
      const match = url.pathname.match(/\/queries\/([^/]+)\/execute$/);
      const queryApiName = match
        ? decodeURIComponent(match[1])
        : '';
      const body = req.postDataJSON() as Record<string, unknown>;
      state.executeCalls.push({ queryApiName, body });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          state.nextResult ?? { value: null },
        ),
      });
    },
  );
}

describeFeature('QueryTypes sandbox', () => {
  test('Scenario: the list renders QueryTypes with their status and apiName @smoke', async ({
    page,
    request,
  }) => {
    const sandbox = new QueryTypesSandboxPage(page);
    const state = makeState([topCustomers, recentOrders]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given(
      'two QueryTypes are seeded with distinct statuses',
      async () => {
        await stubQueryTypeApi(page, state);
      },
    );

    await When(
      'the user navigates to /queries/northwind',
      async () => {
        await sandbox.goto(ONTOLOGY);
      },
    );

    await Then(
      'the sandbox root + list are visible with both rows',
      async () => {
        await expect(sandbox.root).toBeVisible();
        await expect(sandbox.list).toBeVisible();
        await expect(sandbox.rows).toHaveCount(2);
      },
    );

    await Then(
      'each row exposes the apiName + status through its label',
      async () => {
        const top = sandbox.rowByApiName('topCustomers');
        const recent = sandbox.rowByApiName('recentOrders');
        await expect(top).toBeVisible();
        await expect(recent).toBeVisible();
        await expect(top).toContainText('topCustomers');
        await expect(top).toContainText('ACTIVE');
        await expect(recent).toContainText('EXPERIMENTAL');
        // Initial state: nothing selected yet — the detail empty hint is up.
        await expect(sandbox.detailEmpty).toBeVisible();
        // Count chip in the header mirrors the row count.
        await expect(sandbox.count).toContainText('2 QueryTypes');
      },
    );
  });

  test('Scenario: selecting a QueryType auto-generates the parameter form from its schema @smoke', async ({
    page,
  }) => {
    const sandbox = new QueryTypesSandboxPage(page);
    const state = makeState([topCustomers, recentOrders]);

    await Given('the QueryTypes endpoint is stubbed', async () => {
      await stubQueryTypeApi(page, state);
    });

    await Given('the user is on the sandbox', async () => {
      await sandbox.goto(ONTOLOGY);
      await expect(sandbox.list).toBeVisible();
    });

    await When(
      'the user picks Top Customers from the list',
      async () => {
        await sandbox.selectButton('topCustomers').click();
      },
    );

    await Then(
      'the detail pane shows the displayName and the form mirrors the schema',
      async () => {
        await expect(sandbox.detail).toBeVisible();
        await expect(sandbox.displayName).toHaveText('Top Customers');
        await expect(sandbox.parameterForm).toBeVisible();
        // The integer + string parameters surface as their typed inputs —
        // ParameterForm dispatches on dataType.type. We assert by input id
        // so the spec stays decoupled from label copy.
        const limit = sandbox.paramInput('limit');
        const region = sandbox.paramInput('region');
        await expect(limit).toBeVisible();
        await expect(region).toBeVisible();
        await expect(limit).toHaveAttribute('type', 'number');
        await expect(region).toHaveAttribute('type', 'text');
      },
    );

    await When(
      'the user picks Recent Orders (no parameters)',
      async () => {
        await sandbox.selectButton('recentOrders').click();
      },
    );

    await Then(
      'the form swaps to the no-parameters placeholder',
      async () => {
        await expect(sandbox.displayName).toHaveText('Recent Orders');
        await expect(sandbox.parameterForm).toContainText(
          'No parameters defined for this action.',
        );
      },
    );
  });

  test('Scenario: executing a QueryType POSTs the typed parameters and renders the result in both Table and JSON views @smoke', async ({
    page,
  }) => {
    const sandbox = new QueryTypesSandboxPage(page);
    const state = makeState([topCustomers]);
    // The latched payload is the Foundry-style `{ value: { customers: [...] } }`
    // wrapper the goja executor emits — the sandbox should derive the table
    // rows from the single array-valued nested key (`customers`).
    state.nextResult = {
      value: {
        customers: [
          { customerId: 'C-1', name: 'Alpha Inc', orderCount: 12 },
          { customerId: 'C-2', name: 'Beta LLC', orderCount: 7 },
        ],
        totalCount: 2,
      },
    };

    await Given(
      'the QueryTypes endpoint is stubbed with one ACTIVE query',
      async () => {
        await stubQueryTypeApi(page, state);
      },
    );

    await Given('the user is on the sandbox', async () => {
      await sandbox.goto(ONTOLOGY);
      await expect(sandbox.list).toBeVisible();
    });

    await When('the user selects Top Customers', async () => {
      await sandbox.selectButton('topCustomers').click();
      await expect(sandbox.parameterForm).toBeVisible();
    });

    await When(
      'the user fills the required integer parameter and submits',
      async () => {
        await sandbox.paramInput('limit').fill('2');
        // ParameterForm wires the integer input through react-hook-form's
        // setValueAs so blur → number coercion. Trigger blur explicitly so
        // the form picks up the change before submit.
        await sandbox.paramInput('limit').blur();
        await sandbox.executeButton.click();
      },
    );

    await Then(
      'POST /queries/topCustomers/execute was captured with the nested parameters envelope',
      async () => {
        await expect.poll(() => state.executeCalls.length).toBe(1);
        const call = state.executeCalls[0];
        expect(call.queryApiName).toBe('topCustomers');
        // The wire shape is {parameters: {...}} — the handler nests params
        // under that key so older Foundry SDKs round-trip.
        expect(call.body).toEqual({
          parameters: { limit: 2 },
        });
      },
    );

    await Then(
      'the result panel renders the Table view with column headers derived from the customer records',
      async () => {
        await expect(sandbox.resultPanel).toBeVisible();
        // Table view is the default.
        await expect(sandbox.resultTableTab).toHaveAttribute(
          'data-active',
          'true',
        );
        await expect(sandbox.resultTable).toBeVisible();
        await expect(sandbox.resultRows()).toHaveCount(2);
        // Columns are auto-derived from the union of keys across the rows.
        await expect(sandbox.resultColumns()).toHaveCount(3);
        await expect(
          page.getByTestId('query-result-column-customerId'),
        ).toBeVisible();
        await expect(
          page.getByTestId('query-result-column-name'),
        ).toBeVisible();
        await expect(
          page.getByTestId('query-result-column-orderCount'),
        ).toBeVisible();
      },
    );

    await When('the user toggles into JSON view', async () => {
      await sandbox.resultJsonTab.click();
    });

    await Then(
      'the JSON tab is active and the full result payload is on screen',
      async () => {
        await expect(sandbox.resultJsonTab).toHaveAttribute(
          'data-active',
          'true',
        );
        await expect(sandbox.resultJson).toBeVisible();
        await expect(sandbox.resultJson).toContainText('"totalCount": 2');
        await expect(sandbox.resultJson).toContainText('"customerId"');
      },
    );
  });

  test('Scenario: the sandbox shows an empty state when the ontology defines no QueryTypes', async ({
    page,
  }) => {
    const sandbox = new QueryTypesSandboxPage(page);
    const state = makeState([]);

    await Given(
      'the QueryTypes endpoint is stubbed with no rows',
      async () => {
        await stubQueryTypeApi(page, state);
      },
    );

    await When('the user navigates to /queries/northwind', async () => {
      await sandbox.goto(ONTOLOGY);
    });

    await Then('the sandbox shows the empty list affordance', async () => {
      await expect(sandbox.root).toBeVisible();
      await expect(sandbox.empty).toBeVisible();
      // Detail pane stays in its placeholder state since nothing can be picked.
      await expect(sandbox.detailEmpty).toBeVisible();
      // Header count chip pluralises "0 QueryTypes".
      await expect(sandbox.count).toContainText('0 QueryTypes');
    });
  });
});
