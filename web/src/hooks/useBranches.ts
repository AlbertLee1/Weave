import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createBranch,
  deleteBranch,
  getBranchDiff,
  mergeBranch,
  postBranchDiff,
  rebaseBranch,
} from '../api/ontologies';
import type {
  CreateBranchRequest,
  MergeBranchRequest,
  MergeBranchResponse,
  OntologyBranch,
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

// US-113 / US-383: create a branch off the active ontology. Invalidates the
// branch list so the picker reflects the new entry immediately.
export function useCreateBranch(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation<OntologyBranch, Error, CreateBranchRequest>({
    mutationFn: (body) => createBranch(ontologyApiName, body),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['branches', ontologyApiName],
      });
    },
  });
}

// US-116: close a branch and drop it from the list cache.
export function useDeleteBranch(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (branchId) => deleteBranch(ontologyApiName, branchId),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['branches', ontologyApiName],
      });
    },
  });
}

// US-383: fast-forward a branch onto the latest main trunk. Invalidates the
// reconcile/diff caches since base_version (and before-states) move.
export function useRebaseBranch(ontologyApiName: string, branchId: string) {
  const queryClient = useQueryClient();
  return useMutation<OntologyBranch, Error, void>({
    mutationFn: () => rebaseBranch(ontologyApiName, branchId),
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
