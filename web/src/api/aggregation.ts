import { request } from './client';
import type { AggregationRequest } from './types';

export interface AggregationResponse {
  data: AggregationBucket[];
}

export interface AggregationBucket {
  group?: Record<string, unknown>;
  metrics: Record<string, number>;
}

export function aggregate(
  ontologyApiName: string,
  objectType: string,
  aggRequest: AggregationRequest,
): Promise<AggregationResponse> {
  return request<AggregationResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objects/${objectType}/aggregate`,
    aggRequest,
  );
}
