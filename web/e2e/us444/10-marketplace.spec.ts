import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 10 — marketplace.
 *
 * Pulls the built-in marketplace catalog (US-414) and the installed
 * packages list (US-413). A reachable full-stack backend must expose
 * both package lifecycle endpoints.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — marketplace', () => {
  test('built-in catalog returns the 3 example packages', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/pkg/builtin`);
    await expectOK(res, 'built-in catalog endpoint must be wired');
    const body = (await res.json()) as { data?: { slug: string; name?: string }[] };
    const slugs = (body.data ?? []).map((p) => p.slug).sort();
    expect(
      slugs,
      'built-in catalog must include seeded packages',
    ).toEqual(expect.arrayContaining(['northwind', 'chinook', 'iot-demo']));
  });

  test('installed packages list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/pkg`);
    await expectOK(res, 'installed packages list endpoint must be wired');
    const body = (await res.json()) as { data?: unknown[] };
    expect(Array.isArray(body.data)).toBe(true);
  });
});
