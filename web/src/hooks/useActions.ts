import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { listActionTypes } from '../api/ontologies';
import { applyAction } from '../api/actions';
import type { ActionApplyRequest } from '../api/types';

export function useActionTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['actionTypes', ontologyApiName],
    queryFn: () => listActionTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useApplyAction(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: ActionApplyRequest) =>
      applyAction(ontologyApiName, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects'] });
    },
  });
}
