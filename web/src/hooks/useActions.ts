import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { listActionTypes } from '../api/ontologies';
import { applyAction, applyBatch } from '../api/actions';
import type { ActionApplyOptions } from '../api/types';

export function useActionTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['actionTypes', ontologyApiName],
    queryFn: () => listActionTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

// ApplyActionVariables pairs the action API name (now a path segment)
// with the body parameters and optional Foundry OSv2 options.
export interface ApplyActionVariables {
  action: string;
  parameters: Record<string, unknown>;
  options?: ActionApplyOptions;
}

export function useApplyAction(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: ApplyActionVariables) =>
      applyAction(ontologyApiName, vars.action, {
        parameters: vars.parameters,
        ...(vars.options ? { options: vars.options } : {}),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects'] });
    },
  });
}

export interface ApplyBatchVariables {
  action: string;
  requests: Array<{ parameters: Record<string, unknown> }>;
  returnEdits?: 'ALL' | 'NONE';
}

export function useApplyBatch(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: ApplyBatchVariables) =>
      applyBatch(ontologyApiName, vars.action, {
        requests: vars.requests,
        ...(vars.returnEdits ? { options: { returnEdits: vars.returnEdits } } : {}),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects'] });
    },
  });
}
