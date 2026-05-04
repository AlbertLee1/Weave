import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 05 — saga.
 *
 * Hits the multi-step Action saga apply path (US-369) with an empty
 * step list and a fresh idempotency key. The handler should reject the
 * empty step list with a structured 4xx — not a 500. Also probes the
 * saga DLQ list endpoint (US-440) to confirm it is wired.
 */
test.describe('US-444 — saga', () => {
  test('applySaga rejects empty step list with structured error', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/applySaga`,
      {
        data: {
          idempotencyKey: `us444-${Date.now()}`,
          steps: [],
        },
      },
    );
    test.skip(res.status() === 404, 'saga endpoint not wired in this deployment');
    expect(res.status()).toBeGreaterThanOrEqual(400);
    expect(res.status()).toBeLessThan(500);

    const body = (await res.json()) as { errorCode?: string; errorName?: string };
    const code = body.errorCode ?? body.errorName ?? '';
    expect(code.length).toBeGreaterThan(0);
  });

  test('saga DLQ list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/saga/dlq`,
    );
    test.skip(res.status() === 404, 'saga DLQ endpoint not wired');
    expect(res.ok() || res.status() === 503).toBe(true);
  });
});
