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
 * BDD coverage of `/admin/:ontology/security` Cell Masks (CEL) tab — the
 * third pane of the Security Policies UI rendered by
 * `src/components/securityPolicies/SecurityPoliciesPage.tsx` (US-043,
 * PC-A07c).
 *
 * Scenarios map to the US-043 acceptance criteria:
 *
 *   - "Cell Masks (CEL) tab：列出 mask 规则、CEL 编辑器、实时 lint"
 *     → "list renders existing cell masks with strategy / trigger" locks
 *       the per-row contract; "lint flags malformed CEL while empty
 *       expression falls back to allow list" locks the editor's lint
 *       contract.
 *   - "Create/Edit/Delete；预览 mask 结果"
 *     → "create posts the wire shape with expression + strategy" locks
 *       Create (incl. the preview output rendering); "delete confirm
 *       dialog removes the row" locks Delete.
 *   - "Test as user 模拟器联动"
 *     → "simulator distinguishes server-side CEL masks from allow-list
 *       masks" locks the dual-mode simulator contract — CEL-bearing
 *       masks are surfaced as 'server-side' while allow-list masks reuse
 *       the column-mask exempt/masked semantics.
 *
 * Honest-mapping callouts:
 *   - CEL evaluation needs cel-go + the row's properties. The
 *     simulator panel cannot reproduce that client-side, so it labels
 *     expression-bearing masks as 'server-side' rather than guessing a
 *     verdict. The legacy AppliesTo allow-list path matches column-mask
 *     semantics (matching identities are exempt).
 *   - The PRD says "实时 lint" — the client-side lint is structural
 *     (balanced parens/brackets/quotes, no trailing operator). The
 *     authoritative CEL parse runs server-side via
 *     pkg/cellsec/celmask.Validate; spec scenarios lock the structural
 *     fast-fail without claiming full CEL coverage.
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

interface MockCellMask {
  rid: string;
  objectTypeRid: string;
  primaryKey: string;
  propertyApiName: string;
  maskRule?: 'hash' | 'redact' | 'partial';
  maskStrategy?: 'REDACT' | 'HASH' | 'NULL' | 'PARTIAL';
  expression?: string;
  appliesTo: { roles?: string[]; groups?: string[]; users?: string[] };
  description?: string;
}

interface CapturedRequest {
  method: string;
  url: string;
  body: unknown;
}

interface Stubs {
  masks: MockCellMask[];
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
      country: {
        dataType: { type: 'string' },
        rid: 'ri.ontology.main.property.country',
      },
    },
    ...overrides,
  };
}

function cellMaskFixture(overrides: Partial<MockCellMask> = {}): MockCellMask {
  return {
    rid: 'ri.cellsec.main.cell-mask.mask-1',
    objectTypeRid: 'ri.ontology.main.objectType.customer',
    primaryKey: 'CUST-001',
    propertyApiName: 'email',
    maskStrategy: 'REDACT',
    expression: '"PII" in user.markings',
    appliesTo: {},
    description: 'Redact email when caller has PII marking',
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

  // Sibling tabs (row-policies / column-masks) stay empty so the shell
  // panel can render without leaking to the real backend if a scenario
  // accidentally toggles tabs.
  await page.route('**/api/admin/row-policies*', async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ policies: [] }),
    });
  });
  await page.route('**/api/admin/column-masks*', async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ masks: [] }),
    });
  });

  // Cell-masks single-resource (PATCH / DELETE / GET /{rid}). Register
  // before the catch-all list per US-040 / US-042 template — Playwright
  // glob `*` does not cross `/`, so this handler matches
  // `/cell-masks/{rid}` and the catch-all owns `/cell-masks` +
  // `/cell-masks?objectType=...`.
  const PREFIX = '**/api/admin/cell-masks';

  await page.route(`${PREFIX}/*`, async (route: Route) => {
    const req = route.request();
    const method = req.method();
    const url = req.url();
    const m = url.split('?')[0].match(/\/cell-masks\/([^/?#]+)$/);
    const maskRid = m?.[1] ?? '';

    if (method === 'PATCH') {
      const body = (req.postDataJSON() ?? {}) as Partial<MockCellMask>;
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
            errorName: 'CellMaskNotFound',
            errorInstanceId: 'spec',
            parameters: { rid: maskRid },
          }),
        });
        return;
      }
      stubs.masks[idx] = {
        ...stubs.masks[idx],
        ...(body.maskRule ? { maskRule: body.maskRule } : {}),
        ...(body.maskStrategy ? { maskStrategy: body.maskStrategy } : {}),
        ...(body.expression !== undefined
          ? { expression: body.expression }
          : {}),
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
            errorName: 'CellMaskNotFound',
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
            errorName: 'CellMaskNotFound',
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

  // List (GET) + Create (POST).
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
      const body = (req.postDataJSON() ?? {}) as MockCellMask;
      stubs.posts.push({ method, url, body });
      if (stubs.failNextWith) {
        const name = stubs.failNextWith;
        stubs.failNextWith = null;
        await route.fulfill(failWith(name, 'Synthetic create failure'));
        return;
      }
      const created: MockCellMask = {
        rid: `ri.cellsec.main.cell-mask.created-${stubs.posts.length}`,
        objectTypeRid: body.objectTypeRid,
        primaryKey: body.primaryKey,
        propertyApiName: body.propertyApiName,
        maskRule: body.maskRule,
        maskStrategy: body.maskStrategy,
        expression: body.expression,
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

async function openCellMasksTab(securityPage: SecurityPoliciesPage) {
  await securityPage.goto(ONTOLOGY);
  await expect(securityPage.root).toBeVisible();
  await securityPage.tabBtn('cell').click();
  await expect(securityPage.cellMasksTab).toBeVisible();
}

describeFeature('Security Policies — Cell Masks (CEL) tab', () => {
  test('Scenario: list renders existing cell masks with strategy badge and trigger code @smoke', async ({
    page,
    request,
  }) => {
    // AC: "Cell Masks (CEL) tab：列出 mask 规则". Seed two masks — one
    // with a CEL expression, one with allow-list only — and lock the
    // per-row contract: resolved ObjectType apiName, primary key,
    // property apiName, mask-strategy badge data attribute, and the
    // "trigger" column rendering (expression code vs AppliesTo badges).
    const customerOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [customerOT],
      masks: [
        cellMaskFixture({
          rid: 'ri.cellsec.main.cell-mask.cel-pii',
          primaryKey: 'CUST-001',
          propertyApiName: 'email',
          maskStrategy: 'REDACT',
          expression: '"PII" in user.markings',
          appliesTo: {},
          description: 'Redact email for PII markings',
        }),
        cellMaskFixture({
          rid: 'ri.cellsec.main.cell-mask.allow-list-only',
          primaryKey: 'CUST-002',
          propertyApiName: 'country',
          maskStrategy: 'HASH',
          expression: '',
          appliesTo: { roles: ['dpo'] },
          description: 'DPO sees country; everyone else gets hash',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('two cell masks exist', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the Cell Masks tab', async () => {
      await openCellMasksTab(securityPage);
    });

    await Then(
      'both rows render with their object type, primary key, property and strategy',
      async () => {
        await expect(securityPage.cellMasksList).toBeVisible();
        const celRow = securityPage.cellMaskRowByRid(
          'ri.cellsec.main.cell-mask.cel-pii',
        );
        const allowRow = securityPage.cellMaskRowByRid(
          'ri.cellsec.main.cell-mask.allow-list-only',
        );
        await expect(celRow).toBeVisible();
        await expect(allowRow).toBeVisible();
        await expect(celRow).toHaveAttribute(
          'data-object-type-api-name',
          'customer',
        );
        await expect(celRow).toHaveAttribute('data-primary-key', 'CUST-001');
        await expect(celRow).toHaveAttribute(
          'data-property-api-name',
          'email',
        );
        await expect(celRow).toHaveAttribute('data-mask-strategy', 'REDACT');
        await expect(celRow).toHaveAttribute('data-has-expression', 'true');
        await expect(allowRow).toHaveAttribute(
          'data-primary-key',
          'CUST-002',
        );
        await expect(allowRow).toHaveAttribute('data-mask-strategy', 'HASH');
        await expect(allowRow).toHaveAttribute(
          'data-has-expression',
          'false',
        );
      },
    );

    await Then(
      'the CEL row renders its expression in the trigger column while the allow-list row renders AppliesTo badges',
      async () => {
        const celRow = securityPage.cellMaskRowByRid(
          'ri.cellsec.main.cell-mask.cel-pii',
        );
        const allowRow = securityPage.cellMaskRowByRid(
          'ri.cellsec.main.cell-mask.allow-list-only',
        );
        await expect(
          celRow.locator('[data-testid="cell-masks-expression"]'),
        ).toContainText('user.markings');
        await expect(
          allowRow.locator(
            '[data-testid="column-masks-applies-badge"][data-kind="role"][data-value="dpo"]',
          ),
        ).toBeVisible();
      },
    );
  });

  test('Scenario: create posts the wire shape with maskStrategy, expression and previews the masked output @smoke', async ({
    page,
  }) => {
    // AC: "CEL 编辑器、实时 lint" + "Create/Edit/Delete；预览 mask 结果".
    // Lock the create-mutation wire-shape (objectTypeRid + primaryKey +
    // propertyApiName + maskStrategy + expression + appliesTo) and the
    // editor's preview output rendering. Honest mapping with PRD wording
    // — backend wire field is `maskStrategy` (uppercase US-376 taxonomy)
    // / `expression`; PRD describes the surface as "CEL editor". Spec
    // negatively asserts a few field names that would breach the wire
    // contract if a future refactor renamed them.
    const customerOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [customerOT],
      masks: [],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the cell mask list is empty', async () => {
      await stubEndpoints(page, stubs);
    });

    await When(
      'the user opens the Cell Masks tab and clicks Create cell mask',
      async () => {
        await openCellMasksTab(securityPage);
        await expect(securityPage.cellMasksEmptyState).toBeVisible();
        await securityPage.cellMasksCreateBtn.click();
        await expect(securityPage.cellMaskEditor).toBeVisible();
      },
    );

    await When(
      'the user fills the cell coordinates and a CEL expression',
      async () => {
        // Property dropdown is populated by /objectTypes/{apiName} — wait
        // for the options to settle before selecting.
        await expect.poll(async () =>
          (await securityPage
            .cellMaskEditorPropertyInput()
            .evaluate((el: Element) => {
              const sel = el as HTMLSelectElement;
              return sel.tagName === 'SELECT' ? sel.options.length : -1;
            })),
        ).toBeGreaterThan(1);
        await securityPage
          .cellMaskEditorPrimaryKey()
          .fill('CUST-007');
        await securityPage
          .cellMaskEditorPropertyInput()
          .selectOption('email');
        await securityPage
          .cellMaskEditorStrategySelect()
          .selectOption('HASH');
        await securityPage
          .cellMaskEditorExpression()
          .fill('"PII" in user.markings || row.country == "CN"');
        await securityPage
          .cellMaskEditorDescription()
          .fill('Hash email for PII callers or CN rows');
      },
    );

    await Then(
      'the editor reports the CEL expression is structurally valid and previews a HASH output',
      async () => {
        await expect(
          securityPage.cellMaskEditorExpressionOk(),
        ).toBeVisible();
        // Preview output: HASH strategy → sha256-prefixed digest. We
        // don't lock the exact hex (the simulator is deterministic but
        // not byte-stable across refactors); we lock the strategy
        // prefix so the contract "preview reflects the strategy" stays
        // observable.
        await expect(securityPage.cellMaskEditorPreviewOutput()).toContainText(
          'sha256:',
        );
        await expect(securityPage.cellMaskEditorSubmitBtn()).toBeEnabled();
      },
    );

    await When('the user submits the form', async () => {
      await securityPage.cellMaskEditorSubmitBtn().click();
    });

    await Then(
      'a POST to /api/admin/cell-masks with the wire shape is captured',
      async () => {
        await expect.poll(() => stubs.posts.length).toBeGreaterThanOrEqual(1);
        const last = stubs.posts.at(-1)!;
        expect(last.method).toBe('POST');
        expect(last.url).toMatch(/\/api\/admin\/cell-masks(\?|$)/);
        expect(last.body).toMatchObject({
          objectTypeRid: customerOT.rid,
          primaryKey: 'CUST-007',
          propertyApiName: 'email',
          maskStrategy: 'HASH',
          expression: '"PII" in user.markings || row.country == "CN"',
          description: 'Hash email for PII callers or CN rows',
        });
        // Negative assertions: backend wire field is `expression`, NOT
        // `cel`, `script`, `predicate`, or `condition`. Strategy uses
        // the uppercase taxonomy — `maskRule` may also be set in legacy
        // payloads but this UI uses MaskStrategy as the canonical write.
        const body = last.body as Record<string, unknown>;
        expect(body.cel).toBeUndefined();
        expect(body.celExpression).toBeUndefined();
        expect(body.script).toBeUndefined();
        expect(body.predicate).toBeUndefined();
        expect(body.condition).toBeUndefined();
        expect(body.objectType).toBeUndefined();
        expect(body.objectTypeRid).toMatch(/^ri\.ontology\./);
      },
    );

    await Then(
      'the editor closes and the new row appears with the strategy badge',
      async () => {
        await expect(securityPage.cellMaskEditor).toHaveCount(0);
        await expect(securityPage.cellMasksList).toBeVisible();
        const created = securityPage.cellMaskRowByRid(
          'ri.cellsec.main.cell-mask.created-1',
        );
        await expect(created).toBeVisible();
        await expect(created).toHaveAttribute('data-mask-strategy', 'HASH');
        await expect(created).toHaveAttribute('data-has-expression', 'true');
      },
    );
  });

  test('Scenario: lint flags malformed CEL while empty expression falls back to AppliesTo allow list', async ({
    page,
  }) => {
    // AC: "实时 lint" — the CEL editor surfaces structural problems
    // before the server-side parser sees them. This scenario locks two
    // flips: (a) an unbalanced-paren expression disables Submit and
    // shows an error tag; (b) clearing the expression switches the
    // editor into the empty-state hint ("falls back to AppliesTo allow
    // list"), keeping Submit enabled because allow-list-only masks are
    // a valid wire shape.
    const customerOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [customerOT],
      masks: [],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('the cell mask list is empty', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the Cell Masks tab and the editor', async () => {
      await openCellMasksTab(securityPage);
      await securityPage.cellMasksCreateBtn.click();
      await expect(securityPage.cellMaskEditor).toBeVisible();
      await expect.poll(async () =>
        (await securityPage
          .cellMaskEditorPropertyInput()
          .evaluate((el: Element) => {
            const sel = el as HTMLSelectElement;
            return sel.tagName === 'SELECT' ? sel.options.length : -1;
          })),
      ).toBeGreaterThan(1);
      await securityPage.cellMaskEditorPrimaryKey().fill('CUST-007');
      await securityPage.cellMaskEditorPropertyInput().selectOption('email');
    });

    await When(
      'the user types an expression with unbalanced parentheses',
      async () => {
        await securityPage
          .cellMaskEditorExpression()
          .fill('("PII" in user.markings');
      },
    );

    await Then(
      'the lint surfaces an error tag and Submit is disabled',
      async () => {
        await expect(
          securityPage.cellMaskEditorExpressionError(),
        ).toBeVisible();
        await expect(
          securityPage.cellMaskEditorExpressionError(),
        ).toContainText(/unbalanced/i);
        await expect(
          securityPage.cellMaskEditorSubmitBtn(),
        ).toBeDisabled();
      },
    );

    await When('the user clears the expression', async () => {
      await securityPage.cellMaskEditorExpression().fill('');
    });

    await Then(
      'the editor switches to the empty-expression hint and Submit becomes enabled (allow-list fallback)',
      async () => {
        await expect(
          securityPage.cellMaskEditorExpressionEmpty(),
        ).toBeVisible();
        await expect(
          securityPage.cellMaskEditorExpressionEmpty(),
        ).toContainText(/allow list/i);
        await expect(
          securityPage.cellMaskEditorSubmitBtn(),
        ).toBeEnabled();
      },
    );
  });

  test('Scenario: delete confirm dialog removes the row and DELETE is captured', async ({
    page,
  }) => {
    // AC: "… Delete …". The Delete button opens a confirm dialog;
    // clicking Confirm fires DELETE /api/admin/cell-masks/{rid} and the
    // row disappears after invalidation.
    const stubs = newStubs({
      objectTypes: [objectTypeFixture()],
      masks: [
        cellMaskFixture({
          rid: 'ri.cellsec.main.cell-mask.delete-me',
          description: 'Doomed cell mask',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given('one cell mask exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the Cell Masks tab', async () => {
      await openCellMasksTab(securityPage);
      await expect(
        securityPage.cellMaskRowByRid(
          'ri.cellsec.main.cell-mask.delete-me',
        ),
      ).toBeVisible();
    });

    await When('the user clicks Delete on the row', async () => {
      await securityPage
        .cellMaskDeleteButton('ri.cellsec.main.cell-mask.delete-me')
        .click();
      await expect(securityPage.cellMaskDeleteDialog).toBeVisible();
    });

    await When('the user confirms the deletion', async () => {
      await securityPage.cellMaskDeleteConfirmBtn().click();
    });

    await Then('a DELETE to the cell mask endpoint is captured', async () => {
      await expect.poll(() => stubs.deletes.length).toBeGreaterThanOrEqual(1);
      const last = stubs.deletes.at(-1)!;
      expect(last.method).toBe('DELETE');
      expect(last.url).toMatch(
        /\/api\/admin\/cell-masks\/ri\.cellsec\.main\.cell-mask\.delete-me$/,
      );
    });

    await Then(
      'the dialog closes and the list flips to the empty state',
      async () => {
        await expect(securityPage.cellMaskDeleteDialog).toHaveCount(0);
        await expect(securityPage.cellMasksEmptyState).toBeVisible();
        await expect(
          securityPage.cellMaskRowByRid(
            'ri.cellsec.main.cell-mask.delete-me',
          ),
        ).toHaveCount(0);
      },
    );
  });

  test('Scenario: simulator distinguishes server-side CEL masks from allow-list masks', async ({
    page,
  }) => {
    // AC: "Test as user 模拟器联动".
    //
    // Honest mapping: CEL evaluation needs cel-go + the row's
    // properties so the simulator cannot reproduce verdicts for
    // expression-bearing masks client-side. Spec locks the dual-mode
    // surface: expression-bearing masks render as "server-side"
    // decisions; AppliesTo-only masks reuse the column-mask exempt /
    // masked semantics. This guards against silent regressions that
    // would either (a) hide expression-bearing masks from the
    // simulator entirely or (b) claim a verdict the UI cannot
    // authoritatively make.
    const customerOT = objectTypeFixture();
    const stubs = newStubs({
      objectTypes: [customerOT],
      masks: [
        cellMaskFixture({
          rid: 'ri.cellsec.main.cell-mask.cel',
          propertyApiName: 'email',
          expression: 'row.country == "CN"',
          appliesTo: {},
          description: 'Server-side CEL',
        }),
        cellMaskFixture({
          rid: 'ri.cellsec.main.cell-mask.alice-only',
          propertyApiName: 'country',
          maskStrategy: 'REDACT',
          expression: '',
          appliesTo: { users: ['alice@test'] },
          description: 'alice-only allow list',
        }),
        cellMaskFixture({
          rid: 'ri.cellsec.main.cell-mask.dpo-only',
          propertyApiName: 'address',
          maskStrategy: 'PARTIAL',
          expression: '',
          appliesTo: { roles: ['dpo'] },
          description: 'dpo-only allow list',
        }),
      ],
    });
    const securityPage = new SecurityPoliciesPage(page);

    await Given(
      'three cell masks exist (one CEL, two allow-list)',
      async () => {
        await stubEndpoints(page, stubs);
      },
    );

    await When('the user opens the cell-mask simulator', async () => {
      await openCellMasksTab(securityPage);
      await expect(securityPage.cellMasksList).toBeVisible();
      await securityPage.cellMasksSimulatorToggleBtn.click();
      await expect(securityPage.cellMasksSimulator).toBeVisible();
    });

    await Then(
      'the default simulated user (viewer / alice@test) is exempt from alice-only and masked elsewhere; the CEL mask stays server-side',
      async () => {
        await expect(
          securityPage.cellMaskSimulatorExemptCount(),
        ).toContainText('1');
        await expect(
          securityPage.cellMaskSimulatorMaskedCount(),
        ).toContainText('1');
        await expect(
          securityPage.cellMaskSimulatorServerSideCount(),
        ).toContainText('1');
        await expect(
          securityPage.cellMaskSimulatorDecisionRow(
            'ri.cellsec.main.cell-mask.cel',
          ),
        ).toHaveAttribute('data-decision-kind', 'server-side');
        await expect(
          securityPage.cellMaskSimulatorDecisionRow(
            'ri.cellsec.main.cell-mask.alice-only',
          ),
        ).toHaveAttribute('data-exempt', 'true');
        await expect(
          securityPage.cellMaskSimulatorDecisionRow(
            'ri.cellsec.main.cell-mask.dpo-only',
          ),
        ).toHaveAttribute('data-exempt', 'false');
      },
    );

    await When(
      'the user switches the simulated identity to a DPO with no email/groups',
      async () => {
        await securityPage.cellMaskSimulatorEmail().fill('');
        await securityPage.cellMaskSimulatorRoles().fill('dpo');
        await securityPage.cellMaskSimulatorGroups().fill('');
      },
    );

    await Then(
      'only the dpo-only mask flips to exempt and the CEL mask is still surfaced as server-side',
      async () => {
        await expect(
          securityPage.cellMaskSimulatorExemptCount(),
        ).toContainText('1');
        await expect(
          securityPage.cellMaskSimulatorMaskedCount(),
        ).toContainText('1');
        await expect(
          securityPage.cellMaskSimulatorServerSideCount(),
        ).toContainText('1');
        await expect(
          securityPage.cellMaskSimulatorDecisionRow(
            'ri.cellsec.main.cell-mask.dpo-only',
          ),
        ).toHaveAttribute('data-exempt', 'true');
        await expect(
          securityPage.cellMaskSimulatorDecisionRow(
            'ri.cellsec.main.cell-mask.alice-only',
          ),
        ).toHaveAttribute('data-exempt', 'false');
        await expect(
          securityPage.cellMaskSimulatorDecisionRow(
            'ri.cellsec.main.cell-mask.cel',
          ),
        ).toHaveAttribute('data-decision-kind', 'server-side');
      },
    );
  });
});
