import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  ObjectSetDiffPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/objectsets/:ontology/diff` — the ObjectSet Diff Viewer
 * rendered by `src/components/objectsets/ObjectSetDiffPage.tsx`.
 *
 * Scenarios map the PRD AC for US-028 (frontend-backend-gap-coverage):
 *   AC: "至少 3 scenarios: 保存两次快照-diff 高亮 / 新增·删除·修改三色标识 /
 *        导出 diff CSV"。
 *
 * Mapping notes (honest mapping, mirroring US-025/026/027):
 *   - "保存两次快照-diff 高亮": the diff page reads SavedObjectSets from
 *     localStorage (the same key the Composer writes); scenarios seed two
 *     entries via `addInitScript` and assert the lifecycle pending →
 *     results transition (locks `objectset-diff-pending` and
 *     `objectset-diff-results` testids in lockstep with their state
 *     branches).
 *   - "新增/删除/修改三色标识": the three categories surface under three
 *     stable testids (`diff-only-in-a` / `diff-only-in-b` /
 *     `diff-changed`). We assert each category's representative PK by
 *     `within(section).getByText(...)`, which locks the row → section
 *     contract; the three accent colour classes (cyan / amber / magenta)
 *     are CSS-only and stay implicit in the section structure.
 *   - "导出 diff CSV": the page has no export affordance today. Per the
 *     US-025/026 honest-mapping rule we keep an explicit absence
 *     assertion that documents the gap and would fail the day an
 *     "Export CSV" button is added without follow-up.
 *
 * Each scenario:
 *   - seeds saved ObjectSets via `addInitScript` so React Query's
 *     `useSavedObjectSets` reads them on the first render (no
 *     `localStorage.setItem` after navigation, which would run too late);
 *   - stubs `objectTypes/:apiName` for schema lookup and
 *     `objectSets/loadObjects` to deterministically return A's and B's
 *     row sets so the diff result is byte-exact;
 *   - drives the page through the existing
 *     `<select aria-label="Object Set A|B">` controls; the only new
 *     selectors invented here are the four state-branch wrappers and the
 *     compute button (see ObjectSetDiffPage.tsx).
 */

const ONTOLOGY = 'northwind';
const EMPLOYEE = 'employee';

interface LoadObjectsBody {
  objectSet: { type: string } & Record<string, unknown>;
  select?: string[];
  pageSize?: number;
}

interface CapturedLoad {
  body: LoadObjectsBody;
}

function employeeObjectType(): {
  rid: string;
  apiName: string;
  displayName: string;
  primaryKey: string;
  status: string;
  visibility: string;
  properties: Record<string, { dataType: { type: string }; rid: string }>;
} {
  return {
    rid: 'ri.ontology.main.object-type.employee',
    apiName: EMPLOYEE,
    displayName: 'Employee',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    properties: {
      employeeId: { dataType: { type: 'string' }, rid: 'ri.p.employee.id' },
      name: { dataType: { type: 'string' }, rid: 'ri.p.employee.name' },
      title: { dataType: { type: 'string' }, rid: 'ri.p.employee.title' },
    },
  };
}

const ROWS_A = [
  {
    __rid: 'ri.obj.employee.alice',
    __primaryKey: 'EMP-1',
    __apiName: EMPLOYEE,
    employeeId: 'EMP-1',
    name: 'Alice',
    title: 'Engineer',
  },
  {
    __rid: 'ri.obj.employee.bob',
    __primaryKey: 'EMP-2',
    __apiName: EMPLOYEE,
    employeeId: 'EMP-2',
    name: 'Bob',
    title: 'Manager',
  },
  {
    __rid: 'ri.obj.employee.carol',
    __primaryKey: 'EMP-3',
    __apiName: EMPLOYEE,
    employeeId: 'EMP-3',
    name: 'Carol',
    title: 'Analyst',
  },
];

const ROWS_B = [
  // EMP-1 (Alice) absent → only-in-A
  {
    __rid: 'ri.obj.employee.bob.b',
    __primaryKey: 'EMP-2',
    __apiName: EMPLOYEE,
    employeeId: 'EMP-2',
    name: 'Bob',
    title: 'Manager',
  },
  {
    __rid: 'ri.obj.employee.carol.b',
    __primaryKey: 'EMP-3',
    __apiName: EMPLOYEE,
    employeeId: 'EMP-3',
    name: 'Caroline', // changed value (renamed)
    title: 'Senior Analyst', // changed value (promoted)
  },
  {
    __rid: 'ri.obj.employee.dave.b',
    __primaryKey: 'EMP-4',
    __apiName: EMPLOYEE,
    employeeId: 'EMP-4',
    name: 'Dave',
    title: 'Designer',
  },
];

/**
 * Stub the three endpoints the diff page hits before/after Compute:
 *   - GET /api/v2/ontologies/{ont}/objectTypes (list for typesLoading flag)
 *   - GET /api/v2/ontologies/{ont}/objectTypes/{apiName} (schema lookup)
 *   - POST /api/v2/ontologies/{ont}/objectSets/loadObjects ×2 (A and B
 *     branches; we alternate responses by call ordinal so two POSTs in
 *     `Promise.all` produce two different row sets regardless of which
 *     one Playwright resolves first)
 *
 * The `loadObjects` handler captures the wire body so scenarios can lock
 * the "page sends a base employee def with the right `select` list".
 *
 * `rowsForOrdinal(0|1)` lets a single test return either two non-empty
 * pages (the happy three-category scenario) or both empty (the
 * pending-after-load smoke).
 */
async function stubDiffEndpoints(
  page: Page,
  captured: CapturedLoad[],
  rowsForOrdinal: (
    ordinal: number,
    body: LoadObjectsBody,
  ) => Array<Record<string, unknown>>,
): Promise<void> {
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [employeeObjectType()] }),
      });
    },
  );

  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/*`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = new URL(route.request().url());
      const apiName = url.pathname.split('/').pop() ?? '';
      if (apiName !== EMPLOYEE) {
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
        body: JSON.stringify(employeeObjectType()),
      });
    },
  );

  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectSets/loadObjects`,
    async (route: Route) => {
      if (route.request().method() !== 'POST') {
        await route.continue();
        return;
      }
      const body = route.request().postDataJSON() as LoadObjectsBody;
      const ordinal = captured.length;
      captured.push({ body });
      const rows = rowsForOrdinal(ordinal, body);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: rows,
          totalCount: String(rows.length),
        }),
      });
    },
  );
}

describeFeature('ObjectSet Diff Viewer', () => {
  test('Scenario: saved snapshots → pending state until Compute is clicked @smoke', async ({
    page,
    request,
  }) => {
    // Locks the "保存两次快照" lifecycle: the page reads two
    // SavedObjectSets from localStorage on mount, surfaces the
    // "Pick two saved object sets" pending wrapper, keeps Compute Diff
    // disabled until both selects have a value, and only then enables
    // the click target. No network requests fire from this scenario.
    const diff = new ObjectSetDiffPage(page);
    const captured: CapturedLoad[] = [];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('two SavedObjectSets are persisted for the ontology', async () => {
      await diff.seedSavedObjectSets(ONTOLOGY, [
        { id: 'sa-1', name: 'Snapshot A', objectType: EMPLOYEE },
        { id: 'sb-1', name: 'Snapshot B', objectType: EMPLOYEE },
      ]);
      await stubDiffEndpoints(page, captured, () => []);
    });

    await When('the user opens /objectsets/northwind/diff', async () => {
      await diff.goto(ONTOLOGY);
    });

    await Then('the diff page renders with the pending empty state', async () => {
      await expect(diff.root).toBeVisible();
      await expect(diff.pending).toBeVisible();
      await expect(diff.results).toHaveCount(0);
    });

    await Then('Compute Diff is disabled until both selects are filled', async () => {
      await expect(diff.computeBtn).toBeDisabled();
      await diff.savedASelect().selectOption('sa-1');
      await expect(diff.computeBtn).toBeDisabled();
      await diff.savedBSelect().selectOption('sb-1');
      await expect(diff.computeBtn).toBeEnabled();
    });

    await Then('no loadObjects POST has fired before the user clicks Compute', async () => {
      expect(captured.length).toBe(0);
    });
  });

  test('Scenario: Compute Diff surfaces three sections — only-in-A, only-in-B, changed @smoke', async ({
    page,
  }) => {
    // Happy path: A has {EMP-1 Alice, EMP-2 Bob, EMP-3 Carol},
    // B has {EMP-2 Bob, EMP-3 Caroline*, EMP-4 Dave}. The three
    // sections should isolate EMP-1 (only-in-A), EMP-4 (only-in-B), and
    // EMP-3 (changed: name + title differ).
    //
    // Locks both the "新增/删除/修改三色标识" three-category contract
    // (each section's testid is hit) and the "diff 高亮" lifecycle
    // (pending → results wrapper swap).
    const diff = new ObjectSetDiffPage(page);
    const captured: CapturedLoad[] = [];

    await Given('two SavedObjectSets resolve to disjoint employee rows', async () => {
      await diff.seedSavedObjectSets(ONTOLOGY, [
        { id: 'sa-1', name: 'Snapshot A', objectType: EMPLOYEE },
        { id: 'sb-1', name: 'Snapshot B', objectType: EMPLOYEE },
      ]);
      // Ordinal 0 → A's rows; ordinal 1 → B's rows. The diff page
      // issues both POSTs inside a Promise.all but the route handler
      // captures them in order of arrival, which is sequential within
      // a single browser context — the alternate-by-ordinal trick is
      // the same one the existing vitest uses (just keyed on captured
      // count instead of a boolean).
      await stubDiffEndpoints(page, captured, (ordinal) =>
        ordinal === 0 ? ROWS_A : ROWS_B,
      );
    });

    await Given('the user is on the diff page', async () => {
      await diff.goto(ONTOLOGY);
      await expect(diff.root).toBeVisible();
      await expect(diff.pending).toBeVisible();
    });

    await When('the user picks Snapshot A and Snapshot B then clicks Compute Diff', async () => {
      await diff.savedASelect().selectOption('sa-1');
      await diff.savedBSelect().selectOption('sb-1');
      await diff.computeBtn.click();
    });

    await Then('the page swaps from pending to results', async () => {
      await expect(diff.results).toBeVisible();
      await expect(diff.pending).toHaveCount(0);
    });

    await Then('both branches were loaded with the same select list', async () => {
      await expect.poll(() => captured.length).toBe(2);
      expect(captured[0].body.select).toEqual(['employeeId', 'name', 'title']);
      expect(captured[1].body.select).toEqual(['employeeId', 'name', 'title']);
      expect(captured[0].body.objectSet).toEqual({
        type: 'base',
        objectType: EMPLOYEE,
      });
      expect(captured[1].body.objectSet).toEqual({
        type: 'base',
        objectType: EMPLOYEE,
      });
    });

    await Then('only-in-A shows the orphan PK from snapshot A (EMP-1 Alice)', async () => {
      await expect(diff.onlyInA).toBeVisible();
      await expect(diff.onlyInA).toContainText('Alice');
      await expect(diff.onlyInA).toContainText('EMP-1');
      await expect(diff.onlyInA).not.toContainText('Bob');
      await expect(diff.onlyInA).not.toContainText('Carol');
    });

    await Then('only-in-B shows the orphan PK from snapshot B (EMP-4 Dave)', async () => {
      await expect(diff.onlyInB).toBeVisible();
      await expect(diff.onlyInB).toContainText('Dave');
      await expect(diff.onlyInB).toContainText('EMP-4');
      await expect(diff.onlyInB).not.toContainText('Alice');
      await expect(diff.onlyInB).not.toContainText('Bob');
    });

    await Then('changed shows EMP-3 with both side-by-side field values', async () => {
      await expect(diff.changed).toBeVisible();
      await expect(diff.changed).toContainText('EMP-3');
      // name: Carol → Caroline
      await expect(diff.changed).toContainText('Carol');
      await expect(diff.changed).toContainText('Caroline');
      // title: Analyst → Senior Analyst
      await expect(diff.changed).toContainText('Analyst');
      await expect(diff.changed).toContainText('Senior Analyst');
      // EMP-2 Bob is identical in both branches — must not appear
      // in any section.
      await expect(diff.changed).not.toContainText('Bob');
    });
  });

  test('Scenario: the page exposes no CSV-export affordance today', async ({
    page,
  }) => {
    // Honest mapping for AC "导出 diff CSV": the diff page in its
    // current shape (read-only viewer over two saved sets) has no
    // export button. We document the gap explicitly so the day a PR
    // adds an "Export CSV" button, this scenario fails and the team
    // must replace this absence assertion with a click-driven one.
    // Same pattern as US-025 "回滚 → no rollback button" and
    // US-026 "stale-pending → no TIMED_OUT badge".
    const diff = new ObjectSetDiffPage(page);
    const captured: CapturedLoad[] = [];

    await Given('the diff page is set up with two saved sets and stubs', async () => {
      await diff.seedSavedObjectSets(ONTOLOGY, [
        { id: 'sa-1', name: 'Snapshot A', objectType: EMPLOYEE },
        { id: 'sb-1', name: 'Snapshot B', objectType: EMPLOYEE },
      ]);
      await stubDiffEndpoints(page, captured, (ordinal) =>
        ordinal === 0 ? ROWS_A : ROWS_B,
      );
    });

    await When('the user reaches the post-Compute results state', async () => {
      await diff.goto(ONTOLOGY);
      await diff.savedASelect().selectOption('sa-1');
      await diff.savedBSelect().selectOption('sb-1');
      await diff.computeBtn.click();
      await expect(diff.results).toBeVisible();
    });

    await Then('no "Export CSV" affordance is present on the page', async () => {
      // Accept variants like "Export CSV", "Download CSV", "Export
      // diff CSV", or a plain CSV button label — all of them would
      // count as adding the missing affordance.
      await expect(
        page.getByRole('button', { name: /export\s*(diff)?\s*csv|download\s*csv|\bcsv\b/i }),
      ).toHaveCount(0);
      await expect(
        page.getByRole('link', { name: /export\s*(diff)?\s*csv|download\s*csv|\bcsv\b/i }),
      ).toHaveCount(0);
    });
  });

  test('Scenario: empty-saved-sets state surfaces the no-saved-sets wrapper', async ({
    page,
  }) => {
    // State-branch lock: when zero SavedObjectSets exist in
    // localStorage and the typesLoading flag is false, the diff page
    // must render the `objectset-diff-no-saved-sets` wrapper instead
    // of the pending pane. A future refactor that collapses the two
    // empties into one fails this scenario.
    const diff = new ObjectSetDiffPage(page);
    const captured: CapturedLoad[] = [];

    await Given('no SavedObjectSets are persisted for the ontology', async () => {
      // Intentionally no `seedSavedObjectSets` call — the route loads
      // with an empty list. We still stub `/objectTypes` so
      // typesLoading flips to false (the empty branch is gated on
      // !typesLoading to avoid flicker during the initial fetch).
      await stubDiffEndpoints(page, captured, () => []);
    });

    await When('the user opens /objectsets/northwind/diff', async () => {
      await diff.goto(ONTOLOGY);
    });

    await Then('the no-saved-sets empty state is visible', async () => {
      await expect(diff.root).toBeVisible();
      await expect(diff.noSavedSets).toBeVisible();
      await expect(diff.pending).toHaveCount(0);
      await expect(diff.results).toHaveCount(0);
    });

    await Then('Compute Diff is disabled (no sets to pick from)', async () => {
      await expect(diff.computeBtn).toBeDisabled();
    });
  });

  test('Scenario: a non-base saved set surfaces the static-root-type error', async ({
    page,
  }) => {
    // Edge case: SavedObjectSet whose def is a root-level
    // `searchAround` returns "" from `staticRootType(def)` (mirrors
    // the same gap US-027 documents in the composer). The diff page
    // catches the resulting "Cannot statically resolve..." error and
    // renders it inside the `objectset-diff-error` wrapper.
    const diff = new ObjectSetDiffPage(page);
    const captured: CapturedLoad[] = [];

    await Given('saved sets include a searchAround whose static root cannot resolve', async () => {
      // We bypass `seedSavedObjectSets` (which only emits `base`
      // defs) and write the searchAround tree by hand to mimic what
      // the composer's Save-As emits.
      const key = `weave:objectSets:${ONTOLOGY}`;
      const now = new Date().toISOString();
      const payload = [
        {
          id: 'sa-search',
          name: 'Snapshot A',
          def: {
            type: 'searchAround',
            objectSet: { type: 'base', objectType: EMPLOYEE },
            link: 'worksIn',
            direction: 'forward',
          },
          createdAt: now,
          versions: [
            {
              versionId: 'sa-search-v1',
              def: {
                type: 'searchAround',
                objectSet: { type: 'base', objectType: EMPLOYEE },
                link: 'worksIn',
                direction: 'forward',
              },
              createdAt: now,
            },
          ],
          activeVersionId: 'sa-search-v1',
        },
        {
          id: 'sb-search',
          name: 'Snapshot B',
          def: {
            type: 'searchAround',
            objectSet: { type: 'base', objectType: EMPLOYEE },
            link: 'worksIn',
            direction: 'forward',
          },
          createdAt: now,
          versions: [
            {
              versionId: 'sb-search-v1',
              def: {
                type: 'searchAround',
                objectSet: { type: 'base', objectType: EMPLOYEE },
                link: 'worksIn',
                direction: 'forward',
              },
              createdAt: now,
            },
          ],
          activeVersionId: 'sb-search-v1',
        },
      ];
      await page.addInitScript(
        ({ k, v }: { k: string; v: string }) => {
          window.localStorage.setItem(k, v);
        },
        { k: key, v: JSON.stringify(payload) },
      );
      await stubDiffEndpoints(page, captured, () => []);
    });

    await When('the user picks both searchAround sets and clicks Compute', async () => {
      await diff.goto(ONTOLOGY);
      await diff.savedASelect().selectOption('sa-search');
      await diff.savedBSelect().selectOption('sb-search');
      await diff.computeBtn.click();
    });

    await Then('the error wrapper surfaces the static-root-type message', async () => {
      await expect(diff.error).toBeVisible();
      await expect(diff.error).toContainText(/cannot statically resolve/i);
      // The error branch short-circuits before any loadObjects POST.
      expect(captured.length).toBe(0);
      await expect(diff.results).toHaveCount(0);
    });
  });
});
