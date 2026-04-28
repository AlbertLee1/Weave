import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  approvePermissionRequest,
  createPermissionRequest,
  getPermissionRequest,
  listPermissionRequests,
  rejectPermissionRequest,
  type ListPermissionRequestsQuery,
  type ListPermissionRequestsResponse,
  type PermissionRequest,
} from '../api/permissionRequests';
import { ApiRequestError } from '../api/client';

const EMPTY: ListPermissionRequestsResponse = {
  requests: [],
  total: 0,
  limit: 0,
  offset: 0,
};

// usePermissionRequests reads a page of permission requests. In degraded
// mode (no PG / route unmounted) the call 404s; treat it as an empty
// list so the inbox UI can render a "feature unavailable" placeholder
// instead of failing. Non-approver callers automatically receive only
// their own rows server-side; the `mine` flag opts the approver into
// the same scoped view.
export function usePermissionRequests(query: ListPermissionRequestsQuery = {}) {
  const cacheKey = ['permission-requests', 'list', JSON.stringify(query)];
  return useQuery<ListPermissionRequestsResponse & { available: boolean }>({
    queryKey: cacheKey,
    queryFn: async () => {
      try {
        const resp = await listPermissionRequests(query);
        return { ...resp, available: true };
      } catch (e) {
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'PermissionRequestsUnavailable')
        ) {
          return { ...EMPTY, available: false };
        }
        throw e;
      }
    },
  });
}

export function usePermissionRequest(id: string | null | undefined) {
  return useQuery<PermissionRequest>({
    queryKey: ['permission-requests', 'item', id ?? '__none__'],
    queryFn: () => getPermissionRequest(id as string),
    enabled: !!id,
  });
}

export function useCreatePermissionRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { targetRid: string; reason?: string }) =>
      createPermissionRequest(input.targetRid, input.reason),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['permission-requests'] });
    },
  });
}

export function useApprovePermissionRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; note?: string }) =>
      approvePermissionRequest(input.id, input.note),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['permission-requests'] });
    },
  });
}

export function useRejectPermissionRequest() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; note?: string }) =>
      rejectPermissionRequest(input.id, input.note),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['permission-requests'] });
    },
  });
}
