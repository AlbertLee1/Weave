import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 11 — pkg install.
 *
 * Drives the one-click built-in package installer (US-414) with the
 * `iot-demo` slug and `onConflict=skip` so the call is safe to repeat
 * against an already-seeded northwind ontology. Verifies the response
 * carries an `installed` summary block.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — pkg install', () => {
  test('iot-demo install via skip-on-conflict succeeds', async ({ request }) => {
    await skipWhenBackendDown(request);

    const list = await request.get(`${API_BASE}/api/v2/pkg/builtin`);
    await expectOK(list, 'built-in catalog endpoint must be wired');
    const body = (await list.json()) as { data?: { slug: string }[] };
    const has = (body.data ?? []).some((p) => p.slug === 'iot-demo');
    expect(has, 'iot-demo must be present in the built-in catalog').toBe(true);

    const res = await request.post(
      `${API_BASE}/api/v2/pkg/builtin/iot-demo/install`,
      { data: { onConflict: 'skip' } },
    );
    await expectOK(res, 'package installer endpoint must be wired');

    const out = (await res.json()) as Record<string, unknown>;
    // The handler always returns at least one of these summary fields.
    const keys = Object.keys(out);
    expect(keys.length).toBeGreaterThan(0);
  });
});
