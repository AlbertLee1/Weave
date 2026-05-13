import type { ApiError } from './types';
import { authedFetch } from '../auth/interceptor';
import { activeBranchFor, DEFAULT_BRANCH } from '../stores/branchStore';
import { activeAsOfFor } from '../stores/timeTravelStore';

export class ApiRequestError extends Error {
  public statusCode: number;
  public errorCode: string;
  public errorName: string;
  public errorInstanceId: string;
  public parameters?: Record<string, string>;

  constructor(error: ApiError) {
    super(`${error.errorCode}: ${error.errorName}`);
    this.name = 'ApiRequestError';
    this.statusCode = error.statusCode;
    this.errorCode = error.errorCode;
    this.errorName = error.errorName;
    this.errorInstanceId = error.errorInstanceId;
    this.parameters = error.parameters;
  }
}

// US-386: extract the ontology apiName from a request path so the branch
// store can supply ?branch= for every API call rooted at the ontology
// surface. Matches both /api/v2/ontologies/{name}/... and
// /api/admin/ontologies/{name}/... — the two surfaces every branch-scoped
// route lives on. Returns null when the path does not target an ontology
// (e.g. /api/auth/login, /api/v2/notifications), so the request is left
// untouched.
const ONTOLOGY_PATH_RE = /^\/api\/(?:v2|admin)\/ontologies\/([^/?#]+)/;

export function extractOntologyApiName(path: string): string | null {
  const m = path.match(ONTOLOGY_PATH_RE);
  if (!m) return null;
  const decoded = decodeURIComponent(m[1]);
  return decoded.length === 0 ? null : decoded;
}

// withActiveBranch injects ?branch=<active> into the path when (a) the path
// targets an ontology surface, (b) the active branch is non-default, and
// (c) the caller has not already supplied an explicit ?branch=. The
// explicit-override clause lets diagnostic / cross-branch tooling bypass
// the store-driven default without having to mutate global state first.
export function withActiveBranch(path: string): string {
  const ontologyApiName = extractOntologyApiName(path);
  if (!ontologyApiName) return path;
  const branch = activeBranchFor(ontologyApiName);
  if (branch === DEFAULT_BRANCH) return path;

  const [pathPart, queryAndHash = ''] = splitPathAndQuery(path);
  const [queryPart, hashPart] = splitQueryAndHash(queryAndHash);

  const params = new URLSearchParams(queryPart);
  if (params.has('branch')) return path;
  params.set('branch', branch);

  const nextQuery = params.toString();
  const nextHash = hashPart.length > 0 ? `#${hashPart}` : '';
  return `${pathPart}?${nextQuery}${nextHash}`;
}

// US-048: inject ?asOf=<value> for ontology-scoped paths whenever the
// time-travel store has an entry for the target ontology. Value is
// either an RFC3339 timestamp (US-223) or a `tx-...` reference (US-379)
// — the OSS LoadObjects handler accepts both shapes (handler.go:258).
// Mirrors withActiveBranch's "skip when explicit ?asOf= present" clause
// so diagnostic call sites can override without mutating global state.
export function withActiveAsOf(path: string): string {
  const ontologyApiName = extractOntologyApiName(path);
  if (!ontologyApiName) return path;
  const asOf = activeAsOfFor(ontologyApiName);
  if (asOf.length === 0) return path;

  const [pathPart, queryAndHash = ''] = splitPathAndQuery(path);
  const [queryPart, hashPart] = splitQueryAndHash(queryAndHash);

  const params = new URLSearchParams(queryPart);
  if (params.has('asOf')) return path;
  params.set('asOf', asOf);

  const nextQuery = params.toString();
  const nextHash = hashPart.length > 0 ? `#${hashPart}` : '';
  return `${pathPart}?${nextQuery}${nextHash}`;
}

function splitPathAndQuery(path: string): [string, string] {
  const qIdx = path.indexOf('?');
  if (qIdx < 0) {
    const hIdx = path.indexOf('#');
    if (hIdx < 0) return [path, ''];
    return [path.slice(0, hIdx), `#${path.slice(hIdx + 1)}`];
  }
  return [path.slice(0, qIdx), path.slice(qIdx + 1)];
}

function splitQueryAndHash(queryAndHash: string): [string, string] {
  if (queryAndHash.startsWith('#')) return ['', queryAndHash.slice(1)];
  const hIdx = queryAndHash.indexOf('#');
  if (hIdx < 0) return [queryAndHash, ''];
  return [queryAndHash.slice(0, hIdx), queryAndHash.slice(hIdx + 1)];
}

export async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const options: RequestInit = {
    method,
    headers: {
      'Content-Type': 'application/json',
    },
  };

  if (body !== undefined) {
    options.body = JSON.stringify(body);
  }

  const response = await authedFetch(withActiveAsOf(withActiveBranch(path)), options);

  if (!response.ok) {
    let errorData: Partial<ApiError>;
    try {
      errorData = await response.json();
    } catch {
      errorData = {};
    }
    throw new ApiRequestError({
      errorCode: errorData.errorCode ?? 'UNKNOWN',
      errorName: errorData.errorName ?? response.statusText,
      errorInstanceId: errorData.errorInstanceId ?? '',
      parameters: errorData.parameters,
      statusCode: response.status,
    });
  }

  const text = await response.text();
  if (!text) {
    return undefined as T;
  }

  return JSON.parse(text) as T;
}
