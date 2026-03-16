import { useQuery } from '@tanstack/react-query';
import { aggregate } from '../api/aggregation';
import type { AggregationRequest } from '../api/types';

export function useAggregation(
  ontologyApiName: string,
  objectType: string,
  aggRequest: AggregationRequest | null,
) {
  return useQuery({
    queryKey: ['aggregation', ontologyApiName, objectType, aggRequest],
    queryFn: () => aggregate(ontologyApiName, objectType, aggRequest!),
    enabled: !!ontologyApiName && !!objectType && aggRequest !== null,
  });
}
