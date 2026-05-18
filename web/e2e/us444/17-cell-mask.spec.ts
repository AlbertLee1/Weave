import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, loginAdmin, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 17 — cell mask.
 *
 * Mirrors spec 16 against the cell-level mask CRUD surface (US-258 +
 * US-376 CEL expression engine). List + invalid-write give us a
 * structural witness without needing a wired CEL evaluator.
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

test.describe('US-444 — cell mask', () => {
  test('cell-mask list endpoint is reachable', async ({ request }) => {
    await skipWhenBackendDown(request);
    const headers = await loginAdmin(request);

    const res = await request.get(`${API_BASE}/api/admin/cell-masks`, {
      headers,
    });
    await expectOK(res, 'cell-mask list endpoint must be wired');
    const body = (await res.json()) as { masks?: unknown[] };
    expect(Array.isArray(body.masks)).toBe(true);
  });

  test('cell-mask create with empty body is rejected as 4xx', async ({ request }) => {
    await skipWhenBackendDown(request);
    const headers = await loginAdmin(request);

    const res = await request.post(`${API_BASE}/api/admin/cell-masks`, {
      headers,
      data: {},
    });
    await expectStructuredClientError(
      res,
      'cell-mask create endpoint must be wired',
      'InvalidCellMask',
    );
  });
});
