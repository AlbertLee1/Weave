import { useQuery } from '@tanstack/react-query';
import { listOntologies, getOntology } from '../api/ontologies';

export function useOntologies() {
  return useQuery({
    queryKey: ['ontologies'],
    queryFn: listOntologies,
  });
}

export function useOntology(apiName: string) {
  return useQuery({
    queryKey: ['ontologies', apiName],
    queryFn: () => getOntology(apiName),
    enabled: !!apiName,
  });
}
