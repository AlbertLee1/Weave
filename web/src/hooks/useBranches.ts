import { useQuery } from '@tanstack/react-query';
import { getBranchDiff } from '../api/ontologies';

export function useBranchDiff(ontologyApiName: string, branchId: string) {
  return useQuery({
    queryKey: ['branchDiff', ontologyApiName, branchId],
    queryFn: () => getBranchDiff(ontologyApiName, branchId),
    enabled: !!ontologyApiName && !!branchId,
  });
}
