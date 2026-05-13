import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getFunction,
  getFunctionRepoCommit,
  listFunctionRepoCommits,
  listFunctionVersions,
  replayFunction,
  type FunctionSummary,
  type FunctionRepoCommitWithSource,
  type FunctionRepoCommitsResponse,
  type FunctionVersionsResponse,
  type ReplayFunctionRequest,
  type ReplayFunctionResponse,
} from '../api/functions';

// US-046 (PC-A03): React Query bindings for the per-Function code repo
// page. Each surface (function metadata / commit log / single commit
// source / versions list / replay mutation) gets its own hook so the
// invalidation graph is explicit. queryKeys all start with
// `function-repo` to namespace away from the FunctionCodePage /
// FunctionDiffPage queries (which use `function-code` and
// `function-diff` respectively) — they each maintain their own draft /
// diff state and we do not want a refetch on one page to trigger a
// refetch on the other.

export function useFunctionSummary(
  ontologyApiName: string,
  functionRid: string,
) {
  return useQuery<FunctionSummary>({
    queryKey: ['function-repo', 'function', ontologyApiName, functionRid],
    queryFn: () => getFunction(ontologyApiName, functionRid),
    enabled: ontologyApiName !== '' && functionRid !== '',
    retry: false,
  });
}

export function useFunctionRepoCommits(
  ontologyApiName: string,
  functionRid: string,
  limit?: number,
) {
  return useQuery<FunctionRepoCommitsResponse>({
    queryKey: ['function-repo', 'log', ontologyApiName, functionRid, limit ?? null],
    queryFn: () => listFunctionRepoCommits(ontologyApiName, functionRid, limit),
    enabled: ontologyApiName !== '' && functionRid !== '',
    retry: false,
  });
}

export function useFunctionRepoCommit(
  ontologyApiName: string,
  functionRid: string,
  hash: string | null,
) {
  return useQuery<FunctionRepoCommitWithSource>({
    queryKey: ['function-repo', 'commit', ontologyApiName, functionRid, hash],
    queryFn: () =>
      getFunctionRepoCommit(ontologyApiName, functionRid, hash ?? ''),
    enabled: ontologyApiName !== '' && functionRid !== '' && hash !== null,
    retry: false,
  });
}

export function useFunctionVersions(
  ontologyApiName: string,
  functionName: string | null,
) {
  return useQuery<FunctionVersionsResponse>({
    queryKey: ['function-repo', 'versions', ontologyApiName, functionName],
    queryFn: () => listFunctionVersions(ontologyApiName, functionName ?? ''),
    enabled:
      ontologyApiName !== '' && functionName !== null && functionName !== '',
    retry: false,
  });
}

export function useFunctionReplay(
  ontologyApiName: string,
  functionRid: string,
) {
  const queryClient = useQueryClient();
  return useMutation<
    ReplayFunctionResponse,
    Error,
    ReplayFunctionRequest
  >({
    mutationFn: (body) =>
      replayFunction(ontologyApiName, functionRid, body),
    onSuccess: () => {
      // Replays persist a new execution row server-side (is_replay=true)
      // — invalidate the commit log so any badge / counter derived from
      // executions refetches on its own cadence.
      void queryClient.invalidateQueries({
        queryKey: ['function-repo', 'log', ontologyApiName, functionRid],
      });
    },
  });
}
