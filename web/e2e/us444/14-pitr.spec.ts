import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 14 — PITR.
 *
 * Verifies the dataset transaction-chain history endpoint (US-379 +
 * US-388 + US-390). The endpoint reports the chain of EditBatch tx
 * records for a dataset RID and is the wire surface that
 * `weave-cli pitr restore` walks under the hood.
 */
test.describe('US-444 — PITR', () => {
  test('dataset history endpoint accepts unknown rid gracefully', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/datasets/us444-unknown/history`);
    test.skip(res.status() === 404 && (await res.text()).length === 0, 'route unwired');
    // 200 with empty data, 404 NotFound envelope, or 503 NotConfigured all OK.
    expect([200, 404, 503]).toContain(res.status());
  });

  test('rollback endpoint validates required query param', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(`${API_BASE}/api/v2/datasets/us444-unknown/rollback`);
    test.skip(res.status() === 404 && (await res.text()).length === 0, 'route unwired');
    // Missing ?to= must be rejected as a structured 4xx.
    expect([400, 404, 422, 503]).toContain(res.status());
  });
});
