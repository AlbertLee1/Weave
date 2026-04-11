import { useQuery } from '@tanstack/react-query';
import { getObjectHistory } from '../api/objects';

export interface UseObjectVersionParams {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
}

// Reads the current object version (totalVersions from object_history) so
// callers can stamp an `expectedVersion` on action apply requests for
// optimistic concurrency (US-023/US-024).
export function useObjectVersion(params: UseObjectVersionParams) {
  const { ontologyApiName, objectType, primaryKey } = params;
  const enabled = Boolean(ontologyApiName && objectType && primaryKey);

  const query = useQuery({
    queryKey: ['objectVersion', ontologyApiName, objectType, primaryKey],
    queryFn: () =>
      getObjectHistory({
        ontologyApiName,
        objectType,
        primaryKey,
        limit: 1,
      }),
    enabled,
    staleTime: 0,
  });

  return {
    ...query,
    version: query.data?.totalVersions,
  };
}
