import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  approveProposal,
  createProposal,
  getBranchBreakingChanges,
  getBranchDiff,
  getProposal,
  listProposals,
  mergeProposal,
  rejectProposal,
  type CreateProposalRequest,
  type ListProposalsParams,
  type ReviewRequest,
} from '../api/proposals';

const PROPOSALS_KEY = (ontology: string) => ['proposals', ontology] as const;
const PROPOSAL_KEY = (ontology: string, proposalId: string) =>
  ['proposals', ontology, proposalId] as const;
const BRANCH_DIFF_KEY = (ontology: string, branchId: string) =>
  ['branches', ontology, branchId, 'diff'] as const;
const BRANCH_BREAKING_KEY = (ontology: string, branchId: string) =>
  ['branches', ontology, branchId, 'breaking-changes'] as const;

export function useProposals(
  ontology: string,
  params: ListProposalsParams = {},
) {
  return useQuery({
    queryKey: [...PROPOSALS_KEY(ontology), params.status ?? 'all'],
    queryFn: () => listProposals(ontology, params),
    enabled: !!ontology,
  });
}

export function useProposal(ontology: string, proposalId: string | null) {
  return useQuery({
    queryKey: proposalId
      ? PROPOSAL_KEY(ontology, proposalId)
      : ['proposals', ontology, '__nil__'],
    queryFn: () => getProposal(ontology, proposalId as string),
    enabled: !!ontology && !!proposalId,
  });
}

export function useBranchDiff(ontology: string, branchId: string | null) {
  return useQuery({
    queryKey: branchId
      ? BRANCH_DIFF_KEY(ontology, branchId)
      : ['branches', ontology, '__nil__', 'diff'],
    queryFn: () => getBranchDiff(ontology, branchId as string),
    enabled: !!ontology && !!branchId,
  });
}

export function useBranchBreakingChanges(
  ontology: string,
  branchId: string | null,
) {
  return useQuery({
    queryKey: branchId
      ? BRANCH_BREAKING_KEY(ontology, branchId)
      : ['branches', ontology, '__nil__', 'breaking-changes'],
    queryFn: () => getBranchBreakingChanges(ontology, branchId as string),
    enabled: !!ontology && !!branchId,
  });
}

export function useCreateProposal(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateProposalRequest) => createProposal(ontology, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: PROPOSALS_KEY(ontology) });
    },
  });
}

export function useApproveProposal(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      proposalId,
      body,
    }: {
      proposalId: string;
      body: ReviewRequest;
    }) => approveProposal(ontology, proposalId, body),
    onSuccess: (_data, vars) => {
      qc.invalidateQueries({ queryKey: PROPOSALS_KEY(ontology) });
      qc.invalidateQueries({
        queryKey: PROPOSAL_KEY(ontology, vars.proposalId),
      });
    },
  });
}

export function useRejectProposal(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      proposalId,
      body,
    }: {
      proposalId: string;
      body: ReviewRequest;
    }) => rejectProposal(ontology, proposalId, body),
    onSuccess: (_data, vars) => {
      qc.invalidateQueries({ queryKey: PROPOSALS_KEY(ontology) });
      qc.invalidateQueries({
        queryKey: PROPOSAL_KEY(ontology, vars.proposalId),
      });
    },
  });
}

export function useMergeProposal(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (proposalId: string) => mergeProposal(ontology, proposalId),
    onSuccess: (_data, proposalId) => {
      qc.invalidateQueries({ queryKey: PROPOSALS_KEY(ontology) });
      qc.invalidateQueries({ queryKey: PROPOSAL_KEY(ontology, proposalId) });
    },
  });
}
