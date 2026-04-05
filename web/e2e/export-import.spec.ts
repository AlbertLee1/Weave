import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  createPropertyViaAPI,
  createLinkTypeViaAPI,
  uniqueName,
} from './helpers';

const API_BASE = 'http://localhost:8080';

test.describe('Export / Import / Snapshots', () => {
  let ontologyApiName: string;
  let ontologyRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('exp-ont');
    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `Export Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;

    const ot1 = await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: 'product',
      displayName: 'Product',
      primaryKey: 'product_id',
    });
    const ot2 = await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: 'category',
      displayName: 'Category',
      primaryKey: 'category_id',
    });

    await createPropertyViaAPI(request, ot1.rid, {
      apiName: 'product_id',
      baseType: 'integer',
      displayName: 'Product ID',
    });
    await createPropertyViaAPI(request, ot1.rid, {
      apiName: 'name',
      baseType: 'string',
      displayName: 'Name',
    });
    await createPropertyViaAPI(request, ot2.rid, {
      apiName: 'category_id',
      baseType: 'integer',
      displayName: 'Category ID',
    });

    await createLinkTypeViaAPI(request, ontologyRid, {
      apiName: 'product_to_category',
      displayName: 'Product to Category',
      sourceObjectType: ot1.rid,
      targetObjectType: ot2.rid,
      cardinality: 'ONE_TO_MANY',
    });
  });

  test('export ontology returns complete JSON', async ({ request }) => {
    const res = await request.get(`${API_BASE}/api/admin/ontologies/${ontologyRid}/export`);
    expect(res.ok()).toBeTruthy();

    const data = await res.json();

    // Verify structure
    expect(data.ontology).toBeDefined();
    expect(data.ontology.apiName).toBe(ontologyApiName);
    expect(data.objectTypes).toBeInstanceOf(Array);
    expect(data.objectTypes.length).toBe(2);
    expect(data.linkTypes).toBeInstanceOf(Array);
    expect(data.linkTypes.length).toBeGreaterThanOrEqual(1);

    // Verify object types have properties
    const product = data.objectTypes.find((ot: { apiName: string }) => ot.apiName === 'product');
    expect(product).toBeDefined();
    expect(product.properties).toBeDefined();
    expect(product.properties.length).toBeGreaterThanOrEqual(2);
  });

  test('import ontology from exported JSON', async ({ request }) => {
    // Export first
    const exportRes = await request.get(`${API_BASE}/api/admin/ontologies/${ontologyRid}/export`);
    const exportData = await exportRes.json();

    // Modify apiName to create a new ontology
    const importName = uniqueName('imported');
    exportData.ontology.apiName = importName;
    exportData.ontology.displayName = `Imported ${importName}`;

    // Import
    const importRes = await request.post(`${API_BASE}/api/admin/ontologies/import`, {
      data: exportData,
    });
    expect(importRes.ok()).toBeTruthy();

    // Verify the new ontology exists
    const listRes = await request.get(`${API_BASE}/api/v2/ontologies`);
    const ontologies = await listRes.json();
    const imported = ontologies.data.find((o: { apiName: string }) => o.apiName === importName);
    expect(imported).toBeDefined();

    // Verify object types were imported (use RID for API call)
    const otRes = await request.get(`${API_BASE}/api/v2/ontologies/${imported.rid}/objectTypes`);
    const objectTypes = await otRes.json();
    expect(objectTypes.data.length).toBe(2);
  });

  test('create and list snapshots', async ({ request }) => {
    // Create a snapshot
    const snapRes = await request.post(
      `${API_BASE}/api/admin/ontologies/${ontologyRid}/snapshots`,
      { data: { label: 'v1.0', description: 'Initial snapshot' } },
    );
    expect(snapRes.ok()).toBeTruthy();

    const snapshot = await snapRes.json();
    expect(snapshot.version).toBe(1);
    expect(snapshot.label).toBe('v1.0');

    // List snapshots
    const listRes = await request.get(
      `${API_BASE}/api/admin/ontologies/${ontologyRid}/snapshots`,
    );
    expect(listRes.ok()).toBeTruthy();

    const snapshots = await listRes.json();
    expect(snapshots.data.length).toBeGreaterThanOrEqual(1);
    expect(snapshots.data[0].version).toBe(1);

    // Get specific snapshot
    const getRes = await request.get(
      `${API_BASE}/api/admin/ontologies/${ontologyRid}/snapshots/1`,
    );
    expect(getRes.ok()).toBeTruthy();

    const detail = await getRes.json();
    expect(detail.snapshot).toBeDefined();

    // Snapshot should contain the ontology data
    const snapData = typeof detail.snapshot === 'string' ? JSON.parse(detail.snapshot) : detail.snapshot;
    expect(snapData.objectTypes).toBeDefined();
  });

  test('search ontology resources', async ({ request }) => {
    const res = await request.get(
      `${API_BASE}/api/admin/ontologies/${ontologyRid}/search?q=product`,
    );
    expect(res.ok()).toBeTruthy();

    const data = await res.json();
    expect(data.data.length).toBeGreaterThanOrEqual(1);

    const found = data.data.find((r: { apiName: string }) => r.apiName === 'product');
    expect(found).toBeDefined();
    expect(found.resourceType).toBe('objectType');
  });
});
