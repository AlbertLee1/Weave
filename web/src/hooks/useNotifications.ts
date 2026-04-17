import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listNotifications,
  markNotificationRead,
  type ListNotificationsOptions,
} from '../api/notifications';

export function useNotifications(
  options: ListNotificationsOptions & { enabled?: boolean; refetchInterval?: number } = {},
) {
  const { enabled = true, refetchInterval = 30_000, unreadOnly } = options;
  return useQuery({
    queryKey: ['notifications', { unreadOnly: unreadOnly ?? false }],
    queryFn: () => listNotifications({ unreadOnly }),
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
