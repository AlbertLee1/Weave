import { request } from './client';

export interface Notification {
  id: string;
  userId: string;
  title: string;
  body: string;
  type: string;
  link?: string;
  read: boolean;
  createdAt: string;
}

export interface ListNotificationsResponse {
  data: Notification[];
}

export interface ListNotificationsOptions {
  unreadOnly?: boolean;
  /** Filter to one or more notification type tags (US-343). Empty values are dropped. */
  types?: string[];
}

export interface MarkAllReadResponse {
  updated: number;
}

export function listNotifications(
  options: ListNotificationsOptions = {},
): Promise<ListNotificationsResponse> {
  const query = new URLSearchParams();
  if (options.unreadOnly) query.set('unread', 'true');
  if (options.types && options.types.length > 0) {
    for (const t of options.types) {
      const trimmed = t.trim();
      if (trimmed) query.append('type', trimmed);
    }
  }
  const qs = query.toString();
  return request<ListNotificationsResponse>(
    'GET',
    `/api/v2/notifications${qs ? `?${qs}` : ''}`,
  );
}

export function markNotificationRead(notificationId: string): Promise<void> {
  return request<void>(
    'POST',
    `/api/v2/notifications/${encodeURIComponent(notificationId)}/read`,
  );
}

/**
 * Bulk-mark every unread notification belonging to the caller as read (US-343).
 * Optionally narrow by `types` to scope to specific notification type tags.
 */
export function markAllNotificationsRead(
  types: string[] = [],
): Promise<MarkAllReadResponse> {
  const query = new URLSearchParams();
  for (const t of types) {
    const trimmed = t.trim();
    if (trimmed) query.append('type', trimmed);
  }
  const qs = query.toString();
  return request<MarkAllReadResponse>(
    'POST',
    `/api/v2/notifications/read-all${qs ? `?${qs}` : ''}`,
  );
}
