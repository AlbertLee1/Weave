import { useQuery } from '@tanstack/react-query';
import { getApplicationUsage, listApplications } from '../api/developer';

export function useApplications() {
  return useQuery({
    queryKey: ['developer', 'applications'],
    queryFn: listApplications,
  });
}

export function useApplicationUsage(applicationId: string | null) {
  return useQuery({
    queryKey: ['developer', 'applications', applicationId, 'usage'],
    queryFn: () => getApplicationUsage(applicationId as string),
    enabled: !!applicationId,
    refetchInterval: 30_000,
  });
}
