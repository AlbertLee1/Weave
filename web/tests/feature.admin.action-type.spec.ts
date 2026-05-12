import { expect, test, type Page, type Route } from '@playwright/test';
import {
  ActionTypeAdminPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/:ontology/actionTypes` — the ActionType editor
 * rendered by `src/components/admin/ActionTypeAdminPage.tsx`.
 *
 * Scenarios map the PRD AC for US-031 (frontend-backend-gap-coverage):
 *   AC mapping → scenario:
 *     create               → create-flow-posts-expected-body-and-refreshes
 *     edit                 → edit-flow-puts-expected-body-and-keeps-api-name-immutable
 *     参数 schema          → create-flow-roundtrips-parameter-definitions
 *                            + edit-flow-pre-populates-parameters-and-rules-from-wire
 *     rules                → create-flow-roundtrips-rule-property-bindings
 *                            + edit-flow-pre-populates-parameters-and-rules-from-wire
 *     delete               → delete-flow-confirms-and-removes-the-row
 *
 * All scenarios stub the admin endpoints through `page.route` so the page
 * renders deterministic fixtures without touching real backend data, and
 * mutations latch a `state` object so subsequent refetches reflect the
 * server transition (same convention as US-022 action-history revert,
 * US-026 approve/reject, US-029 object-type create/update/delete, US-030
 * link-type create/update/delete).
 */

const ONTOLOGY = 'northwind';

type Status = 'ACTIVE' | 'EXPERIMENTAL' | 'DEPRECATED';

interface MockActionType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  status: Status;
  parameters: Record<string, unknown>;
  rules: Array<Record<string, unknown>>;
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
];

const LINK_TYPES = [
  {
    rid: 'ri.ontology.main.link-type.l1',
    apiName: 'employeeProjects',
    displayName: 'Employee → Projects',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Project',
    cardinality: 'MANY_TO_MANY',
    required: false,
  },
];

const archiveEmployee: MockActionType = {
  rid: 'ri.ontology.main.action-type.archive-emp',
  apiName: 'archiveEmployee',
  displayName: 'Archive Employee',
  status: 'ACTIVE',
  parameters: {
    employeeId: {
      dataType: { type: 'string' },
      required: true,
    },
  },
  rules: [
    {
      type: 'modifyObject',
      objectType: 'Employee',
      propertyBindings: {
        archived: { type: 'static', value: 'true' },
      },
    },
  ],
};

const createEmployee: MockActionType = {
  rid: 'ri.ontology.main.action-type.create-emp',
  apiName: 'createEmployee',
  displayName: 'Create Employee',
  status: 'EXPERIMENTAL',
  parameters: {
    name: { dataType: { type: 'string' }, required: true },
  },
  rules: [
    {
      type: 'createObject',
      objectType: 'Employee',
      propertyBindings: {
        name: { type: 'parameter', value: 'name' },
      },
    },
  ],
};

const promoteEmployee: MockActionType = {
  rid: 'ri.ontology.main.action-type.promote-emp',
  apiName: 'promoteEmployee',
  displayName: 'Promote Employee',
  status: 'DEPRECATED',
  parameters: {},
  rules: [],
};

interface StubState {
  rows: MockActionType[];
  listCalls: number;
  createCalls: number;
  createBody: unknown;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  forceCreateError?: number;
}

function makeState(initial: MockActionType[]): StubState {
  return {
    rows: initial.map((row) => structuredClone(row)),
    listCalls: 0,
    createCalls: 0,
    createBody: null,
    updateCalls: [],
    deleteCalls: [],
  };
}

/**
 * Installs all ActionType admin endpoints needed by ActionTypeAdminPage +
 * the builder's object-type / link-type pick-lists. Mutations latch into
 * `state.rows` so a refetch after submit reflects the change, mirroring
 * real server transitions (template established by US-029 / US-030).
 *
 * The list endpoint hits `/actionTypesAdmin` (NOT `/actionTypes`) — the
 * admin route returns full ActionType records with the internal
 * `parameters` map + `rules` array. `/actionTypes` is the runtime V2
 * read-only projection.
 */
async function stubActionTypeAdmin(
  page: Page,
  state: StubState,
): Promise<void> {
  // List on the admin-only collection endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypesAdmin`,
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
      await route.continue();
    },
  );

  // Create on the collection endpoint (mutation side).
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'POST') {
        state.createCalls += 1;
        const body = req.postDataJSON() as Record<string, unknown>;
        state.createBody = body;
        if (state.forceCreateError) {
          await route.fulfill({
            status: state.forceCreateError,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'ActionTypeAlreadyExists',
              errorName: 'Conflict',
              errorInstanceId: 'spec',
              parameters: { error: 'apiName already exists' },
            }),
          });
          return;
        }
        const apiName = String(body.apiName ?? 'newAction');
        const parametersArr =
          (body.parameters as Array<Record<string, unknown>>) ?? [];
        // The handler emits parameters as an array (internal stored format)
        // but ActionType.parameters on the wire is a map keyed by id. Reflect
        // that conversion in the stub so a subsequent refetch + Edit modal
        // sees the same shape the real backend produces.
        const parametersMap: Record<string, unknown> = {};
        for (const p of parametersArr) {
          const id = String(p.id ?? '');
          if (!id) continue;
          parametersMap[id] = {
            dataType: { type: String(p.type ?? 'string') },
            required: Boolean(p.required ?? false),
            ...(p.description ? { description: String(p.description) } : {}),
          };
        }
        const created: MockActionType = {
          rid: `ri.ontology.main.action-type.${apiName}`,
          apiName,
          displayName: String(body.displayName ?? apiName),
          description: body.description as string | undefined,
          status: (body.status as Status) ?? 'ACTIVE',
          parameters: parametersMap,
          rules:
            (body.rules as Array<Record<string, unknown>>) ?? [],
        };
        state.rows.push(created);
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(created),
        });
        return;
      }
      // GET would shadow this and break the V2 runtime projection if the
      // page ever fetched it — but ActionTypeAdminPage only hits
      // /actionTypesAdmin, so just let other methods pass through.
      await route.continue();
    },
  );

  // Update + Delete on the per-rid endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes/byRid/*`,
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
          const parametersArr =
            (body.parameters as Array<Record<string, unknown>>) ?? [];
          const parametersMap: Record<string, unknown> = {};
          for (const p of parametersArr) {
            const id = String(p.id ?? '');
            if (!id) continue;
            parametersMap[id] = {
              dataType: { type: String(p.type ?? 'string') },
              required: Boolean(p.required ?? false),
              ...(p.description ? { description: String(p.description) } : {}),
            };
          }
          state.rows[idx] = {
            ...state.rows[idx],
            displayName: String(
              body.displayName ?? state.rows[idx].displayName,
            ),
            description: (body.description as string | undefined) ??
              state.rows[idx].description,
            status: (body.status as Status) ?? state.rows[idx].status,
            parameters: parametersMap,
            rules:
              (body.rules as Array<Record<string, unknown>>) ??
              state.rows[idx].rules,
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

  // objectTypes for the rule editor's Object Type select.
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

  // linkTypes for the rule editor's Link Type select (createLink /
  // deleteLink rules).
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/linkTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: LINK_TYPES }),
      });
    },
  );
}

describeFeature('Admin: Action Type editor', () => {
  test('Scenario: the table renders the seeded rows with per-row data attrs @smoke', async ({
    page,
    request,
  }) => {
    const admin = new ActionTypeAdminPage(page);
    const state = makeState([archiveEmployee, createEmployee, promoteEmployee]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three ActionTypes are seeded on northwind', async () => {
      await stubActionTypeAdmin(page, state);
    });

    await When('the user opens /admin/northwind/actionTypes', async () => {
      await admin.goto(ONTOLOGY);
    });

    await Then('the page root + table are visible with three rows', async () => {
      await expect(admin.root).toBeVisible();
      await expect(admin.loading).toBeHidden();
      await expect(admin.table).toBeVisible();
      await expect(admin.rows).toHaveCount(3);
    });

    await Then('each row carries the apiName + status + parameter/rule count data attrs', async () => {
      // Rows sorted by displayName asc:
      //   Archive Employee  (ACTIVE)        1 param  1 rule
      //   Create Employee   (EXPERIMENTAL)  1 param  1 rule
      //   Promote Employee  (DEPRECATED)    0 param  0 rule
      await expect(admin.rowByApiName('archiveEmployee')).toBeVisible();
      await expect(admin.rowByApiName('createEmployee')).toBeVisible();
      await expect(admin.rowByApiName('promoteEmployee')).toBeVisible();
      // data-* status attrs lock the enum *value* (not Badge i18n text)
      // — same template as US-030 data-link-type-cardinality.
      await expect(admin.rowByApiName('archiveEmployee')).toHaveAttribute(
        'data-action-type-status',
        'ACTIVE',
      );
      await expect(admin.rowByApiName('createEmployee')).toHaveAttribute(
        'data-action-type-status',
        'EXPERIMENTAL',
      );
      await expect(admin.rowByApiName('promoteEmployee')).toHaveAttribute(
        'data-action-type-status',
        'DEPRECATED',
      );
      // Parameter/rule counts are numeric data attrs so the spec doesn't
      // depend on the rendered column text (which is just the integer).
      // This locks "the table reports the right schema density" while
      // staying decoupled from cell layout / formatting.
      await expect(admin.rowByApiName('archiveEmployee')).toHaveAttribute(
        'data-action-type-parameter-count',
        '1',
      );
      await expect(admin.rowByApiName('archiveEmployee')).toHaveAttribute(
        'data-action-type-rule-count',
        '1',
      );
      await expect(admin.rowByApiName('promoteEmployee')).toHaveAttribute(
        'data-action-type-parameter-count',
        '0',
      );
      await expect(admin.rowByApiName('promoteEmployee')).toHaveAttribute(
        'data-action-type-rule-count',
        '0',
      );
    });
  });

  test('Scenario: creating an ActionType POSTs the expected body with parameters + rules and roundtrips into a new row @smoke', async ({
    page,
  }) => {
    const admin = new ActionTypeAdminPage(page);
    const state = makeState([archiveEmployee, createEmployee, promoteEmployee]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubActionTypeAdmin(page, state);
    });

    await Given('the user is on the ActionType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Action Type modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When('the user fills displayName, adds a parameter, adds a createObject rule with a parameter binding, and submits', async () => {
      await admin.displayNameInput.fill('Greet Employee');
      // apiName auto-populates from displayName — see toApiName() and
      // updateDisplayName() in ActionTypeAdminPage.tsx.
      await expect(admin.apiNameInput).toHaveValue('greetEmployee');

      // Add a single parameter row: name (string, required).
      await admin.addParameterButton.click();
      await admin.parametersSection
        .getByLabel(/Parameter 1 id/i)
        .fill('name');
      // Default type is 'string' and required defaults to false; toggle on.
      await admin.parametersSection
        .getByLabel(/Parameter 1 type/i)
        .selectOption('string');
      await admin.parametersSection
        .getByRole('checkbox', { name: /Required/i })
        .check();

      // Add a single createObject rule with a property binding that pulls
      // from the parameter. Covers AC "rules" + binding wire shape.
      await admin.addRuleButton.click();
      await admin.rulesSection
        .getByLabel(/Rule 1 type/i)
        .selectOption('createObject');
      await admin.rulesSection
        .getByLabel(/Rule 1 object type/i)
        .selectOption('Employee');
      await admin.rulesSection
        .getByRole('button', { name: /\+ Add binding/i })
        .click();
      await admin.rulesSection
        .getByLabel(/Rule 1 binding 1 property/i)
        .fill('name');
      // Default source is 'parameter'; explicit select to lock the path.
      await admin.rulesSection
        .getByLabel(/Rule 1 binding 1 source/i)
        .selectOption('parameter');
      await admin.rulesSection
        .getByLabel(/Rule 1 binding 1 value/i)
        .selectOption('name');

      // The Builder modal is the same large-form layout established by
      // US-029 / US-030 — Submit can fall outside a 720px viewport when
      // the form grows past ~10 fields (here: 6 base + 1 parameter +
      // 1 rule + 1 binding rows). requestSubmit() is the same React
      // onSubmit code path that Enter-on-input triggers.
      await admin.createForm.evaluate((form) =>
        (form as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('POST /actionTypes was invoked exactly once with the expected payload', async () => {
      await expect.poll(() => state.createCalls).toBe(1);
      const body = state.createBody as Record<string, unknown>;
      expect(body).toMatchObject({
        apiName: 'greetEmployee',
        displayName: 'Greet Employee',
        status: 'ACTIVE',
      });
      // The internal stored format emits parameters as an array of {id,
      // type, required, ...} entries (ActionTypeParamDef[]). This locks
      // the friendly-builder → wire conversion in
      // ActionTypeAdminPage.tsx:onSubmit, not just that *some* parameter
      // was sent.
      const parameters = body.parameters as Array<Record<string, unknown>>;
      expect(parameters).toHaveLength(1);
      expect(parameters[0]).toMatchObject({
        id: 'name',
        type: 'string',
        required: true,
      });
      // The rule array preserves the createObject discriminator + the
      // parameter binding shape ({type: 'parameter', value: 'name'}).
      const rules = body.rules as Array<Record<string, unknown>>;
      expect(rules).toHaveLength(1);
      expect(rules[0]).toMatchObject({
        type: 'createObject',
        objectType: 'Employee',
        propertyBindings: {
          name: { type: 'parameter', value: 'name' },
        },
      });
    });

    await Then('the modal closes and the new row joins the table', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rows).toHaveCount(4);
      await expect(admin.rowByApiName('greetEmployee')).toBeVisible();
      await expect(admin.rowByApiName('greetEmployee')).toHaveAttribute(
        'data-action-type-status',
        'ACTIVE',
      );
      await expect(admin.rowByApiName('greetEmployee')).toHaveAttribute(
        'data-action-type-parameter-count',
        '1',
      );
      await expect(admin.rowByApiName('greetEmployee')).toHaveAttribute(
        'data-action-type-rule-count',
        '1',
      );
    });
  });

  test('Scenario: editing an ActionType pre-populates parameters + rules from the wire and PUTs an updated displayName while keeping apiName immutable @smoke', async ({
    page,
  }) => {
    const admin = new ActionTypeAdminPage(page);
    const state = makeState([archiveEmployee, createEmployee, promoteEmployee]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubActionTypeAdmin(page, state);
    });

    await Given('the user is on the ActionType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Edit modal for archiveEmployee', async () => {
      await admin.editButtonFor('archiveEmployee').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.editForm).toBeVisible();
    });

    await Then('the Edit modal is pre-populated from the wire row (params + rules)', async () => {
      // displayName from the wire row.
      await expect(admin.displayNameInput).toHaveValue('Archive Employee');
      // apiName is locked once an Action is created — see the
      // disabled={isEdit} input in ActionTypeAdminPage.tsx.
      await expect(admin.apiNameInput).toHaveValue('archiveEmployee');
      await expect(admin.apiNameInput).toBeDisabled();
      // The JSON preview is the canonical "what will get submitted"
      // surface; assert it contains the pre-populated parameter id +
      // rule type + static binding from the seeded row.
      await expect(admin.jsonPreview).toContainText('employeeId');
      await expect(admin.jsonPreview).toContainText('modifyObject');
      await expect(admin.jsonPreview).toContainText('archived');
    });

    await When('the user changes displayName and submits', async () => {
      await admin.displayNameInput.fill('Archive Employee Record');
      await admin.editForm.evaluate((form) =>
        (form as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('PUT /actionTypes/byRid was invoked exactly once with the narrow update body', async () => {
      await expect.poll(() => state.updateCalls.length).toBe(1);
      expect(state.updateCalls[0]).toMatchObject({
        rid: 'ri.ontology.main.action-type.archive-emp',
        body: expect.objectContaining({
          displayName: 'Archive Employee Record',
          status: 'ACTIVE',
        }),
      });
      // apiName is not part of UpdateActionTypeRequest — locks the
      // narrow-DTO contract from api/ontologies.ts:413. Same template as
      // US-030 LinkType wire-shape negative assertion.
      const updateBody = state.updateCalls[0]?.body as Record<string, unknown>;
      expect(updateBody.apiName).toBeUndefined();
      // The wire format expects parameters as an array (ActionTypeParamDef[])
      // even on edit, sourced from the same builder state as create. Lock
      // the array shape + first element to catch regressions where the
      // edit path accidentally sends the wire-map shape instead.
      const parameters = updateBody.parameters as Array<
        Record<string, unknown>
      >;
      expect(parameters).toHaveLength(1);
      expect(parameters[0]).toMatchObject({
        id: 'employeeId',
        type: 'string',
        required: true,
      });
    });

    await Then('the modal closes and the row shows the updated displayName', async () => {
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rowByApiName('archiveEmployee')).toContainText(
        'Archive Employee Record',
      );
      await expect(admin.rowByApiName('archiveEmployee')).toHaveAttribute(
        'data-action-type-status',
        'ACTIVE',
      );
    });
  });

  test('Scenario: deleting an ActionType calls DELETE and removes the row from the table', async ({
    page,
  }) => {
    const admin = new ActionTypeAdminPage(page);
    const state = makeState([archiveEmployee, createEmployee, promoteEmployee]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubActionTypeAdmin(page, state);
    });

    await Given('the user is on the ActionType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Delete modal for promoteEmployee', async () => {
      await admin.deleteButtonFor('promoteEmployee').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.deleteModal).toBeVisible();
    });

    await Then('the delete modal cites the apiName + rid via data attributes', async () => {
      // The Delete modal locks the resource identity in data-* attrs so
      // the spec doesn't depend on i18n copy in the confirmation prose.
      await expect(admin.deleteModal).toHaveAttribute(
        'data-action-type-api-name',
        'promoteEmployee',
      );
      await expect(admin.deleteModal).toHaveAttribute(
        'data-action-type-rid',
        'ri.ontology.main.action-type.promote-emp',
      );
    });

    await When('the user confirms the delete', async () => {
      await admin.deleteConfirm.click();
    });

    await Then('DELETE /actionTypes/byRid was captured and the row is gone', async () => {
      await expect.poll(() => state.deleteCalls.length).toBe(1);
      expect(state.deleteCalls[0]).toBe(
        'ri.ontology.main.action-type.promote-emp',
      );
      await expect(admin.modalOverlay).toBeHidden();
      await expect(admin.rows).toHaveCount(2);
      await expect(admin.rowByApiName('promoteEmployee')).toHaveCount(0);
    });
  });

  test('Scenario: the create form blocks duplicate apiNames with an inline alert and no POST', async ({
    page,
  }) => {
    const admin = new ActionTypeAdminPage(page);
    const state = makeState([archiveEmployee, createEmployee, promoteEmployee]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubActionTypeAdmin(page, state);
    });

    await Given('the user is on the ActionType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Action Type modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When('the user fills displayName then overrides apiName to an existing one', async () => {
      await admin.displayNameInput.fill('Another Archive');
      // Manually overriding apiName flips apiNameDirty=true so the
      // auto-populate stays off — see ActionTypeAdminPage.tsx:apiName
      // onChange handler.
      await admin.apiNameInput.fill('archiveEmployee');
    });

    await Then('the submit button is disabled and the duplicate alert surfaces', async () => {
      // duplicateApiName=true (computed via apiNameTaken set) drives
      // canSubmit=false → button disabled, and renders an inline error
      // on the API Name Field component. The UI contract is "the user
      // cannot submit via the visible affordance" — the inline alert is
      // the validator's user-facing surface, the disabled submit button
      // is the gate.
      await expect(admin.createSubmit).toBeDisabled();
      await expect(admin.createForm).toContainText(
        'An Action with apiName "archiveEmployee" already exists.',
      );
      // Clicking the disabled submit button is a no-op — Playwright
      // .click() on a disabled button errors out before any DOM event
      // fires, so the only way to observe "user cannot submit" is to
      // assert the button stays disabled while the alert is up. We
      // deliberately do NOT exercise form.requestSubmit() here: that
      // API bypasses the button's `disabled` state and would mask the
      // UI-level contract by directly invoking the onSubmit handler.
      // Future work to harden the onSubmit handler itself with a
      // duplicate-apiName guard should add its own positive scenario.
      expect(state.createCalls).toBe(0);
    });
  });

  test('Scenario: the JSON preview reflects rule edits live without submitting', async ({
    page,
  }) => {
    const admin = new ActionTypeAdminPage(page);
    const state = makeState([archiveEmployee, createEmployee, promoteEmployee]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubActionTypeAdmin(page, state);
    });

    await Given('the user is on the ActionType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Action Type modal and adds a parameter + rule', async () => {
      await admin.newButton.click();
      await expect(admin.createForm).toBeVisible();
      await admin.displayNameInput.fill('Promote Employee');
      await admin.addParameterButton.click();
      await admin.parametersSection
        .getByLabel(/Parameter 1 id/i)
        .fill('targetId');
      await admin.addRuleButton.click();
      await admin.rulesSection
        .getByLabel(/Rule 1 type/i)
        .selectOption('modifyObject');
      await admin.rulesSection
        .getByLabel(/Rule 1 object type/i)
        .selectOption('Employee');
    });

    await Then('the JSON preview reflects the builder state without submission', async () => {
      // The preview locks the friendly-builder → wire conversion as a
      // live read-only surface (separate from the actual submit path),
      // so users see exactly what will be sent before clicking Create.
      // Same role as US-024 LogicFlows JSON-preview / US-028 ObjectSet
      // Diff plain-JSON sections.
      await expect(admin.jsonPreview).toContainText('promoteEmployee');
      await expect(admin.jsonPreview).toContainText('targetId');
      await expect(admin.jsonPreview).toContainText('modifyObject');
      // No POST was made — the preview is purely a render of local state.
      expect(state.createCalls).toBe(0);
    });
  });
});
