import { expect, test, type Page, type Route } from '@playwright/test';
import {
  BindingsTab,
  Given,
  ObjectTypeAdminPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of the Bindings tab inside the ObjectType edit modal
 * (`/admin/:ontology/objectTypes` → Edit → Bindings tab), rendered by
 * `src/components/admin/BindingsEditor.tsx` (US-052 / PC-A06).
 *
 * AC mapping → scenario:
 *   table renders dataset/table/columnMapping/lineage trigger
 *     → list-renders-rows-with-mapping-and-lineage-badge @smoke
 *   create with column mapping form
 *     → create-flow-posts-binding-with-mapping-and-refreshes @smoke
 *   delete
 *     → delete-flow-removes-row
 *   edit
 *     → update-flow-puts-mapping-and-bumps-row
 *
 * Stubs the admin endpoints with `page.route` so the page renders
 * deterministic fixtures without touching real backend data. Mutations
 * latch a `state` object so subsequent refetches reflect the server
 * transition. Same convention as US-051 ValueType admin spec.
 */

const ONTOLOGY = 'northwind';
const OBJECT_TYPE_API_NAME = 'employee';
const OBJECT_TYPE_RID = 'ri.ontology.main.object-type.employee';

interface MockObjectType {
  rid: string;
  apiName: string;
  displayName: string;
  pluralDisplayName: string;
  description: string;
  primaryKey: string;
  status: string;
  visibility: string;
}

interface MockBinding {
  rid: string;
  objectTypeRid: string;
  datasetRid: string;
  branch: string;
  columnMapping: Record<string, string>;
  isPrimary: boolean;
}

const employeeOT: MockObjectType = {
  rid: OBJECT_TYPE_RID,
  apiName: OBJECT_TYPE_API_NAME,
  displayName: 'Employee',
  pluralDisplayName: 'Employees',
  description: '',
  primaryKey: 'employeeId',
  status: 'ACTIVE',
  visibility: 'PROMINENT',
};

const primaryBinding: MockBinding = {
  rid: 'ri.ontology.main.datasource-binding.employee-primary',
  objectTypeRid: OBJECT_TYPE_RID,
  datasetRid: 'ri.dataset.main.dataset.northwind-employees',
  branch: 'main',
  columnMapping: {
    employeeId: 'employee_id',
    firstName: 'first_name',
    lastName: 'last_name',
  },
  isPrimary: true,
};

const secondaryBinding: MockBinding = {
  rid: 'ri.ontology.main.datasource-binding.employee-mirror',
  objectTypeRid: OBJECT_TYPE_RID,
  datasetRid: 'ri.dataset.main.dataset.northwind-employees-mirror',
  branch: 'preview',
  columnMapping: {},
  isPrimary: false,
};

interface StubState {
  rows: MockBinding[];
  listCalls: number;
  createCalls: number;
  createBody: unknown;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
}

function makeState(initial: MockBinding[]): StubState {
  return {
    rows: initial.map((b) => structuredClone(b)),
    listCalls: 0,
    createCalls: 0,
    createBody: null,
    updateCalls: [],
    deleteCalls: [],
  };
}

async function stubObjectTypesList(page: Page): Promise<void> {
  // The ObjectType admin page must successfully fetch its ObjectType list
  // before the Edit modal can mount. Stub the list with a single
  // employee row so admin.editButtonFor() resolves deterministically.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: [employeeOT] }),
        });
        return;
      }
      await route.continue();
    },
  );
  // Outgoing link types + action types are fetched by the page on mount
  // for the delete-impact column. Return empty arrays so the page renders
  // without race-prone 404s.
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/objectTypes/[^/]+/outgoingLinkTypes`,
    ),
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    },
  );
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    },
  );
  // The Properties tab fetches per-ObjectType properties; an empty list is
  // sufficient for these scenarios which never open the Properties tab.
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/[^/]+/properties`,
    ),
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: [] }),
        });
        return;
      }
      await route.continue();
    },
  );
}

async function stubBindings(page: Page, state: StubState): Promise<void> {
  // List + Create live under the per-ObjectType collection endpoint.
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/[^/]+/datasourceBindings`,
    ),
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'GET') {
        state.listCalls += 1;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: state.rows }),
        });
        return;
      }
      if (req.method() === 'POST') {
        state.createCalls += 1;
        const body = req.postDataJSON() as Record<string, unknown>;
        state.createBody = body;
        const created: MockBinding = {
          rid: `ri.ontology.main.datasource-binding.${Date.now()}`,
          objectTypeRid: OBJECT_TYPE_RID,
          datasetRid: String(body.datasetRid ?? ''),
          branch: String(body.branch ?? 'main'),
          columnMapping:
            (body.columnMapping as Record<string, string> | undefined) ?? {},
          isPrimary: Boolean(body.isPrimary),
        };
        state.rows.push(created);
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(created),
        });
        return;
      }
      await route.continue();
    },
  );
  // Per-binding endpoints (PUT + DELETE).
  await page.route(
    new RegExp(
      `/api/v2/ontologies/${ONTOLOGY}/datasourceBindings/byRid/[^/]+`,
    ),
    async (route: Route) => {
      const req = route.request();
      const url = new URL(req.url());
      const match = url.pathname.match(
        /\/datasourceBindings\/byRid\/([^/]+)$/,
      );
      const rid = match ? decodeURIComponent(match[1]) : '';
      if (req.method() === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.updateCalls.push({ rid, body });
        const idx = state.rows.findIndex((r) => r.rid === rid);
        if (idx >= 0) {
          state.rows[idx] = {
            ...state.rows[idx],
            datasetRid: String(
              body.datasetRid ?? state.rows[idx].datasetRid,
            ),
            branch: String(body.branch ?? state.rows[idx].branch),
            columnMapping:
              (body.columnMapping as Record<string, string> | undefined) ??
              state.rows[idx].columnMapping,
            isPrimary: Boolean(body.isPrimary),
          };
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(state.rows[idx]),
          });
          return;
        }
        await route.fulfill({ status: 404, body: '' });
        return;
      }
      if (req.method() === 'DELETE') {
        state.deleteCalls.push(rid);
        state.rows = state.rows.filter((r) => r.rid !== rid);
        await route.fulfill({ status: 204, body: '' });
        return;
      }
      await route.continue();
    },
  );
}

async function openBindingsTab(
  admin: ObjectTypeAdminPage,
  bindings: BindingsTab,
): Promise<void> {
  await admin.goto(ONTOLOGY);
  await expect(admin.table).toBeVisible();
  await admin.editButtonFor(OBJECT_TYPE_API_NAME).click();
  await expect(bindings.editTabBindings).toBeVisible();
  await bindings.editTabBindings.click();
  await expect(bindings.editor).toBeVisible();
}

describeFeature('Admin: ObjectType Bindings tab', () => {
  test('Scenario: the bindings tab lists dataset / column-mapping count / lineage trigger + primary badge @smoke', async ({
    page,
    request,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const bindings = new BindingsTab(page);
    const state = makeState([primaryBinding, secondaryBinding]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given(
      'two bindings (one primary with column mapping, one mirror without) are seeded',
      async () => {
        await stubObjectTypesList(page);
        await stubBindings(page, state);
      },
    );

    await When(
      'the user opens the ObjectType admin page, edits employee, and switches to the Bindings tab',
      async () => {
        await openBindingsTab(admin, bindings);
      },
    );

    await Then(
      'the table renders two rows with dataset RID + per-row data attrs',
      async () => {
        await expect(bindings.table).toBeVisible();
        await expect(bindings.rows).toHaveCount(2);
        // GET /datasourceBindings happens at least once on mount.
        await expect.poll(() => state.listCalls).toBeGreaterThanOrEqual(1);

        const primaryRow = bindings.rowByRid(primaryBinding.rid);
        await expect(primaryRow).toHaveAttribute(
          'data-binding-dataset-rid',
          primaryBinding.datasetRid,
        );
        await expect(primaryRow).toHaveAttribute(
          'data-binding-mapping-count',
          '3',
        );
        await expect(primaryRow).toHaveAttribute(
          'data-binding-lineage-triggers',
          'true',
        );
        await expect(primaryRow).toHaveAttribute(
          'data-binding-is-primary',
          'true',
        );

        const mirrorRow = bindings.rowByRid(secondaryBinding.rid);
        await expect(mirrorRow).toHaveAttribute(
          'data-binding-branch',
          'preview',
        );
        await expect(mirrorRow).toHaveAttribute(
          'data-binding-mapping-count',
          '0',
        );
        await expect(mirrorRow).toHaveAttribute(
          'data-binding-lineage-triggers',
          'false',
        );
        await expect(mirrorRow).toHaveAttribute(
          'data-binding-is-primary',
          'false',
        );
      },
    );
  });

  test('Scenario: creating a binding POSTs dataset + column mapping and the new row joins the table @smoke', async ({
    page,
    request,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const bindings = new BindingsTab(page);
    const state = makeState([]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('no bindings are seeded yet', async () => {
      await stubObjectTypesList(page);
      await stubBindings(page, state);
    });

    await When(
      'the user opens the bindings tab and clicks Add Binding',
      async () => {
        await openBindingsTab(admin, bindings);
        await expect(bindings.empty).toBeVisible();
        await bindings.newButton.click();
        await expect(bindings.createForm).toBeVisible();
      },
    );

    await When(
      'the user fills the dataset RID, two column mappings, and submits',
      async () => {
        await bindings.createDatasetRid.fill(
          'ri.dataset.main.dataset.northwind-employees',
        );
        // Pre-existing first row exists by default; the user fills it
        // (property + column) and adds a second row.
        await bindings.mappingPropertyAt(0).fill('employeeId');
        await bindings.mappingColumnAt(0).fill('employee_id');
        await bindings.mappingAddButton.click();
        await bindings.mappingPropertyAt(1).fill('firstName');
        await bindings.mappingColumnAt(1).fill('first_name');
        await bindings.createIsPrimary.check();
        // requestSubmit() drives React's onSubmit regardless of viewport
        // — same pattern as US-029 admin builder modals.
        await bindings.createForm.evaluate((form) =>
          (form as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'POST /datasourceBindings was invoked exactly once with the mapping payload',
      async () => {
        await expect.poll(() => state.createCalls).toBe(1);
        const body = state.createBody as Record<string, unknown>;
        expect(body).toMatchObject({
          datasetRid: 'ri.dataset.main.dataset.northwind-employees',
          branch: 'main',
          isPrimary: true,
        });
        // columnMapping is an object map (property → upstream column).
        expect(body.columnMapping).toEqual({
          employeeId: 'employee_id',
          firstName: 'first_name',
        });
      },
    );

    await Then(
      'the modal closes and the new row appears with mapping-count + primary badge',
      async () => {
        await expect(bindings.createForm).toBeHidden();
        await expect(bindings.rows).toHaveCount(1);
        const row = bindings.rows.first();
        await expect(row).toHaveAttribute('data-binding-mapping-count', '2');
        await expect(row).toHaveAttribute('data-binding-is-primary', 'true');
        await expect(row).toHaveAttribute(
          'data-binding-lineage-triggers',
          'true',
        );
      },
    );
  });

  test('Scenario: deleting a binding hits DELETE and the row disappears', async ({
    page,
    request,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const bindings = new BindingsTab(page);
    const state = makeState([primaryBinding]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('one binding is seeded', async () => {
      await stubObjectTypesList(page);
      await stubBindings(page, state);
    });

    await When(
      'the user opens the bindings tab and confirms delete on the row',
      async () => {
        await openBindingsTab(admin, bindings);
        await expect(bindings.rows).toHaveCount(1);
        await bindings.deleteButtonFor(primaryBinding.rid).click();
        await expect(bindings.deleteModal).toBeVisible();
        await bindings.deleteSubmit.click();
      },
    );

    await Then(
      'DELETE /datasourceBindings was invoked exactly once for the row',
      async () => {
        await expect.poll(() => state.deleteCalls).toEqual([primaryBinding.rid]);
      },
    );

    await Then(
      'the delete modal closes and the table falls back to the empty state',
      async () => {
        await expect(bindings.deleteModal).toBeHidden();
        await expect(bindings.empty).toBeVisible();
        await expect(bindings.rows).toHaveCount(0);
      },
    );
  });

  test('Scenario: editing a binding PUTs the new dataset + mapping and refreshes the row', async ({
    page,
    request,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const bindings = new BindingsTab(page);
    const state = makeState([primaryBinding]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('one primary binding is seeded with three mappings', async () => {
      await stubObjectTypesList(page);
      await stubBindings(page, state);
    });

    await When(
      'the user opens the Edit modal, replaces the dataset, adds a fourth mapping row, and saves',
      async () => {
        await openBindingsTab(admin, bindings);
        await bindings.editButtonFor(primaryBinding.rid).click();
        await expect(bindings.editForm).toBeVisible();
        await bindings.editDatasetRid.fill(
          'ri.dataset.main.dataset.northwind-employees-v2',
        );
        // Add a new mapping row at index 3 (after the three seeded ones).
        await bindings.mappingAddButton.click();
        await bindings.mappingPropertyAt(3).fill('title');
        await bindings.mappingColumnAt(3).fill('job_title');
        await bindings.editForm.evaluate((form) =>
          (form as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'PUT /datasourceBindings was invoked once with the updated dataset and mapping payload',
      async () => {
        await expect.poll(() => state.updateCalls.length).toBe(1);
        const call = state.updateCalls[0]!;
        expect(call.rid).toBe(primaryBinding.rid);
        const body = call.body as Record<string, unknown>;
        expect(body).toMatchObject({
          datasetRid: 'ri.dataset.main.dataset.northwind-employees-v2',
        });
        expect(body.columnMapping).toEqual({
          employeeId: 'employee_id',
          firstName: 'first_name',
          lastName: 'last_name',
          title: 'job_title',
        });
      },
    );

    await Then(
      'the row reflects the new dataset and updated mapping count',
      async () => {
        await expect(bindings.editForm).toBeHidden();
        const row = bindings.rowByRid(primaryBinding.rid);
        await expect(row).toHaveAttribute(
          'data-binding-dataset-rid',
          'ri.dataset.main.dataset.northwind-employees-v2',
        );
        await expect(row).toHaveAttribute(
          'data-binding-mapping-count',
          '4',
        );
      },
    );
  });
});
