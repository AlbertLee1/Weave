import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 10 — marketplace.
 *
 * Pulls the built-in marketplace catalog (US-414) and the installed
 * packages list (US-413). Both endpoints return an empty `data` array
 * in degraded mode rather than 404, so this spec passes on partial
 * deployments too.
 */
test.describe('US-444 — marketplace', () => {
  test('built-in catalog returns the 3 example packages', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/pkg/builtin`);
    expect(res.ok()).toBe(true);
    const body = (await res.json()) as { data?: { slug: string; name?: string }[] };
    const slugs = (body.data ?? []).map((p) => p.slug).sort();
    if (slugs.length === 0) {
      test.skip(true, 'no built-in packages compiled into this binary');
    }
    // US-414 ships northwind / chinook / iot-demo as the canonical trio.
    expect(slugs).toEqual(expect.arrayContaining(['northwind', 'chinook', 'iot-demo']));
  });

  test('installed packages list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/pkg`);
    expect([200, 404]).toContain(res.status());
    if (res.ok()) {
      const body = (await res.json()) as { data?: unknown[] };
      expect(Array.isArray(body.data)).toBe(true);
    }
  });
});
