import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 14 — PITR.
 *
 * Verifies the dataset transaction-chain history endpoint (US-379 +
 * US-388 + US-390). The endpoint reports the chain of EditBatch tx
 * records for a dataset RID and is the wire surface that
 * `weave-cli pitr restore` walks under the hood.
 */
async function readStructuredJSON(res: APIResponse, context: string): Promise<Record<string, unknown>> {
  const text = await res.text();
  expect(text.trim().length, `${context}: empty response for ${res.status()}`).toBeGreaterThan(0);

  try {
    return JSON.parse(text) as Record<string, unknown>;
  } catch {
    throw new Error(`${context}: response must be JSON: ${res.status()} ${text}`);
  }
}

function expectErrorEnvelope(body: Record<string, unknown>, context: string): void {
  const code = body.errorCode ?? body.errorName;
  expect(typeof code, `${context}: expected errorCode or errorName`).toBe('string');
  expect((code as string).length, `${context}: empty error code`).toBeGreaterThan(0);
}

test.describe('US-444 — PITR', () => {
  test('dataset history endpoint accepts unknown rid gracefully', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/datasets/us444-unknown/history`);
    const body = await readStructuredJSON(res, 'dataset history endpoint must be wired');
    if (res.ok()) {
      expect(Array.isArray(body.transactions)).toBe(true);
      return;
    }

    expect(res.status(), `dataset history endpoint must be wired: ${res.status()} ${JSON.stringify(body)}`).toBeGreaterThanOrEqual(400);
    expect(res.status(), `dataset history endpoint must be wired: ${res.status()} ${JSON.stringify(body)}`).toBeLessThan(500);
    expectErrorEnvelope(body, 'dataset history endpoint must be wired');
  });

  test('rollback endpoint validates required query param', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(`${API_BASE}/api/v2/datasets/us444-unknown/rollback`);
    const body = await readStructuredJSON(res, 'rollback endpoint must validate missing target');

    expect(res.status(), `rollback endpoint must validate missing target: ${res.status()} ${JSON.stringify(body)}`).toBeGreaterThanOrEqual(400);
    expect(res.status(), `rollback endpoint must validate missing target: ${res.status()} ${JSON.stringify(body)}`).toBeLessThan(500);
    expectErrorEnvelope(body, 'rollback endpoint must validate missing target');
  });
});
