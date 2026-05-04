import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  dropSagaDLQ,
  listSagaDLQ,
  retrySagaDLQ,
  type ListSagaDLQParams,
} from '../api/sagaDLQ';

export function useSagaDLQ(ontologyApiName: string, params: ListSagaDLQParams = {}) {
  return useQuery({
    queryKey: ['sagaDLQ', ontologyApiName, params.status ?? 'PENDING'],
    queryFn: () => listSagaDLQ(ontologyApiName, params),
    enabled: !!ontologyApiName,
  });
}

export function useRetrySagaDLQ(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (dlqId: string) => retrySagaDLQ(ontologyApiName, dlqId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sagaDLQ', ontologyApiName] });
    },
  });
}

export function useDropSagaDLQ(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (dlqId: string) => dropSagaDLQ(ontologyApiName, dlqId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sagaDLQ', ontologyApiName] });
    },
  });
}
