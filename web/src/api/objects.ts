import { request } from './client';
import type {
  ObjectPage,
  ObjectHistoryResponse,
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
  select: string[];
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
      select: params.select,
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
}

export function listLinkedObjects(
  params: ListLinkedObjectsParams,
): Promise<ObjectPage> {
  const query = new URLSearchParams();
  if (params.pageSize) query.set('pageSize', String(params.pageSize));
  if (params.pageToken) query.set('pageToken', params.pageToken);
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
