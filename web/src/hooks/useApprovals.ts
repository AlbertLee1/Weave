import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  approveAction,
  listApprovals,
  rejectAction,
  type ListApprovalsParams,
} from '../api/approvals';

export function useApprovals(ontologyApiName: string, params: ListApprovalsParams = {}) {
  return useQuery({
    queryKey: ['approvals', ontologyApiName, params.status ?? 'PENDING', params.mine ?? true],
    queryFn: () => listApprovals(ontologyApiName, params),
    enabled: !!ontologyApiName,
  });
}

export interface ReviewApprovalVariables {
  approvalId: string;
  reason?: string;
}

export function useApproveAction(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: ReviewApprovalVariables) =>
      approveAction(ontologyApiName, vars.approvalId, vars.reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['approvals', ontologyApiName] });
    },
  });
}

export function useRejectAction(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: ReviewApprovalVariables) =>
      rejectAction(ontologyApiName, vars.approvalId, vars.reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['approvals', ontologyApiName] });
    },
  });
}
