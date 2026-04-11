import { request } from './client';
import type {
  ObjectSetDefinition,
  LoadObjectSetRequest,
  LoadObjectSetResponse,
  CreateTemporaryResponse,
  AggregationMetric,
  GroupByClause,
} from './types';
import type { AggregationResponse } from './aggregation';

export function loadObjectSet(
  ontologyApiName: string,
  params: Omit<LoadObjectSetRequest, 'objectSet'> & { objectSet: ObjectSetDefinition },
): Promise<LoadObjectSetResponse> {
  return request<LoadObjectSetResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/loadObjects`,
    params,
  );
}

export interface AggregateObjectSetRequest {
  objectSet: ObjectSetDefinition;
  aggregation: AggregationMetric[];
  groupBy?: GroupByClause[];
}

export function aggregateObjectSet(
  ontologyApiName: string,
  objectSet: ObjectSetDefinition,
  aggregation: AggregationMetric[],
  groupBy?: GroupByClause[],
): Promise<AggregationResponse> {
  const body: AggregateObjectSetRequest = {
    objectSet,
    aggregation,
    ...(groupBy && groupBy.length > 0 ? { groupBy } : {}),
  };
  return request<AggregationResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/aggregate`,
    body,
  );
}

export function createTemporaryObjectSet(
  ontologyApiName: string,
  objectSet: ObjectSetDefinition,
): Promise<CreateTemporaryResponse> {
  return request<CreateTemporaryResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/createTemporary`,
    { objectSet },
  );
}

export function getObjectSet(
  ontologyApiName: string,
  objectSetRid: string,
): Promise<ObjectSetDefinition> {
  return request<ObjectSetDefinition>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/${objectSetRid}`,
  );
}

export function loadLinks(
  ontologyApiName: string,
  objectSet: ObjectSetDefinition,
  linkType: string,
  select: string[],
  pageSize?: number,
  pageToken?: string,
): Promise<LoadObjectSetResponse> {
  const body: Record<string, unknown> = { objectSet, linkType, select };
  if (pageSize !== undefined) body.pageSize = pageSize;
  if (pageToken) body.pageToken = pageToken;
  return request<LoadObjectSetResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/loadLinks`,
    body,
  );
}
