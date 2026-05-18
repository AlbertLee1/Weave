import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, fetchJSON, skipWhenBackendDown } from './helpers';

const ACTION_API_NAME = 'createCustomer';
const INVALID_PARAMETERS = { __us444_invalid_marker__: 1 };

/**
 * US-444 spec 04 — action.
 *
 * Sanity-checks the Action surface end-to-end: the full Northwind seed must
 * expose a known ActionType, then the apply-by-name dispatcher path must return
 * a structured 4xx ApiError envelope for a deliberately invalid payload.
 */
test.describe('US-444 — action', () => {
  test('seeded action catalog includes createCustomer + dispatcher returns ApiError on bad params', async ({ request }) => {
    await skipWhenBackendDown(request);

    const list = await fetchJSON<{ data?: { apiName: string }[] }>(
      request,
      `/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    );
    expect(list.body).not.toBeNull();
    const actionNames = (list.body?.data ?? []).map((type) => type.apiName);
    expect(
      actionNames,
      `${ACTION_API_NAME} action type missing from seeded northwind action catalog`,
    ).toContain(ACTION_API_NAME);

    const res = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/${ACTION_API_NAME}/apply`,
      { data: { parameters: INVALID_PARAMETERS } },
    );
    const responseText = await res.text();
    expect(res.status(), responseText).toBeGreaterThanOrEqual(400);
    expect(res.status(), responseText).toBeLessThan(500);

    const body = JSON.parse(responseText) as { errorCode?: string; errorName?: string };
    const code = body.errorCode ?? body.errorName ?? '';
    expect(code, responseText).not.toBe('');
  });
});
