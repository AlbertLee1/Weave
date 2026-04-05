import { type Page, type APIRequestContext } from '@playwright/test';

const API_BASE = 'http://localhost:8080';

export interface CreatedOntology {
  rid: string;
  apiName: string;
  displayName: string;
}

/** Create an ontology via API and return it (including rid). */
export async function createOntologyViaAPI(
  request: APIRequestContext,
  input: { apiName: string; displayName: string; description?: string },
): Promise<CreatedOntology> {
  const res = await request.post(`${API_BASE}/api/admin/ontologies`, { data: input });
  if (!res.ok()) throw new Error(`Failed to create ontology: ${res.status()} ${await res.text()}`);
  return res.json();
}

/** Create an object type via API. Uses ontology RID (not apiName) in URL. */
export async function createObjectTypeViaAPI(
  request: APIRequestContext,
  ontologyRid: string,
  input: {
    apiName: string;
    displayName: string;
    primaryKey: string;
    status?: string;
    visibility?: string;
  },
) {
  const data = { status: 'EXPERIMENTAL', visibility: 'NORMAL', ...input };
  const res = await request.post(
    `${API_BASE}/api/admin/ontologies/${ontologyRid}/objectTypes`,
    { data },
  );
  if (!res.ok()) throw new Error(`Failed to create object type: ${res.status()} ${await res.text()}`);
  return res.json();
}

/** Create a property via API. */
export async function createPropertyViaAPI(
  request: APIRequestContext,
  objectTypeRid: string,
  input: { apiName: string; baseType: string; displayName?: string },
) {
  const res = await request.post(
    `${API_BASE}/api/admin/objectTypes/${objectTypeRid}/properties`,
    { data: input },
  );
  if (!res.ok()) throw new Error(`Failed to create property: ${res.status()} ${await res.text()}`);
  return res.json();
}

/** Create a link type via API. Uses ontology RID in URL. */
export async function createLinkTypeViaAPI(
  request: APIRequestContext,
  ontologyRid: string,
  input: {
    apiName: string;
    displayName: string;
    sourceObjectType: string;
    targetObjectType: string;
    cardinality: string;
  },
) {
  const data = {
    apiName: input.apiName,
    displayName: input.displayName,
    objectTypeApiName: input.sourceObjectType,
    linkedObjectTypeApiName: input.targetObjectType,
    cardinality: input.cardinality,
  };
  const res = await request.post(
    `${API_BASE}/api/admin/ontologies/${ontologyRid}/linkTypes`,
    { data },
  );
  if (!res.ok()) throw new Error(`Failed to create link type: ${res.status()} ${await res.text()}`);
  return res.json();
}

/** Create an action type via API. Uses ontology RID in URL. */
export async function createActionTypeViaAPI(
  request: APIRequestContext,
  ontologyRid: string,
  input: {
    apiName: string;
    displayName: string;
    status?: string;
    parameters?: unknown;
    rules?: unknown;
  },
) {
  const data = { status: 'EXPERIMENTAL', parameters: [], rules: [], ...input };
  const res = await request.post(
    `${API_BASE}/api/admin/ontologies/${ontologyRid}/actionTypes`,
    { data },
  );
  if (!res.ok()) throw new Error(`Failed to create action type: ${res.status()} ${await res.text()}`);
  return res.json();
}

/** Navigate to admin page and select an ontology by apiName (frontend URL). */
export async function navigateToAdmin(page: Page, ontologyApiName?: string) {
  if (ontologyApiName) {
    await page.goto(`/admin/${ontologyApiName}`);
  } else {
    await page.goto('/admin');
  }
  await page.waitForLoadState('networkidle');
}

/** Generate a unique name for test isolation. */
export function uniqueName(prefix: string): string {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`;
}
