import { request } from './client';
import type {
  ObjectPage,
  ObjectHistoryResponse,
  ObjectActivityResponse,
  WireObject,
  WhereClause,
  CountObjectsResponse,
} from './types';

export interface ListObjectsParams {
  ontologyApiName: string;
  objectType: string;
  pageSize?: number;
  pageToken?: string;
  orderBy?: string;
}

export function listObjects(params: ListObjectsParams): Promise<ObjectPage> {
  const query = new URLSearchParams();
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.pageToken) query.set('pageToken', params.pageToken);
  if (params.orderBy) query.set('orderBy', params.orderBy);
  const qs = query.toString();
  return request<ObjectPage>(
    'GET',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}${qs ? `?${qs}` : ''}`,
  );
}

export interface SearchObjectsParams {
  ontologyApiName: string;
  objectType: string;
  where?: WhereClause;
  pageSize?: number;
  pageToken?: string;
  orderBy?: { field: string; direction?: 'asc' | 'desc' };
  highlight?: { fields?: string[]; style?: string };
  select: string[];
  facets?: string[];
  // Bleve fuzzy-matching edit distance (Levenshtein). Sent to the backend as
  // the `?fuzziness=` query param on /search; valid values are 0, 1 or 2
  // (pkg/oss/where.MaxFuzziness == 2). 0 disables fuzzy matching, matching
  // the handler's "fuzziness overrides body" convention. Omitted entirely
  // when undefined so the backend keeps its default exact-match behaviour.
  fuzziness?: number;
}

export function searchObjects(params: SearchObjectsParams): Promise<ObjectPage> {
  const query = new URLSearchParams();
  if (params.fuzziness !== undefined) {
    query.set('fuzziness', String(params.fuzziness));
  }
  const qs = query.toString();
  return request<ObjectPage>(
    'POST',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}/search${qs ? `?${qs}` : ''}`,
    {
      where: params.where,
      pageSize: params.pageSize,
      pageToken: params.pageToken,
      // Backend contract is Foundry's SearchOrderByV2:
      // {fields: [{field, direction}]}. Lower the caller-friendly single
      // {field, direction} param here so every call site sorts for real —
      // the bare shape used to be dropped by the server as an empty
      // ordering (HTTP 200, unsorted data).
      orderBy: params.orderBy
        ? {
            fields: [
              {
                field: params.orderBy.field,
                direction: params.orderBy.direction ?? 'asc',
              },
            ],
          }
        : undefined,
      highlight: params.highlight,
      select: params.select,
      facets: params.facets && params.facets.length > 0 ? params.facets : undefined,
    },
  );
}

export interface GetObjectParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
}

export function getObject(params: GetObjectParams): Promise<WireObject> {
  return request<WireObject>(
    'GET',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}/${params.primaryKey}`,
  );
}

export interface ListLinkedObjectsParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  linkType: string;
  pageSize?: number;
  pageToken?: string;
  // Traversal direction. "forward" (the default) walks the link in its
  // declared source -> target direction; "reverse" walks target -> source
  // to surface incoming/reverse links. Mirrors the backend `?direction=`
  // query param read in pkg/oss/handlers.go.
  direction?: 'forward' | 'reverse';
}

export function listLinkedObjects(
  params: ListLinkedObjectsParams,
): Promise<ObjectPage> {
  const query = new URLSearchParams();
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.pageToken) query.set('pageToken', params.pageToken);
  if (params.direction) query.set('direction', params.direction);
  const qs = query.toString();
  return request<ObjectPage>(
    'GET',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}/${params.primaryKey}/links/${params.linkType}${qs ? `?${qs}` : ''}`,
  );
}

export function countObjects(
  ontologyApiName: string,
  objectType: string,
): Promise<CountObjectsResponse> {
  return request<CountObjectsResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objects/${objectType}/count`,
    {},
  );
}

export function getLinkedObject(
  ontologyApiName: string,
  objectType: string,
  primaryKey: string,
  linkType: string,
  linkedObjectPrimaryKey: string,
): Promise<WireObject> {
  return request<WireObject>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objects/${objectType}/${primaryKey}/links/${linkType}/${linkedObjectPrimaryKey}`,
  );
}

export interface GetObjectActivityParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  pageSize?: number;
  pageToken?: string;
}

// US-312: cursor-paginated activity timeline for a single object. Each
// entry is an object_history snapshot pair (prevState/newState); the
// server orders rows by version DESC and emits an opaque nextPageToken
// for cursoring backwards.
export function getObjectActivity(
  params: GetObjectActivityParams,
): Promise<ObjectActivityResponse> {
  const query = new URLSearchParams();
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.pageToken) query.set('pageToken', params.pageToken);
  const qs = query.toString();
  return request<ObjectActivityResponse>(
    'GET',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}/${params.primaryKey}/activity${qs ? `?${qs}` : ''}`,
  );
}

export interface GetObjectHistoryParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  limit?: number;
}

// Tier 2.3: fetch the change history for a single object.
export function getObjectHistory(
  params: GetObjectHistoryParams,
): Promise<ObjectHistoryResponse> {
  const query = new URLSearchParams();
  if (params.limit) query.set('limit', String(params.limit));
  const qs = query.toString();
  return request<ObjectHistoryResponse>(
    'GET',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}/${params.primaryKey}/history${qs ? `?${qs}` : ''}`,
  );
}
