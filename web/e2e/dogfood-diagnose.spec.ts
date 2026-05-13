import { test, expect, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

// Diagnose dogfood regression report for #4 and #6 by inspecting BOTH the
// Vite dev server (:5173, latest source) and the Go backend (:9117, embedded
// dist). If the embedded dist is stale, hitting :9117 will reproduce the
// original Schema Graph / blank-page behaviour from report.md while :5173
// already shows the fixed pages.

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = join(HERE, '..', '..');
const OUT_DIR = join(REPO_ROOT, 'dogfood-output', 'diagnose');

const ONTOLOGY = 'iotDemo';

interface Probe {
  label: string;
  origin: string;
  path: string;
}

const PROBES: Probe[] = [
  { label: '5173-automation', origin: 'http://localhost:5173', path: `/automation/${ONTOLOGY}` },
  { label: '5173-security',   origin: 'http://localhost:5173', path: `/admin/${ONTOLOGY}/security` },
  { label: '5173-metrics',    origin: 'http://localhost:5173', path: '/developer/metrics' },
  { label: '9117-automation', origin: 'http://localhost:9117', path: `/automation/${ONTOLOGY}` },
  { label: '9117-security',   origin: 'http://localhost:9117', path: `/admin/${ONTOLOGY}/security` },
  { label: '9117-metrics',    origin: 'http://localhost:9117', path: '/developer/metrics' },
];

async function inspect(page: Page, p: Probe) {
  const consoleErrors: string[] = [];
  page.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));
  page.on('console', (msg) => {
    if (msg.type() === 'error' || msg.type() === 'warning') {
      consoleErrors.push(`${msg.type()}: ${msg.text()}`);
    }
  });
  await page.goto(p.origin + p.path, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(2500);

  const url = page.url();
  const ownTestids: Record<string, number> = {};
  for (const id of [
    'automation-rules-page',
    'security-policies-page',
    'proposals-page',
    'explorer-page',
    'metrics-empty-applications',
  ]) {
    ownTestids[id] = await page.locator(`[data-testid="${id}"]`).count();
  }
  const h1Text = await page
    .locator('h1, h2')
    .first()
    .textContent({ timeout: 2000 })
    .catch(() => null);
  const visibleText = (
    (await page.locator('body').textContent({ timeout: 2000 }).catch(() => '')) ?? ''
  ).slice(0, 200);

  mkdirSync(OUT_DIR, { recursive: true });
  const screenshot = join(OUT_DIR, `${p.label}.png`);
  await page.screenshot({ path: screenshot }).catch(() => {});

  return {
    url,
    ownTestids,
    h1Text,
    visibleText,
    consoleErrors,
    screenshot,
  };
}

test.describe.serial('Dogfood regression diagnose (#4 + #6 across :5173 / :9117)', () => {
  test.setTimeout(120_000);
  test('probe all 6 (origin × page) and dump a diagnosis report', async ({ browser }) => {
    const results: Array<Probe & { result: Awaited<ReturnType<typeof inspect>> }> = [];
    for (const p of PROBES) {
      const ctx = await browser.newContext();
      const page = await ctx.newPage();
      const result = await inspect(page, p);
      results.push({ ...p, result });
      await ctx.close();
    }

    const lines: string[] = [];
    lines.push('# Dogfood diagnose: #4 + #6 across :5173 vs :9117');
    lines.push('');
    lines.push(`生成: ${new Date().toISOString()}`);
    lines.push('');
    for (const r of results) {
      lines.push(`## ${r.label} — ${r.origin}${r.path}`);
      lines.push('');
      lines.push(`- 最终 URL: ${r.result.url}`);
      lines.push(`- 标题: ${r.result.h1Text ?? '<none>'}`);
      lines.push('- testid 命中:');
      for (const [k, v] of Object.entries(r.result.ownTestids)) {
        lines.push(`  - ${k}: ${v}`);
      }
      lines.push(`- 截图: ${r.result.screenshot}`);
      lines.push(`- 可见文本预览: \`${r.result.visibleText.replace(/\n+/g, ' ').slice(0, 160)}\``);
      if (r.result.consoleErrors.length > 0) {
        lines.push('- 控制台:');
        for (const e of r.result.consoleErrors.slice(0, 5)) {
          lines.push(`  - ${e}`);
        }
      }
      lines.push('');
    }
    const reportPath = join(REPO_ROOT, 'dogfood-output', 'diagnose-report.md');
    writeFileSync(reportPath, lines.join('\n'), 'utf8');
    // eslint-disable-next-line no-console
    console.log(`\nDiagnose report: ${reportPath}`);

    // Sanity assertion: at least :5173 should not render explorer-page on
    // the automation / security URLs.
    expect(results.find((r) => r.label === '5173-automation')!.result.ownTestids['explorer-page']).toBe(0);
    expect(results.find((r) => r.label === '5173-security')!.result.ownTestids['explorer-page']).toBe(0);
  });
});
