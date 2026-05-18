import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, loginAdmin, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 16 — column mask.
 *
 * Probes the column-mask admin CRUD surface (US-257 base + US-433
 * algorithm split). The list endpoint should always reach a structured
 * response. The write surface is exercised with an obviously-invalid
 * payload to verify the validator returns a structured 4xx.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

async function expectStructuredClientError(
  res: APIResponse,
  context: string,
  errorName: string,
): Promise<void> {
  expect(
    [404, 503].includes(res.status()),
    `${context}: ${res.status()} ${await res.text()}`,
  ).toBe(false);
  expect(res.status()).toBeGreaterThanOrEqual(400);
  expect(res.status()).toBeLessThan(500);

  const body = (await res.json()) as { errorCode?: string; errorName?: string };
  expect(body.errorCode?.length ?? 0).toBeGreaterThan(0);
  expect(body.errorName).toBe(errorName);
}

test.describe('US-444 — mask', () => {
  test('column-mask list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);
    const headers = await loginAdmin(request);

    const res = await request.get(`${API_BASE}/api/admin/column-masks`, {
      headers,
    });
    await expectOK(res, 'column-mask list endpoint must be wired');
    const body = (await res.json()) as { masks?: unknown[] };
    expect(Array.isArray(body.masks)).toBe(true);
  });

  test('column-mask create with empty body is rejected as 4xx', async ({ request }) => {
    await skipWhenBackendDown(request);
    const headers = await loginAdmin(request);

    const res = await request.post(`${API_BASE}/api/admin/column-masks`, {
      headers,
      data: {},
    });
    await expectStructuredClientError(
      res,
      'column-mask create endpoint must be wired',
      'InvalidColumnMask',
    );
  });
});
