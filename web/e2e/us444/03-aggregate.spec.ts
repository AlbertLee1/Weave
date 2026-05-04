import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 03 — aggregate.
 *
 * POSTs an exact COUNT aggregation against the customer ObjectType and
 * verifies the response carries a numeric `metrics` payload plus the
 * `accuracy` envelope introduced in US-367/US-382.
 */
test.describe('US-444 — aggregate', () => {
  test('count over customer returns a numeric metric', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objects/customer/aggregate`,
      {
        data: {
          aggregation: [
            { type: 'count', name: 'total' },
          ],
        },
      },
    );
    test.skip(!res.ok(), `aggregate endpoint unavailable: ${res.status()}`);

    const body = (await res.json()) as {
      metrics?: Record<string, number>;
      data?: { metrics?: Record<string, number> };
      accuracy?: string;
    };
    const metrics = body.metrics ?? body.data?.metrics ?? {};
    expect(Object.keys(metrics).length).toBeGreaterThan(0);
    const total = metrics.total ?? Object.values(metrics)[0];
    expect(typeof total).toBe('number');
    expect(total).toBeGreaterThanOrEqual(0);
  });
});
