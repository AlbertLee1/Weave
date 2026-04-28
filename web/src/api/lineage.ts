import { request } from './client';

export type LineageDirection = 'upstream' | 'downstream' | 'both';

export interface LineageNode {
  rid: string;
  type?: string;
}

export interface LineageEdge {
  from: string;
  to: string;
  operation?: string;
  timestamp: string;
}

export interface LineageResponse {
  root: string;
  direction: LineageDirection;
  depth: number;
  truncated: boolean;
  nodes: LineageNode[];
  edges: LineageEdge[];
}

export interface GetLineageParams {
  direction?: LineageDirection;
  depth?: number;
}

export function getLineage(
  rid: string,
  params: GetLineageParams = {},
): Promise<LineageResponse> {
  const query = new URLSearchParams();
  if (params.direction) query.set('direction', params.direction);
  if (params.depth !== undefined) query.set('depth', String(params.depth));
  const qs = query.toString();
  return request<LineageResponse>(
    'GET',
    `/api/v2/objects/${encodeURIComponent(rid)}/lineage${qs ? `?${qs}` : ''}`,
  );
}
