import { test, expect } from '@playwright/test';

// Dogfood Round 3 issues #6/#7/#8 — empty pages should surface a primary
// CTA so first-time visitors know how to proceed. The unit tests live next
// to each page under src/components/**/__tests__/; this spec is a thin
// integration check against the running app so a regression in the
// real-stack wiring also gets caught.
//
// Drive only with the live :5173 dev server (or DOGFOOD_BASE_URL). The
// underlying APIs are still mocked at the dev server level — when the
// list endpoint returns rows the assertion below would no-op (we only
// require the CTA to be reachable when the list is empty).

const BASE_URL = process.env.DOGFOOD_BASE_URL ?? '';
const ONTOLOGY = 'iotDemo';

function abs(path: string): string {
  return BASE_URL ? `${BASE_URL}${path}` : path;
}

interface EmptyCase {
  path: string;
  ctaLabel: RegExp;
  testid?: string;
}

const CASES: EmptyCase[] = [
  { path: '/pipelines', ctaLabel: /new pipeline/i, testid: 'pipeline-list-empty' },
  { path: '/logic-flows', ctaLabel: /new flow/i, testid: 'logic-flow-list-empty' },
  { path: `/queries/${ONTOLOGY}`, ctaLabel: /new querytype/i, testid: 'query-types-sandbox-empty' },
];

test.describe('Dogfood #6: empty pages surface a primary CTA', () => {
  for (const c of CASES) {
    test(`${c.path} shows a "${c.ctaLabel}" CTA when empty`, async ({ page }) => {
      await page.goto(abs(c.path));
      await page.waitForLoadState('networkidle');

      // When the list happens to be non-empty in dev, the empty block is
      // simply absent — record that as a soft skip rather than a failure.
      const empty = c.testid ? page.getByTestId(c.testid) : page.locator('body');
      const isEmpty = c.testid ? (await empty.count()) > 0 : true;
      test.skip(!isEmpty, `${c.path} is not in the empty branch on this run`);

      const cta = empty.getByRole('button', { name: c.ctaLabel });
      await expect(cta).toBeVisible();
    });
  }
});
