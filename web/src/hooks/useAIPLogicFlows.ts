import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createLogicFlow,
  deleteLogicFlow,
  dryRunLogicNode,
  executeLogicFlow,
  getLogicFlow,
  listLogicFlows,
  listLogicRuns,
  updateLogicFlow,
  type CreateLogicFlowRequest,
  type DryRunLogicNodeRequest,
  type ExecuteLogicFlowRequest,
  type UpdateLogicFlowRequest,
} from '../api/aipLogic';

export const aipLogicQueryKeys = {
  flows: ['aip', 'logic-flows'] as const,
  flow: (flowId: string) => ['aip', 'logic-flow', flowId] as const,
  runs: (flowId: string) => ['aip', 'logic-flow', flowId, 'runs'] as const,
};

export function useAIPLogicFlows(enabled = true) {
  return useQuery({
    queryKey: aipLogicQueryKeys.flows,
    queryFn: () => listLogicFlows(),
    enabled,
  });
}

export function useAIPLogicFlow(flowId: string | null) {
  return useQuery({
    queryKey: flowId
      ? aipLogicQueryKeys.flow(flowId)
      : ['aip', 'logic-flow', '__none__'],
    queryFn: () => getLogicFlow(flowId as string),
    enabled: !!flowId,
  });
}

export function useAIPLogicRuns(flowId: string | null) {
  return useQuery({
    queryKey: flowId
      ? aipLogicQueryKeys.runs(flowId)
      : ['aip', 'logic-flow', '__none__', 'runs'],
    queryFn: () => listLogicRuns(flowId as string),
    enabled: !!flowId,
  });
}

export function useCreateAIPLogicFlow() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateLogicFlowRequest) => createLogicFlow(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipLogicQueryKeys.flows });
    },
  });
}

export function useUpdateAIPLogicFlow() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { flowId: string; body: UpdateLogicFlowRequest }) =>
      updateLogicFlow(vars.flowId, vars.body),
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: aipLogicQueryKeys.flows });
      qc.invalidateQueries({ queryKey: aipLogicQueryKeys.flow(vars.flowId) });
    },
  });
}

export function useDeleteAIPLogicFlow() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (flowId: string) => deleteLogicFlow(flowId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipLogicQueryKeys.flows });
    },
  });
}

export function useExecuteAIPLogicFlow(flowId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: ExecuteLogicFlowRequest) =>
      executeLogicFlow(flowId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipLogicQueryKeys.runs(flowId) });
    },
  });
}

export function useDryRunAIPLogicNode(flowId: string) {
  return useMutation({
    mutationFn: (body: DryRunLogicNodeRequest) => dryRunLogicNode(flowId, body),
  });
}
