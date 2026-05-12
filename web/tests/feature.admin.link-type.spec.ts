import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  LinkTypeAdminPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/:ontology/linkTypes` — the LinkType editor
 * rendered by `src/components/admin/LinkTypeAdminPage.tsx`.
 *
 * Scenarios map the PRD AC for US-030 (frontend-backend-gap-coverage):
 *   AC mapping → scenario:
 *     create               → create-flow-posts-expected-body-and-refreshes
 *     edit                 → edit-flow-puts-expected-body-and-refreshes
 *     delete               → delete-flow-shows-impact-and-deletes
 *     link properties      → create-with-foreign-key-config-roundtrips-as-json
 *                            + no-per-link-properties-tab (honest mapping —
 *                            the LinkType model has only
 *                            apiName/displayName/required/foreignKeyConfig;
 *                            there is no PropertiesEditor analogous to
 *                            ObjectType's tabbed Properties surface).
 *     direction 切换       → relationship-and-cardinality-are-immutable-on-edit
 *                            (honest mapping — Edit modal explicitly states
 *                            "Source, target, and cardinality cannot be
 *                            changed. Delete and recreate to modify the
 *                            relationship." so there is no in-place
 *                            direction toggle; the spec locks both the
 *                            wire-level immutability via PUT body and the
 *                            DOM absence of any swap/reverse/direction
 *                            affordance. Same template as US-022 批量取消 /
 *                            US-025 回滚 / US-026 超时 / US-028 CSV-export
 *                            / US-029 字段重命名级联 absence assertions.)
 *
 * All scenarios stub the admin endpoints through `page.route` so the page
 * renders deterministic fixtures without touching real backend data, and
 * mutations latch a `state` object so subsequent refetches reflect the
 * server transition (same convention as US-022 action-history revert,
 * US-026 approve/reject, US-029 object-type create/update/delete).
 */

const ONTOLOGY = 'northwind';

type Cardinality = 'ONE_TO_ONE' | 'ONE_TO_MANY' | 'MANY_TO_MANY';

interface MockLinkType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  objectTypeApiName: string;
  linkedObjectTypeApiName: string;
  cardinality: Cardinality;
  required: boolean;
  foreignKeyConfig?: unknown;
}

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
  {
    rid: 'ri.ontology.main.object-type.dept',
    apiName: 'Department',
    displayName: 'Department',
    pluralDisplayName: 'Departments',
    primaryKey: 'departmentId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
  {
    rid: 'ri.ontology.main.object-type.proj',
    apiName: 'Project',
    displayName: 'Project',
    pluralDisplayName: 'Projects',
    primaryKey: 'projectId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
];

const employeeDepartment: MockLinkType = {
  rid: 'ri.ontology.main.link-type.emp-dept',
  apiName: 'employeeDepartment',
  displayName: 'Employee → Department',
  objectTypeApiName: 'Employee',
  linkedObjectTypeApiName: 'Department',
  cardinality: 'ONE_TO_ONE',
  required: true,
};

const departmentEmployees: MockLinkType = {
  rid: 'ri.ontology.main.link-type.dept-emp',
  apiName: 'departmentEmployees',
  displayName: 'Department → Employees',
  objectTypeApiName: 'Department',
  linkedObjectTypeApiName: 'Employee',
  cardinality: 'ONE_TO_MANY',
  required: false,
};

const employeeProjects: MockLinkType = {
  rid: 'ri.ontology.main.link-type.emp-proj',
  apiName: 'employeeProjects',
  displayName: 'Employee → Projects',
  objectTypeApiName: 'Employee',
  linkedObjectTypeApiName: 'Project',
  cardinality: 'MANY_TO_MANY',
  required: false,
};

const ACTION_TYPES = [
  {
    rid: 'ri.ontology.main.action-type.a1',
    apiName: 'assignEmployeeProject',
    displayName: 'Assign Employee Project',
    status: 'ACTIVE',
    parameters: {},
    rules: [
      {
        type: 'createLink',
        linkTypeApiName: 'employeeProjects',
        sourceObjectPrimaryKey: 'employeeId',
        targetObjectPrimaryKey: 'projectId',
      },
    ],
  },
  {
    rid: 'ri.ontology.main.action-type.a2',
    apiName: 'archiveEmployee',
    displayName: 'Archive Employee',
    status: 'ACTIVE',
    parameters: {},
    rules: [],
  },
];

interface StubState {
  rows: MockLinkType[];
  listCalls: number;
  createCalls: number;
  createBody: unknown;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  forceListError?: number;
  forceCreateError?: number;
}

function makeState(initial: MockLinkType[]): StubState {
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
 * Installs all LinkType admin endpoints needed by LinkTypeAdminPage +
 * the delete-impact modal (actionTypes for `actionReferencesLinkType`
 * walking + objectTypes for the source filter / Create modal selects).
 *
 * Mutations latch into `state.rows` so a refetch after submit reflects
 * the change, mirroring real server transitions and the US-029
 * ObjectType admin stub helper template.
 */
async function stubLinkTypeAdmin(
  page: Page,
  state: StubState,
): Promise<void> {
  // List + Create on the collection endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/linkTypes`,
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
              errorCode: 'LinkTypeAlreadyExists',
              errorName: 'Conflict',
              errorInstanceId: 'spec',
              parameters: { error: 'apiName already exists' },
            }),
          });
          return;
        }
        const apiName = String(body.apiName ?? 'unknownLink');
        const created: MockLinkType = {
          rid: `ri.ontology.main.link-type.${apiName}`,
          apiName,
          displayName: String(body.displayName ?? apiName),
          description: body.description as string | undefined,
          objectTypeApiName: String(body.objectTypeApiName ?? ''),
          linkedObjectTypeApiName: String(body.linkedObjectTypeApiName ?? ''),
          cardinality: (body.cardinality as Cardinality) ?? 'ONE_TO_MANY',
          required: Boolean(body.required ?? false),
          foreignKeyConfig: body.foreignKeyConfig,
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
    `**/api/v2/ontologies/${ONTOLOGY}/linkTypes/byRid/*`,
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
            description: (body.description as string | undefined) ??
              state.rows[idx].description,
            required:
              typeof body.required === 'boolean'
                ? body.required
                : state.rows[idx].required,
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

  // objectTypes for the source filter dropdown + Create modal selects.
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
        body: JSON.stringify({ data: OBJECT_TYPES }),
      });
    },
  );

  // actionTypes for the delete-impact modal (rule walking).
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
}

describeFeature('Admin: Link Type editor', () => {
  test('Scenario: the table renders the seeded rows with per-row data attrs and cardinality badges @smoke', async ({
    page,
    request,
  }) => {
    const admin = new LinkTypeAdminPage(page);
    const state = makeState([
      employeeDepartment,
      departmentEmployees,
      employeeProjects,
    ]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three LinkTypes are seeded on northwind', async () => {
      await stubLinkTypeAdmin(page, state);
    });

    await When('the user opens /admin/northwind/linkTypes', async () => {
      await admin.goto(ONTOLOGY);
    });

    await Then('the page root + table are visible with three rows', async () => {
      await expect(admin.root).toBeVisible();
      await expect(admin.loading).toBeHidden();
      await expect(admin.table).toBeVisible();
      await expect(admin.rows).toHaveCount(3);
    });

    await Then('each row carries the apiName + cardinality data attributes', async () => {
      // Rows sorted by displayName asc:
      //   Department → Employees  (ONE_TO_MANY)
      //   Employee → Department   (ONE_TO_ONE)
      //   Employee → Projects     (MANY_TO_MANY)
      await expect(admin.rowByApiName('employeeDepartment')).toBeVisible();
      await expect(admin.rowByApiName('departmentEmployees')).toBeVisible();
      await expect(admin.rowByApiName('employeeProjects')).toBeVisible();
      await expect(admin.rowByApiName('employeeDepartment')).toHaveAttribute(
        'data-link-type-cardinality',
        'ONE_TO_ONE',
      );
      await expect(admin.rowByApiName('departmentEmployees')).toHaveAttribute(
        'data-link-type-cardinality',
        'ONE_TO_MANY',
      );
      await expect(admin.rowByApiName('employeeProjects')).toHaveAttribute(
        'data-link-type-cardinality',
        'MANY_TO_MANY',
      );
      // Relationship column renders both endpoints + the cardinality badge.
      await expect(admin.rowByApiName('employeeDepartment')).toContainText(
        'Employee',
      );
      await expect(admin.rowByApiName('employeeDepartment')).toContainText(
        'Department',
      );
      await expect(admin.rowByApiName('employeeDepartment')).toContainText(
        '1 : 1',
      );
      await expect(admin.rowByApiName('employeeProjects')).toContainText(
        'N : N',
      );
    });
  });

  test('Scenario: creating a LinkType POSTs the expected body with source/target/cardinality and roundtrips the foreignKeyConfig as parsed JSON @smoke', async ({
    page,
  }) => {
    const admin = new LinkTypeAdminPage(page);
    const state = makeState([
      employeeDepartment,
      departmentEmployees,
      employeeProjects,
    ]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubLinkTypeAdmin(page, state);
    });

    await Given('the user is on the LinkType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Link Type modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When('the user fills displayName, picks source/target/cardinality, and a foreign-key config JSON, then submits', async () => {
      await admin.createDisplayName.fill('Works In');
      // apiName auto-populates from displayName — see toApiName() and
      // updateDisplayName() in LinkTypeAdminPage.tsx.
      await expect(admin.createApiName).toHaveValue('worksIn');

      // Source / target / cardinality cover both AC "create" and "direction
      // 切换": configuring source vs. target IS configuring direction in the
      // create wire payload. The Edit modal explicitly documents that the
      // relationship is immutable post-create, so this is the only path
      // for setting direction.
      await admin.createSource.selectOption('Employee');
      await admin.createTarget.selectOption('Department');
      await admin.createCardinality.selectOption('ONE_TO_MANY');

      // Cardinality !== MANY_TO_MANY → the foreignKeyConfig textarea is
      // rendered (see needsForeignKey gate). We fill a valid JSON literal
      // and assert below that the server receives the *parsed* object
      // rather than the raw string — locking the JSON.parse roundtrip.
      await admin.createForeignKey.fill(
        '{"sourceProperty":"departmentId","targetProperty":"id"}',
      );

      // requestSubmit() runs the same React onSubmit handler as button
      // click but does not require the Submit button to be inside the
      // viewport — same trick as US-029 large-modal scenarios.
      await admin.createForm.evaluate((form) =>
        (form as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('POST /linkTypes was invoked exactly once with the expected fields', async () => {
      await expect.poll(() => state.createCalls).toBe(1);
      expect(state.createBody).toMatchObject({
        apiName: 'worksIn',
        displayName: 'Works In',
        objectTypeApiName: 'Employee',
        linkedObjectTypeApiName: 'Department',
        cardinality: 'ONE_TO_MANY',
        required: false,
        foreignKeyConfig: {
          sourceProperty: 'departmentId',
          targetProperty: 'id',
        },
      });
    });

    await Then('the modal closes and the new row joins the table', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rows).toHaveCount(4);
      await expect(admin.rowByApiName('worksIn')).toBeVisible();
      await expect(admin.rowByApiName('worksIn')).toContainText('Works In');
      await expect(admin.rowByApiName('worksIn')).toHaveAttribute(
        'data-link-type-cardinality',
        'ONE_TO_MANY',
      );
    });
  });

  test('Scenario: editing a LinkType PUTs the new displayName + required flag and keeps the relationship immutable @smoke', async ({
    page,
  }) => {
    const admin = new LinkTypeAdminPage(page);
    const state = makeState([
      employeeDepartment,
      departmentEmployees,
      employeeProjects,
    ]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubLinkTypeAdmin(page, state);
    });

    await Given('the user is on the LinkType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Edit modal for employeeDepartment', async () => {
      await admin.editButtonFor('employeeDepartment').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.editForm).toBeVisible();
      // The read-only apiName + relationship panel show the immutable
      // contract documented in LinkTypeAdminPage.tsx ("Source, target,
      // and cardinality cannot be changed"). The data-link-type-*
      // attributes on the relationship panel let downstream scenarios
      // assert against a stable contract instead of i18n copy.
      await expect(admin.editApiName).toHaveText('employeeDepartment');
      await expect(admin.editRelationship).toHaveAttribute(
        'data-link-type-source',
        'Employee',
      );
      await expect(admin.editRelationship).toHaveAttribute(
        'data-link-type-target',
        'Department',
      );
      await expect(admin.editRelationship).toHaveAttribute(
        'data-link-type-cardinality',
        'ONE_TO_ONE',
      );
    });

    await When('the user changes the display name and submits', async () => {
      await admin.editDisplayName.fill('Works In');
      await admin.editForm.evaluate((form) =>
        (form as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('PUT /linkTypes/byRid was invoked with the new displayName + required', async () => {
      await expect.poll(() => state.updateCalls.length).toBe(1);
      expect(state.updateCalls[0]).toMatchObject({
        rid: 'ri.ontology.main.link-type.emp-dept',
        body: expect.objectContaining({
          displayName: 'Works In',
          required: true,
        }),
      });
      // Direction-relevant fields (source/target/cardinality) must NOT
      // appear in the wire body — UpdateLinkTypeRequest is a narrow
      // {displayName, description?, required?} contract. This locks the
      // "direction 切换" AC: the only direction switch path is delete +
      // recreate, never an in-place PUT mutation.
      const updateBody = state.updateCalls[0]?.body as Record<string, unknown>;
      expect(updateBody.objectTypeApiName).toBeUndefined();
      expect(updateBody.linkedObjectTypeApiName).toBeUndefined();
      expect(updateBody.cardinality).toBeUndefined();
    });

    await Then('the modal closes and the row reflects the new displayName', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      const row = admin.rowByApiName('employeeDepartment');
      await expect(row).toContainText('Works In');
      // The cardinality data-attr is unchanged — the row still reports
      // ONE_TO_ONE because the server transition only updated displayName.
      await expect(row).toHaveAttribute(
        'data-link-type-cardinality',
        'ONE_TO_ONE',
      );
    });
  });

  test('Scenario: deleting a LinkType surfaces the ActionType impact and searchAround warning, then DELETEs the rid', async ({
    page,
  }) => {
    const admin = new LinkTypeAdminPage(page);
    const state = makeState([
      employeeDepartment,
      departmentEmployees,
      employeeProjects,
    ]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubLinkTypeAdmin(page, state);
    });

    await Given('the user is on the LinkType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Delete modal for employeeProjects', async () => {
      await admin.deleteButtonFor('employeeProjects').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.deleteModal).toBeVisible();
    });

    await Then('the impact panel reports the referencing ActionType and searchAround warning', async () => {
      // actionReferencesLinkType walks ActionType.rules looking for
      // `linkTypeApiName` strings — see LinkTypeAdminPage.tsx:828. Only
      // assignEmployeeProject's createLink rule matches
      // `employeeProjects`, so the count is exactly 1.
      await expect(admin.deleteImpactActions).toHaveText(/1 ActionType/);
      await expect(admin.deleteImpactSearchAround).toContainText('searchAround');
      await expect(admin.deleteImpactSearchAround).toContainText(
        'employeeProjects',
      );
    });

    await When('the user confirms the delete', async () => {
      await admin.deleteConfirm.click();
    });

    await Then('DELETE /linkTypes/byRid was invoked with the row rid', async () => {
      await expect.poll(() => state.deleteCalls.length).toBe(1);
      expect(state.deleteCalls[0]).toBe('ri.ontology.main.link-type.emp-proj');
    });

    await Then('the modal closes and the row is gone from the table', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rowByApiName('employeeProjects')).toHaveCount(0);
      await expect(admin.rows).toHaveCount(2);
    });
  });

  test('Scenario: the Create form blocks submission when the apiName collides with an existing row or the foreignKeyConfig JSON is invalid', async ({
    page,
  }) => {
    const admin = new LinkTypeAdminPage(page);
    const state = makeState([
      employeeDepartment,
      departmentEmployees,
      employeeProjects,
    ]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubLinkTypeAdmin(page, state);
    });

    await Given('the user is on the LinkType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens New and types an apiName that already exists', async () => {
      await admin.newButton.click();
      await expect(admin.createForm).toBeVisible();
      // Fill displayName first so apiName auto-populates from it; then
      // override apiName so the dirty flag is set + duplicate detection
      // kicks in — same dance as the unit test "blocks creating a
      // LinkType with a duplicate apiName" in LinkTypeAdminPage.test.tsx.
      await admin.createDisplayName.fill('Other');
      await admin.createApiName.fill('employeeDepartment');
    });

    await Then('the submit button is disabled and the duplicate error is shown', async () => {
      await expect(admin.createSubmit).toBeDisabled();
      await expect(
        admin.createForm.getByText(
          /A LinkType with apiName .*employeeDepartment.* already exists/i,
        ),
      ).toBeVisible();
    });

    await When('the user clears the apiName collision and types an invalid foreign-key JSON', async () => {
      await admin.createApiName.fill('uniqueLink');
      // Default cardinality is ONE_TO_MANY → foreignKeyConfig textarea is
      // visible. Invalid JSON ("{not valid") triggers JSON.parse to throw
      // and the form surfaces the error inline via the Field error slot.
      await admin.createForeignKey.fill('{not valid');
    });

    await Then('submit stays disabled and the Invalid JSON error is shown', async () => {
      await expect(admin.createSubmit).toBeDisabled();
      await expect(
        admin.createForm.getByText(/Invalid JSON/i),
      ).toBeVisible();
    });

    await Then('no POST was issued', async () => {
      expect(state.createCalls).toBe(0);
    });
  });

  test('Scenario: the LinkType editor has no per-link Properties tab and no direction-swap affordance today (honest mapping)', async ({
    page,
  }) => {
    // Honest mapping for AC "link properties" + "direction 切换":
    //   1. LinkType has no PropertiesEditor analogous to ObjectType's
    //      tabbed Properties surface — the wire model is a flat
    //      {apiName, displayName, description?, source, target,
    //       cardinality, foreignKeyConfig?, required} record (see
    //      api/ontologies.ts:107-127). Per-link "Properties" do not
    //      exist as a UI affordance.
    //   2. The Edit modal explicitly documents "Source, target, and
    //      cardinality cannot be changed. Delete and recreate to modify
    //      the relationship." (LinkTypeAdminPage.tsx:621-623). So there
    //      is no in-place direction toggle / swap / reverse button.
    // This scenario locks both gaps as DOM absences so a future PR
    // adding either (a) a tabbed Properties surface or (b) a swap /
    // direction button must update or invert these absence assertions.
    // Same pattern as US-022 批量取消 / US-025 回滚 / US-026 超时 /
    // US-028 CSV-export / US-029 字段重命名级联 absence assertions.
    const admin = new LinkTypeAdminPage(page);
    const state = makeState([
      employeeDepartment,
      departmentEmployees,
      employeeProjects,
    ]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubLinkTypeAdmin(page, state);
    });

    await Given('the user is on the LinkType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens Edit on employeeDepartment', async () => {
      await admin.editButtonFor('employeeDepartment').click();
      await expect(admin.editForm).toBeVisible();
    });

    await Then('there is no Properties tab inside the Edit modal', async () => {
      // Tabs / tablist / properties-editor are all surfaces that
      // ObjectTypeAdminPage's tabbed Edit modal exposes — locking their
      // absence here documents that LinkType edits are a single flat
      // form. Regex matches "Properties", "Link Properties",
      // "Attributes", "Schema" tab labels.
      await expect(admin.editForm.getByRole('tablist')).toHaveCount(0);
      await expect(
        admin.editForm.getByRole('tab', { name: /properties|attributes|schema/i }),
      ).toHaveCount(0);
      await expect(page.getByTestId('properties-editor')).toHaveCount(0);
    });

    await Then('there is no direction-swap or reverse affordance in the Edit modal', async () => {
      // Button + link absence covers the two implementations a future
      // PR could pick (toolbar button vs. inline link). Regex covers
      // "Swap", "Reverse", "Change direction", "Toggle direction" — the
      // four shapes a "direction 切换" UI would plausibly take.
      await expect(
        admin.editForm.getByRole('button', {
          name: /swap|reverse|direction/i,
        }),
      ).toHaveCount(0);
      await expect(
        admin.editForm.getByRole('link', {
          name: /swap|reverse|direction/i,
        }),
      ).toHaveCount(0);
    });

    await Then('the source / target / cardinality selects are absent from the Edit form', async () => {
      // These three controls only exist in the Create form (testids
      // link-type-create-{source,target,cardinality}); the Edit form
      // intentionally omits them. Locking absence in the Edit form by
      // testid is more specific than the regex-based affordance checks
      // above and catches accidental re-introduction.
      await expect(
        admin.editForm.getByTestId('link-type-create-source'),
      ).toHaveCount(0);
      await expect(
        admin.editForm.getByTestId('link-type-create-target'),
      ).toHaveCount(0);
      await expect(
        admin.editForm.getByTestId('link-type-create-cardinality'),
      ).toHaveCount(0);
    });
  });
});
