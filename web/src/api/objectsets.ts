import { request } from './client';
import type {
  ObjectSetDefinition,
  LoadObjectSetRequest,
  LoadObjectSetResponse,
  CreateTemporaryResponse,
  AggregationMetric,
  GroupByClause,
  WireObject,
} from './types';
import type { AggregationResponse } from './aggregation';

export interface CreateObjectSetSnapshotResponse {
  snapshotRid: string;
  objectType: string;
  primaryKeys: string[];
  totalCount: string;
  truncated?: boolean;
  createdAt: string;
  definitionHash?: string;
  snapshotAt?: number;
  isImmutable: boolean;
}

export interface GetObjectSetSnapshotResponse {
  snapshotRid: string;
  objectType: string;
  data: WireObject[];
  totalCount: string;
  createdAt: string;
  definitionHash?: string;
  snapshotAt?: number;
  isImmutable: boolean;
}

export interface ObjectSetLineageNode {
  id: string;
  type: string;
  objectType?: string;
  where?: unknown;
  link?: string;
  direction?: string;
  path?: unknown;
  reference?: string;
  interfaceType?: string;
  interfaceLink?: string;
  input?: string;
  size?: number;
  seed?: number;
  derivedProperties?: unknown;
}

export interface ObjectSetLineageEdge {
  from: string;
  to: string;
  operation: string;
}

export interface ObjectSetLineageResponse {
  rid: string;
  root: string;
  nodes: ObjectSetLineageNode[];
  edges: ObjectSetLineageEdge[];
}

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

export function createObjectSetSnapshot(
  ontologyApiName: string,
  objectSetRid: string,
): Promise<CreateObjectSetSnapshotResponse> {
  return request<CreateObjectSetSnapshotResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/${objectSetRid}/snapshot`,
  );
}

export function getObjectSetSnapshot(
  ontologyApiName: string,
  snapshotRid: string,
): Promise<GetObjectSetSnapshotResponse> {
  return request<GetObjectSetSnapshotResponse>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/snapshots/${snapshotRid}`,
  );
}

export function getObjectSetLineage(
  ontologyApiName: string,
  objectSetRid: string,
): Promise<ObjectSetLineageResponse> {
  return request<ObjectSetLineageResponse>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectSets/${objectSetRid}/lineage`,
  );
}
