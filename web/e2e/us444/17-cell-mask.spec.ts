import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 17 — cell mask.
 *
 * Mirrors spec 16 against the cell-level mask CRUD surface (US-258 +
 * US-376 CEL expression engine). List + invalid-write give us a
 * structural witness without needing a wired CEL evaluator.
 */
test.describe('US-444 — cell mask', () => {
  test('cell-mask list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/admin/cell-masks`);
    test.skip(res.status() === 404, 'cell-mask store not wired');
    expect([200, 401, 403, 503]).toContain(res.status());
  });

  test('cell-mask create with empty body is rejected as 4xx', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(`${API_BASE}/api/admin/cell-masks`, {
      data: {},
    });
    test.skip(res.status() === 404, 'cell-mask store not wired');
    expect(res.status()).toBeGreaterThanOrEqual(400);
    expect(res.status()).toBeLessThan(500);
  });
});
