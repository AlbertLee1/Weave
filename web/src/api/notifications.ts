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
}

export function listNotifications(
  options: ListNotificationsOptions = {},
): Promise<ListNotificationsResponse> {
  const query = new URLSearchParams();
  if (options.unreadOnly) query.set('unread', 'true');
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
