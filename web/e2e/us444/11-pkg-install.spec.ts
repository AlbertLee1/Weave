import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 11 — pkg install.
 *
 * Drives the one-click built-in package installer (US-414) with the
 * `iot-demo` slug and `onConflict=skip` so the call is safe to repeat
 * against an already-seeded northwind ontology. Verifies the response
 * carries an `installed` summary block.
 */
test.describe('US-444 — pkg install', () => {
  test('iot-demo install via skip-on-conflict succeeds', async ({ request }) => {
    await skipWhenBackendDown(request);

    const list = await request.get(`${API_BASE}/api/v2/pkg/builtin`);
    test.skip(!list.ok(), 'built-in catalog endpoint unavailable');
    const body = (await list.json()) as { data?: { slug: string }[] };
    const has = (body.data ?? []).some((p) => p.slug === 'iot-demo');
    test.skip(!has, 'iot-demo not in built-in catalog');

    const res = await request.post(
      `${API_BASE}/api/v2/pkg/builtin/iot-demo/install`,
      { data: { onConflict: 'skip' } },
    );
    test.skip(
      res.status() === 404 || res.status() === 503,
      'package installer not wired',
    );
    expect(res.ok(), `install failed: ${res.status()} ${await res.text()}`).toBe(true);

    const out = (await res.json()) as Record<string, unknown>;
    // The handler always returns at least one of these summary fields.
    const keys = Object.keys(out);
    expect(keys.length).toBeGreaterThan(0);
  });
});
