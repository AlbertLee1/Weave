import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react';
import { Link } from 'react-router';
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useNotifications,
} from '../../hooks/useNotifications';
import type { Notification } from '../../api/notifications';
import { EmptyState } from '../common/EmptyState';
import { formatRelativeTime } from '../../lib/formatters';

// US-050 (PC-A15): Notifications & Mentions Centre — full page at /notifications.
//
// Reaches the user from the Sidebar (top-level "Notifications") and
// complements the existing TopBar bell + NotificationCenter slide panel
// (which renders the same wire endpoint in a compact drawer). The full
// page adds:
//   - persistent filters: type (mention / watch / approval / system / all)
//     + read status (all / unread)
//   - cursor-style page-by-page pagination (PAGE_SIZE rows per slice)
//   - per-tab and per-status row counts on the chip strip
//   - Mark-all-read scoped to the active type tab (reusing the bulk
//     endpoint already wired in NotificationCenter)
//
// Honest mapping notes:
//   * Backend GET /api/v2/notifications has no native page-token; it
//     returns the caller's notifications in a single response slice.
//     Pagination is therefore client-side over the in-memory page
//     window. When/if the backend grows a cursor, the wire-shape
//     stays identical (page already calls listNotifications via the
//     useNotifications hook); only the slice math swaps out.
//   * AC: "顶栏铃铛 + 未读计数" + "下拉面板：mentions / approvals /
//     system 分类 + Mark all read" — both already implemented in
//     TopBar (US-340) + NotificationCenter (US-343). This page is the
//     remaining "/notifications 完整页含过滤与分页" surface.

const PAGE_SIZE = 20;

type TypeKey = 'all' | 'mention' | 'watch' | 'approval' | 'system';
type ReadFilter = 'all' | 'unread';

interface TabSpec {
  key: TypeKey;
  label: string;
  // Wire types passed through to the backend bulk-read endpoint when
  // user clicks "Mark <tab> read". Aligns with NotificationCenter so
  // both surfaces agree on the canonical tag list.
  types: string[] | null;
  match: (n: Notification) => boolean;
}

const MENTION_TYPES = ['mention'];
const WATCH_TYPES = ['watch'];
const APPROVAL_TYPES = ['approval'];
const SYSTEM_TYPES = ['system'];
const TYPED_TABS: TypeKey[] = ['mention', 'watch', 'approval', 'system'];

const TABS: TabSpec[] = [
  { key: 'all', label: 'All', types: null, match: () => true },
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
    // Anything that isn't a typed tag falls into System (legacy
    // automate.alert / system.info / plain info) so no row is hidden
    // from the page.
    match: (n) =>
      n.type === 'system' || !TYPED_TABS.includes(n.type as TypeKey),
  },
];

// DOM order of the read-status radiogroup; drives both rendering and the
// WAI-ARIA radio keyboard handler's index math.
const READ_FILTERS: ReadFilter[] = ['all', 'unread'];

function tabSpec(key: TypeKey): TabSpec {
  return TABS.find((t) => t.key === key) ?? TABS[0];
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

export function NotificationsPage() {
  const [activeTab, setActiveTab] = useState<TypeKey>('all');
  const [readFilter, setReadFilter] = useState<ReadFilter>('all');
  const [page, setPage] = useState(0);

  // Fetch the unfiltered list once so the per-tab badge counts are
  // accurate without one refetch per tab switch. Client-side filter
  // pass below scopes the visible rows. NotificationCenter follows
  // the same approach.
  const { data, error, isLoading, isFetching } = useNotifications({});
  const markRead = useMarkNotificationRead();
  const markAll = useMarkAllNotificationsRead();
  // Stabilise the empty-array reference so the dependent useMemo
  // recomputes only when the wire payload actually changes — avoids
  // the react-hooks/exhaustive-deps warning that fires when a
  // logical-OR fallback recreates `[]` on every render.
  const items = useMemo(() => data?.data ?? [], [data]);

  const tab = tabSpec(activeTab);

  const visible = useMemo(() => {
    let next = items.filter(tab.match);
    if (readFilter === 'unread') next = next.filter((n) => !n.read);
    return next;
  }, [items, tab, readFilter]);

  const visibleUnread = useMemo(
    () => visible.filter((n) => !n.read),
    [visible],
  );

  const tabUnreadCounts = useMemo(() => {
    const counts: Record<TypeKey, number> = {
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

  // Client-side pagination over the filtered slice. When the active
  // tab/filter narrows the list below the current offset, clamp the
  // displayed window so we never render an empty page. The raw
  // `page` state is only ever advanced via the Prev/Next buttons
  // (both disabled at the edges) or reset to 0 via filter selection,
  // so the only out-of-range path is a backend refetch that shrinks
  // the list — render-time clamping handles that without needing a
  // setState-in-effect or setState-in-render hack.
  const totalPages = Math.max(1, Math.ceil(visible.length / PAGE_SIZE));
  const safePage = Math.max(0, Math.min(page, totalPages - 1));
  const pageStart = safePage * PAGE_SIZE;
  const pageEnd = pageStart + PAGE_SIZE;
  const pageRows = visible.slice(pageStart, pageEnd);

  const handleSelectTab = (key: TypeKey) => {
    setActiveTab(key);
    setPage(0);
  };
  const handleSelectRead = (rf: ReadFilter) => {
    setReadFilter(rf);
    setPage(0);
  };

  // Roving-tabindex refs so keyboard navigation can move DOM focus to the
  // activated tab / radio (WAI-ARIA tabs + radio patterns).
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const radioRefs = useRef<(HTMLButtonElement | null)[]>([]);

  // Type-filter tablist (WAI-ARIA tabs pattern): ArrowLeft/Right (+Up/Down
  // mirror) move between tabs with wrap, Home/End jump to the ends. Moving
  // focus also activates the tab (automatic activation) — the filtered list
  // re-renders cheaply, so this is the recommended pattern.
  const handleTabKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const last = TABS.length - 1;
      let nextIndex: number | null = null;
      switch (event.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          nextIndex = index === last ? 0 : index + 1;
          break;
        case 'ArrowLeft':
        case 'ArrowUp':
          nextIndex = index === 0 ? last : index - 1;
          break;
        case 'Home':
          nextIndex = 0;
          break;
        case 'End':
          nextIndex = last;
          break;
        default:
          return;
      }
      event.preventDefault();
      handleSelectTab(TABS[nextIndex].key);
      tabRefs.current[nextIndex]?.focus();
    },
    [],
  );

  // Read-status radiogroup (WAI-ARIA radio pattern): ArrowDown/ArrowRight move
  // to the next radio, ArrowUp/ArrowLeft to the previous (both wrapping), in
  // each case moving focus AND selecting. Space/Enter selects the focused
  // radio. preventDefault only fires on keys we handle.
  const handleRadioKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const last = READ_FILTERS.length - 1;
      let nextIndex = index;
      switch (event.key) {
        case 'ArrowDown':
        case 'ArrowRight':
          nextIndex = index === last ? 0 : index + 1;
          break;
        case 'ArrowUp':
        case 'ArrowLeft':
          nextIndex = index === 0 ? last : index - 1;
          break;
        case ' ':
        case 'Enter':
          nextIndex = index;
          break;
        default:
          return;
      }
      event.preventDefault();
      handleSelectRead(READ_FILTERS[nextIndex]);
      radioRefs.current[nextIndex]?.focus();
    },
    [],
  );
  const handleMarkAll = () => {
    if (visibleUnread.length === 0) return;
    markAll.mutate(tab.types ?? []);
  };
  const handleRowClick = (n: Notification) => {
    if (!n.read) markRead.mutate(n.id);
  };

  const markAllDisabled =
    visibleUnread.length === 0 || markAll.isPending || markRead.isPending;

  return (
    <div
      data-testid="notifications-page"
      data-active-tab={activeTab}
      data-read-filter={readFilter}
      data-page={String(safePage)}
      className="mx-auto max-w-5xl space-y-6 p-6"
    >
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            Notifications
          </h1>
          <p className="text-sm text-text-secondary">
            Inbox of mentions, watches, approvals and system alerts from{' '}
            <span className="font-mono text-text-primary">
              /api/v2/notifications
            </span>
            . Filter by type and read status, page through history, mark a
            scope read.
          </p>
        </div>
        <button
          type="button"
          onClick={handleMarkAll}
          disabled={markAllDisabled}
          data-testid="notifications-mark-all-btn"
          data-scope={tab.key}
          className="rounded-md border border-accent-cyan/50 px-3 py-1.5 text-sm font-medium text-accent-cyan hover:bg-accent-cyan/10 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {tab.key === 'all' ? 'Mark all read' : `Mark ${tab.label.toLowerCase()} read`}
        </button>
      </header>

      <section
        data-testid="notifications-filters"
        className="rounded-lg border border-border/50 bg-bg-secondary/60 p-3 space-y-3"
      >
        <div
          role="tablist"
          aria-label="Notification type filter"
          data-testid="notifications-tabs"
          className="flex flex-wrap items-center gap-1"
        >
          {TABS.map((t, index) => {
            const isActive = t.key === activeTab;
            const unread = tabUnreadCounts[t.key];
            return (
              <button
                key={t.key}
                ref={(el) => {
                  tabRefs.current[index] = el;
                }}
                type="button"
                role="tab"
                aria-selected={isActive}
                tabIndex={isActive ? 0 : -1}
                data-testid={`notifications-tab-${t.key}`}
                data-active={isActive ? 'true' : 'false'}
                onClick={() => handleSelectTab(t.key)}
                onKeyDown={(event) => handleTabKeyDown(event, index)}
                className={[
                  'px-3 py-1.5 text-xs font-medium rounded-md border transition-colors',
                  isActive
                    ? 'border-accent-cyan/60 bg-accent-cyan/15 text-accent-cyan'
                    : 'border-transparent text-text-secondary hover:bg-bg-tertiary hover:text-text-primary',
                ].join(' ')}
              >
                {t.label}
                {unread > 0 && (
                  <span
                    data-testid={`notifications-tab-count-${t.key}`}
                    className="ml-1.5 inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1 text-[10px] rounded-full bg-accent-cyan/20 text-accent-cyan"
                  >
                    {unread}
                  </span>
                )}
              </button>
            );
          })}
        </div>

        <div
          role="radiogroup"
          aria-label="Read status filter"
          data-testid="notifications-read-filter"
          className="flex items-center gap-1"
        >
          {READ_FILTERS.map((rf, index) => {
            const isActive = rf === readFilter;
            return (
              <button
                key={rf}
                ref={(el) => {
                  radioRefs.current[index] = el;
                }}
                type="button"
                role="radio"
                aria-checked={isActive}
                tabIndex={isActive ? 0 : -1}
                data-testid={`notifications-read-${rf}`}
                data-active={isActive ? 'true' : 'false'}
                onClick={() => handleSelectRead(rf)}
                onKeyDown={(event) => handleRadioKeyDown(event, index)}
                className={[
                  'px-3 py-1 text-[11px] font-medium rounded-md border transition-colors',
                  isActive
                    ? 'border-accent-teal/60 bg-accent-teal/15 text-accent-teal'
                    : 'border-border/60 text-text-secondary hover:bg-bg-tertiary hover:text-text-primary',
                ].join(' ')}
              >
                {rf === 'all' ? 'All' : 'Unread only'}
              </button>
            );
          })}
          {isFetching && !isLoading && (
            <span className="ml-2 text-[11px] text-text-secondary">
              Refreshing…
            </span>
          )}
        </div>
      </section>

      {isLoading ? (
        <div
          data-testid="notifications-loading"
          className="text-sm text-text-muted py-12 text-center"
        >
          Loading…
        </div>
      ) : error ? (
        <div data-testid="notifications-error">
          <EmptyState
            title="Failed to load notifications"
            description={(error as Error).message}
          />
        </div>
      ) : visible.length === 0 ? (
        <div data-testid="notifications-empty">
          <EmptyState
            title={
              tab.key === 'all' && readFilter === 'all'
                ? 'No notifications yet'
                : `No ${
                    readFilter === 'unread' ? 'unread ' : ''
                  }${tab.key === 'all' ? '' : tab.label.toLowerCase() + ' '}notifications match`
            }
            description="Adjust the type tab or read-status filter to widen the view."
          />
        </div>
      ) : (
        <>
          <ul
            data-testid="notifications-list"
            data-row-count={String(visible.length)}
            data-page-row-count={String(pageRows.length)}
            className="space-y-2"
          >
            {pageRows.map((n) => (
              <li key={n.id}>
                <NotificationRow notification={n} onClick={handleRowClick} />
              </li>
            ))}
          </ul>

          <nav
            data-testid="notifications-pagination"
            data-total-pages={String(totalPages)}
            data-current-page={String(safePage)}
            aria-label="Notifications pagination"
            className="flex items-center justify-between text-xs text-text-secondary"
          >
            <span>
              Showing{' '}
              <span className="font-mono text-text-primary">
                {visible.length === 0 ? 0 : pageStart + 1}
              </span>
              –
              <span className="font-mono text-text-primary">
                {Math.min(pageEnd, visible.length)}
              </span>{' '}
              of{' '}
              <span className="font-mono text-text-primary">
                {visible.length}
              </span>
            </span>
            <span className="flex items-center gap-2">
              <button
                type="button"
                data-testid="notifications-prev-page-btn"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={safePage === 0}
                className="rounded-md border border-border/60 px-2 py-1 text-[11px] font-medium text-text-secondary hover:bg-bg-tertiary disabled:opacity-40 disabled:cursor-not-allowed"
              >
                ← Prev
              </button>
              <span data-testid="notifications-page-indicator">
                Page {safePage + 1} of {totalPages}
              </span>
              <button
                type="button"
                data-testid="notifications-next-page-btn"
                onClick={() =>
                  setPage((p) => Math.min(totalPages - 1, p + 1))
                }
                disabled={safePage >= totalPages - 1}
                className="rounded-md border border-border/60 px-2 py-1 text-[11px] font-medium text-text-secondary hover:bg-bg-tertiary disabled:opacity-40 disabled:cursor-not-allowed"
              >
                Next →
              </button>
            </span>
          </nav>
        </>
      )}
    </div>
  );
}

interface NotificationRowProps {
  notification: Notification;
  onClick: (n: Notification) => void;
}

function NotificationRow({ notification, onClick }: NotificationRowProps) {
  const isUnread = !notification.read;
  const className = [
    'block w-full rounded-md border p-3 transition-colors text-left',
    isUnread
      ? 'border-accent-cyan/30 bg-accent-cyan/5 hover:bg-accent-cyan/10'
      : 'border-border bg-bg-primary hover:bg-bg-elevated',
  ].join(' ');

  const content = (
    <>
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          {isUnread && (
            <span
              data-testid={`notifications-dot-${notification.id}`}
              className="inline-block w-2 h-2 rounded-full bg-accent-cyan flex-shrink-0"
              aria-hidden="true"
            />
          )}
          <TypeBadge type={notification.type} />
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
        data-testid={`notifications-row-${notification.id}`}
        data-notification-id={notification.id}
        data-notification-type={notification.type}
        data-unread={isUnread ? 'true' : 'false'}
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
      data-testid={`notifications-row-${notification.id}`}
      data-notification-id={notification.id}
      data-notification-type={notification.type}
      data-unread={isUnread ? 'true' : 'false'}
      className={`${className} cursor-pointer`}
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

function TypeBadge({ type }: { type: string }) {
  const spec = BADGE_SPEC[type];
  if (!spec) return null;
  return (
    <span
      data-testid={`notifications-type-badge-${type}`}
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
