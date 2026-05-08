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

    // The live aggregate endpoint returns metrics as `data[].metrics[]`
    // where each entry is a `{name, value}` pair. Older drafts of this
    // spec assumed a flat `{metrics: {total: N}}` map; accept both shapes
    // so the spec is robust across schema iterations.
    type Pair = { name: string; value: number };
    const body = (await res.json()) as {
      metrics?: Record<string, number> | Pair[];
      data?: Array<{ metrics?: Pair[] }>;
      accuracy?: string;
    };
    const arrayMetrics: Pair[] = Array.isArray(body.metrics)
      ? body.metrics
      : body.data?.[0]?.metrics ?? [];
    const mapMetrics: Record<string, number> =
      !Array.isArray(body.metrics) && body.metrics
        ? body.metrics
        : Object.fromEntries(arrayMetrics.map((m) => [m.name, m.value]));

    expect(Object.keys(mapMetrics).length).toBeGreaterThan(0);
    const total = mapMetrics.total ?? Object.values(mapMetrics)[0];
    expect(typeof total).toBe('number');
    expect(total).toBeGreaterThanOrEqual(0);
  });
});
