import { useMutation, useQuery } from '@tanstack/react-query';
import {
  executeQueryType,
  getQueryType,
  listQueryTypes,
} from '../api/ontologies';

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
