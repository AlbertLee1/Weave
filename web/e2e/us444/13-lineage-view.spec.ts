import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 13 — lineage view.
 *
 * Targets the column-level lineage surfaces (US-377). Asks for the
 * impact list of a synthetic dataset/column pair — the endpoint MUST
 * return a structured response (empty `impacted` array is valid) rather
 * than 404/5xx.
 */
type PropertyMetadata = { rid?: string };
type MetadataProperties = PropertyMetadata[] | Record<string, PropertyMetadata>;

type ObjectTypeFullMetadata = {
  properties?: MetadataProperties;
  objectType?: { properties?: MetadataProperties };
};

type PropertyLineageResponse = {
  propertyRid?: string;
  upstream?: unknown[];
  truncated?: boolean;
};

type DatasetColumnImpactResponse = {
  datasetRid?: string;
  column?: string;
  impacted?: unknown[];
  truncated?: boolean;
};

async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

function propertyList(meta: ObjectTypeFullMetadata): PropertyMetadata[] {
  const props = meta.properties ?? meta.objectType?.properties;
  if (!props) return [];
  if (Array.isArray(props)) return props;
  return Object.values(props);
}

test.describe('US-444 — lineage view', () => {
  test('property lineage endpoint is reachable for seeded property', async ({ request }) => {
    await skipWhenBackendDown(request);

    const otRes = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objectTypes/customer/fullMetadata?preview=true`,
    );
    await expectOK(otRes, 'customer fullMetadata is required for lineage probing');

    const meta = (await otRes.json()) as ObjectTypeFullMetadata;
    const propRid = propertyList(meta).find((prop) => prop.rid)?.rid;
    if (!propRid) {
      throw new Error('customer fullMetadata did not surface any property RID for lineage probing');
    }

    const res = await request.get(`${API_BASE}/api/v2/lineage/property/${propRid}`);
    await expectOK(res, 'property lineage endpoint must be wired');

    const body = (await res.json()) as PropertyLineageResponse;
    expect(body.propertyRid).toBe(propRid);
    expect(Array.isArray(body.upstream)).toBe(true);
    expect(typeof body.truncated).toBe('boolean');
  });

  test('dataset-column impact endpoint accepts query params', async ({ request }) => {
    await skipWhenBackendDown(request);

    const dataset = 'ds-us444';
    const column = 'col';
    const res = await request.get(
      `${API_BASE}/api/v2/lineage/dataset-columns/impact?dataset=${dataset}&column=${column}`,
    );
    await expectOK(res, 'dataset-column impact endpoint must be wired');

    const body = (await res.json()) as DatasetColumnImpactResponse;
    expect(body.datasetRid).toBe(dataset);
    expect(body.column).toBe(column);
    expect(Array.isArray(body.impacted)).toBe(true);
    expect(typeof body.truncated).toBe('boolean');
  });
});
