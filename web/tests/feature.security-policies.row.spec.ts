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
 * BDD coverage of `/admin/:ontology/security` — the Security Policies UI
 * rendered by `src/components/securityPolicies/SecurityPoliciesPage.tsx`
 * (US-041, PC-A07a).
 *
 * Scenarios map to the US-041 acceptance criteria:
 *
 *   - "Admin 新增 Security Policies 页面壳子 + 三 tab 路由"
 *     → Scenario "shell renders three tabs and switches the active panel"
 *       boots the page and exercises Row / Column / Cell tab switching.
 *   - "Row Policies tab：列表、Create/Edit/Delete、CEL 编辑器（语法高亮 + lint）"
 *     → "list renders existing row policies …" locks the list contract;
 *       "create posts the wire-shape …" locks Create + JSON editor lint;
 *       "delete confirm dialog removes the row" locks Delete; "predicate
 *       editor rejects invalid JSON" locks the lint contract. The PRD AC
 *       says "CEL editor" but the backend wire format is in fact JSON
 *       (pkg/oss/where.WhereClause) — honest mapping per the US-025 /
 *       US-029 / US-033 patterns documented in progress.txt.
 *   - "Test as user 模拟器（选用户角色 → 显示 policy 决策）"
 *     → "test-as-user simulator marks applicable policies" locks the
 *       client-side AppliesTo evaluator. There is no backend simulator
 *       endpoint — IsApplicable is replicated client-side from
 *       pkg/rls/model.go:38 so the gesture is honest about its scope.
 *
 * Every scenario stubs the backend through `page.route` so the page
 * renders deterministic fixtures without touching the real PG / RLS
 * engine.
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
}

interface MockRowPolicy {
  rid: string;
  objectTypeRid: string;
  predicate: unknown;
  appliesTo: { roles?: string[]; groups?: string[]; users?: string[] };
  description?: string;
}

interface CapturedRequest {
  method: string;
  url: string;
  body: unknown;
}

interface Stubs {
  policies: MockRowPolicy[];
  objectTypes: MockObjectType[];
  posts: CapturedRequest[];
  patches: CapturedRequest[];
  deletes: CapturedRequest[];
  /**
   * When non-null, the next POST/PATCH/DELETE returns 500 with this
   * errorName so the failure scenario can lock the toast contract.
   * Cleared after one mutation.
   */
  failNextWith: string | null;
}

function newStubs(initial: Partial<Stubs> = {}): Stubs {
  return {
    policies: initial.policies ? initial.policies.map((p) => ({ ...p })) : [],
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
    rid: 'ri.ontology.main.objectType.product',
    apiName: 'product',
    displayName: 'Product',
    pluralDisplayName: 'Products',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    ...overrides,
  };
}

function rowPolicyFixture(overrides: Partial<MockRowPolicy> = {}): MockRowPolicy {
  return {
    rid: 'ri.rls.main.row-policy.policy-1',
    objectTypeRid: 'ri.ontology.main.objectType.product',
    predicate: { type: 'eq', field: 'status', value: 'active' },
    appliesTo: { roles: ['viewer'] },
    description: 'Viewers see only active products',
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
  // ObjectType list — used by the editor's "Object Type" select. The
  // catch-all matches the unfiltered list endpoint exactly; the RowPolicy
  // editor reads it to populate the dropdown.
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

  // US-042 / US-043: switching to the Column / Cell Masks tabs triggers
  // useColumnMasks() / useCellMasks() which would otherwise leak through
  // to the real backend during the row-policies shell scenario. Stub
  // empty lists so the sibling tabs boot into their empty state cleanly.
  // The column-masks / cell-masks specs own the mutation / list /
  // simulator contracts; row-spec only needs absence of cross-tab fetch
  // leakage.
  await page.route(
    '**/api/admin/column-masks*',
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ masks: [] }),
      });
    },
  );
  await page.route(
    '**/api/admin/cell-masks*',
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ masks: [] }),
      });
    },
  );

  // Row policies single-resource (PATCH / DELETE / GET /{rid}). Register
  // before the catch-all list so Playwright's LIFO dispatch picks the
  // single-resource handler for `/row-policies/{rid}` and the catch-all
  // for `/row-policies`. Method dispatch lives inside the handler.
  const POLICIES_PREFIX = `**/api/admin/row-policies`;

  await page.route(`${POLICIES_PREFIX}/*`, async (route: Route) => {
    const req = route.request();
    const method = req.method();
    const url = req.url();
    const m = url.split('?')[0].match(/\/row-policies\/([^/?#]+)$/);
    const policyRid = m?.[1] ?? '';

    if (method === 'PATCH') {
      const body = (req.postDataJSON() ?? {}) as Partial<MockRowPolicy>;
      stubs.patches.push({ method, url, body });
      if (stubs.failNextWith) {
        const name = stubs.failNextWith;
        stubs.failNextWith = null;
        await route.fulfill(failWith(name, 'Synthetic update failure'));
        return;
      }
      const idx = stubs.policies.findIndex((p) => p.rid === policyRid);
      if (idx === -1) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'RowPolicyNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: policyRid },
          }),
        });
        return;
      }
      stubs.policies[idx] = {
        ...stubs.policies[idx],
        ...(body.predicate !== undefined ? { predicate: body.predicate } : {}),
        ...(body.appliesTo ? { appliesTo: body.appliesTo } : {}),
        ...(body.description !== undefined
          ? { description: body.description }
          : {}),
      };
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(stubs.policies[idx]),
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
      const idx = stubs.policies.findIndex((p) => p.rid === policyRid);
      if (idx === -1) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'RowPolicyNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: policyRid },
          }),
        });
        return;
      }
      stubs.policies.splice(idx, 1);
      await route.fulfill({ status: 204, body: '' });
      return;
    }

    if (method === 'GET') {
      const found = stubs.policies.find((p) => p.rid === policyRid);
      if (!found) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'RowPolicyNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: policyRid },
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

  // Row policies list (GET) + create (POST). The Playwright glob `*`
  // does not cross `/`, so this catch-all matches `/row-policies` and
  // `/row-policies?objectType=...` but NOT `/row-policies/{rid}`.
  await page.route(`${POLICIES_PREFIX}*`, async (route: Route) => {
    const req = route.request();
    const method = req.method();
    const url = req.url();

    if (method === 'GET') {
      const u = new URL(url);
      const objectTypeFilter = u.searchParams.get('objectType');
      const data = objectTypeFilter
        ? stubs.policies.filter((p) => p.objectTypeRid === objectTypeFilter)
        : stubs.policies;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ policies: data }),
      });
      return;
    }

    if (method === 'POST') {
      const body = (req.postDataJSON() ?? {}) as MockRowPolicy;
      stubs.posts.push({ method, url, body });
      if (stubs.failNextWith) {
        const name = stubs.failNextWith;
        stubs.failNextWith = null;
        await route.fulfill(failWith(name, 'Synthetic create failure'));
        return;
      }
      const created: MockRowPolicy = {
        rid: `ri.rls.main.row-policy.created-${stubs.posts.length}`,
        objectTypeRid: body.objectTypeRid,
        predicate: body.predicate,
        appliesTo: body.appliesTo ?? {},
        description: body.description,
      };
      stubs.policies.push(created);
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

describeFeature('Security Policies — Row Policies tab', () => {
  test('Scenario: shell renders three tabs and switches the active panel @smoke', async ({
    page,
    request,
  }) => {
    // AC: "Admin 新增 Security Policies 页面壳子 + 三 tab 路由". Boot the
    // page, assert all three tabs render, switch to Column Masks and Cell
    // Masks, and lock the per-tab placeholder content (US-042 / US-043
    // own the column / cell implementations).
    const stubs = newStubs({
      objectTypes: [objectTypeFixture()],
      policies: [],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('row policies are stubbed (empty list)', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens /admin/northwind/security', async () => {
      await securityPage.goto(ONTOLOGY);
      await expect(securityPage.root).toBeVisible();
    });

    await Then('the page renders three tabs (row / column / cell)', async () => {
      await expect(securityPage.tabs).toBeVisible();
      await expect(securityPage.tabBtn('row')).toBeVisible();
      await expect(securityPage.tabBtn('column')).toBeVisible();
      await expect(securityPage.tabBtn('cell')).toBeVisible();
    });

    await Then('the Row Policies tab is active by default', async () => {
      await expect(securityPage.tabBtn('row')).toHaveAttribute(
        'data-active',
        'true',
      );
      await expect(securityPage.tabPanel).toHaveAttribute(
        'data-active-tab',
        'row',
      );
      await expect(securityPage.rowPoliciesTab).toBeVisible();
    });

    await When('the user clicks the Column Masks tab', async () => {
      await securityPage.tabBtn('column').click();
    });

    await Then(
      'the Column Masks tab renders (US-042) and the panel switches',
      async () => {
        // US-042 ships the Column Masks tab. The shell scenario still
        // exercises tab navigation but now the panel renders the live
        // ColumnMasksTab (empty state for the unconfigured stub) rather
        // than the US-041 placeholder. The next scenario (Cell Masks)
        // still hits the US-043 placeholder.
        await expect(securityPage.columnMasksTab).toBeVisible();
        await expect(securityPage.tabPanel).toHaveAttribute(
          'data-active-tab',
          'column',
        );
      },
    );

    await When('the user clicks the Cell Masks tab', async () => {
      await securityPage.tabBtn('cell').click();
    });

    await Then(
      'the Cell Masks tab renders (US-043) and the panel switches',
      async () => {
        // US-043 ships the Cell Masks (CEL) tab. The shell scenario now
        // exercises live tab navigation across all three panes; the
        // empty-state stub keeps the row-spec hermetic without taking on
        // the cell-masks spec contract. Per-tab contracts live in
        // feature.security-policies.cell.spec.ts.
        await expect(securityPage.cellMasksTab).toBeVisible();
        await expect(securityPage.tabPanel).toHaveAttribute(
          'data-active-tab',
          'cell',
        );
      },
    );
  });

  test('Scenario: list renders existing row policies with object-type and applies-to badges', async ({
    page,
  }) => {
    // AC: "Row Policies tab：列表 …". Seed two policies and lock the
    // per-row contract: object-type apiName resolved from the rid map,
    // and AppliesTo badges per kind (role / group / user).
    const productOT = objectTypeFixture();
    const orderOT = objectTypeFixture({
      rid: 'ri.ontology.main.objectType.order',
      apiName: 'order',
      displayName: 'Order',
    });
    const stubs = newStubs({
      objectTypes: [productOT, orderOT],
      policies: [
        rowPolicyFixture({
          rid: 'ri.rls.main.row-policy.product-viewer',
          objectTypeRid: productOT.rid,
          appliesTo: { roles: ['viewer'] },
          description: 'Viewers see only active products',
        }),
        rowPolicyFixture({
          rid: 'ri.rls.main.row-policy.order-finance',
          objectTypeRid: orderOT.rid,
          predicate: { type: 'eq', field: 'region', value: 'EU' },
          appliesTo: { groups: ['finance'], users: ['alice@test'] },
          description: 'Finance + Alice see EU orders',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('two row policies exist', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page', async () => {
      await securityPage.goto(ONTOLOGY);
      await expect(securityPage.root).toBeVisible();
    });

    await Then('both rows render with their object types resolved', async () => {
      await expect(securityPage.list).toBeVisible();
      const productRow = securityPage.rowByPolicyRid(
        'ri.rls.main.row-policy.product-viewer',
      );
      const orderRow = securityPage.rowByPolicyRid(
        'ri.rls.main.row-policy.order-finance',
      );
      await expect(productRow).toBeVisible();
      await expect(orderRow).toBeVisible();
      await expect(productRow).toHaveAttribute(
        'data-object-type-api-name',
        'product',
      );
      await expect(orderRow).toHaveAttribute(
        'data-object-type-api-name',
        'order',
      );
    });

    await Then(
      'each row renders the AppliesTo badges (role / group / user)',
      async () => {
        const productRow = securityPage.rowByPolicyRid(
          'ri.rls.main.row-policy.product-viewer',
        );
        const orderRow = securityPage.rowByPolicyRid(
          'ri.rls.main.row-policy.order-finance',
        );
        await expect(
          productRow.locator(
            '[data-testid="row-policies-applies-badge"][data-kind="role"][data-value="viewer"]',
          ),
        ).toBeVisible();
        await expect(
          orderRow.locator(
            '[data-testid="row-policies-applies-badge"][data-kind="group"][data-value="finance"]',
          ),
        ).toBeVisible();
        await expect(
          orderRow.locator(
            '[data-testid="row-policies-applies-badge"][data-kind="user"][data-value="alice@test"]',
          ),
        ).toBeVisible();
      },
    );
  });

  test('Scenario: create posts the wire-shape and lints invalid predicate JSON @smoke', async ({
    page,
  }) => {
    // AC: "Create … CEL 编辑器（语法高亮 + lint）" — honest mapping: the
    // wire format is JSON (pkg/oss/where.WhereClause), so the lint
    // contract is "invalid JSON disables Submit + surfaces a parse error
    // inline; valid JSON shape sends as POST body verbatim."
    const productOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [productOT],
      policies: [],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the row policy list is empty', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page and clicks Create row policy', async () => {
      await securityPage.goto(ONTOLOGY);
      await expect(securityPage.emptyState).toBeVisible();
      await securityPage.createBtn.click();
      await expect(securityPage.editor).toBeVisible();
    });

    await When(
      'the user types invalid JSON into the predicate editor',
      async () => {
        await securityPage.editorPredicate().fill('{not valid');
      },
    );

    await Then(
      'the lint surfaces a parse error and Submit stays disabled',
      async () => {
        await expect(securityPage.editorPredicateError()).toBeVisible();
        await expect(securityPage.editorPredicateError()).toContainText(
          /Invalid JSON/i,
        );
        await expect(securityPage.editorSubmitBtn()).toBeDisabled();
      },
    );

    await When(
      'the user replaces the predicate with a valid where-clause and fills the form',
      async () => {
        await securityPage.editorPredicate().fill(
          '{"type":"eq","field":"status","value":"active"}',
        );
        await expect(securityPage.editorPredicateOk()).toBeVisible();
        await securityPage.editorRoles().fill('viewer, editor');
        await securityPage.editorUsers().fill('alice@test');
        await securityPage.editorDescription().fill('viewers + editors + alice');
      },
    );

    await Then('Submit is now enabled', async () => {
      await expect(securityPage.editorSubmitBtn()).toBeEnabled();
    });

    await When('the user submits the form', async () => {
      await securityPage.editorSubmitBtn().click();
    });

    await Then('a POST to /api/admin/row-policies with the wire shape is captured', async () => {
      await expect.poll(() => stubs.posts.length).toBeGreaterThanOrEqual(1);
      const last = stubs.posts.at(-1)!;
      expect(last.method).toBe('POST');
      expect(last.url).toMatch(/\/api\/admin\/row-policies(\?|$)/);
      // Wire-shape contract: predicate verbatim, AppliesTo built from
      // comma-separated lists, empty groups omitted as []. The PRD AC
      // says "CEL editor" but the wire format is the JSON
      // pkg/oss/where.WhereClause — locking the body shape doubles as the
      // honest-mapping enforcement.
      expect(last.body).toMatchObject({
        objectTypeRid: 'ri.ontology.main.objectType.product',
        predicate: { type: 'eq', field: 'status', value: 'active' },
        appliesTo: {
          roles: ['viewer', 'editor'],
          users: ['alice@test'],
        },
        description: 'viewers + editors + alice',
      });
      // Negative assertion: the JSON editor sends the actual WhereClause,
      // NOT a `cel` string field. This locks the honest-mapping contract
      // — future CEL migration would need to add the field deliberately.
      const body = last.body as Record<string, unknown>;
      expect(body.cel).toBeUndefined();
      expect(body.celExpression).toBeUndefined();
    });

    await Then(
      'the editor closes and the new row appears after invalidation',
      async () => {
        await expect(securityPage.editor).toHaveCount(0);
        await expect(securityPage.list).toBeVisible();
        // The stub generates an rid like ri.rls.main.row-policy.created-1.
        await expect(
          securityPage.rowByPolicyRid('ri.rls.main.row-policy.created-1'),
        ).toBeVisible();
      },
    );
  });

  test('Scenario: delete confirm dialog removes the row and DELETE is captured', async ({
    page,
  }) => {
    // AC: "… Delete …". The Delete button opens a confirm dialog; clicking
    // Confirm fires DELETE /api/admin/row-policies/{rid} and the row
    // disappears after invalidation.
    const stubs = newStubs({
      objectTypes: [objectTypeFixture()],
      policies: [
        rowPolicyFixture({
          rid: 'ri.rls.main.row-policy.delete-me',
          description: 'Doomed policy',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('one row policy exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page', async () => {
      await securityPage.goto(ONTOLOGY);
      await expect(
        securityPage.rowByPolicyRid('ri.rls.main.row-policy.delete-me'),
      ).toBeVisible();
    });

    await When('the user clicks Delete on the row', async () => {
      await securityPage.deleteButton('ri.rls.main.row-policy.delete-me').click();
      await expect(securityPage.deleteDialog).toBeVisible();
    });

    await When('the user confirms the deletion', async () => {
      await securityPage.deleteConfirmBtn().click();
    });

    await Then('a DELETE to the policy endpoint is captured', async () => {
      await expect.poll(() => stubs.deletes.length).toBeGreaterThanOrEqual(1);
      const last = stubs.deletes.at(-1)!;
      expect(last.method).toBe('DELETE');
      expect(last.url).toMatch(
        /\/api\/admin\/row-policies\/ri\.rls\.main\.row-policy\.delete-me$/,
      );
    });

    await Then(
      'the dialog closes and the list flips to the empty state',
      async () => {
        await expect(securityPage.deleteDialog).toHaveCount(0);
        await expect(securityPage.emptyState).toBeVisible();
        await expect(
          securityPage.rowByPolicyRid('ri.rls.main.row-policy.delete-me'),
        ).toHaveCount(0);
      },
    );
  });

  test('Scenario: test-as-user simulator marks applicable policies for the simulated user', async ({
    page,
  }) => {
    // AC: "Test as user 模拟器（选用户角色 → 显示 policy 决策）".
    //
    // Honest mapping: the backend has no per-user simulator endpoint
    // (Engine.Compile is internal and does not surface per-policy hits).
    // The simulator replicates pkg/rls AppliesTo.IsApplicable
    // client-side. This scenario locks: (a) the simulator panel opens,
    // (b) editing the simulated user's roles flips per-policy decision
    // badges, (c) match-count reflects the union of role / group / user
    // hits — matching the IsApplicable contract.
    const productOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [productOT],
      policies: [
        rowPolicyFixture({
          rid: 'ri.rls.main.row-policy.viewer-only',
          appliesTo: { roles: ['viewer'] },
          description: 'viewer-only',
        }),
        rowPolicyFixture({
          rid: 'ri.rls.main.row-policy.alice-only',
          appliesTo: { users: ['alice@test'] },
          description: 'alice-only',
        }),
        rowPolicyFixture({
          rid: 'ri.rls.main.row-policy.finance-only',
          appliesTo: { groups: ['finance'] },
          description: 'finance-only',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('three policies exist with different applies-to scopes', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the simulator', async () => {
      await securityPage.goto(ONTOLOGY);
      await expect(securityPage.list).toBeVisible();
      await securityPage.simulatorToggleBtn.click();
      await expect(securityPage.simulator).toBeVisible();
    });

    await Then(
      'the default simulated user (viewer / alice@test) matches viewer-only and alice-only',
      async () => {
        await expect(securityPage.simulatorMatchCount()).toContainText('2');
        await expect(
          securityPage.simulatorDecisionRow('ri.rls.main.row-policy.viewer-only'),
        ).toHaveAttribute('data-applies', 'true');
        await expect(
          securityPage.simulatorDecisionRow('ri.rls.main.row-policy.alice-only'),
        ).toHaveAttribute('data-applies', 'true');
        await expect(
          securityPage.simulatorDecisionRow(
            'ri.rls.main.row-policy.finance-only',
          ),
        ).toHaveAttribute('data-applies', 'false');
      },
    );

    await When(
      'the user clears the simulated email and roles and adds the finance group',
      async () => {
        await securityPage.simulatorEmail().fill('');
        await securityPage.simulatorRoles().fill('');
        await securityPage.simulatorGroups().fill('finance');
      },
    );

    await Then(
      'only finance-only matches and viewer-only / alice-only flip to no-match',
      async () => {
        await expect(securityPage.simulatorMatchCount()).toContainText('1');
        await expect(
          securityPage.simulatorDecisionRow(
            'ri.rls.main.row-policy.finance-only',
          ),
        ).toHaveAttribute('data-applies', 'true');
        await expect(
          securityPage.simulatorDecisionRow('ri.rls.main.row-policy.viewer-only'),
        ).toHaveAttribute('data-applies', 'false');
        await expect(
          securityPage.simulatorDecisionRow('ri.rls.main.row-policy.alice-only'),
        ).toHaveAttribute('data-applies', 'false');
      },
    );
  });
});
