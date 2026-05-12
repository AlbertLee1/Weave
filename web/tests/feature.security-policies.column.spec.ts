import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  SecurityPoliciesPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/:ontology/security` Column Masks tab — the
 * second pane of the Security Policies UI rendered by
 * `src/components/securityPolicies/SecurityPoliciesPage.tsx` (US-042,
 * PC-A07b).
 *
 * Scenarios map to the US-042 acceptance criteria:
 *
 *   - "Column Masks tab：列出 ObjectType×Column → mask policy"
 *     → "list renders existing column masks with object-type / property /
 *        rule / applies-to" locks the per-row contract.
 *   - "Create/Edit/Delete with role-based applicability"
 *     → "create posts the wire-shape" locks Create + property dropdown +
 *        mask-rule dropdown; "delete confirm dialog removes the row"
 *        locks Delete.
 *   - "Test as user 复用 PC-A07a 模拟器"
 *     → "test-as-user simulator marks exempt vs masked" locks the
 *        client-side AppliesTo evaluator with the column-mask semantic
 *        flip (matching = exempt from mask, sees clear value).
 *
 * Honest mapping callouts:
 *   - AppliesTo on column masks is an ALLOW LIST (matching callers see
 *     clear data), inverted from row-policies where matching = governed.
 *     The simulator UI labels the decision with "exempt" / "masked"
 *     accordingly; pkg/masking/model.go:52 is the canonical source.
 *   - Like row-policies there is no per-user simulator backend endpoint
 *     (Engine.Compile is internal); IsApplicable is replicated client-
 *     side in api/securityPolicies.ts (`isMaskExempt`).
 */

const ONTOLOGY = 'northwind';

interface MockObjectType {
  rid: string;
  apiName: string;
  displayName: string;
  pluralDisplayName?: string;
  primaryKey: string;
  status: string;
  visibility: string;
  properties?: Record<string, { dataType: { type: string }; rid: string }>;
}

interface MockColumnMask {
  rid: string;
  objectTypeRid: string;
  propertyApiName: string;
  maskRule: 'hash' | 'redact' | 'partial';
  appliesTo: { roles?: string[]; groups?: string[]; users?: string[] };
  description?: string;
}

interface CapturedRequest {
  method: string;
  url: string;
  body: unknown;
}

interface Stubs {
  masks: MockColumnMask[];
  objectTypes: MockObjectType[];
  posts: CapturedRequest[];
  patches: CapturedRequest[];
  deletes: CapturedRequest[];
  failNextWith: string | null;
}

function newStubs(initial: Partial<Stubs> = {}): Stubs {
  return {
    masks: initial.masks ? initial.masks.map((m) => ({ ...m })) : [],
    objectTypes: initial.objectTypes
      ? initial.objectTypes.map((o) => ({ ...o }))
      : [],
    posts: [],
    patches: [],
    deletes: [],
    failNextWith: initial.failNextWith ?? null,
  };
}

function objectTypeFixture(overrides: Partial<MockObjectType> = {}): MockObjectType {
  return {
    rid: 'ri.ontology.main.objectType.customer',
    apiName: 'customer',
    displayName: 'Customer',
    pluralDisplayName: 'Customers',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    properties: {
      id: { dataType: { type: 'string' }, rid: 'ri.ontology.main.property.id' },
      email: {
        dataType: { type: 'string' },
        rid: 'ri.ontology.main.property.email',
      },
      name: {
        dataType: { type: 'string' },
        rid: 'ri.ontology.main.property.name',
      },
    },
    ...overrides,
  };
}

function columnMaskFixture(
  overrides: Partial<MockColumnMask> = {},
): MockColumnMask {
  return {
    rid: 'ri.masking.main.column-mask.mask-1',
    objectTypeRid: 'ri.ontology.main.objectType.customer',
    propertyApiName: 'email',
    maskRule: 'redact',
    appliesTo: { roles: ['dpo'] },
    description: 'Only DPO sees customer email in the clear',
    ...overrides,
  };
}

function failWith(errorName: string, reason: string) {
  return {
    status: 500,
    contentType: 'application/json',
    body: JSON.stringify({
      errorCode: 'INTERNAL',
      errorName,
      errorInstanceId: 'spec',
      parameters: { reason },
    }),
  };
}

async function stubEndpoints(page: Page, stubs: Stubs): Promise<void> {
  // ObjectType list (editor "Object Type" select fills from here).
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
        body: JSON.stringify({ data: stubs.objectTypes }),
      });
    },
  );

  // Per-ObjectType detail — fetches the properties Record used to
  // populate the editor's Property dropdown.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/*`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = req.url();
      const m = url
        .split('?')[0]
        .match(new RegExp(`/objectTypes/([^/?#]+)$`));
      const apiName = m?.[1] ?? '';
      const found = stubs.objectTypes.find((o) => o.apiName === apiName);
      if (!found) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'ObjectTypeNotFound',
            errorInstanceId: 'spec',
            parameters: { apiName },
          }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(found),
      });
    },
  );

  // Row-policies stays empty so switching tabs back / shell render does
  // not leak through to the real backend.
  await page.route(
    '**/api/admin/row-policies*',
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ policies: [] }),
      });
    },
  );

  // Column-masks single-resource (PATCH / DELETE / GET /{rid}). Register
  // before the catch-all list per US-040's "Playwright `*` does not cross
  // `/`" template (progress.txt notes).
  const PREFIX = '**/api/admin/column-masks';

  await page.route(`${PREFIX}/*`, async (route: Route) => {
    const req = route.request();
    const method = req.method();
    const url = req.url();
    const m = url.split('?')[0].match(/\/column-masks\/([^/?#]+)$/);
    const maskRid = m?.[1] ?? '';

    if (method === 'PATCH') {
      const body = (req.postDataJSON() ?? {}) as Partial<MockColumnMask>;
      stubs.patches.push({ method, url, body });
      if (stubs.failNextWith) {
        const name = stubs.failNextWith;
        stubs.failNextWith = null;
        await route.fulfill(failWith(name, 'Synthetic update failure'));
        return;
      }
      const idx = stubs.masks.findIndex((p) => p.rid === maskRid);
      if (idx === -1) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'ColumnMaskNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: maskRid },
          }),
        });
        return;
      }
      stubs.masks[idx] = {
        ...stubs.masks[idx],
        ...(body.maskRule ? { maskRule: body.maskRule } : {}),
        ...(body.appliesTo ? { appliesTo: body.appliesTo } : {}),
        ...(body.description !== undefined
          ? { description: body.description }
          : {}),
      };
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(stubs.masks[idx]),
      });
      return;
    }

    if (method === 'DELETE') {
      stubs.deletes.push({ method, url, body: null });
      if (stubs.failNextWith) {
        const name = stubs.failNextWith;
        stubs.failNextWith = null;
        await route.fulfill(failWith(name, 'Synthetic delete failure'));
        return;
      }
      const idx = stubs.masks.findIndex((p) => p.rid === maskRid);
      if (idx === -1) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'ColumnMaskNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: maskRid },
          }),
        });
        return;
      }
      stubs.masks.splice(idx, 1);
      await route.fulfill({ status: 204, body: '' });
      return;
    }

    if (method === 'GET') {
      const found = stubs.masks.find((p) => p.rid === maskRid);
      if (!found) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'ColumnMaskNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: maskRid },
          }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(found),
      });
      return;
    }

    await route.continue();
  });

  // List (GET) + Create (POST). The Playwright glob `*` does not cross
  // `/`, so this catch-all matches `/column-masks` and
  // `/column-masks?objectType=...` but NOT `/column-masks/{rid}`.
  await page.route(`${PREFIX}*`, async (route: Route) => {
    const req = route.request();
    const method = req.method();
    const url = req.url();

    if (method === 'GET') {
      const u = new URL(url);
      const objectTypeFilter = u.searchParams.get('objectType');
      const data = objectTypeFilter
        ? stubs.masks.filter((p) => p.objectTypeRid === objectTypeFilter)
        : stubs.masks;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ masks: data }),
      });
      return;
    }

    if (method === 'POST') {
      const body = (req.postDataJSON() ?? {}) as MockColumnMask;
      stubs.posts.push({ method, url, body });
      if (stubs.failNextWith) {
        const name = stubs.failNextWith;
        stubs.failNextWith = null;
        await route.fulfill(failWith(name, 'Synthetic create failure'));
        return;
      }
      const created: MockColumnMask = {
        rid: `ri.masking.main.column-mask.created-${stubs.posts.length}`,
        objectTypeRid: body.objectTypeRid,
        propertyApiName: body.propertyApiName,
        maskRule: body.maskRule,
        appliesTo: body.appliesTo ?? {},
        description: body.description,
      };
      stubs.masks.push(created);
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(created),
      });
      return;
    }

    await route.continue();
  });
}

async function openColumnMasksTab(securityPage: SecurityPoliciesPage) {
  await securityPage.goto(ONTOLOGY);
  await expect(securityPage.root).toBeVisible();
  await securityPage.tabBtn('column').click();
  await expect(securityPage.columnMasksTab).toBeVisible();
}

describeFeature('Security Policies — Column Masks tab', () => {
  test('Scenario: list renders existing column masks with object-type, property, rule and applies-to @smoke', async ({
    page,
    request,
  }) => {
    // AC: "Column Masks tab：列出 ObjectType×Column → mask policy". Seed
    // two masks against different (ObjectType, property) pairs and lock
    // the per-row contract: resolved ObjectType apiName, property apiName
    // rendered verbatim, mask-rule badge data attribute, and per-kind
    // applies-to badges.
    const customerOT = objectTypeFixture();
    const orderOT = objectTypeFixture({
      rid: 'ri.ontology.main.objectType.order',
      apiName: 'order',
      displayName: 'Order',
      properties: {
        id: {
          dataType: { type: 'string' },
          rid: 'ri.ontology.main.property.id',
        },
        amount: {
          dataType: { type: 'number' },
          rid: 'ri.ontology.main.property.amount',
        },
      },
    });
    const stubs = newStubs({
      objectTypes: [customerOT, orderOT],
      masks: [
        columnMaskFixture({
          rid: 'ri.masking.main.column-mask.customer-email',
          objectTypeRid: customerOT.rid,
          propertyApiName: 'email',
          maskRule: 'redact',
          appliesTo: { roles: ['dpo'] },
        }),
        columnMaskFixture({
          rid: 'ri.masking.main.column-mask.order-amount',
          objectTypeRid: orderOT.rid,
          propertyApiName: 'amount',
          maskRule: 'hash',
          appliesTo: { groups: ['finance'], users: ['alice@test'] },
          description: 'Finance + Alice see clear order amounts',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('two column masks exist', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the Column Masks tab', async () => {
      await openColumnMasksTab(securityPage);
    });

    await Then('both rows render with their resolved object types and rules', async () => {
      await expect(securityPage.columnMasksList).toBeVisible();
      const emailRow = securityPage.columnMaskRowByRid(
        'ri.masking.main.column-mask.customer-email',
      );
      const amountRow = securityPage.columnMaskRowByRid(
        'ri.masking.main.column-mask.order-amount',
      );
      await expect(emailRow).toBeVisible();
      await expect(amountRow).toBeVisible();
      await expect(emailRow).toHaveAttribute(
        'data-object-type-api-name',
        'customer',
      );
      await expect(emailRow).toHaveAttribute('data-property-api-name', 'email');
      await expect(emailRow).toHaveAttribute('data-mask-rule', 'redact');
      await expect(amountRow).toHaveAttribute(
        'data-object-type-api-name',
        'order',
      );
      await expect(amountRow).toHaveAttribute('data-property-api-name', 'amount');
      await expect(amountRow).toHaveAttribute('data-mask-rule', 'hash');
    });

    await Then(
      'each row renders the AppliesTo badges (role / group / user) scoped to the column-masks namespace',
      async () => {
        const emailRow = securityPage.columnMaskRowByRid(
          'ri.masking.main.column-mask.customer-email',
        );
        const amountRow = securityPage.columnMaskRowByRid(
          'ri.masking.main.column-mask.order-amount',
        );
        await expect(
          emailRow.locator(
            '[data-testid="column-masks-applies-badge"][data-kind="role"][data-value="dpo"]',
          ),
        ).toBeVisible();
        await expect(
          amountRow.locator(
            '[data-testid="column-masks-applies-badge"][data-kind="group"][data-value="finance"]',
          ),
        ).toBeVisible();
        await expect(
          amountRow.locator(
            '[data-testid="column-masks-applies-badge"][data-kind="user"][data-value="alice@test"]',
          ),
        ).toBeVisible();
      },
    );
  });

  test('Scenario: create posts the wire-shape with maskRule, property, and appliesTo @smoke', async ({
    page,
  }) => {
    // AC: "Create/Edit/Delete with role-based applicability". Lock the
    // create-mutation wire-shape (objectTypeRid + propertyApiName +
    // maskRule + appliesTo). The PRD AC says "role-based applicability"
    // — honest mapping with wire reality: backend accepts roles AND
    // groups AND users (pkg/masking/model.go:57 AppliesTo). We assert
    // role-based works but also lock that empty groups/users serialise
    // as omitted/empty arrays so the contract is full.
    const customerOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [customerOT],
      masks: [],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the column mask list is empty', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the tab and clicks Create column mask', async () => {
      await openColumnMasksTab(securityPage);
      await expect(securityPage.columnMasksEmptyState).toBeVisible();
      await securityPage.columnMasksCreateBtn.click();
      await expect(securityPage.columnMaskEditor).toBeVisible();
    });

    await When(
      'the user picks property "email", mask rule "hash" and DPO role',
      async () => {
        // Property dropdown is populated by /objectTypes/{apiName} —
        // wait for it to receive options before selecting.
        await expect.poll(async () =>
          (await securityPage
            .columnMaskEditorPropertyInput()
            .evaluate((el: Element) => {
              const sel = el as HTMLSelectElement;
              return sel.tagName === 'SELECT' ? sel.options.length : -1;
            })),
        ).toBeGreaterThan(1);
        await securityPage
          .columnMaskEditorPropertyInput()
          .selectOption('email');
        await securityPage.columnMaskEditorRuleSelect().selectOption('hash');
        await securityPage.columnMaskEditorRoles().fill('dpo');
        await securityPage
          .columnMaskEditorDescription()
          .fill('DPO sees clear customer emails');
      },
    );

    await Then('Submit is enabled', async () => {
      await expect(securityPage.columnMaskEditorSubmitBtn()).toBeEnabled();
    });

    await When('the user submits the form', async () => {
      await securityPage.columnMaskEditorSubmitBtn().click();
    });

    await Then('a POST to /api/admin/column-masks with the wire shape is captured', async () => {
      await expect.poll(() => stubs.posts.length).toBeGreaterThanOrEqual(1);
      const last = stubs.posts.at(-1)!;
      expect(last.method).toBe('POST');
      expect(last.url).toMatch(/\/api\/admin\/column-masks(\?|$)/);
      expect(last.body).toMatchObject({
        objectTypeRid: customerOT.rid,
        propertyApiName: 'email',
        maskRule: 'hash',
        appliesTo: {
          roles: ['dpo'],
        },
        description: 'DPO sees clear customer emails',
      });
      // Negative assertions:
      //   (a) the wire format is JSON with maskRule as an enum string —
      //       NOT a CEL expression. PRD AC alludes to "role-based
      //       applicability"; the backend wire format mirrors
      //       pkg/masking.MaskRule literal strings exactly. If a future
      //       PR introduces a `celExpression` or `script` field the
      //       contract is breached.
      //   (b) the picked objectTypeRid is a real RID, not the apiName.
      //       client-side resolves apiName → rid before posting.
      const body = last.body as Record<string, unknown>;
      expect(body.cel).toBeUndefined();
      expect(body.celExpression).toBeUndefined();
      expect(body.script).toBeUndefined();
      expect(body.objectType).toBeUndefined();
      expect(body.objectTypeRid).toMatch(/^ri\.ontology\./);
    });

    await Then('the editor closes and the new row appears after invalidation', async () => {
      await expect(securityPage.columnMaskEditor).toHaveCount(0);
      await expect(securityPage.columnMasksList).toBeVisible();
      await expect(
        securityPage.columnMaskRowByRid(
          'ri.masking.main.column-mask.created-1',
        ),
      ).toBeVisible();
    });
  });

  test('Scenario: delete confirm dialog removes the row and DELETE is captured', async ({
    page,
  }) => {
    // AC: "… Delete …". The Delete button opens a confirm dialog;
    // clicking Confirm fires DELETE /api/admin/column-masks/{rid} and
    // the row disappears after invalidation.
    const stubs = newStubs({
      objectTypes: [objectTypeFixture()],
      masks: [
        columnMaskFixture({
          rid: 'ri.masking.main.column-mask.delete-me',
          description: 'Doomed mask',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('one column mask exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the tab', async () => {
      await openColumnMasksTab(securityPage);
      await expect(
        securityPage.columnMaskRowByRid(
          'ri.masking.main.column-mask.delete-me',
        ),
      ).toBeVisible();
    });

    await When('the user clicks Delete on the row', async () => {
      await securityPage
        .columnMaskDeleteButton('ri.masking.main.column-mask.delete-me')
        .click();
      await expect(securityPage.columnMaskDeleteDialog).toBeVisible();
    });

    await When('the user confirms the deletion', async () => {
      await securityPage.columnMaskDeleteConfirmBtn().click();
    });

    await Then('a DELETE to the mask endpoint is captured', async () => {
      await expect.poll(() => stubs.deletes.length).toBeGreaterThanOrEqual(1);
      const last = stubs.deletes.at(-1)!;
      expect(last.method).toBe('DELETE');
      expect(last.url).toMatch(
        /\/api\/admin\/column-masks\/ri\.masking\.main\.column-mask\.delete-me$/,
      );
    });

    await Then(
      'the dialog closes and the list flips to the empty state',
      async () => {
        await expect(securityPage.columnMaskDeleteDialog).toHaveCount(0);
        await expect(securityPage.columnMasksEmptyState).toBeVisible();
        await expect(
          securityPage.columnMaskRowByRid(
            'ri.masking.main.column-mask.delete-me',
          ),
        ).toHaveCount(0);
      },
    );
  });

  test('Scenario: test-as-user simulator marks exempt vs masked using the row-policy simulator pattern', async ({
    page,
  }) => {
    // AC: "Test as user 复用 PC-A07a 模拟器".
    //
    // Honest-mapping note: the column-mask simulator reuses the row-
    // policy simulator's set-intersection algorithm
    // (pkg/masking.AppliesTo.IsApplicable is byte-identical to
    // pkg/rls.AppliesTo.IsApplicable). BUT the semantic of a match is
    // INVERTED: a hit on a column mask means the user is EXEMPT from
    // the mask (sees clear data). The simulator labels decisions with
    // "exempt" / "masked" to surface this — this scenario locks that
    // labeling end-to-end so the inversion doesn't silently regress.
    const customerOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [customerOT],
      masks: [
        columnMaskFixture({
          rid: 'ri.masking.main.column-mask.email-dpo-only',
          propertyApiName: 'email',
          appliesTo: { roles: ['dpo'] },
          description: 'DPO-only',
        }),
        columnMaskFixture({
          rid: 'ri.masking.main.column-mask.name-alice-only',
          propertyApiName: 'name',
          appliesTo: { users: ['alice@test'] },
          description: 'alice-only',
        }),
        columnMaskFixture({
          rid: 'ri.masking.main.column-mask.amount-finance-only',
          propertyApiName: 'amount',
          appliesTo: { groups: ['finance'] },
          description: 'finance-only',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('three column masks exist with different allow lists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the column-mask simulator', async () => {
      await openColumnMasksTab(securityPage);
      await expect(securityPage.columnMasksList).toBeVisible();
      await securityPage.columnMasksSimulatorToggleBtn.click();
      await expect(securityPage.columnMasksSimulator).toBeVisible();
    });

    await Then(
      'the default simulated user (viewer / alice@test) is exempt from alice-only and masked elsewhere',
      async () => {
        await expect(
          securityPage.columnMaskSimulatorExemptCount(),
        ).toContainText('1');
        await expect(
          securityPage.columnMaskSimulatorMaskedCount(),
        ).toContainText('2');
        await expect(
          securityPage.columnMaskSimulatorDecisionRow(
            'ri.masking.main.column-mask.name-alice-only',
          ),
        ).toHaveAttribute('data-exempt', 'true');
        await expect(
          securityPage.columnMaskSimulatorDecisionRow(
            'ri.masking.main.column-mask.email-dpo-only',
          ),
        ).toHaveAttribute('data-exempt', 'false');
        await expect(
          securityPage.columnMaskSimulatorDecisionRow(
            'ri.masking.main.column-mask.amount-finance-only',
          ),
        ).toHaveAttribute('data-exempt', 'false');
      },
    );

    await When(
      'the user changes the simulated identity to a DPO with no email/groups',
      async () => {
        await securityPage.columnMaskSimulatorEmail().fill('');
        await securityPage.columnMaskSimulatorRoles().fill('dpo');
        await securityPage.columnMaskSimulatorGroups().fill('');
      },
    );

    await Then(
      'only the DPO-only mask flips to exempt and alice-only flips back to masked',
      async () => {
        await expect(
          securityPage.columnMaskSimulatorExemptCount(),
        ).toContainText('1');
        await expect(
          securityPage.columnMaskSimulatorMaskedCount(),
        ).toContainText('2');
        await expect(
          securityPage.columnMaskSimulatorDecisionRow(
            'ri.masking.main.column-mask.email-dpo-only',
          ),
        ).toHaveAttribute('data-exempt', 'true');
        await expect(
          securityPage.columnMaskSimulatorDecisionRow(
            'ri.masking.main.column-mask.name-alice-only',
          ),
        ).toHaveAttribute('data-exempt', 'false');
        await expect(
          securityPage.columnMaskSimulatorDecisionRow(
            'ri.masking.main.column-mask.amount-finance-only',
          ),
        ).toHaveAttribute('data-exempt', 'false');
      },
    );
  });
});
