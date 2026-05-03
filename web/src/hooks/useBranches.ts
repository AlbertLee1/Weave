import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getBranchDiff,
  mergeBranch,
  postBranchDiff,
} from '../api/ontologies';
import type {
  MergeBranchRequest,
  MergeBranchResponse,
} from '../api/types';

export function useBranchDiff(ontologyApiName: string, branchId: string) {
  return useQuery({
    queryKey: ['branchDiff', ontologyApiName, branchId],
    queryFn: () => getBranchDiff(ontologyApiName, branchId),
    enabled: !!ontologyApiName && !!branchId,
  });
}

// US-387: annotated diff used by the reconcile UI. Shares cache with
// `branchDiffPost` so a successful merge can invalidate the row.
export function useBranchReconcileDiff(
  ontologyApiName: string,
  branchId: string,
) {
  return useQuery({
    queryKey: ['branchDiffPost', ontologyApiName, branchId],
    queryFn: () => postBranchDiff(ontologyApiName, branchId),
    enabled: !!ontologyApiName && !!branchId,
  });
}

export function useMergeBranch(ontologyApiName: string, branchId: string) {
  const queryClient = useQueryClient();
  return useMutation<MergeBranchResponse, Error, MergeBranchRequest>({
    mutationFn: (body) => mergeBranch(ontologyApiName, branchId, body),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['branchDiffPost', ontologyApiName, branchId],
      });
      queryClient.invalidateQueries({
        queryKey: ['branchDiff', ontologyApiName, branchId],
      });
      queryClient.invalidateQueries({
        queryKey: ['branches', ontologyApiName],
      });
    },
  });
}
