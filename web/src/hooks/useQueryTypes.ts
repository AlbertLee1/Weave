import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createQueryType,
  deleteQueryType,
  executeQueryType,
  getQueryType,
  listQueryTypes,
  updateQueryType,
} from '../api/ontologies';
import type {
  CreateQueryTypeRequest,
  UpdateQueryTypeRequest,
} from '../api/types';

export function useQueryTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['queryTypes', ontologyApiName],
    queryFn: () => listQueryTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useQueryType(ontologyApiName: string, queryApiName: string) {
  return useQuery({
    queryKey: ['queryType', ontologyApiName, queryApiName],
    queryFn: () => getQueryType(ontologyApiName, queryApiName),
    enabled: !!ontologyApiName && !!queryApiName,
  });
}

// useExecuteQueryType drives the sandbox's Run button. Each invocation is a
// one-shot mutation — the page tracks the latest result locally rather than
// caching by queryKey so re-running the same QueryType with new params
// always re-fetches.
export function useExecuteQueryType(ontologyApiName: string) {
  return useMutation({
    mutationFn: (vars: {
      queryApiName: string;
      parameters: Record<string, unknown>;
    }) => executeQueryType(ontologyApiName, vars.queryApiName, vars.parameters),
  });
}

// --- QueryType admin CRUD mutations ---

export function useCreateQueryType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateQueryTypeRequest) =>
      createQueryType(ontologyApiName, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['queryTypes', ontologyApiName] });
    },
  });
}

export function useUpdateQueryType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateQueryTypeRequest }) =>
      updateQueryType(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['queryTypes', ontologyApiName] });
      qc.invalidateQueries({ queryKey: ['queryType', ontologyApiName] });
    },
  });
}

export function useDeleteQueryType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteQueryType(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['queryTypes', ontologyApiName] });
    },
  });
}
