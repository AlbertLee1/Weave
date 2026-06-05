import { request, ApiRequestError, withActiveBranch } from './client';
import { authedFetch } from '../auth/interceptor';
import type { ApiError } from './types';

// OntologyExport is the JSON payload returned by
// GET /api/v2/ontologies/{ontologyApiName}/export — a complete, self-contained
// dump of every metadata entity that makes up an ontology definition. Each
// collection is optional on the wire (older snapshots may omit empty
// sections), so the UI treats a missing key as an empty list when it
// summarizes counts.
export interface OntologyExport {
  ontology: Record<string, unknown>;
  objectTypes?: unknown[];
  linkTypes?: unknown[];
  actionTypes?: unknown[];
  interfaces?: unknown[];
  sharedProperties?: unknown[];
  valueTypes?: unknown[];
  typeGroups?: unknown[];
  functions?: unknown[];
  queryTypes?: unknown[];
}

// SdkLanguage enumerates the client-SDK targets the sdkgen endpoint accepts.
// `lang` is required and must be one of these three; the backend answers 400
// otherwise.
export type SdkLanguage = 'ts' | 'python' | 'go';

// exportOntology fetches the full ontology definition as JSON. A 404 (ontology
// not found) surfaces as an ApiRequestError via the shared request() client.
export function exportOntology(ontology: string): Promise<OntologyExport> {
  return request<OntologyExport>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/export`,
  );
}

// generateSdk POSTs to the sdkgen endpoint and returns the generated SDK as a
// zip Blob. It deliberately bypasses the JSON request() client because the
// response body is binary (application/zip), not JSON. On a non-2xx response it
// throws an ApiRequestError so call sites can route the failure through
// describeApiError exactly like every other API error — the error body is read
// as JSON when present (the backend returns the standard error envelope for
// 400s) and falls back to the HTTP status text otherwise.
export async function generateSdk(
  ontology: string,
  lang: SdkLanguage,
): Promise<Blob> {
  // sdkgen is ontology-RID-scoped, so honor the active branch the same way
  // request() does for exportOntology (and mergeBranch/rebaseBranch do for
  // their authedFetch paths) — otherwise the SDK would be generated from the
  // default branch's schema while the page is viewing a non-default branch.
  const res = await authedFetch(
    withActiveBranch(
      `/api/v2/ontologies/${encodeURIComponent(ontology)}/sdkgen?lang=${encodeURIComponent(lang)}`,
    ),
    { method: 'POST' },
  );

  if (!res.ok) {
    let errorData: Partial<ApiError> = {};
    try {
      errorData = (await res.json()) as Partial<ApiError>;
    } catch {
      errorData = {};
    }
    throw new ApiRequestError({
      errorCode: errorData.errorCode ?? 'UNKNOWN',
      errorName: errorData.errorName ?? res.statusText,
      errorInstanceId: errorData.errorInstanceId ?? '',
      parameters: errorData.parameters,
      statusCode: res.status,
    });
  }

  return res.blob();
}
