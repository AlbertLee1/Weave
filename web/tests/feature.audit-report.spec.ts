import { expect, test, type Page, type Route } from '@playwright/test';
import {
  AuditReportPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/audit` — the global Audit Report rendered by
 * `src/components/audit/AuditReportPage.tsx` (US-045, PC-A11).
 *
 * Scenarios map to the US-045 acceptance criteria:
 *
 *   - "新建路由 /audit 页面" + "过滤：时间范围、用户、resource type、action、status"
 *     → render scenario seeds 3 cross-ontology audit events,
 *       asserts rows render with actor / action / resource-type
 *       cells + filter inputs are present. Honest mapping: backend
 *       `audit_events` has no `status` column, so the page omits
 *       that control and the spec negative-asserts that no `status`
 *       query param is ever sent.
 *   - "过滤：…" → filter scenario types into actor + resource-type
 *     + since, clicks Apply, and asserts the GET to
 *     /api/v2/admin/auditEvents captures every supported filter as
 *     a query param (positive wire-shape) and `status` is absent
 *     (negative wire-shape).
 *   - "表格行可展开看 request/response payload"
 *     → expand scenario clicks the row toggle, asserts the payload
 *       row appears with the diff_json rendered as JSON, and
 *       collapses back when toggled again.
 *   - "导出 CSV / JSON 按钮"
 *     → export scenario clicks Export CSV → a download event fires
 *       with a `.csv` filename + the in-page export-status banner
 *       reflects the row count. Same for Export JSON.
 *
 * Every scenario stubs `/api/v2/admin/auditEvents` through
 * `page.route` so the page renders deterministic fixtures without
 * touching real PG. Dev-mode auth (default `admin` role) already
 * grants `user.manage` — no extra wiring needed for the gate.
 */

interface MockAuditEvent {
  id: string;
  actor_id: string;
  action: string;
  resource_type: string;
  resource_rid: string;
  diff_json?: unknown;
  ip: string;
  user_agent: string;
  ts: string;
}

interface CapturedRequest {
  url: string;
  method: string;
}

interface Stubs {
  events: MockAuditEvent[];
  gets: CapturedRequest[];
}

function newStubs(): Stubs {
  return { events: [], gets: [] };
}

function auditFixture(overrides: Partial<MockAuditEvent>): MockAuditEvent {
  return {
    id: overrides.id ?? 'evt-1',
    actor_id: 'alice@test',
    action: 'create',
    resource_type: 'ObjectType',
    resource_rid: 'ri.oms.main.object-type.northwind-employee',
    diff_json: { before: null, after: { displayName: 'Employee' } },
    ip: '10.0.0.1',
    user_agent: 'curl/8.0',
    ts: '2026-05-13T12:00:00Z',
    ...overrides,
  };
}

async function stubAuditEndpoint(page: Page, stubs: Stubs): Promise<void> {
  await page.route('**/api/v2/admin/auditEvents*', async (route: Route) => {
    const url = route.request().url();
    stubs.gets.push({ url, method: route.request().method() });
    const u = new URL(url);
    let rows = stubs.events;
    const actor = u.searchParams.get('actor');
    if (actor) rows = rows.filter((e) => e.actor_id === actor);
    const action = u.searchParams.get('action');
    if (action) rows = rows.filter((e) => e.action === action);
    const resourceType = u.searchParams.get('resource_type');
    if (resourceType) rows = rows.filter((e) => e.resource_type === resourceType);
    const since = u.searchParams.get('since');
    if (since) rows = rows.filter((e) => e.ts >= since);
    const until = u.searchParams.get('until');
    if (until) rows = rows.filter((e) => e.ts <= until);
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: rows }),
    });
  });
}

describeFeature('Global Audit Report page', () => {
  test('Scenario: renders cross-ontology audit events with filter controls @smoke', async ({
    page,
    request,
  }) => {
    // AC: "新建路由 /audit 页面" + "过滤：时间范围、用户、resource type、action、status".
    // Seed three events across two ontologies and two resource types.
    // Assert the four supported filter inputs render + the page does
    // NOT expose a status control (honest mapping for the missing
    // backend column).
    const stubs = newStubs();
    stubs.events = [
      auditFixture({
        id: 'evt-1',
        actor_id: 'alice@test',
        action: 'create',
        resource_type: 'ObjectType',
        resource_rid: 'ri.oms.main.object-type.northwind-employee',
        ts: '2026-05-13T12:00:00Z',
      }),
      auditFixture({
        id: 'evt-2',
        actor_id: 'bob@test',
        action: 'update',
        resource_type: 'ActionType',
        resource_rid: 'ri.oms.main.action-type.northwind-fireEmployee',
        ts: '2026-05-13T13:00:00Z',
      }),
      auditFixture({
        id: 'evt-3',
        actor_id: 'carol@test',
        action: 'delete',
        resource_type: 'LinkType',
        resource_rid: 'ri.oms.main.link-type.chinook-track-album',
        ts: '2026-05-13T14:00:00Z',
      }),
    ];

    const auditPage = new AuditReportPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three cross-ontology audit events exist', async () => {
      await stubAuditEndpoint(page, stubs);
    });

    await When('the user opens /audit', async () => {
      await auditPage.goto();
      await expect(auditPage.root).toBeVisible();
    });

    await Then('the audit list renders all three rows', async () => {
      await expect(auditPage.list).toBeVisible();
      await expect(auditPage.rows()).toHaveCount(3);
      await expect(auditPage.rowById('evt-1')).toBeVisible();
      await expect(auditPage.rowById('evt-2')).toBeVisible();
      await expect(auditPage.rowById('evt-3')).toBeVisible();
    });

    await Then('each row carries the action and resource-type metadata', async () => {
      await expect(auditPage.rowById('evt-1')).toHaveAttribute(
        'data-audit-action',
        'create',
      );
      await expect(auditPage.rowById('evt-1')).toHaveAttribute(
        'data-audit-resource-type',
        'ObjectType',
      );
      await expect(auditPage.rowById('evt-2')).toHaveAttribute(
        'data-audit-action',
        'update',
      );
      await expect(auditPage.rowById('evt-3')).toHaveAttribute(
        'data-audit-resource-type',
        'LinkType',
      );
    });

    await Then(
      'the four supported filter inputs are present',
      async () => {
        await expect(auditPage.actorInput()).toBeVisible();
        await expect(auditPage.actionInput()).toBeVisible();
        await expect(auditPage.resourceTypeInput()).toBeVisible();
        await expect(auditPage.sinceInput()).toBeVisible();
        await expect(auditPage.untilInput()).toBeVisible();
      },
    );

    await Then(
      'the page does not expose a status filter (backend has no status column)',
      async () => {
        // Honest mapping (US-033 / US-034 negative wire-shape
        // template): backend `audit_events` (migration 000020) has
        // no status column; the lifecycle status is encoded inside
        // diff_json when relevant. A future migration that adds a
        // status field promotes this absence assertion to a
        // positive assertion + adds a select to the filter row.
        await expect(
          page.getByRole('combobox', { name: /status/i }),
        ).toHaveCount(0);
        await expect(
          page.getByRole('textbox', { name: /^status$/i }),
        ).toHaveCount(0);
      },
    );
  });

  test('Scenario: applying filters refetches the list with the supported query params @smoke', async ({
    page,
  }) => {
    // AC: "过滤：时间范围、用户、resource type、action、status".
    // Type into actor + resource-type + since + click Apply.
    // Assert the GET captures actor=alice@test + resource_type=ObjectType
    // + since=<rfc3339> + the negative: NO `status` param is ever sent.
    const stubs = newStubs();
    stubs.events = [
      auditFixture({
        id: 'evt-1',
        actor_id: 'alice@test',
        action: 'create',
        resource_type: 'ObjectType',
        ts: '2026-05-13T12:00:00Z',
      }),
      auditFixture({
        id: 'evt-2',
        actor_id: 'bob@test',
        action: 'update',
        resource_type: 'ActionType',
        ts: '2026-05-13T13:00:00Z',
      }),
    ];

    const auditPage = new AuditReportPage(page);

    await Given('audit events exist for multiple actors', async () => {
      await stubAuditEndpoint(page, stubs);
    });

    await When('the user opens the page', async () => {
      await auditPage.goto();
      await expect(auditPage.root).toBeVisible();
      // Initial unfiltered load.
      await expect.poll(() => stubs.gets.length).toBeGreaterThanOrEqual(1);
    });

    await When(
      'the user fills the actor, resource-type and since filters and clicks Apply',
      async () => {
        await auditPage.actorInput().fill('alice@test');
        await auditPage.resourceTypeInput().fill('ObjectType');
        await auditPage.sinceInput().fill('2026-05-01T00:00');
        await auditPage.applyButton.click();
      },
    );

    await Then(
      'a GET captures every supported filter as a query param',
      async () => {
        await expect
          .poll(() => {
            const last = stubs.gets.at(-1);
            if (!last) return null;
            const u = new URL(last.url);
            return {
              actor: u.searchParams.get('actor'),
              resource_type: u.searchParams.get('resource_type'),
              hasSince: u.searchParams.has('since'),
            };
          })
          .toEqual({
            actor: 'alice@test',
            resource_type: 'ObjectType',
            hasSince: true,
          });
      },
    );

    await Then(
      'no `status` query param is ever sent (honest mapping)',
      async () => {
        // Wire-shape negative assertion: even if a future PR adds a
        // status control to the UI, this guard prevents it from
        // leaking into the request without a coordinated backend
        // migration. See US-033 / US-034 codebase notes.
        for (const r of stubs.gets) {
          const keys = [...new URL(r.url).searchParams.keys()];
          expect(keys).not.toContain('status');
        }
      },
    );

    await Then(
      'only the filtered row remains visible',
      async () => {
        await expect(auditPage.rowById('evt-1')).toBeVisible();
        await expect(auditPage.rowById('evt-2')).toHaveCount(0);
      },
    );
  });

  test('Scenario: expanding a row reveals the request/response payload', async ({
    page,
  }) => {
    // AC: "表格行可展开看 request/response payload".
    // Click the expand toggle, assert the payload row appears with
    // the diff_json rendered as JSON, then collapse it again.
    const stubs = newStubs();
    stubs.events = [
      auditFixture({
        id: 'evt-payload',
        actor_id: 'alice@test',
        action: 'create',
        resource_type: 'ObjectType',
        diff_json: {
          before: null,
          after: { displayName: 'Employee', apiName: 'employee' },
        },
        ts: '2026-05-13T12:00:00Z',
      }),
    ];

    const auditPage = new AuditReportPage(page);

    await Given('one audit event with a diff_json payload exists', async () => {
      await stubAuditEndpoint(page, stubs);
    });

    await When('the user opens the page', async () => {
      await auditPage.goto();
      await expect(auditPage.root).toBeVisible();
      await expect(auditPage.rowById('evt-payload')).toBeVisible();
    });

    await Then('the row is initially collapsed', async () => {
      await expect(auditPage.rowById('evt-payload')).toHaveAttribute(
        'data-audit-expanded',
        'false',
      );
      await expect(auditPage.payloadFor('evt-payload')).toHaveCount(0);
    });

    await When('the user clicks the expand toggle', async () => {
      await auditPage.expandButtonFor('evt-payload').click();
    });

    await Then(
      'the payload row appears with the JSON-rendered diff',
      async () => {
        await expect(auditPage.rowById('evt-payload')).toHaveAttribute(
          'data-audit-expanded',
          'true',
        );
        await expect(auditPage.payloadFor('evt-payload')).toBeVisible();
        await expect(auditPage.payloadJsonFor('evt-payload')).toContainText(
          'Employee',
        );
        await expect(auditPage.payloadJsonFor('evt-payload')).toContainText(
          'apiName',
        );
      },
    );

    await When('the user clicks the toggle again', async () => {
      await auditPage.expandButtonFor('evt-payload').click();
    });

    await Then('the payload collapses again', async () => {
      await expect(auditPage.rowById('evt-payload')).toHaveAttribute(
        'data-audit-expanded',
        'false',
      );
      await expect(auditPage.payloadFor('evt-payload')).toHaveCount(0);
    });
  });

  test('Scenario: clicking Export CSV triggers a CSV download and surfaces the export status', async ({
    page,
  }) => {
    // AC: "导出 CSV / JSON 按钮". Click Export CSV → browser fires a
    // download event whose filename ends in .csv → the in-page
    // export-status banner reflects the row count + format.
    const stubs = newStubs();
    stubs.events = [
      auditFixture({ id: 'evt-1', actor_id: 'alice@test', action: 'create' }),
      auditFixture({ id: 'evt-2', actor_id: 'bob@test', action: 'update' }),
    ];

    const auditPage = new AuditReportPage(page);

    await Given('two audit events exist', async () => {
      await stubAuditEndpoint(page, stubs);
    });

    await When('the user opens the page', async () => {
      await auditPage.goto();
      await expect(auditPage.root).toBeVisible();
      await expect(auditPage.rows()).toHaveCount(2);
    });

    await When('the user clicks Export CSV and the download starts', async () => {
      const [download] = await Promise.all([
        page.waitForEvent('download'),
        auditPage.exportCsvButton.click(),
      ]);
      expect(download.suggestedFilename()).toMatch(/audit-report-.*\.csv$/);
    });

    await Then('the export-status banner reflects the CSV export', async () => {
      await expect(auditPage.exportStatus).toBeVisible();
      await expect(auditPage.exportStatus).toHaveAttribute(
        'data-format',
        'csv',
      );
      await expect(auditPage.exportStatus).toHaveAttribute(
        'data-row-count',
        '2',
      );
      await expect(auditPage.exportStatus).toHaveAttribute(
        'data-filename',
        /audit-report-.*\.csv$/,
      );
    });
  });

  test('Scenario: clicking Export JSON triggers a JSON download', async ({
    page,
  }) => {
    // AC: "导出 CSV / JSON 按钮" — the JSON arm. Same pattern as the
    // CSV scenario but asserts the JSON variant and a different row
    // count so the export-status banner content is event-driven.
    const stubs = newStubs();
    stubs.events = [
      auditFixture({ id: 'evt-only', actor_id: 'alice@test', action: 'create' }),
    ];

    const auditPage = new AuditReportPage(page);

    await Given('one audit event exists', async () => {
      await stubAuditEndpoint(page, stubs);
    });

    await When('the user opens the page', async () => {
      await auditPage.goto();
      await expect(auditPage.root).toBeVisible();
      await expect(auditPage.rows()).toHaveCount(1);
    });

    await When('the user clicks Export JSON and the download starts', async () => {
      const [download] = await Promise.all([
        page.waitForEvent('download'),
        auditPage.exportJsonButton.click(),
      ]);
      expect(download.suggestedFilename()).toMatch(/audit-report-.*\.json$/);
    });

    await Then('the export-status banner reflects the JSON export', async () => {
      await expect(auditPage.exportStatus).toBeVisible();
      await expect(auditPage.exportStatus).toHaveAttribute(
        'data-format',
        'json',
      );
      await expect(auditPage.exportStatus).toHaveAttribute(
        'data-row-count',
        '1',
      );
    });
  });
});
