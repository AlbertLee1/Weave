import { request } from './client';
import type { ObjectPage, WireObject, WhereClause } from './types';

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
  select?: string[];
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
