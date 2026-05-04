import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, fetchJSON, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 04 — action.
 *
 * Sanity-checks the Action surface end-to-end: list ActionTypes for the
 * seeded ontology and, when at least one is present, exercise the
 * apply-by-name dispatcher path with a deliberately invalid parameter
 * payload. We expect a structured 4xx ApiError envelope (NOT a 500),
 * which proves the dispatcher reached the validator and returned the
 * canonical error wire shape.
 */
test.describe('US-444 — action', () => {
  test('actionTypes list is reachable + dispatcher returns ApiError on bad params', async ({ request }) => {
    await skipWhenBackendDown(request);

    const list = await fetchJSON<{ data?: { apiName: string }[] }>(
      request,
      `/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    );
    expect(list.body).not.toBeNull();
    const types = list.body?.data ?? [];
    if (types.length === 0) {
      test.skip(true, 'no action types in seeded northwind — nothing to dispatch against');
    }
    const apiName = types[0].apiName;

    const res = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/actions/${apiName}/apply`,
      { data: { parameters: { __us444_invalid_marker__: 1 } } },
    );
    expect(res.status()).toBeGreaterThanOrEqual(400);
    expect(res.status()).toBeLessThan(500);

    const body = (await res.json()) as { errorCode?: string; errorName?: string };
    const code = body.errorCode ?? body.errorName ?? '';
    expect(code.length).toBeGreaterThan(0);
  });
});
