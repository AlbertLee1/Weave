import { type APIRequestContext } from '@playwright/test';
import { ontologyPayload } from './dataFactory';

const API_BASE = 'http://localhost:9117';

export interface SeededOntology {
  rid: string;
  apiName: string;
  displayName: string;
}

export interface SeedOntologyOptions {
  apiName?: string;
  displayName?: string;
  description?: string;
  /** Auth headers from `signIn()`; omit under AUTH_MODE=dev. */
  authHeaders?: Record<string, string>;
  /**
   * If the apiName already exists, return the existing row instead of
   * failing with 409. Defaults to true so reruns against the baseline
   * Northwind fixture don't fight the seed script.
   */
  reuseExisting?: boolean;
}

/**
 * Bring an ontology into existence and return its rid / apiName / displayName.
 *
 * Tries a GET first when `reuseExisting` is on (default) so specs can be
 * pointed at the `scripts/e2e-setup.sh` baseline by passing
 * `apiName: 'northwind'` without re-creating it. Falls through to the admin
 * create endpoint when the row is missing.
 */
export async function seedOntology(
  request: APIRequestContext,
  options: SeedOntologyOptions = {},
): Promise<SeededOntology> {
  const reuseExisting = options.reuseExisting ?? true;
  const payload = ontologyPayload({
    apiName: options.apiName,
    displayName: options.displayName,
    description: options.description,
  });
  const headers = options.authHeaders ?? {};

  if (reuseExisting) {
    const existing = await request.get(
      `${API_BASE}/api/v2/ontologies/${payload.apiName}`,
      { headers },
    );
    if (existing.ok()) {
      return (await existing.json()) as SeededOntology;
    }
  }

  const res = await request.post(`${API_BASE}/api/admin/ontologies`, {
    data: payload,
    headers,
  });
  if (!res.ok()) {
    throw new Error(
      `seedOntology(${payload.apiName}) failed: ${res.status()} ${await res.text()}`,
    );
  }
  return (await res.json()) as SeededOntology;
}
