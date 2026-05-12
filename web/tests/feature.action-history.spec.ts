import { expect, test, type Page, type Route } from '@playwright/test';
import {
  ActionHistoryPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/actions/:ontology/history` — the Action History page
 * rendered by `src/components/actions/ActionHistoryPage.tsx`.
 *
 * Scenarios cover the AC for US-022 (frontend-backend-gap-coverage PRD):
 *   1. list-renders happy path with status badges + apiName mapping
 *   2. filter by FAILED status pushes `status=FAILED` through to the API
 *   3. filter by Action Type pushes `actionType=<apiName>` through
 *   4. opening a row's detail drawer shows parameters / edits JSON
 *   5. Undo on a SUCCESS row invokes /actions/revert + refreshes the list
 *   6. empty state when the history endpoint returns no rows
 *   7. error state when the history endpoint fails with 500
 *
 * All scenarios stub the action history + action types endpoints through
 * `page.route` so the page renders deterministic fixtures without touching
 * real backend data (matches US-021 dashboard pattern). The ontology slug
 * used in the URL is `northwind` because the dev seed already advertises
 * it — but the page itself never refetches the ontology, so the slug is
 * really just a router parameter.
 */

interface MockEntry {
  id: number;
  actionTypeRid: string;
  userId: string;
  status: 'SUCCESS' | 'FAILED' | 'REVERTED';
  errorMessage?: string;
  createdAt: string;
  parameters?: unknown;
  edits?: unknown;
  prevEdits?: unknown;
}

const actionTypes = [
  {
    rid: 'rid:at:create',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    parameters: {},
    status: 'ACTIVE',
  },
  {
    rid: 'rid:at:delete',
    apiName: 'deleteEmployee',
    displayName: 'Delete Employee',
    parameters: {},
    status: 'ACTIVE',
  },
];

const successEntry: MockEntry = {
  id: 101,
  actionTypeRid: 'rid:at:create',
  userId: 'user:alice',
  parameters: { companyName: 'Acme', customerID: 'ACME01' },
  edits: [{ type: 'createObject', objectType: 'customer' }],
  status: 'SUCCESS',
  createdAt: '2026-04-28T14:00:00Z',
};

const failedEntry: MockEntry = {
  id: 102,
  actionTypeRid: 'rid:at:delete',
  userId: 'user:bob',
  parameters: { customerID: 'ZZTOP' },
  edits: [],
  status: 'FAILED',
  errorMessage: 'precondition failed',
  createdAt: '2026-04-28T13:00:00Z',
};

const ONTOLOGY = 'northwind';

async function stubActionTypes(page: Page): Promise<void> {
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: actionTypes }),
      });
    },
  );
}

/**
 * Captures every observed history request so scenarios can assert that
 * the React Query refetch carries the expected query params (status,
 * actionType, userId). The route handler is GET-only and falls through
 * for the revert / detail endpoints rooted on the same prefix.
 */
function stubActionHistory(
  page: Page,
  rowsFor: (query: URLSearchParams) => MockEntry[],
  capturedQueries: URLSearchParams[],
): Promise<void> {
  return page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actions/history*`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = new URL(req.url());
      // Strip the `branch=` param injected by withActiveBranch when the
      // active branch is non-default — irrelevant to scenario assertions.
      url.searchParams.delete('branch');
      capturedQueries.push(url.searchParams);
      const rows = rowsFor(url.searchParams);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: rows, total: rows.length }),
      });
    },
  );
}

describeFeature('Action History page', () => {
  test('Scenario: the page renders the action execution list with status badges @smoke', async ({
    page,
    request,
  }) => {
    const history = new ActionHistoryPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the action history endpoint advertises two executions', async () => {
      await stubActionTypes(page);
      await stubActionHistory(page, () => [successEntry, failedEntry], captured);
    });

    await When('the user opens /actions/northwind/history', async () => {
      await history.goto(ONTOLOGY);
    });

    await Then('the page root is visible and loading skeleton has cleared', async () => {
      await expect(history.root).toBeVisible();
      await expect(history.loading).toBeHidden();
    });

    await Then('the list shows both rows with correct status + action type', async () => {
      await expect(history.list).toBeVisible();
      await expect(history.rows).toHaveCount(2);
      const successRow = history.rowByLogId(successEntry.id);
      const failedRow = history.rowByLogId(failedEntry.id);
      await expect(successRow).toContainText('SUCCESS');
      await expect(successRow).toContainText('createEmployee');
      await expect(failedRow).toContainText('FAILED');
      await expect(failedRow).toContainText('deleteEmployee');
    });
  });

  test('Scenario: switching to the Failed status tab pushes status=FAILED to the API @smoke', async ({
    page,
  }) => {
    const history = new ActionHistoryPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the action history endpoint mirrors the status filter', async () => {
      await stubActionTypes(page);
      await stubActionHistory(
        page,
        (q) => {
          const s = q.get('status');
          if (s === 'FAILED') return [failedEntry];
          if (s === 'SUCCESS') return [successEntry];
          return [successEntry, failedEntry];
        },
        captured,
      );
    });

    await Given('the user is on the Action History page', async () => {
      await history.goto(ONTOLOGY);
      await expect(history.list).toBeVisible();
    });

    await When('the user clicks the Failed status tab', async () => {
      await history.filterStatusFailed.click();
    });

    await Then('only the FAILED row remains in the list', async () => {
      await expect(history.rows).toHaveCount(1);
      await expect(history.rowByLogId(failedEntry.id)).toBeVisible();
    });

    await Then('the most recent /actions/history request carried status=FAILED', async () => {
      await expect
        .poll(() => captured.length > 0 && captured[captured.length - 1].get('status'))
        .toBe('FAILED');
    });
  });

  test('Scenario: selecting an action type pushes actionType=<apiName> to the API @smoke', async ({
    page,
  }) => {
    const history = new ActionHistoryPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the action history endpoint mirrors the actionType filter', async () => {
      await stubActionTypes(page);
      await stubActionHistory(
        page,
        (q) => {
          const a = q.get('actionType');
          if (a === 'createEmployee') return [successEntry];
          if (a === 'deleteEmployee') return [failedEntry];
          return [successEntry, failedEntry];
        },
        captured,
      );
    });

    await Given('the user is on the Action History page', async () => {
      await history.goto(ONTOLOGY);
      await expect(history.list).toBeVisible();
    });

    await When('the user picks "createEmployee" from the action type dropdown', async () => {
      await expect(history.filterActionType).toBeVisible();
      // Wait until the actionTypes query has populated the <option> list
      // (without this, selectOption can race against React Query settle).
      await expect(
        history.filterActionType.locator('option[value="createEmployee"]'),
      ).toHaveCount(1);
      await history.filterActionType.selectOption('createEmployee');
    });

    await Then('only the matching execution remains', async () => {
      await expect(history.rows).toHaveCount(1);
      await expect(history.rowByLogId(successEntry.id)).toBeVisible();
    });

    await Then('the most recent /actions/history request carried actionType=createEmployee', async () => {
      await expect
        .poll(() => captured.length > 0 && captured[captured.length - 1].get('actionType'))
        .toBe('createEmployee');
    });
  });

  test('Scenario: clicking View opens the detail drawer with parameters and edits', async ({
    page,
  }) => {
    const history = new ActionHistoryPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the action history endpoint returns one execution', async () => {
      await stubActionTypes(page);
      await stubActionHistory(page, () => [successEntry], captured);
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/history/${successEntry.id}*`,
        async (route) => {
          if (route.request().method() !== 'GET') {
            await route.continue();
            return;
          }
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              ...successEntry,
              prevEdits: { customer: 'Acme' },
            }),
          });
        },
      );
    });

    await Given('the user is on the Action History page', async () => {
      await history.goto(ONTOLOGY);
      await expect(history.list).toBeVisible();
    });

    await When('the user clicks View on the success row', async () => {
      await history.viewDetailButton(successEntry.id).click();
    });

    await Then('the detail drawer is visible', async () => {
      await expect(history.modalOverlay).toBeVisible();
      await expect(history.detail).toBeVisible();
    });

    await Then('the drawer surfaces the action type, parameters, edits, and prev-edits', async () => {
      await expect(history.detail).toContainText('createEmployee');
      // Open the <details> blocks so their <pre> contents become visible.
      await page.getByTestId('detail-parameters').scrollIntoViewIfNeeded();
      await expect(page.getByTestId('detail-parameters')).toContainText('Acme');
      await expect(page.getByTestId('detail-edits')).toContainText('createObject');
      await expect(page.getByTestId('detail-prev-edits')).toContainText('Acme');
    });
  });

  test('Scenario: clicking Undo on a SUCCESS row calls /actions/revert and refreshes the list', async ({
    page,
  }) => {
    const history = new ActionHistoryPage(page);
    const captured: URLSearchParams[] = [];

    let revertCalls = 0;
    let revertedOnServer = false;

    await Given('the action history endpoint and revert endpoint are stubbed', async () => {
      await stubActionTypes(page);
      await stubActionHistory(
        page,
        () => {
          if (revertedOnServer) {
            return [{ ...successEntry, status: 'REVERTED' }];
          }
          return [successEntry];
        },
        captured,
      );
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/revert*`,
        async (route) => {
          if (route.request().method() !== 'POST') {
            await route.continue();
            return;
          }
          revertCalls += 1;
          revertedOnServer = true;
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              operationId: 'op-reverse-101',
              edits: { addedObjectCount: 0, modifiedObjectsCount: 1, deletedObjectsCount: 0 },
            }),
          });
        },
      );
    });

    await Given('the user is on the Action History page', async () => {
      await history.goto(ONTOLOGY);
      await expect(history.list).toBeVisible();
    });

    await When('the user clicks Undo on the success row', async () => {
      await history.undoButton(successEntry.id).click();
    });

    await Then('the /actions/revert endpoint is invoked exactly once', async () => {
      await expect.poll(() => revertCalls).toBe(1);
    });

    await Then('the row status flips to REVERTED on refresh', async () => {
      const row = history.rowByLogId(successEntry.id);
      await expect(row).toContainText('REVERTED');
      // Undo button no longer rendered for non-SUCCESS rows.
      await expect(history.undoButton(successEntry.id)).toHaveCount(0);
    });
  });

  test('Scenario: the page renders the empty state when no executions exist', async ({
    page,
  }) => {
    const history = new ActionHistoryPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the action history endpoint returns an empty list', async () => {
      await stubActionTypes(page);
      await stubActionHistory(page, () => [], captured);
    });

    await When('the user opens the Action History page', async () => {
      await history.goto(ONTOLOGY);
    });

    await Then('the empty-state panel is visible', async () => {
      await expect(history.empty).toBeVisible();
    });

    await Then('no list rows are rendered', async () => {
      await expect(history.rows).toHaveCount(0);
      await expect(history.list).toHaveCount(0);
    });
  });

  test('Scenario: the page renders the error state when /actions/history fails with 500', async ({
    page,
  }) => {
    const history = new ActionHistoryPage(page);

    await Given('the action history endpoint is stubbed to return 500', async () => {
      await stubActionTypes(page);
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/history*`,
        async (route) => {
          if (route.request().method() !== 'GET') {
            await route.continue();
            return;
          }
          await route.fulfill({
            status: 500,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'INTERNAL',
              errorName: 'InternalError',
              errorInstanceId: 'spec',
              statusCode: 500,
            }),
          });
        },
      );
    });

    await When('the user opens the Action History page', async () => {
      await history.goto(ONTOLOGY);
    });

    await Then('the error panel is visible', async () => {
      await expect(history.error).toBeVisible();
    });

    await Then('the list and empty state are not rendered', async () => {
      await expect(history.list).toHaveCount(0);
      await expect(history.empty).toHaveCount(0);
    });
  });
});
