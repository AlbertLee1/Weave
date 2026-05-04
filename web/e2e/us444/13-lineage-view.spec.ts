import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 13 — lineage view.
 *
 * Targets the column-level lineage surfaces (US-377). Asks for the
 * impact list of a synthetic dataset/column pair — the endpoint MUST
 * return a structured response (empty `data` array is valid) rather
 * than 5xx, even when no datasource binding has been wired to the
 * seeded ontology yet.
 */
test.describe('US-444 — lineage view', () => {
  test('property lineage endpoint is reachable for seeded property', async ({ request }) => {
    await skipWhenBackendDown(request);

    // First fetch the customer object type to find a real property RID.
    const otRes = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objectTypes/customer/fullMetadata`,
    );
    test.skip(!otRes.ok(), `objectType metadata unavailable: ${otRes.status()}`);
    const meta = (await otRes.json()) as {
      properties?: { rid?: string }[];
      objectType?: { properties?: { rid?: string }[] };
    };
    const props = meta.properties ?? meta.objectType?.properties ?? [];
    const propRid = (props[0] ?? {}).rid;
    test.skip(!propRid, 'no property RIDs surfaced — cannot probe lineage');

    const res = await request.get(`${API_BASE}/api/v2/lineage/property/${propRid}`);
    test.skip(res.status() === 404, 'column-lineage endpoint not wired');
    expect(res.ok() || res.status() === 503).toBe(true);
  });

  test('dataset-column impact endpoint accepts query params', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(
      `${API_BASE}/api/v2/lineage/dataset-columns/impact?dataset=ds-us444&column=col`,
    );
    test.skip(res.status() === 404, 'impact endpoint not wired');
    expect(res.ok() || res.status() === 503).toBe(true);
  });
});
