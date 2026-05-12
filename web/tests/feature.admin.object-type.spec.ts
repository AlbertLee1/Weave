import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  ObjectTypeAdminPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/:ontology/objectTypes` — the ObjectType editor
 * rendered by `src/components/admin/ObjectTypeAdminPage.tsx`.
 *
 * Scenarios map the PRD AC for US-029 (frontend-backend-gap-coverage):
 *   AC mapping → scenario:
 *     create               → create-flow-posts-expected-body-and-refreshes
 *     edit                 → edit-flow-puts-expected-body-and-refreshes
 *     delete               → delete-flow-shows-impact-and-deletes
 *     validation 错误      → create-form-blocks-duplicate-api-name
 *     字段重命名级联       → properties-tab-has-no-rename-cascade-affordance
 *                            (honest mapping — PropertiesEditor today has
 *                            Add / Edit / Delete only; the AC "字段重命名
 *                            级联" affordance does not exist. Same pattern
 *                            as US-022 批量取消 / US-025 回滚 / US-026
 *                            超时 absence assertions.)
 *
 * All scenarios stub the admin endpoints through `page.route` so the page
 * renders deterministic fixtures without touching real backend data, and
 * mutations latch a `state` object so subsequent refetches reflect the
 * server transition (same convention as US-022 action-history revert and
 * US-026 approve/reject).
 */

const ONTOLOGY = 'northwind';

type Status = 'ACTIVE' | 'ENDORSED' | 'EXPERIMENTAL' | 'DEPRECATED';
type Visibility = 'PROMINENT' | 'NORMAL' | 'HIDDEN';

interface MockObjectType {
  rid: string;
  apiName: string;
  displayName: string;
  pluralDisplayName: string;
  primaryKey: string;
  status: Status;
  visibility: Visibility;
  description?: string;
  classification?: string;
}

const employee: MockObjectType = {
  rid: 'ri.ontology.main.object-type.emp-1',
  apiName: 'Employee',
  displayName: 'Employee',
  pluralDisplayName: 'Employees',
  primaryKey: 'employeeId',
  status: 'ACTIVE',
  visibility: 'PROMINENT',
};

const department: MockObjectType = {
  rid: 'ri.ontology.main.object-type.dept-1',
  apiName: 'Department',
  displayName: 'Department',
  pluralDisplayName: 'Departments',
  primaryKey: 'departmentId',
  status: 'EXPERIMENTAL',
  visibility: 'NORMAL',
};

const customer: MockObjectType = {
  rid: 'ri.ontology.main.object-type.cust-1',
  apiName: 'Customer',
  displayName: 'Customer',
  pluralDisplayName: 'Customers',
  primaryKey: 'customerId',
  status: 'DEPRECATED',
  visibility: 'HIDDEN',
};

const OUTGOING_LINKS = [
  {
    rid: 'ri.ontology.main.link-type.l1',
    apiName: 'employeeDepartment',
    displayName: 'Employee → Department',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Department',
    cardinality: 'ONE_TO_ONE',
    required: true,
  },
];

const ACTION_TYPES = [
  {
    rid: 'ri.ontology.main.action-type.a1',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    status: 'ACTIVE',
    parameters: {
      employee: {
        dataType: { type: 'object', objectTypeApiName: 'Employee' },
        required: true,
      },
    },
  },
];

interface StubState {
  rows: MockObjectType[];
  listCalls: number;
  createCalls: number;
  createBody: unknown;
  forceCreateError?: number;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  forceListError?: number;
}

function makeState(initial: MockObjectType[]): StubState {
  return {
    rows: [...initial],
    listCalls: 0,
    createCalls: 0,
    createBody: null,
    updateCalls: [],
    deleteCalls: [],
  };
}

/**
 * Installs all ObjectType admin endpoints needed by ObjectTypeAdminPage +
 * the delete-impact modal (outgoingLinkTypes + actionTypes). Mutations
 * latch into `state.rows` so a refetch after submit reflects the change,
 * mirroring real server transitions.
 *
 * The list endpoint is split off from the per-rid PUT/DELETE handler so
 * narrower glob patterns don't accidentally swallow the GET list call
 * (Playwright LIFO — register list AFTER per-rid is fine since the
 * patterns target different methods).
 */
async function stubObjectTypeAdmin(
  page: Page,
  state: StubState,
): Promise<void> {
  // List + Create on the collection endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'GET') {
        state.listCalls += 1;
        if (state.forceListError) {
          await route.fulfill({
            status: state.forceListError,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'INTERNAL',
              errorName: 'Internal',
              errorInstanceId: 'spec',
              parameters: { error: 'forced for test' },
            }),
          });
          return;
        }
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
        if (state.forceCreateError) {
          await route.fulfill({
            status: state.forceCreateError,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'ObjectTypeAlreadyExists',
              errorName: 'Conflict',
              errorInstanceId: 'spec',
              parameters: { error: 'apiName already exists' },
            }),
          });
          return;
        }
        const apiName = String(body.apiName ?? 'invoice');
        const created: MockObjectType = {
          rid: `ri.ontology.main.object-type.${apiName}`,
          apiName,
          displayName: String(body.displayName ?? apiName),
          pluralDisplayName: String(body.pluralDisplayName ?? apiName),
          primaryKey: String(body.primaryKey ?? `${apiName}Id`),
          status: (body.status as Status) ?? 'ACTIVE',
          visibility: (body.visibility as Visibility) ?? 'NORMAL',
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

  // Update + Delete on the per-rid endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/*`,
    async (route: Route) => {
      const req = route.request();
      const url = new URL(req.url());
      const segments = url.pathname.split('/');
      const ridEncoded = segments[segments.length - 1] ?? '';
      const rid = decodeURIComponent(ridEncoded);
      if (req.method() === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.updateCalls.push({ rid, body });
        const idx = state.rows.findIndex((r) => r.rid === rid);
        if (idx >= 0) {
          state.rows[idx] = {
            ...state.rows[idx],
            displayName: String(
              body.displayName ?? state.rows[idx].displayName,
            ),
            pluralDisplayName: String(
              body.pluralDisplayName ?? state.rows[idx].pluralDisplayName,
            ),
            status: (body.status as Status) ?? state.rows[idx].status,
            visibility:
              (body.visibility as Visibility) ?? state.rows[idx].visibility,
          };
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(state.rows[idx]),
        });
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

  // outgoingLinkTypes for the delete-impact modal.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/*/outgoingLinkTypes`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: OUTGOING_LINKS }),
      });
    },
  );

  // actionTypes for the delete-impact modal.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: ACTION_TYPES }),
      });
    },
  );

  // Properties listing for the Properties tab (delivered as empty so the
  // Properties scenario stays focused on the absence assertion).
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/*/properties`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    },
  );
}

describeFeature('Admin: Object Type editor', () => {
  test('Scenario: the table renders the seeded rows with per-row data attrs @smoke', async ({
    page,
    request,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const state = makeState([employee, department, customer]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three ObjectTypes are seeded on northwind', async () => {
      await stubObjectTypeAdmin(page, state);
    });

    await When('the user opens /admin/northwind/objectTypes', async () => {
      await admin.goto(ONTOLOGY);
    });

    await Then('the page root + table are visible with three rows', async () => {
      await expect(admin.root).toBeVisible();
      await expect(admin.loading).toBeHidden();
      await expect(admin.table).toBeVisible();
      await expect(admin.rows).toHaveCount(3);
    });

    await Then('each row carries the apiName data attribute', async () => {
      await expect(admin.rowByApiName('Employee')).toBeVisible();
      await expect(admin.rowByApiName('Department')).toBeVisible();
      await expect(admin.rowByApiName('Customer')).toBeVisible();
      await expect(admin.rowByApiName('Employee')).toContainText('employeeId');
      await expect(admin.rowByApiName('Employee')).toContainText('ACTIVE');
      await expect(admin.rowByApiName('Department')).toContainText(
        'EXPERIMENTAL',
      );
    });
  });

  test('Scenario: creating an ObjectType POSTs the expected body and refreshes the list @smoke', async ({
    page,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const state = makeState([employee, department, customer]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubObjectTypeAdmin(page, state);
    });

    await Given('the user is on the ObjectType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Object Type modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When('the user fills display name and primary key and submits', async () => {
      await admin.createDisplayName.fill('Invoice');
      // apiName + plural auto-populate from displayName — see
      // ObjectTypeAdminPage.tsx:updateDisplayName.
      await expect(admin.createApiName).toHaveValue('invoice');
      await admin.createPrimaryKey.fill('invoiceId');
      // The modal layout is content-sized + vertically centered, so when
      // the form has all 10 fields (description / classification / etc.)
      // the Submit button can fall outside the 720px-tall default
      // viewport. requestSubmit() is the same code path that hitting
      // Enter on any field would trigger and exercises the React
      // `onSubmit` handler directly.
      await admin.createForm.evaluate((form) =>
        (form as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('POST /objectTypes was invoked exactly once with the expected fields', async () => {
      await expect.poll(() => state.createCalls).toBe(1);
      expect(state.createBody).toMatchObject({
        apiName: 'invoice',
        displayName: 'Invoice',
        pluralDisplayName: 'Invoices',
        primaryKey: 'invoiceId',
        status: 'ACTIVE',
        visibility: 'NORMAL',
      });
    });

    await Then('the modal closes and the new row joins the table', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rows).toHaveCount(4);
      await expect(admin.rowByApiName('invoice')).toBeVisible();
      await expect(admin.rowByApiName('invoice')).toContainText('Invoice');
    });
  });

  test('Scenario: editing an ObjectType PUTs the updated displayName and refreshes the list @smoke', async ({
    page,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const state = makeState([employee, department, customer]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubObjectTypeAdmin(page, state);
    });

    await Given('the user is on the ObjectType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Edit modal for Employee', async () => {
      await admin.editButtonFor('Employee').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.editForm).toBeVisible();
      // The Details tab is the default landing tab.
      await expect(admin.editApiName).toHaveText('Employee');
    });

    await When('the user changes the display name and submits', async () => {
      await admin.editDisplayName.fill('Team Member');
      // See create-flow scenario for the requestSubmit rationale — the
      // Edit modal is even taller (icon / color / classification rows)
      // so the Save button is often off-screen at default viewport.
      await admin.editForm.evaluate((form) =>
        (form as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('PUT /objectTypes/byRid was invoked with the new displayName', async () => {
      await expect.poll(() => state.updateCalls.length).toBe(1);
      expect(state.updateCalls[0]).toMatchObject({
        rid: 'ri.ontology.main.object-type.emp-1',
        body: expect.objectContaining({
          displayName: 'Team Member',
          status: 'ACTIVE',
          visibility: 'PROMINENT',
        }),
      });
    });

    await Then('the modal closes and the row reflects the new displayName', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      const row = admin.rowByApiName('Employee');
      await expect(row).toContainText('Team Member');
      await expect(row).toContainText('employeeId');
    });
  });

  test('Scenario: deleting an ObjectType surfaces impact counts and DELETEs the rid', async ({
    page,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const state = makeState([employee, department, customer]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubObjectTypeAdmin(page, state);
    });

    await Given('the user is on the ObjectType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Delete modal for Employee', async () => {
      await admin.deleteButtonFor('Employee').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.deleteModal).toBeVisible();
    });

    await Then('the impact panel reports the outgoing LinkType and ActionType counts', async () => {
      // The delete-impact panel runs two queries — outgoingLinkTypes and
      // actionTypes — and counts ActionTypes whose parameters reference
      // this ObjectType (see actionReferencesObjectType in
      // ObjectTypeAdminPage.tsx:954).
      await expect(admin.deleteImpactLinks).toHaveText(/1 outgoing LinkType/);
      await expect(admin.deleteImpactActions).toHaveText(/1 ActionType/);
    });

    await When('the user confirms the delete', async () => {
      await admin.deleteConfirm.click();
    });

    await Then('DELETE /objectTypes/byRid was invoked with the row rid', async () => {
      await expect.poll(() => state.deleteCalls.length).toBe(1);
      expect(state.deleteCalls[0]).toBe('ri.ontology.main.object-type.emp-1');
    });

    await Then('the modal closes and the row is gone from the table', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rowByApiName('Employee')).toHaveCount(0);
      await expect(admin.rows).toHaveCount(2);
    });
  });

  test('Scenario: the Create form blocks submission when the apiName collides with an existing row', async ({
    page,
  }) => {
    const admin = new ObjectTypeAdminPage(page);
    const state = makeState([employee, department, customer]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubObjectTypeAdmin(page, state);
    });

    await Given('the user is on the ObjectType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens New and types an apiName that already exists', async () => {
      await admin.newButton.click();
      await expect(admin.createForm).toBeVisible();
      // Fill displayName first so apiName auto-populates from it; then
      // override apiName so the dirty flag is set + duplicate detection
      // kicks in. Mirrors the unit test in ObjectTypeAdminPage.test.tsx
      // "blocks creating an ObjectType with a duplicate apiName".
      await admin.createDisplayName.fill('Extra Employee');
      await admin.createApiName.fill('Employee');
      await admin.createPrimaryKey.fill('id');
    });

    await Then('the submit button is disabled and the duplicate error is shown', async () => {
      await expect(admin.createSubmit).toBeDisabled();
      // The error is rendered inside the Field's <span role="alert"> hint
      // slot (see Field component in ObjectTypeAdminPage.tsx:920) — match
      // by text instead of testid because the inline hint span doesn't
      // need its own testid.
      await expect(
        admin.createForm.getByText(
          /An ObjectType with apiName .*Employee.* already exists/i,
        ),
      ).toBeVisible();
    });

    await Then('no POST was issued', async () => {
      expect(state.createCalls).toBe(0);
    });
  });

  test('Scenario: the Properties tab has no rename-cascade affordance today (honest mapping)', async ({
    page,
  }) => {
    // Honest mapping for AC "字段重命名级联": the PropertiesEditor today
    // exposes Add / Edit / Delete only (see PropertiesEditor.tsx) — there
    // is no "Rename" affordance that cascades references across
    // LinkTypes / ActionTypes / saved ObjectSets / cell-masking policies.
    // This scenario locks the gap so a future PR adding a Rename Cascade
    // button (or any selector matching /rename|cascade/i) must update
    // either the absence assertion below or replace it with a positive
    // one. Same pattern as US-022 批量取消 / US-025 回滚 / US-026 超时
    // absence assertions.
    const admin = new ObjectTypeAdminPage(page);
    const state = makeState([employee, department, customer]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubObjectTypeAdmin(page, state);
    });

    await Given('the user is on the ObjectType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens Edit on Employee and switches to Properties', async () => {
      await admin.editButtonFor('Employee').click();
      await expect(admin.editTabs).toBeVisible();
      await admin.editTabProperties.click();
    });

    await Then('the Properties editor is rendered', async () => {
      await expect(page.getByTestId('properties-editor')).toBeVisible();
    });

    await Then('no Rename / Cascade button is rendered', async () => {
      // Button + link absence covers the two implementations a future PR
      // could pick (toolbar button vs. inline link). Regex covers
      // 'Rename', 'Rename Cascade', 'Cascade Rename', 'Cascade rename
      // references' — same defensive style as US-028 'Export CSV' check.
      await expect(
        page.getByRole('button', { name: /rename|cascade/i }),
      ).toHaveCount(0);
      await expect(
        page.getByRole('link', { name: /rename|cascade/i }),
      ).toHaveCount(0);
    });
  });
});
