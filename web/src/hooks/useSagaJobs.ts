import { useQuery } from '@tanstack/react-query';
import {
  getSaga,
  listSagas,
  type ListSagasParams,
} from '../api/sagaJobs';

// US-044 (PC-A08): React Query bindings for the saga / job monitoring
// page. listSagas drives the list view (status filter baked into the
// queryKey so tab switches refetch without thrash); getSaga drives the
// detail drawer (enabled only when a sagaId is selected).

export function useSagaJobs(
  ontologyApiName: string,
  params: ListSagasParams = {},
) {
  return useQuery({
    queryKey: ['sagaJobs', ontologyApiName, params.status ?? 'ALL'],
    queryFn: () => listSagas(ontologyApiName, params),
    enabled: !!ontologyApiName,
  });
}

export function useSagaDetail(
  ontologyApiName: string,
  sagaId: string | null,
) {
  return useQuery({
    queryKey: ['sagaDetail', ontologyApiName, sagaId],
    queryFn: () => getSaga(ontologyApiName, sagaId ?? ''),
    enabled: !!ontologyApiName && !!sagaId,
  });
}
