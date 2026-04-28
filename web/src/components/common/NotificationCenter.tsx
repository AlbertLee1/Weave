import { Link } from 'react-router';
import { SlidePanel } from './SlidePanel';
import type { Notification } from '../../api/notifications';
import {
  useMarkNotificationRead,
  useNotifications,
} from '../../hooks/useNotifications';
import { formatRelativeTime } from '../../lib/formatters';

interface NotificationCenterProps {
  open: boolean;
  onClose: () => void;
}

export function NotificationCenter({ open, onClose }: NotificationCenterProps) {
  const { data, isLoading, isError } = useNotifications({ enabled: open });
  const markRead = useMarkNotificationRead();
  const items = data?.data ?? [];
  const unread = items.filter((n) => !n.read);

  function handleMarkAll() {
    unread.forEach((n) => markRead.mutate(n.id));
  }

  function handleItemClick(n: Notification) {
    if (!n.read) markRead.mutate(n.id);
    if (n.link) onClose();
  }

  return (
    <SlidePanel open={open} onClose={onClose} title="Notifications">
      <div className="flex items-center justify-between mb-3">
        <span className="text-xs text-text-muted">
          {unread.length > 0
            ? `${unread.length} unread`
            : items.length === 0
              ? ''
              : 'All read'}
        </span>
        <button
          type="button"
          onClick={handleMarkAll}
          disabled={unread.length === 0 || markRead.isPending}
          className="text-xs text-accent-cyan hover:text-accent-teal disabled:text-text-muted disabled:cursor-not-allowed"
        >
          Mark all read
        </button>
      </div>

      {isLoading && (
        <div className="text-sm text-text-muted py-8 text-center">Loading…</div>
      )}

      {isError && (
        <div className="text-sm text-accent-error py-8 text-center">
          Failed to load notifications
        </div>
      )}

      {!isLoading && !isError && items.length === 0 && (
        <div className="text-sm text-text-muted py-12 text-center">
          No notifications yet
        </div>
      )}

      <ul className="space-y-2">
        {items.map((n) => (
          <li key={n.id}>
            <NotificationRow notification={n} onClick={handleItemClick} />
          </li>
        ))}
      </ul>
    </SlidePanel>
  );
}

interface NotificationRowProps {
  notification: Notification;
  onClick: (n: Notification) => void;
}

function NotificationRow({ notification, onClick }: NotificationRowProps) {
  const isUnread = !notification.read;
  const className = [
    'block w-full text-left rounded-md border p-3 transition-colors cursor-pointer',
    isUnread
      ? 'border-accent-cyan/30 bg-accent-cyan/5 hover:bg-accent-cyan/10'
      : 'border-border bg-bg-primary hover:bg-bg-elevated',
  ].join(' ');

  const typeBadge = renderTypeBadge(notification.type);

  const content = (
    <>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          {isUnread && (
            <span
              data-testid={`notification-dot-${notification.id}`}
              className="inline-block w-2 h-2 rounded-full bg-accent-cyan flex-shrink-0"
              aria-hidden="true"
            />
          )}
          {typeBadge}
          <span className="text-sm font-medium text-text-primary truncate">
            {notification.title}
          </span>
        </div>
        <span className="text-xs text-text-muted whitespace-nowrap flex-shrink-0">
          {formatRelativeTime(notification.createdAt)}
        </span>
      </div>
      {notification.body && (
        <p className="text-xs text-text-secondary mt-1 line-clamp-2">
          {notification.body}
        </p>
      )}
    </>
  );

  if (notification.link) {
    return (
      <Link
        to={notification.link}
        data-testid={`notification-item-${notification.id}`}
        data-unread={isUnread ? 'true' : 'false'}
        data-type={notification.type}
        className={className}
        onClick={() => onClick(notification)}
      >
        {content}
      </Link>
    );
  }

  return (
    <div
      role="button"
      tabIndex={0}
      data-testid={`notification-item-${notification.id}`}
      data-unread={isUnread ? 'true' : 'false'}
      data-type={notification.type}
      className={className}
      onClick={() => onClick(notification)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onClick(notification);
        }
      }}
    >
      {content}
    </div>
  );
}

// renderTypeBadge surfaces a short, type-aware glyph next to the row
// title. Currently the only typed treatment is `mention` (US-340) — the
// `@` glyph plus accent border keeps mentions visually distinct from
// the watch / approval / system traffic sharing the same dropdown.
// Unknown / untyped notifications render no badge to preserve the
// pre-US-340 layout for legacy automation alerts.
function renderTypeBadge(type: string): React.ReactNode {
  if (type === 'mention') {
    return (
      <span
        data-testid="notification-type-badge-mention"
        className="inline-flex items-center justify-center w-5 h-5 text-[10px] font-mono font-bold rounded-full bg-accent-cyan/15 text-accent-cyan border border-accent-cyan/40 flex-shrink-0"
        aria-label="Mention"
        title="Mention"
      >
        @
      </span>
    );
  }
  return null;
}
