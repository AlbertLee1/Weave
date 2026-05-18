import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 05 — saga.
 *
 * Hits the multi-step Action saga apply path (US-369) with an empty
 * step list and a fresh idempotency key. The handler should reject the
 * empty step list with a structured 4xx — not a 500. Also probes the
 * saga DLQ list endpoint (US-440) to confirm it is wired.
 */
async function expectWiredClientError(res: APIResponse, context: string): Promise<void> {
  expect(
    [404, 503].includes(res.status()),
    `${context}: ${res.status()} ${await res.text()}`,
  ).toBe(false);
  expect(res.status()).toBeGreaterThanOrEqual(400);
  expect(res.status()).toBeLessThan(500);
}

async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

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
    await expectWiredClientError(res, 'saga apply endpoint must be wired');

    const body = (await res.json()) as { errorCode?: string; errorName?: string };
    const code = body.errorCode ?? body.errorName ?? '';
    expect(code.length).toBeGreaterThan(0);
  });

  test('saga DLQ list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/saga/dlq`,
    );
    await expectOK(res, 'saga DLQ endpoint must be wired');
    const body = (await res.json()) as { entries?: unknown[] };
    expect(Array.isArray(body.entries)).toBe(true);
  });
});
