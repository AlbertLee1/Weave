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
}

export function searchObjects(params: SearchObjectsParams): Promise<ObjectPage> {
  return request<ObjectPage>(
    'POST',
    `/api/v2/ontologies/${params.ontologyApiName}/objects/${params.objectType}/search`,
    {
      where: params.where,
      pageSize: params.pageSize,
      pageToken: params.pageToken,
      orderBy: params.orderBy,
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
