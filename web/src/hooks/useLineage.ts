import { useQuery } from '@tanstack/react-query';
import {
  getLineage,
  type GetLineageParams,
  type LineageResponse,
} from '../api/lineage';

export function useLineage(
  rid: string | undefined,
  params: GetLineageParams = {},
) {
  return useQuery<LineageResponse>({
    queryKey: ['lineage', rid, params.direction ?? 'upstream', params.depth ?? 1],
    queryFn: () => getLineage(rid as string, params),
    enabled: !!rid,
    staleTime: 30_000,
  });
}
