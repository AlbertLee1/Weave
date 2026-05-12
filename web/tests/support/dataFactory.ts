/**
 * Deterministic test-data factories for the Playwright BDD suite.
 *
 * Kept dependency-free per US-002 acceptance criteria ("自建，不引入新依赖").
 * Every factory returns a fresh value each call so the same `seedOntology`
 * fixture can be invoked repeatedly inside one scenario without collisions.
 *
 * For ad-hoc values reach for `uniqueName`; for full create-payloads reach
 * for the named builders so spec authors don't need to remember which
 * fields the admin API requires.
 */

let counter = 0;

/** Generate an identifier with the given prefix, unique across reruns. */
export function uniqueName(prefix: string): string {
  counter += 1;
  return `${prefix}_${Date.now()}_${counter}_${Math.random().toString(36).slice(2, 7)}`;
}

export interface OntologyPayload {
  apiName: string;
  displayName: string;
  description?: string;
}

export function ontologyPayload(overrides: Partial<OntologyPayload> = {}): OntologyPayload {
  const apiName = overrides.apiName ?? uniqueName('bdd_ont');
  return {
    apiName,
    displayName: overrides.displayName ?? `BDD Ontology ${apiName}`,
    description: overrides.description,
  };
}

export type ObjectTypeStatus = 'EXPERIMENTAL' | 'ACTIVE' | 'DEPRECATED';
export type ObjectTypeVisibility = 'NORMAL' | 'HIDDEN';

export interface ObjectTypePayload {
  apiName: string;
  displayName: string;
  primaryKey: string;
  status: ObjectTypeStatus;
  visibility: ObjectTypeVisibility;
}

export function objectTypePayload(overrides: Partial<ObjectTypePayload> = {}): ObjectTypePayload {
  const apiName = overrides.apiName ?? uniqueName('bdd_type');
  return {
    apiName,
    displayName: overrides.displayName ?? `BDD Type ${apiName}`,
    primaryKey: overrides.primaryKey ?? 'id',
    status: overrides.status ?? 'EXPERIMENTAL',
    visibility: overrides.visibility ?? 'NORMAL',
  };
}
