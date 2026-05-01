import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  type ListNotificationsOptions,
} from '../api/notifications';

export function useNotifications(
  options: ListNotificationsOptions & { enabled?: boolean; refetchInterval?: number } = {},
) {
  const { enabled = true, refetchInterval = 30_000, unreadOnly, types } = options;
  // Sort the type list so callers passing the same tags in different orders
  // hit a stable cache key. Empty / whitespace-only entries are dropped to
  // match the backend's parseNotificationTypeFilter normalisation.
  const normalisedTypes = (types ?? [])
    .map((t) => t.trim())
    .filter((t) => t.length > 0)
    .sort();
  return useQuery({
    queryKey: [
      'notifications',
      { unreadOnly: unreadOnly ?? false, types: normalisedTypes },
    ],
    queryFn: () =>
      listNotifications({
        unreadOnly,
        types: normalisedTypes.length > 0 ? normalisedTypes : undefined,
      }),
    enabled,
    refetchInterval: enabled ? refetchInterval : false,
  });
}

export function useMarkNotificationRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (notificationId: string) => markNotificationRead(notificationId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notifications'] });
    },
  });
}

/**
 * Bulk-mark every unread notification belonging to the caller as read (US-343).
 * Pass `types` to narrow to specific notification type tags.
 */
export function useMarkAllNotificationsRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (types: string[] = []) => markAllNotificationsRead(types),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notifications'] });
    },
  });
}
