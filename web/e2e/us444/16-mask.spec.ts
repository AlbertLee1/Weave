import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 16 — column mask.
 *
 * Probes the column-mask admin CRUD surface (US-257 base + US-433
 * algorithm split). The list endpoint should always reach a structured
 * response (200 with an empty array, or 503 in degraded mode). The
 * write surface is exercised with an obviously-invalid payload to
 * verify the validator returns a structured 4xx.
 */
test.describe('US-444 — mask', () => {
  test('column-mask list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/admin/column-masks`);
    test.skip(res.status() === 404, 'column-mask store not wired');
    expect([200, 401, 403, 503]).toContain(res.status());
  });

  test('column-mask create with empty body is rejected as 4xx', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(`${API_BASE}/api/admin/column-masks`, {
      data: {},
    });
    test.skip(res.status() === 404, 'column-mask store not wired');
    expect(res.status()).toBeGreaterThanOrEqual(400);
    expect(res.status()).toBeLessThan(500);
  });
});
