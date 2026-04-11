import { type Page, type APIRequestContext } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

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

/**
 * Navigate to the v2 Browser page for a given ontology + object type.
 * Route: /browser/:ontology/:objectType
 */
export async function navigateToBrowser(
  page: Page,
  ontologyApiName: string,
  objectTypeApiName: string,
) {
  await page.goto(`/browser/${ontologyApiName}/${objectTypeApiName}`);
  await page.waitForLoadState('domcontentloaded');
}

/** Generate a unique name for test isolation. */
export function uniqueName(prefix: string): string {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`;
}
