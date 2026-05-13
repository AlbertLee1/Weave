import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  Then,
  ValueTypeAdminPage,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/:ontology/valueTypes` — the ValueType editor
 * rendered by `src/components/admin/ValueTypeAdminPage.tsx` (US-051 /
 * PC-A05).
 *
 * AC mapping → scenario:
 *   table / list                      → list-renders-rows-with-base-type-and-constraint-summary
 *   create with constraint editor     → create-flow-posts-pattern-constraint-and-refreshes
 *                                       create-flow-posts-min-max-constraint
 *   update                            → update-flow-puts-enum-constraint
 *   delete                            → delete-flow-confirms-and-removes-row
 *   used-by reverse references        → usages-modal-lists-properties-by-base-type
 *   duplicate apiName guardrail       → create-form-blocks-duplicate-api-name
 *
 * Stubs the admin endpoints with `page.route` so the page renders
 * deterministic fixtures without touching real backend data, and
 * mutations latch a `state` object so subsequent refetches reflect the
 * server transition. Same convention as US-029 ~ US-032 admin specs.
 */

const ONTOLOGY = 'northwind';

interface MockValueType {
  rid: string;
  apiName: string;
  displayName: string;
  baseType: string;
  constraints?: Record<string, unknown>;
  version: number;
}

interface MockUsage {
  objectTypeRid: string;
  objectTypeApiName: string;
  propertyRid: string;
  propertyApiName: string;
}

const emailVT: MockValueType = {
  rid: 'ri.ontology.main.value-type.email',
  apiName: 'EmailAddress',
  displayName: 'Email Address',
  baseType: 'string',
  constraints: { pattern: '^[^@]+@[^@]+\\.[^@]+$' },
  version: 1,
};

const currencyVT: MockValueType = {
  rid: 'ri.ontology.main.value-type.currency',
  apiName: 'Currency',
  displayName: 'Currency',
  baseType: 'double',
  constraints: { min: 0, max: 1000000 },
  version: 1,
};

const statusVT: MockValueType = {
  rid: 'ri.ontology.main.value-type.status',
  apiName: 'Status',
  displayName: 'Status',
  baseType: 'string',
  constraints: { enum: ['active', 'pending', 'archived'] },
  version: 1,
};

interface StubState {
  rows: MockValueType[];
  usages: Record<string, MockUsage[]>; // keyed by ValueType rid
  listCalls: number;
  createCalls: number;
  createBody: unknown;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  usagesCalls: string[];
}

function makeState(
  initial: MockValueType[],
  usages: Record<string, MockUsage[]> = {},
): StubState {
  return {
    rows: initial.map((row) => structuredClone(row)),
    usages: structuredClone(usages),
    listCalls: 0,
    createCalls: 0,
    createBody: null,
    updateCalls: [],
    deleteCalls: [],
    usagesCalls: [],
  };
}

async function stubValueTypeAdmin(
  page: Page,
  state: StubState,
): Promise<void> {
  // Admin list endpoint (no `?preview=true` gate; returns `{data}` envelope
  // to match the InterfaceAdminPage list contract). The page hits this
  // /valueTypesAdmin path so it stays orthogonal to the read-only V2
  // /valueTypes endpoint that the SDK + runtime still call.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/valueTypesAdmin`,
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

  // Create on the collection endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/valueTypes`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'POST') {
        state.createCalls += 1;
        const body = req.postDataJSON() as Record<string, unknown>;
        state.createBody = body;
        const apiName = String(body.apiName ?? 'newValueType');
        const created: MockValueType = {
          rid: `ri.ontology.main.value-type.${apiName}`,
          apiName,
          displayName: String(body.displayName ?? apiName),
          baseType: String(body.baseType ?? 'string'),
          constraints: body.constraints as Record<string, unknown> | undefined,
          version: 1,
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

  // Per-rid endpoints: PUT (Update), DELETE (Delete), and the nested
  // /usages endpoint (Used By reverse lookup). All three live under the
  // same byRid/{rid} prefix; we route by URL suffix.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/valueTypes/byRid/*`,
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
            baseType: String(body.baseType ?? state.rows[idx].baseType),
            constraints: body.constraints as
              | Record<string, unknown>
              | undefined,
            version: state.rows[idx].version + 1,
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
        delete state.usages[rid];
        await route.fulfill({ status: 204, body: '' });
        return;
      }
      await route.continue();
    },
  );

  // Used By endpoint (separate path so it doesn't collide with the byRid
  // catch-all above).
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/valueTypes/byRid/*/usages`,
    async (route: Route) => {
      const req = route.request();
      const url = new URL(req.url());
      const match = url.pathname.match(
        /\/valueTypes\/byRid\/([^/]+)\/usages$/,
      );
      const rid = match ? decodeURIComponent(match[1]) : '';
      if (req.method() === 'GET') {
        state.usagesCalls.push(rid);
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: state.usages[rid] ?? [] }),
        });
        return;
      }
      await route.continue();
    },
  );
}

describeFeature('Admin: ValueType editor', () => {
  test('Scenario: the table renders ValueTypes with base type + constraint summary @smoke', async ({
    page,
    request,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([emailVT, currencyVT, statusVT]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three ValueTypes are seeded with varied constraints', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await When('the user opens /admin/northwind/valueTypes', async () => {
      await admin.goto(ONTOLOGY);
    });

    await Then('the page root + table are visible with three rows', async () => {
      await expect(admin.root).toBeVisible();
      await expect(admin.loading).toBeHidden();
      await expect(admin.table).toBeVisible();
      await expect(admin.rows).toHaveCount(3);
    });

    await Then(
      'each row exposes apiName + base type + constraint kind via data attrs',
      async () => {
        // Rows are sorted by displayName:
        //   Currency       (double, range)
        //   Email Address  (string, pattern)
        //   Status         (string, enum)
        await expect(admin.rowByApiName('EmailAddress')).toBeVisible();
        await expect(admin.rowByApiName('Currency')).toBeVisible();
        await expect(admin.rowByApiName('Status')).toBeVisible();

        // Base type is the resolved primitive (`string`, `double`, …) — locked
        // as a data attr so the assertion stays decoupled from cell rendering.
        await expect(admin.rowByApiName('EmailAddress')).toHaveAttribute(
          'data-value-type-base-type',
          'string',
        );
        await expect(admin.rowByApiName('Currency')).toHaveAttribute(
          'data-value-type-base-type',
          'double',
        );

        // Constraint kind is a derived label — pattern / range / enum / none —
        // computed from the constraints JSON so the spec doesn't depend on a
        // free-form prose summary.
        await expect(admin.rowByApiName('EmailAddress')).toHaveAttribute(
          'data-value-type-constraint-kind',
          'pattern',
        );
        await expect(admin.rowByApiName('Currency')).toHaveAttribute(
          'data-value-type-constraint-kind',
          'range',
        );
        await expect(admin.rowByApiName('Status')).toHaveAttribute(
          'data-value-type-constraint-kind',
          'enum',
        );
      },
    );
  });

  test('Scenario: creating a ValueType with a pattern constraint POSTs the expected body and roundtrips into a new row @smoke', async ({
    page,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([emailVT, currencyVT, statusVT]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await Given('the user is on the ValueType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New ValueType modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When(
      'the user fills displayName, picks string base type + pattern constraint, and submits',
      async () => {
        await admin.displayNameInput.fill('Phone Number');
        // apiName auto-populates from displayName via the same camelCase
        // helper used by other admin builders.
        await expect(admin.apiNameInput).toHaveValue('phoneNumber');

        await admin.baseTypeSelect.selectOption('string');
        await admin.constraintKindSelect.selectOption('pattern');
        await admin.patternInput.fill('^\\+?[0-9 ()-]{7,}$');

        // The Builder modal uses requestSubmit() — same pattern as the other
        // admin builders — to drive React's onSubmit handler regardless of
        // viewport scroll position.
        await admin.createForm.evaluate((form) =>
          (form as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'POST /valueTypes was invoked exactly once with the constraint payload',
      async () => {
        await expect.poll(() => state.createCalls).toBe(1);
        const body = state.createBody as Record<string, unknown>;
        expect(body).toMatchObject({
          apiName: 'phoneNumber',
          displayName: 'Phone Number',
          baseType: 'string',
        });
        // Constraint payload contract: { pattern: '...' } — only the active
        // constraint key is sent so the server doesn't have to discriminate
        // between unset and intentionally empty min/max/enum.
        expect(body.constraints).toEqual({
          pattern: '^\\+?[0-9 ()-]{7,}$',
        });
      },
    );

    await Then(
      'the modal closes and the new row joins the table',
      async () => {
        await expect(admin.modalOverlay).toBeHidden();
        await expect(admin.rows).toHaveCount(4);
        await expect(admin.rowByApiName('phoneNumber')).toBeVisible();
        await expect(admin.rowByApiName('phoneNumber')).toHaveAttribute(
          'data-value-type-constraint-kind',
          'pattern',
        );
      },
    );
  });

  test('Scenario: creating a ValueType with a min/max range constraint POSTs numeric bounds', async ({
    page,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([]);

    await Given('the admin endpoints are stubbed with no seeds', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await Given('the user is on the ValueType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.empty).toBeVisible();
    });

    await When(
      'the user opens the New ValueType modal and picks range constraint on integer',
      async () => {
        await admin.newButton.click();
        await expect(admin.createForm).toBeVisible();
        await admin.displayNameInput.fill('Age');
        await admin.baseTypeSelect.selectOption('integer');
        await admin.constraintKindSelect.selectOption('range');
        await admin.minInput.fill('0');
        await admin.maxInput.fill('150');
        await admin.createForm.evaluate((form) =>
          (form as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'POST /valueTypes was captured with min + max numeric values',
      async () => {
        await expect.poll(() => state.createCalls).toBe(1);
        const body = state.createBody as Record<string, unknown>;
        expect(body).toMatchObject({
          apiName: 'age',
          baseType: 'integer',
        });
        // The bounds round-trip as numbers, not strings — the editor coerces
        // numeric input on submit so the wire contract stays typed.
        expect(body.constraints).toEqual({ min: 0, max: 150 });
      },
    );
  });

  test('Scenario: editing an existing ValueType swaps its constraint to an enum list and PUTs the new constraints', async ({
    page,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([emailVT, currencyVT, statusVT]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await Given('the user is on the ValueType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Edit modal for EmailAddress', async () => {
      await admin.editButtonFor('EmailAddress').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.editForm).toBeVisible();
    });

    await When(
      'the user swaps the constraint kind to enum and supplies three values',
      async () => {
        // apiName field is disabled in Edit mode (the apiName is the stable
        // identity; same convention as Interface / ActionType admin).
        await expect(admin.apiNameInput).toBeDisabled();
        await expect(admin.apiNameInput).toHaveValue('EmailAddress');

        await admin.constraintKindSelect.selectOption('enum');
        await admin.enumInput.fill('work, personal, system');
        await admin.editForm.evaluate((form) =>
          (form as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'PUT /valueTypes/byRid/{rid} was invoked with the enum payload',
      async () => {
        await expect.poll(() => state.updateCalls.length).toBe(1);
        const call = state.updateCalls[0];
        expect(call.rid).toBe(emailVT.rid);
        const body = call.body as Record<string, unknown>;
        // The editor splits the comma-separated input, trims each value, and
        // strips empties — the wire payload is a real string[] array.
        expect(body.constraints).toEqual({
          enum: ['work', 'personal', 'system'],
        });
      },
    );

    await Then(
      'the modal closes and the row reflects the new enum constraint',
      async () => {
        await expect(admin.modalOverlay).toBeHidden();
        await expect(admin.rowByApiName('EmailAddress')).toHaveAttribute(
          'data-value-type-constraint-kind',
          'enum',
        );
      },
    );
  });

  test('Scenario: deleting a ValueType confirms via the modal and removes the row from the table', async ({
    page,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([emailVT, currencyVT, statusVT]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await Given('the user is on the ValueType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Delete modal for Status', async () => {
      await admin.deleteButtonFor('Status').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.deleteModal).toBeVisible();
    });

    await Then('the delete modal cites the apiName + rid via data attrs', async () => {
      await expect(admin.deleteModal).toHaveAttribute(
        'data-value-type-api-name',
        'Status',
      );
      await expect(admin.deleteModal).toHaveAttribute(
        'data-value-type-rid',
        statusVT.rid,
      );
    });

    await When('the user confirms the delete', async () => {
      await admin.deleteConfirm.click();
    });

    await Then(
      'DELETE /valueTypes/byRid was captured and the row is gone',
      async () => {
        await expect.poll(() => state.deleteCalls.length).toBe(1);
        expect(state.deleteCalls[0]).toBe(statusVT.rid);
        await expect(admin.modalOverlay).toBeHidden();
        await expect(admin.rows).toHaveCount(2);
        await expect(admin.rowByApiName('Status')).toHaveCount(0);
      },
    );
  });

  test('Scenario: the Used By modal lists Properties whose base_type references this ValueType @smoke', async ({
    page,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([emailVT, currencyVT, statusVT], {
      [emailVT.rid]: [
        {
          objectTypeRid: 'ri.ontology.main.object-type.emp',
          objectTypeApiName: 'Employee',
          propertyRid: 'ri.ontology.main.property.emp.email',
          propertyApiName: 'email',
        },
        {
          objectTypeRid: 'ri.ontology.main.object-type.cust',
          objectTypeApiName: 'Customer',
          propertyRid: 'ri.ontology.main.property.cust.email',
          propertyApiName: 'email',
        },
      ],
      [statusVT.rid]: [],
    });

    await Given('the admin endpoints are stubbed with usages seeded', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await Given('the user is on the ValueType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When(
      'the user opens the Used By modal for EmailAddress',
      async () => {
        await admin.usagesButtonFor('EmailAddress').click();
        await expect(admin.modalOverlay).toBeVisible();
        await expect(admin.usagesModal).toBeVisible();
      },
    );

    await Then(
      'GET /usages was called and the modal lists Employee.email + Customer.email',
      async () => {
        await expect.poll(() => state.usagesCalls.length).toBeGreaterThanOrEqual(
          1,
        );
        expect(state.usagesCalls).toContain(emailVT.rid);

        await expect(admin.usagesList).toBeVisible();
        await expect(admin.usagesRows).toHaveCount(2);

        // Each usage row exposes the ObjectType + Property apiName via data
        // attrs so the spec stays stable across i18n label changes.
        await expect(
          page.locator(
            `[data-testid="value-type-usage-row"][data-object-type-api-name="Employee"][data-property-api-name="email"]`,
          ),
        ).toBeVisible();
        await expect(
          page.locator(
            `[data-testid="value-type-usage-row"][data-object-type-api-name="Customer"][data-property-api-name="email"]`,
          ),
        ).toBeVisible();
      },
    );

    await When('the user closes the Used By modal and opens it for Status', async () => {
      await admin.usagesClose.click();
      await expect(admin.usagesModal).toBeHidden();
      await admin.usagesButtonFor('Status').click();
      await expect(admin.usagesModal).toBeVisible();
    });

    await Then(
      'the empty state is shown because no Property references Status yet',
      async () => {
        await expect(admin.usagesEmpty).toBeVisible();
        await expect(admin.usagesRows).toHaveCount(0);
      },
    );
  });

  test('Scenario: the create form blocks duplicate apiNames with an inline alert and no POST', async ({
    page,
  }) => {
    const admin = new ValueTypeAdminPage(page);
    const state = makeState([emailVT, currencyVT, statusVT]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubValueTypeAdmin(page, state);
    });

    await Given('the user is on the ValueType admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New ValueType modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When(
      'the user types a displayName then overrides apiName to an existing one',
      async () => {
        await admin.displayNameInput.fill('Another Email');
        await admin.apiNameInput.fill('EmailAddress');
      },
    );

    await Then(
      'the submit button is disabled and the duplicate alert surfaces',
      async () => {
        await expect(admin.createSubmit).toBeDisabled();
        await expect(admin.createForm).toContainText(
          'A ValueType with apiName "EmailAddress" already exists.',
        );
        // We deliberately don't requestSubmit() — the duplicate guardrail is
        // a UI-level gate (disabled button + inline alert); bypassing it via
        // form.requestSubmit() would mask the contract.
        expect(state.createCalls).toBe(0);
      },
    );
  });
});
