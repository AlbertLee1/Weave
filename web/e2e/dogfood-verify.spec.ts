import { test, expect, type ConsoleMessage, type Page } from '@playwright/test';
import { writeFileSync, mkdirSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

// Dogfood verification spec — re-runs the 6 problems listed in
// dogfood-output/report.md through a real browser to confirm each is
// fixed. Emits dogfood-output/verify-report.md when the suite finishes
// (using a process-exit hook on the last test).
//
// This is the same methodology as the original Hermes dogfood pass:
// navigate to each URL, watch React Router warnings, assert the right
// page renders.

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = join(HERE, '..', '..');
// DOGFOOD_BASE_URL lets the harness target either the Vite dev server
// (:5173, source-of-truth for the latest fixes) or the Go server's
// embedded dist (:9117, what the dogfood agent actually exercises).
// Falls back to Playwright's configured baseURL when unset.
const BASE_URL = process.env.DOGFOOD_BASE_URL ?? '';
const REPORT_TAG = BASE_URL
  ? new URL(BASE_URL).host.replace(/[^a-z0-9]+/gi, '-')
  : 'default';
const REPORT_PATH = join(
  REPO_ROOT,
  'dogfood-output',
  `verify-report.${REPORT_TAG}.md`,
);
const ONTOLOGY = 'iotDemo';

function abs(path: string): string {
  return BASE_URL ? `${BASE_URL}${path}` : path;
}

interface IssueResult {
  id: string;
  title: string;
  status: 'pass' | 'fail';
  notes: string[];
}

const results: IssueResult[] = [];

function captureConsole(page: Page) {
  const messages: { type: string; text: string }[] = [];
  const onMessage = (msg: ConsoleMessage) => {
    messages.push({ type: msg.type(), text: msg.text() });
  };
  page.on('console', onMessage);
  return {
    messages,
    detach: () => page.off('console', onMessage),
  };
}

function recordIssue(result: IssueResult) {
  results.push(result);
}

test.describe.serial('Dogfood verification: report.md 6 issues', () => {
  test('#1 Audit Report at /admin/audit resolves to AuditReportPage', async ({
    page,
  }) => {
    const console = captureConsole(page);
    await page.goto(abs('/admin/audit'));
    await page.waitForLoadState('networkidle');

    const noMatch = console.messages.filter((m) =>
      m.text.includes('No routes matched'),
    );
    const url = page.url();
    const heading = await page
      .locator('h1, h2')
      .filter({ hasText: /Audit Report/i })
      .first()
      .textContent()
      .catch(() => null);

    const ok = noMatch.length === 0 && /\/audit$/.test(new URL(url).pathname) && !!heading;
    recordIssue({
      id: '#1',
      title: 'Audit Report at /admin/audit',
      status: ok ? 'pass' : 'fail',
      notes: [
        `final URL: ${url}`,
        `heading: ${heading ?? '<none>'}`,
        `No-routes-matched warnings: ${noMatch.length}`,
      ],
    });
    console.detach();
    expect(noMatch).toEqual([]);
    expect(url).toMatch(/\/audit$/);
    expect(heading).toMatch(/Audit Report/i);
  });

  test('#2 AIP Logic at /aip-logic resolves to LogicFlowsPage', async ({
    page,
  }) => {
    const console = captureConsole(page);
    await page.goto(abs('/aip-logic'));
    await page.waitForLoadState('networkidle');

    const noMatch = console.messages.filter((m) =>
      m.text.includes('No routes matched'),
    );
    const url = page.url();
    const pageRoot = await page
      .locator('[data-testid="logic-flows-page"]')
      .count();

    const ok =
      noMatch.length === 0 &&
      /\/logic-flows$/.test(new URL(url).pathname) &&
      pageRoot > 0;
    recordIssue({
      id: '#2',
      title: 'AIP Logic at /aip-logic',
      status: ok ? 'pass' : 'fail',
      notes: [
        `final URL: ${url}`,
        `logic-flows-page testid found: ${pageRoot}`,
        `No-routes-matched warnings: ${noMatch.length}`,
      ],
    });
    console.detach();
    expect(noMatch).toEqual([]);
    expect(url).toMatch(/\/logic-flows$/);
    expect(pageRoot).toBeGreaterThan(0);
  });

  test('#3 Query Builder & Quiver TS only render with an active ontology', async ({
    page,
  }) => {
    // On Dashboard (no active ontology), these sidebar items must not
    // appear as dead links pointing to /.
    await page.goto(abs('/'));
    await page.waitForLoadState('networkidle');
    const dashSidebar = page.getByTestId('sidebar');
    const dashboardQB = await dashSidebar
      .locator('a', { hasText: 'Query Builder' })
      .count();
    const dashboardQuiver = await dashSidebar
      .locator('a', { hasText: 'Quiver TS' })
      .count();

    // Once we select an ontology (visit /explorer/iotDemo), the sidebar
    // gains both items pointing at ontology-scoped URLs.
    await page.goto(abs(`/explorer/${ONTOLOGY}`));
    await page.waitForLoadState('networkidle');
    const ontSidebar = page.getByTestId('sidebar');
    const qbLink = ontSidebar.locator('a', { hasText: 'Query Builder' });
    const quiverLink = ontSidebar.locator('a', { hasText: 'Quiver TS' });
    const qbHref = await qbLink.first().getAttribute('href');
    const quiverHref = await quiverLink.first().getAttribute('href');

    const ok =
      dashboardQB === 0 &&
      dashboardQuiver === 0 &&
      qbHref === `/objectsets/${ONTOLOGY}` &&
      quiverHref === `/quiver/${ONTOLOGY}`;

    recordIssue({
      id: '#3',
      title: 'Query Builder / Quiver TS no longer fall back to /',
      status: ok ? 'pass' : 'fail',
      notes: [
        `Dashboard: Query Builder link count = ${dashboardQB} (expect 0)`,
        `Dashboard: Quiver TS link count = ${dashboardQuiver} (expect 0)`,
        `${ONTOLOGY}: Query Builder href = ${qbHref}`,
        `${ONTOLOGY}: Quiver TS href = ${quiverHref}`,
      ],
    });

    expect(dashboardQB).toBe(0);
    expect(dashboardQuiver).toBe(0);
    expect(qbHref).toBe(`/objectsets/${ONTOLOGY}`);
    expect(quiverHref).toBe(`/quiver/${ONTOLOGY}`);
  });

  test('#4 Automation / SecurityPolicies / Proposals render their own pages', async ({
    page,
  }) => {
    const checks: Array<{
      url: string;
      ownTestid: string;
      label: string;
    }> = [
      {
        url: `/automation/${ONTOLOGY}`,
        ownTestid: 'automation-rules-page',
        label: 'Automation Rules',
      },
      {
        url: `/admin/${ONTOLOGY}/security`,
        ownTestid: 'security-policies-page',
        label: 'Security Policies',
      },
      {
        url: `/proposals/${ONTOLOGY}`,
        ownTestid: 'proposals-page',
        label: 'Proposals',
      },
    ];

    const notes: string[] = [];
    let allOk = true;
    for (const c of checks) {
      await page.goto(abs(c.url));
      await page.waitForLoadState('networkidle');
      const ownCount = await page
        .locator(`[data-testid="${c.ownTestid}"]`)
        .count();
      const explorerCount = await page
        .locator('[data-testid="explorer-page"]')
        .count();
      const ok = ownCount > 0 && explorerCount === 0;
      if (!ok) allOk = false;
      notes.push(
        `${c.label} (${c.url}): testid=${c.ownTestid} found=${ownCount}, explorer-page found=${explorerCount}`,
      );
    }

    recordIssue({
      id: '#4',
      title: 'Ontology sub-routes no longer render Schema Graph',
      status: allOk ? 'pass' : 'fail',
      notes,
    });

    for (const c of checks) {
      await page.goto(abs(c.url));
      await page.waitForLoadState('networkidle');
      await expect(page.locator(`[data-testid="${c.ownTestid}"]`)).toHaveCount(1);
      await expect(page.locator('[data-testid="explorer-page"]')).toHaveCount(0);
    }
  });

  test('#5 No "two children with the same key" warnings on any page', async ({
    page,
  }) => {
    const urls = [
      '/',
      '/dashboards',
      '/apps',
      '/threads',
      '/logic-flows',
      '/pipelines',
      '/developer/playground',
      '/developer/metrics',
      '/schema/infer',
      '/permission-requests',
      '/notifications',
      '/mentions',
      '/marketplace',
      '/settings',
      `/explorer/${ONTOLOGY}`,
      `/objectsets/${ONTOLOGY}`,
      `/quiver/${ONTOLOGY}`,
      `/automation/${ONTOLOGY}`,
      `/proposals/${ONTOLOGY}`,
      `/admin/${ONTOLOGY}/security`,
      '/admin/markings',
      '/admin/compliance',
      '/audit',
    ];

    const warnings: Record<string, string[]> = {};
    for (const u of urls) {
      const seen: string[] = [];
      const handler = (msg: ConsoleMessage) => {
        const text = msg.text();
        if (text.includes('two children with the same key')) {
          seen.push(text);
        }
      };
      page.on('console', handler);
      try {
        await page.goto(abs(u));
        await page.waitForLoadState('networkidle');
      } finally {
        page.off('console', handler);
      }
      if (seen.length > 0) warnings[u] = seen;
    }

    const total = Object.values(warnings).reduce((a, b) => a + b.length, 0);
    const notes = total === 0
      ? [`Visited ${urls.length} pages, no duplicate-key warnings observed`]
      : Object.entries(warnings).map(
          ([u, msgs]) => `${u}: ${msgs.length} warning(s)`,
        );

    recordIssue({
      id: '#5',
      title: 'React duplicate key warnings',
      status: total === 0 ? 'pass' : 'fail',
      notes,
    });

    expect(total).toBe(0);
  });

  test('#6 API Metrics empty state surfaces curl snippet + Playground link', async ({
    page,
  }) => {
    await page.goto(abs('/developer/metrics'));
    await page.waitForLoadState('networkidle');

    const emptyState = page.locator('[data-testid="metrics-empty-applications"]');
    const playgroundLink = page.locator(
      '[data-testid="metrics-empty-playground-link"]',
    );

    let curlCount = 0;
    let playgroundHref: string | null = null;
    let snippetSeen = '';
    const emptyExists = (await emptyState.count()) > 0;
    if (emptyExists) {
      curlCount = await emptyState.locator('code').count();
      playgroundHref = await playgroundLink.getAttribute('href').catch(() => null);
      snippetSeen = (await emptyState.locator('code').first().textContent()) ?? '';
    }

    const hasCurl = snippetSeen.includes('/api/v2/developer/applications');
    const linksToPlayground = playgroundHref === '/developer/playground';
    const ok = emptyExists && hasCurl && linksToPlayground;

    recordIssue({
      id: '#6',
      title: 'API Metrics empty state',
      status: ok ? 'pass' : 'fail',
      notes: [
        `empty-state element found: ${emptyExists}`,
        `code blocks: ${curlCount}`,
        `curl snippet references applications endpoint: ${hasCurl}`,
        `Playground link href: ${playgroundHref}`,
      ],
    });

    expect(emptyExists).toBe(true);
    expect(hasCurl).toBe(true);
    expect(linksToPlayground).toBe(true);
  });

  // Round 2 — slug aliases the dogfood agent inferred from sidebar
  // labels (e.g. /aip-threads, /explorer/iotDemo/query-builder). Each
  // should redirect to its real canonical route without "No routes
  // matched" warnings.
  test('round2 #1-#4 slug aliases redirect to canonical routes', async ({ page }) => {
    const aliases: Array<{ from: string; expectPath: RegExp; testid: string }> = [
      { from: '/aip-threads', expectPath: /\/threads$/, testid: 'threads-page' },
      { from: '/api-playground', expectPath: /\/developer\/playground$/, testid: 'playground-page' },
      { from: '/api-metrics', expectPath: /\/developer\/metrics$/, testid: 'metrics-page' },
      { from: '/schema-inference', expectPath: /\/schema\/infer$/, testid: 'schema-inference-page' },
    ];
    const notes: string[] = [];
    let allOk = true;
    for (const a of aliases) {
      const console = captureConsole(page);
      await page.goto(abs(a.from));
      await page.waitForLoadState('networkidle');
      const noMatch = console.messages.filter((m) => m.text.includes('No routes matched'));
      const url = page.url();
      const testidCount = await page.locator(`[data-testid="${a.testid}"]`).count();
      console.detach();
      const ok = noMatch.length === 0 && a.expectPath.test(new URL(url).pathname);
      if (!ok) allOk = false;
      notes.push(`${a.from} → ${url} (testid=${a.testid} count=${testidCount}, no-match=${noMatch.length})`);
    }
    recordIssue({
      id: 'round2 #1-#4',
      title: 'Slug aliases (/aip-threads, /api-playground, /api-metrics, /schema-inference)',
      status: allOk ? 'pass' : 'fail',
      notes,
    });
    expect(allOk).toBe(true);
  });

  test('round2 #5 explorer sub-route slugs redirect to canonical routes', async ({ page }) => {
    const aliases: Array<{ from: string; expectPath: RegExp }> = [
      { from: `/explorer/${ONTOLOGY}/query-builder`, expectPath: new RegExp(`/objectsets/${ONTOLOGY}$`) },
      { from: `/explorer/${ONTOLOGY}/quiver-ts`, expectPath: new RegExp(`/quiver/${ONTOLOGY}$`) },
      { from: `/explorer/${ONTOLOGY}/import-data`, expectPath: new RegExp(`/import/${ONTOLOGY}$`) },
      { from: `/explorer/${ONTOLOGY}/approvals`, expectPath: new RegExp(`/approvals/${ONTOLOGY}$`) },
      { from: `/explorer/${ONTOLOGY}/action-history`, expectPath: new RegExp(`/actions/${ONTOLOGY}/history$`) },
      { from: `/explorer/${ONTOLOGY}/saga-jobs`, expectPath: new RegExp(`/actions/${ONTOLOGY}/jobs$`) },
      { from: `/explorer/${ONTOLOGY}/querytypes`, expectPath: new RegExp(`/queries/${ONTOLOGY}$`) },
      { from: `/explorer/${ONTOLOGY}/automation`, expectPath: new RegExp(`/automation/${ONTOLOGY}$`) },
      { from: `/explorer/${ONTOLOGY}/proposals`, expectPath: new RegExp(`/proposals/${ONTOLOGY}$`) },
    ];
    const notes: string[] = [];
    let allOk = true;
    for (const a of aliases) {
      const console = captureConsole(page);
      await page.goto(abs(a.from));
      await page.waitForLoadState('networkidle');
      const noMatch = console.messages.filter((m) => m.text.includes('No routes matched'));
      const url = page.url();
      const explorerCount = await page.locator('[data-testid="explorer-page"]').count();
      console.detach();
      const ok = noMatch.length === 0 && a.expectPath.test(new URL(url).pathname) && explorerCount === 0;
      if (!ok) allOk = false;
      notes.push(`${a.from} → ${url} (explorer-page=${explorerCount}, no-match=${noMatch.length})`);
    }
    recordIssue({
      id: 'round2 #5',
      title: 'Explorer sub-route slugs no longer render Schema Graph',
      status: allOk ? 'pass' : 'fail',
      notes,
    });
    expect(allOk).toBe(true);
  });

  test('round2 #6 explorer admin sub-route slugs redirect to /admin/:ontology/*', async ({ page }) => {
    const aliases: Array<{ from: string; expectPath: RegExp }> = [
      { from: `/explorer/${ONTOLOGY}/admin/object-types`, expectPath: new RegExp(`/admin/${ONTOLOGY}/objectTypes$`) },
      { from: `/explorer/${ONTOLOGY}/admin/link-types`, expectPath: new RegExp(`/admin/${ONTOLOGY}/linkTypes$`) },
      { from: `/explorer/${ONTOLOGY}/admin/action-types`, expectPath: new RegExp(`/admin/${ONTOLOGY}/actionTypes$`) },
      { from: `/explorer/${ONTOLOGY}/admin/interfaces`, expectPath: new RegExp(`/admin/${ONTOLOGY}/interfaces$`) },
      { from: `/explorer/${ONTOLOGY}/admin/value-types`, expectPath: new RegExp(`/admin/${ONTOLOGY}/valueTypes$`) },
      { from: `/explorer/${ONTOLOGY}/admin/schema-graph`, expectPath: new RegExp(`/admin/${ONTOLOGY}/graph$`) },
      { from: `/explorer/${ONTOLOGY}/admin/history`, expectPath: new RegExp(`/admin/${ONTOLOGY}/history$`) },
      { from: `/explorer/${ONTOLOGY}/admin/saga-dlq`, expectPath: new RegExp(`/admin/${ONTOLOGY}/saga-dlq$`) },
      { from: `/explorer/${ONTOLOGY}/admin/security`, expectPath: new RegExp(`/admin/${ONTOLOGY}/security$`) },
    ];
    const notes: string[] = [];
    let allOk = true;
    for (const a of aliases) {
      const console = captureConsole(page);
      await page.goto(abs(a.from));
      await page.waitForLoadState('networkidle');
      const noMatch = console.messages.filter((m) => m.text.includes('No routes matched'));
      const url = page.url();
      console.detach();
      const ok = noMatch.length === 0 && a.expectPath.test(new URL(url).pathname);
      if (!ok) allOk = false;
      notes.push(`${a.from} → ${url} (no-match=${noMatch.length})`);
    }
    recordIssue({
      id: 'round2 #6',
      title: 'Explorer admin sub-route slugs redirect to /admin/:ontology/*',
      status: allOk ? 'pass' : 'fail',
      notes,
    });
    expect(allOk).toBe(true);
  });

  test('round2 unknown URLs render NotFound instead of blank', async ({ page }) => {
    const console = captureConsole(page);
    await page.goto(abs('/this-route-does-not-exist'));
    await page.waitForLoadState('networkidle');
    const notFoundCount = await page.locator('[data-testid="not-found-page"]').count();
    const noMatch = console.messages.filter((m) => m.text.includes('No routes matched'));
    console.detach();
    recordIssue({
      id: 'round2 404',
      title: 'Unknown URLs render NotFoundPage',
      status: notFoundCount > 0 ? 'pass' : 'fail',
      notes: [
        `not-found-page testid count: ${notFoundCount}`,
        `No-routes-matched warnings: ${noMatch.length}`,
      ],
    });
    expect(notFoundCount).toBeGreaterThan(0);
  });

  test.afterAll(() => {
    const date = new Date().toISOString().slice(0, 10);
    const total = results.length;
    const passed = results.filter((r) => r.status === 'pass').length;
    const lines: string[] = [];
    lines.push('# Weave Dogfood Verification Report');
    lines.push('');
    lines.push(`**测试日期:** ${date}  `);
    lines.push(`**目标:** ${BASE_URL || 'default baseURL (config)'}  `);
    lines.push(`**范围:** report.md 中 6 个问题逐一复测  `);
    lines.push(`**测试方式:** Playwright headless 浏览器，复用 dogfood 方法论`);
    lines.push('');
    lines.push('## 执行摘要');
    lines.push('');
    lines.push(`${passed}/${total} 通过。`);
    lines.push('');
    lines.push('## 问题清单');
    lines.push('');
    for (const r of results) {
      const icon = r.status === 'pass' ? '✅' : '🔴';
      lines.push(`### ${icon} ${r.id} — ${r.title}`);
      lines.push('');
      for (const n of r.notes) lines.push(`- ${n}`);
      lines.push('');
    }
    mkdirSync(dirname(REPORT_PATH), { recursive: true });
    writeFileSync(REPORT_PATH, lines.join('\n'), 'utf8');
    // eslint-disable-next-line no-console
    console.log(`\nVerification report: ${REPORT_PATH}`);
  });
});
