import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getNotificationsUnreadCount,
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

/**
 * Read the caller's unread notification count via the dedicated O(1)
 * `/notifications/unread-count` endpoint. Used by the navbar badge so it
 * never has to load the full unread list just to render a number.
 *
 * Polls on the same 30s cadence as {@link useNotifications} so the badge
 * stays roughly in sync with the full page.
 */
export function useNotificationsUnreadCount(
  options: { enabled?: boolean; refetchInterval?: number } = {},
) {
  const { enabled = true, refetchInterval = 30_000 } = options;
  return useQuery({
    queryKey: ['notifications', 'unread-count'],
    queryFn: () => getNotificationsUnreadCount(),
    enabled,
    staleTime: refetchInterval,
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
