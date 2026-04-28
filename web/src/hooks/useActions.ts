import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { listActionTypes } from '../api/ontologies';
import { applyAction, applyBatch, revertActionLog } from '../api/actions';
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

// useRevertActionLog wraps POST /actions/revert. Used by the toast Undo
// button (5s window after apply, US-319) and the Action History per-row
// Undo button. On success, invalidate ['objects'] (the reverse edits flow
// through the funnel consumer just like an apply) and ['actionHistory'] /
// ['actionHistoryEntry'] so the row's Status flips to REVERTED in the UI
// without a manual refresh.
export function useRevertActionLog(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (actionLogId: number) =>
      revertActionLog(ontologyApiName, actionLogId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects'] });
      queryClient.invalidateQueries({ queryKey: ['actionHistory', ontologyApiName] });
      queryClient.invalidateQueries({ queryKey: ['actionHistoryEntry', ontologyApiName] });
    },
  });
}
