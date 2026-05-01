import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import { SlidePanel } from './SlidePanel';
import type { Notification } from '../../api/notifications';
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useNotifications,
} from '../../hooks/useNotifications';
import { formatRelativeTime } from '../../lib/formatters';

interface NotificationCenterProps {
  open: boolean;
  onClose: () => void;
}

// Tab definitions for the type filter (US-343). The `match` predicate runs
// client-side for the count badge; the actual list/bulk-read backend calls
// pass the `types` array straight through. `all` matches everything.
//
// Notification type strings on the wire follow loose conventions — mention is
// the canonical US-340 tag; watch / approval / system collect the rest of the
// product surfaces. Anything that doesn't match a typed tab falls into
// `system` so legacy automation alerts (`automate.alert`, `system.info`, etc.)
// still surface somewhere visible.
type TabKey = 'all' | 'mention' | 'watch' | 'approval' | 'system';

interface TabSpec {
  key: TabKey;
  label: string;
  // Wire types passed to the backend ?type=... and used for client-side
  // counting. `null` means "no filter" (the All tab).
  types: string[] | null;
  // Predicate used to compute the visible count badge per tab.
  match: (n: Notification) => boolean;
}

const MENTION_TYPES = ['mention'];
const WATCH_TYPES = ['watch'];
const APPROVAL_TYPES = ['approval'];
const SYSTEM_TYPES = ['system'];
const TYPED_TABS: TabKey[] = ['mention', 'watch', 'approval', 'system'];

const TABS: TabSpec[] = [
  {
    key: 'all',
    label: 'All',
    types: null,
    match: () => true,
  },
  {
    key: 'mention',
    label: 'Mentions',
    types: MENTION_TYPES,
    match: (n) => n.type === 'mention',
  },
  {
    key: 'watch',
    label: 'Watches',
    types: WATCH_TYPES,
    match: (n) => n.type === 'watch',
  },
  {
    key: 'approval',
    label: 'Approvals',
    types: APPROVAL_TYPES,
    match: (n) => n.type === 'approval',
  },
  {
    key: 'system',
    label: 'System',
    types: SYSTEM_TYPES,
    // System is the catch-all bucket: anything that isn't one of the typed
    // tags above (legacy `automate.alert`, `system.info`, plain `info`, etc.).
    match: (n) =>
      n.type === 'system' || !TYPED_TABS.includes(n.type as TabKey),
  },
];

function tabSpec(key: TabKey): TabSpec {
  return TABS.find((t) => t.key === key) ?? TABS[0];
}

export function NotificationCenter({ open, onClose }: NotificationCenterProps) {
  const [activeTab, setActiveTab] = useState<TabKey>('all');
  // Always fetch the unfiltered list so the per-tab badge counts are
  // accurate; the post-filter pass below scopes the visible rows. With
  // typical notification volumes (<100 / user) this is cheaper than
  // refetching every tab switch and keeps the count badges in sync with
  // what's about to render.
  const { data, isLoading, isError } = useNotifications({ enabled: open });
  const markRead = useMarkNotificationRead();
  const markAll = useMarkAllNotificationsRead();
  const items = data?.data ?? [];
  const tab = tabSpec(activeTab);
  const visible = useMemo(() => items.filter(tab.match), [items, tab]);
  const visibleUnread = useMemo(() => visible.filter((n) => !n.read), [visible]);
  const tabUnreadCounts = useMemo(() => {
    const counts: Record<TabKey, number> = {
      all: 0,
      mention: 0,
      watch: 0,
      approval: 0,
      system: 0,
    };
    for (const n of items) {
      if (n.read) continue;
      counts.all += 1;
      for (const t of TABS) {
        if (t.key === 'all') continue;
        if (t.match(n)) counts[t.key] += 1;
      }
    }
    return counts;
  }, [items]);

  function handleMarkAll() {
    // Use the backend bulk endpoint scoped to the active tab's types. The
    // single-row markRead path is reserved for click-through item behaviour
    // — looping it here would cost N round-trips and N invalidations.
    markAll.mutate(tab.types ?? []);
  }

  function handleItemClick(n: Notification) {
    if (!n.read) markRead.mutate(n.id);
    if (n.link) onClose();
  }

  const markAllDisabled =
    visibleUnread.length === 0 || markAll.isPending || markRead.isPending;

  return (
    <SlidePanel open={open} onClose={onClose} title="Notifications">
      <div
        role="tablist"
        aria-label="Notification type filter"
        data-testid="notification-tabs"
        className="flex items-center gap-1 mb-3 border-b border-border overflow-x-auto"
      >
        {TABS.map((t) => {
          const isActive = t.key === activeTab;
          const unread = tabUnreadCounts[t.key];
          return (
            <button
              key={t.key}
              type="button"
              role="tab"
              aria-selected={isActive}
              data-testid={`notification-tab-${t.key}`}
              data-active={isActive ? 'true' : 'false'}
              onClick={() => setActiveTab(t.key)}
              className={[
                'px-3 py-2 text-xs font-medium whitespace-nowrap border-b-2 transition-colors',
                isActive
                  ? 'border-accent-cyan text-text-primary'
                  : 'border-transparent text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              {t.label}
              {unread > 0 && (
                <span
                  data-testid={`notification-tab-count-${t.key}`}
                  className="ml-1.5 inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1 text-[10px] rounded-full bg-accent-cyan/15 text-accent-cyan"
                >
                  {unread}
                </span>
              )}
            </button>
          );
        })}
      </div>

      <div className="flex items-center justify-between mb-3">
        <span className="text-xs text-text-muted">
          {visibleUnread.length > 0
            ? `${visibleUnread.length} unread`
            : visible.length === 0
              ? ''
              : 'All read'}
        </span>
        <button
          type="button"
          onClick={handleMarkAll}
          disabled={markAllDisabled}
          data-testid="notification-mark-all"
          className="text-xs text-accent-cyan hover:text-accent-teal disabled:text-text-muted disabled:cursor-not-allowed"
        >
          {tab.key === 'all' ? 'Mark all read' : `Mark ${tab.label.toLowerCase()} read`}
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

      {!isLoading && !isError && visible.length === 0 && (
        <div className="text-sm text-text-muted py-12 text-center">
          {tab.key === 'all'
            ? 'No notifications yet'
            : `No ${tab.label.toLowerCase()} notifications`}
        </div>
      )}

      <ul className="space-y-2">
        {visible.map((n) => (
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

// renderTypeBadge surfaces a short, type-aware glyph next to the row title.
// US-340 introduced the mention `@` glyph; US-343 extends the same single
// switch to watch / approval / system so each tab's rows are visually
// distinct without screenshot diffs. Unknown / untyped notifications render
// no badge to preserve the pre-US-340 layout for legacy automation alerts.
function renderTypeBadge(type: string): React.ReactNode {
  const spec = BADGE_SPEC[type];
  if (!spec) return null;
  return (
    <span
      data-testid={`notification-type-badge-${type}`}
      className={[
        'inline-flex items-center justify-center w-5 h-5 text-[10px] font-mono font-bold rounded-full flex-shrink-0 border',
        spec.className,
      ].join(' ')}
      aria-label={spec.label}
      title={spec.label}
    >
      {spec.glyph}
    </span>
  );
}

const BADGE_SPEC: Record<
  string,
  { glyph: string; label: string; className: string }
> = {
  mention: {
    glyph: '@',
    label: 'Mention',
    className: 'bg-accent-cyan/15 text-accent-cyan border-accent-cyan/40',
  },
  watch: {
    glyph: '◉',
    label: 'Watch',
    className: 'bg-accent-teal/15 text-accent-teal border-accent-teal/40',
  },
  approval: {
    glyph: '✓',
    label: 'Approval',
    className:
      'bg-accent-warning/15 text-accent-warning border-accent-warning/40',
  },
  system: {
    glyph: '!',
    label: 'System',
    className: 'bg-bg-elevated text-text-secondary border-border',
  },
};
