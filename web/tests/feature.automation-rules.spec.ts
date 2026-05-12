import { expect, test, type Page, type Route } from '@playwright/test';
import {
  AutomationRulesPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/automation/:ontology` — the Automation Rules
 * management page rendered by
 * `src/components/automation/AutomationRulesPage.tsx` (US-039, PC-A01).
 *
 * Scenarios map to the US-039 acceptance criteria:
 *
 *   - "新建路由 /automation/:ontology 并在左侧导航加入口"
 *     → Scenario "renders the rules list with trigger badges and statuses"
 *       boots the page via `/automation/northwind` and asserts the list
 *       renders (route works; nav-entry registration is exercised by the
 *       Dashboard sidebar — covered transitively).
 *   - "规则列表：trigger 类型徽标、最近执行状态、启用/暂停 toggle"
 *     → list scenario asserts both badges per row; the pause/resume
 *       scenario locks the toggle flow.
 *   - "规则编辑抽屉：trigger / condition CEL / actions sequence /
 *      debounce / throttle"
 *     → create scenario opens the drawer, fills all six fields, asserts
 *       the POST body merges condition into triggerConfig + carries
 *       debounce/throttle on retryPolicy.
 *   - "Pause/Resume 按钮调对应 endpoint"
 *     → pause scenario asserts the POST /pause endpoint is hit and the
 *       row re-renders as paused after invalidation.
 *   - "执行历史抽屉：调 /api/v2/ontologies/{o}/automationRules/{id}/executions"
 *     → executions scenario opens the drawer + asserts the GET is hit.
 *   - "React Query invalidate 正确；错误以 toast 展示"
 *     → covered by both the pause scenario (invalidate → row repaints
 *       as paused) and the error-toast scenario (mutation failure → toast
 *       surfaces with the apierror parameters.reason text).
 *
 * Every scenario stubs the backend through `page.route` so the page
 * renders deterministic fixtures without touching real PG / NATS.
 */

const ONTOLOGY = 'northwind';

interface MockRule {
  id: string;
  ontologyRid: string;
  name: string;
  description?: string;
  status: 'active' | 'paused' | 'disabled';
  triggerType: 'schedule' | 'dataChange' | 'manual';
  triggerConfig?: unknown;
  effects?: unknown;
  retryPolicy?: unknown;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

interface MockExecution {
  id: string;
  ruleId: string;
  triggerEvent?: unknown;
  startedAt: string;
  completedAt?: string;
  status: 'running' | 'success' | 'error' | 'retrying';
  error?: string;
  retryCount: number;
  result?: unknown;
}

interface CapturedRequest {
  url: string;
  method: string;
  body: unknown;
}

interface Stubs {
  rules: MockRule[];
  executions: Record<string, MockExecution[]>;
  posts: CapturedRequest[];
  puts: CapturedRequest[];
  pauses: CapturedRequest[];
  resumes: CapturedRequest[];
  deletes: CapturedRequest[];
  executionGets: CapturedRequest[];
  /**
   * When non-null, the next POST/PUT/pause mutation returns 500 with
   * this errorName so the toast-error scenario can lock the failure
   * branch. Cleared after one mutation so subsequent calls succeed.
   */
  failNextMutationWith: string | null;
}

function newStubs(initialRules: MockRule[] = []): Stubs {
  return {
    rules: initialRules.map((r) => ({ ...r })),
    executions: {},
    posts: [],
    puts: [],
    pauses: [],
    resumes: [],
    deletes: [],
    executionGets: [],
    failNextMutationWith: null,
  };
}

function ruleFixture(overrides: Partial<MockRule> = {}): MockRule {
  return {
    id: 'rule-1',
    ontologyRid: 'ri.ontology.main.ontology.northwind',
    name: 'Nightly inventory sweep',
    description: 'Run inventory aggregate every night.',
    status: 'active',
    triggerType: 'schedule',
    triggerConfig: { condition: 'true', cron: '0 0 * * *' },
    effects: [{ kind: 'applyAction', actionType: 'computeInventory' }],
    retryPolicy: { debounceMs: 250 },
    createdBy: 'admin@test',
    createdAt: '2026-05-13T00:00:00Z',
    updatedAt: '2026-05-13T00:00:00Z',
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
  const PREFIX = `**/api/v2/ontologies/${ONTOLOGY}/automationRules`;

  // Executions: GET /automationRules/{ruleId}/executions
  // Register the more-specific pattern FIRST so Playwright's LIFO
  // resolution prefers it over the catch-all when both match.
  // (See US-023 codebase note on Playwright route registration order.)
  await page.route(`${PREFIX}/*/executions*`, async (route: Route) => {
    const url = route.request().url();
    stubs.executionGets.push({
      url,
      method: route.request().method(),
      body: null,
    });
    // Extract ruleId between /automationRules/ and /executions
    const m = url.match(/\/automationRules\/([^/?#]+)\/executions/);
    const ruleId = m?.[1] ?? '';
    const data = stubs.executions[ruleId] ?? [];
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data,
        total: data.length,
        offset: 0,
        limit: 50,
      }),
    });
  });

  // Pause / Resume: POST /automationRules/{id}/pause | /resume
  await page.route(`${PREFIX}/*/pause`, async (route: Route) => {
    const url = route.request().url();
    stubs.pauses.push({
      url,
      method: route.request().method(),
      body: route.request().postDataJSON() ?? {},
    });
    if (stubs.failNextMutationWith) {
      const name = stubs.failNextMutationWith;
      stubs.failNextMutationWith = null;
      await route.fulfill(failWith(name, 'Synthetic pause failure for BDD'));
      return;
    }
    const m = url.match(/\/automationRules\/([^/?#]+)\/pause/);
    const ruleId = m?.[1] ?? '';
    const idx = stubs.rules.findIndex((r) => r.id === ruleId);
    if (idx === -1) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'AutomationRuleNotFound',
          errorInstanceId: 'spec',
          parameters: { ruleId },
        }),
      });
      return;
    }
    stubs.rules[idx] = {
      ...stubs.rules[idx],
      status: 'paused',
      updatedAt: '2026-05-13T01:00:00Z',
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(stubs.rules[idx]),
    });
  });

  await page.route(`${PREFIX}/*/resume`, async (route: Route) => {
    const url = route.request().url();
    stubs.resumes.push({
      url,
      method: route.request().method(),
      body: route.request().postDataJSON() ?? {},
    });
    const m = url.match(/\/automationRules\/([^/?#]+)\/resume/);
    const ruleId = m?.[1] ?? '';
    const idx = stubs.rules.findIndex((r) => r.id === ruleId);
    if (idx === -1) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'AutomationRuleNotFound',
          errorInstanceId: 'spec',
          parameters: { ruleId },
        }),
      });
      return;
    }
    stubs.rules[idx] = {
      ...stubs.rules[idx],
      status: 'active',
      updatedAt: '2026-05-13T02:00:00Z',
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(stubs.rules[idx]),
    });
  });

  // GET / POST / PUT / DELETE on /automationRules and /automationRules/{id}
  // Use a single catch-all `${PREFIX}*` and dispatch by method + path.
  await page.route(`${PREFIX}*`, async (route: Route) => {
    const req = route.request();
    const url = req.url();
    const method = req.method();
    // Strip query string before deciding "is this the list path or a
    // single rule path".
    const pathOnly = url.split('?')[0];
    const isSingle = /\/automationRules\/[^/]+$/.test(pathOnly);

    if (method === 'GET' && !isSingle) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: stubs.rules }),
      });
      return;
    }

    if (method === 'POST' && !isSingle) {
      const body = req.postDataJSON() ?? {};
      stubs.posts.push({ url, method, body });
      if (stubs.failNextMutationWith) {
        const name = stubs.failNextMutationWith;
        stubs.failNextMutationWith = null;
        await route.fulfill(failWith(name, 'Synthetic create failure for BDD'));
        return;
      }
      const created: MockRule = {
        id: `rule-${stubs.rules.length + 1}`,
        ontologyRid: 'ri.ontology.main.ontology.northwind',
        name: body.name,
        description: body.description ?? '',
        status: 'active',
        triggerType: body.triggerType,
        triggerConfig: body.triggerConfig,
        effects: body.effects,
        retryPolicy: body.retryPolicy,
        createdBy: body.createdBy ?? 'spec',
        createdAt: '2026-05-13T03:00:00Z',
        updatedAt: '2026-05-13T03:00:00Z',
      };
      stubs.rules.push(created);
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(created),
      });
      return;
    }

    if (isSingle) {
      const m = pathOnly.match(/\/automationRules\/([^/?#]+)$/);
      const ruleId = m?.[1] ?? '';
      const idx = stubs.rules.findIndex((r) => r.id === ruleId);

      if (method === 'PUT') {
        const body = req.postDataJSON() ?? {};
        stubs.puts.push({ url, method, body });
        if (idx === -1) {
          await route.fulfill({
            status: 404,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'NOT_FOUND',
              errorName: 'AutomationRuleNotFound',
              errorInstanceId: 'spec',
              parameters: { ruleId },
            }),
          });
          return;
        }
        stubs.rules[idx] = {
          ...stubs.rules[idx],
          ...('name' in body ? { name: body.name } : {}),
          ...('description' in body ? { description: body.description } : {}),
          ...('triggerType' in body ? { triggerType: body.triggerType } : {}),
          ...('triggerConfig' in body
            ? { triggerConfig: body.triggerConfig }
            : {}),
          ...('effects' in body ? { effects: body.effects } : {}),
          ...('retryPolicy' in body ? { retryPolicy: body.retryPolicy } : {}),
          updatedAt: '2026-05-13T04:00:00Z',
        };
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(stubs.rules[idx]),
        });
        return;
      }

      if (method === 'DELETE') {
        stubs.deletes.push({ url, method, body: null });
        if (idx !== -1) stubs.rules.splice(idx, 1);
        await route.fulfill({ status: 204, body: '' });
        return;
      }

      if (method === 'GET') {
        if (idx === -1) {
          await route.fulfill({
            status: 404,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'NOT_FOUND',
              errorName: 'AutomationRuleNotFound',
              errorInstanceId: 'spec',
              parameters: { ruleId },
            }),
          });
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(stubs.rules[idx]),
        });
        return;
      }
    }

    await route.continue();
  });
}

describeFeature('Automation Rules management', () => {
  test('Scenario: renders the rules list with trigger badges, status badges, and toggle buttons @smoke', async ({
    page,
    request,
  }) => {
    // AC: "规则列表：trigger 类型徽标、最近执行状态、启用/暂停 toggle".
    // Seed two rules with different triggerType + status so both badge
    // variants and both toggle button labels exercise.
    const rules: MockRule[] = [
      ruleFixture({
        id: 'rule-active',
        name: 'Nightly inventory sweep',
        triggerType: 'schedule',
        status: 'active',
      }),
      ruleFixture({
        id: 'rule-paused',
        name: 'On customer create',
        triggerType: 'dataChange',
        status: 'paused',
      }),
    ];
    const stubs = newStubs(rules);
    const automation = new AutomationRulesPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('two automation rules exist on northwind', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens /automation/northwind', async () => {
      await automation.goto(ONTOLOGY);
      await expect(automation.root).toBeVisible();
    });

    await Then('the rules list renders both rows', async () => {
      await expect(automation.list).toBeVisible();
      await expect(automation.rowByRuleId('rule-active')).toBeVisible();
      await expect(automation.rowByRuleId('rule-paused')).toBeVisible();
    });

    await Then('each row exposes its trigger and status badges', async () => {
      await expect(automation.rowTriggerBadge('rule-active')).toContainText(
        'schedule',
      );
      await expect(automation.rowStatusBadge('rule-active')).toContainText(
        'active',
      );
      await expect(automation.rowTriggerBadge('rule-paused')).toContainText(
        'dataChange',
      );
      await expect(automation.rowStatusBadge('rule-paused')).toContainText(
        'paused',
      );
    });

    await Then(
      'the toggle button label reflects the current rule status',
      async () => {
        await expect(automation.toggleButton('rule-active')).toContainText(
          'Pause',
        );
        await expect(automation.toggleButton('rule-paused')).toContainText(
          'Resume',
        );
      },
    );
  });

  test('Scenario: pausing an active rule hits the pause endpoint and the row re-renders as paused @smoke', async ({
    page,
  }) => {
    // AC: "Pause/Resume 按钮调对应 endpoint" + "React Query invalidate
    // 正确". Locks the full mutation→invalidate→refetch→DOM-flip
    // contract: click Pause → POST /pause → re-render row status badge
    // as `paused` + toggle label flips to `Resume`.
    const rules: MockRule[] = [
      ruleFixture({
        id: 'rule-active',
        name: 'Nightly inventory sweep',
        status: 'active',
      }),
    ];
    const stubs = newStubs(rules);
    const automation = new AutomationRulesPage(page);

    await Given('one active rule exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the automation rules page', async () => {
      await automation.goto(ONTOLOGY);
      await expect(automation.root).toBeVisible();
    });

    await Then('the Pause button is initially visible', async () => {
      await expect(automation.toggleButton('rule-active')).toContainText(
        'Pause',
      );
      await expect(automation.rowStatusBadge('rule-active')).toContainText(
        'active',
      );
    });

    await When('the user clicks Pause', async () => {
      await automation.toggleButton('rule-active').click();
    });

    await Then('a POST to the pause endpoint is captured', async () => {
      await expect.poll(() => stubs.pauses.length).toBeGreaterThanOrEqual(1);
      const last = stubs.pauses.at(-1)!;
      expect(last.method).toBe('POST');
      expect(last.url).toMatch(/\/automationRules\/rule-active\/pause$/);
    });

    await Then(
      'the row re-renders with status paused and toggle label "Resume"',
      async () => {
        await expect(automation.rowStatusBadge('rule-active')).toContainText(
          'paused',
        );
        await expect(automation.toggleButton('rule-active')).toContainText(
          'Resume',
        );
      },
    );

    await Then('no resume endpoint was hit', async () => {
      expect(stubs.resumes.length).toBe(0);
    });
  });

  test('Scenario: creating a new rule via the editor drawer POSTs the merged trigger config and retryPolicy', async ({
    page,
  }) => {
    // AC: "规则编辑抽屉：trigger / condition CEL / actions sequence /
    //      debounce / throttle".
    // Lock the wire-shape contract: the POST body must
    //   (a) carry the supplied name + triggerType,
    //   (b) merge `condition` into triggerConfig (the BDD lifecycle gate
    //       contract from US-015),
    //   (c) parse the effects JSON textarea to an array, and
    //   (d) attach debounceMs + throttleMs onto retryPolicy when both
    //       fields were filled.
    const stubs = newStubs([]);
    const automation = new AutomationRulesPage(page);

    await Given('the rules list is initially empty', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page', async () => {
      await automation.goto(ONTOLOGY);
      await expect(automation.emptyState).toBeVisible();
    });

    await When('the user clicks "New rule"', async () => {
      await automation.createBtn.click();
      await expect(automation.editorDrawer).toBeVisible();
    });

    await When('the user fills every form field', async () => {
      await automation.formName().fill('Cleanup stale carts');
      await automation
        .formDescription()
        .fill('Compact aged shopping carts every hour.');
      await automation.formTrigger().selectOption('schedule');
      await automation.formCondition().fill('cart.ageHours > 24');
      await automation
        .formTriggerConfig()
        .fill('{"cron":"0 * * * *"}');
      await automation
        .formEffects()
        .fill('[{"kind":"applyAction","actionType":"deleteStaleCarts"}]');
      await automation.formDebounce().fill('500');
      await automation.formThrottle().fill('1000');
    });

    await When('the user clicks Save', async () => {
      await automation.formSaveBtn().click();
    });

    await Then('a POST to the create endpoint is captured', async () => {
      await expect.poll(() => stubs.posts.length).toBeGreaterThanOrEqual(1);
    });

    await Then('the POST body carries the merged contract', async () => {
      const body = stubs.posts.at(-1)!.body as Record<string, unknown>;
      expect(body.name).toBe('Cleanup stale carts');
      expect(body.triggerType).toBe('schedule');

      // condition is merged INTO triggerConfig (BDD lifecycle gate contract).
      const cfg = body.triggerConfig as Record<string, unknown>;
      expect(cfg).toMatchObject({
        cron: '0 * * * *',
        condition: 'cart.ageHours > 24',
      });

      // effects is a parsed array, not a raw string.
      expect(body.effects).toEqual([
        { kind: 'applyAction', actionType: 'deleteStaleCarts' },
      ]);

      // retryPolicy carries debounceMs + throttleMs as numbers.
      expect(body.retryPolicy).toMatchObject({
        debounceMs: 500,
        throttleMs: 1000,
      });
    });

    await Then(
      'the editor drawer closes and the new row appears in the list',
      async () => {
        await expect(automation.editorDrawer).toHaveCount(0);
        // The synthetic POST handler appends to stubs.rules, the
        // invalidate → GET picks it up, and the new row gets a
        // generated rule-1 id (the seed list was empty).
        await expect(automation.rowByRuleId('rule-1')).toBeVisible();
        await expect(automation.rowByRuleId('rule-1')).toContainText(
          'Cleanup stale carts',
        );
      },
    );
  });

  test('Scenario: the executions drawer hits the executions endpoint and renders the latest runs', async ({
    page,
  }) => {
    // AC: "执行历史抽屉：调
    //      /api/v2/ontologies/{o}/automationRules/{id}/executions".
    // Seed one rule + two executions, click the row's Executions
    // button, assert the GET to the executions endpoint is captured,
    // and the drawer renders both rows with their status badges.
    const rules: MockRule[] = [
      ruleFixture({
        id: 'rule-with-history',
        name: 'Nightly inventory sweep',
        status: 'active',
      }),
    ];
    const stubs = newStubs(rules);
    stubs.executions['rule-with-history'] = [
      {
        id: 'exec-1',
        ruleId: 'rule-with-history',
        startedAt: '2026-05-13T00:00:00Z',
        completedAt: '2026-05-13T00:00:05Z',
        status: 'success',
        retryCount: 0,
      },
      {
        id: 'exec-2',
        ruleId: 'rule-with-history',
        startedAt: '2026-05-13T01:00:00Z',
        completedAt: '2026-05-13T01:00:02Z',
        status: 'error',
        retryCount: 1,
        error: 'compensator timed out',
      },
    ];
    const automation = new AutomationRulesPage(page);

    await Given('one rule with two executions exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page', async () => {
      await automation.goto(ONTOLOGY);
      await expect(automation.root).toBeVisible();
    });

    await When('the user clicks Executions on the rule row', async () => {
      await automation.executionsButton('rule-with-history').click();
      await expect(automation.executionsDrawer).toBeVisible();
    });

    await Then('a GET to the executions endpoint is captured', async () => {
      await expect
        .poll(() => stubs.executionGets.length)
        .toBeGreaterThanOrEqual(1);
      const last = stubs.executionGets.at(-1)!;
      expect(last.method).toBe('GET');
      expect(last.url).toMatch(
        /\/automationRules\/rule-with-history\/executions/,
      );
    });

    await Then('both executions render with their status badges', async () => {
      await expect(automation.executionsList()).toBeVisible();
      await expect(automation.executionRows()).toHaveCount(2);
      const firstStatus = automation
        .executionRows()
        .nth(0)
        .getByTestId('automation-rule-execution-status');
      const secondStatus = automation
        .executionRows()
        .nth(1)
        .getByTestId('automation-rule-execution-status');
      await expect(firstStatus).toContainText('success');
      await expect(secondStatus).toContainText('error');
    });
  });

  test('Scenario: a save failure surfaces a toast carrying the apierror reason', async ({
    page,
  }) => {
    // AC: "React Query invalidate 正确；错误以 toast 展示". The toast
    // store is mounted globally via Shell → Toaster, so every page
    // shares the same `data-testid="toast"` surface. The scenario
    // pushes a synthetic 500 with apierror parameters.reason and
    // asserts the page surfaces the reason text inside the toast tile.
    // It ALSO asserts the form error banner doubles up (the page
    // surfaces both — the toast is the global trail, the inline banner
    // keeps focus near the broken form).
    const stubs = newStubs([]);
    stubs.failNextMutationWith = 'CreateAutomationRuleFailed';
    const automation = new AutomationRulesPage(page);

    await Given('the create endpoint will reject the next POST', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page', async () => {
      await automation.goto(ONTOLOGY);
      await expect(automation.root).toBeVisible();
    });

    await When(
      'the user fills the editor with a minimum-valid payload',
      async () => {
        await automation.createBtn.click();
        await expect(automation.editorDrawer).toBeVisible();
        await automation.formName().fill('Rule that will fail to save');
      },
    );

    await When('the user clicks Save', async () => {
      await automation.formSaveBtn().click();
    });

    await Then(
      'a toast appears carrying the apierror reason text',
      async () => {
        const toast = page.getByTestId('toast').first();
        await expect(toast).toBeVisible();
        await expect(toast).toContainText('CreateAutomationRuleFailed');
        await expect(toast).toContainText(
          /Synthetic create failure for BDD/i,
        );
      },
    );

    await Then('the inline form-error banner is also visible', async () => {
      await expect(automation.formError()).toBeVisible();
      await expect(automation.formError()).toContainText(
        'CreateAutomationRuleFailed',
      );
    });

    await Then('the drawer stays open for retry', async () => {
      await expect(automation.editorDrawer).toBeVisible();
      await expect(automation.formSaveBtn()).toBeEnabled();
    });

    await Then('no rule row was appended to the list yet', async () => {
      expect(stubs.rules.length).toBe(0);
    });
  });
});
