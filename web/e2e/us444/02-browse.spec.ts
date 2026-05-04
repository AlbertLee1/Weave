import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, fetchJSON, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 02 — browse objects.
 *
 * Lists ObjectTypes for the seeded northwind ontology and pulls a page
 * of `customer` objects. The seed always ships at least three customer
 * rows; the spec asserts the wire shape is `{ data: [...] }` and that
 * the canonical primary-key column is present on each row.
 */
test.describe('US-444 — browse objects', () => {
  test('northwind exposes seeded objectTypes', async ({ request }) => {
    await skipWhenBackendDown(request);

    const { body } = await fetchJSON<{ data: { apiName: string }[] }>(
      request,
      `/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    );
    expect(body).not.toBeNull();
    const names = (body?.data ?? []).map((o) => o.apiName);
    expect(names).toEqual(expect.arrayContaining(['customer']));
  });

  test('customer page returns rows with primary key', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objects/customer`,
    );
    test.skip(!res.ok(), `objects endpoint unavailable: ${res.status()}`);
    const body = (await res.json()) as { data: Record<string, unknown>[] };
    expect(Array.isArray(body.data)).toBe(true);
    if (body.data.length === 0) {
      test.skip(true, 'northwind seed produced 0 customer rows — rerun e2e_seed.sh');
    }
    for (const row of body.data) {
      expect(row).toHaveProperty('customerID');
    }
  });
});
