import { useInfiniteQuery } from '@tanstack/react-query';
import { listAuditEvents, type ListAuditEventsParams } from '../api/audit';

export function useAuditEvents(
  filters: Omit<ListAuditEventsParams, 'pageToken'>,
) {
  return useInfiniteQuery({
    queryKey: ['audit', 'events', filters],
    queryFn: ({ pageParam }) =>
      listAuditEvents({
        ...filters,
        pageToken: pageParam || undefined,
      }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextPageToken || undefined,
  });
}
