import { useEffect, useMemo } from 'react';
import { useLocation } from 'react-router';
import { CommentsTab } from '../browser/CommentsTab';
import {
  useMarkNotificationRead,
  useNotifications,
} from '../../hooks/useNotifications';
import type { Notification } from '../../api/notifications';

// MentionsPage backs the deep-link /mentions?rid=<targetRid>&commentId=<id>
// the backend stamps onto each `mention`-typed Notification (US-336 +
// US-340). The page renders the source thread (re-using CommentsTab) with
// the focused comment scrolled into view + ring-highlighted, and lists
// the caller's most recent mention notifications in a sidebar so they
// can hop between unresolved mentions without paging back to the bell.
//
// When the URL omits rid + commentId the page falls back to the inbox
// view — sidebar-only, no thread pane.
export function MentionsPage() {
  const location = useLocation();
  const { rid, commentId, notificationId } = useMemo(() => {
    const params = new URLSearchParams(location.search);
    return {
      rid: params.get('rid') ?? '',
      commentId: params.get('commentId') ?? '',
      notificationId: params.get('id') ?? '',
    };
  }, [location.search]);

  const { data, isLoading } = useNotifications({ enabled: true });
  const markRead = useMarkNotificationRead();
  const mentions = useMemo(
    () => (data?.data ?? []).filter((n) => n.type === 'mention'),
    [data],
  );

  // When the deep link carries an explicit `id=<notificationId>`, mark
  // that notification read on landing so the bell badge clears without
  // the user having to open the slide panel. Idempotent: useNotifications
  // re-renders post-mutation but the `read` guard inside markRead keeps
  // the second pass a no-op.
  useEffect(() => {
    if (!notificationId) return;
    const target = mentions.find((n) => n.id === notificationId);
    if (target && !target.read) {
      markRead.mutate(target.id);
    }
    // markRead.mutate identity is stable across renders so depending on
    // it is safe; React 19's useEffect dep array shape allows the
    // ref-stable mutate fn here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notificationId, mentions]);

  const hasFocus = rid !== '';

  return (
    <div
      className="flex flex-col h-full"
      data-testid="mentions-page"
      data-target-rid={rid}
      data-comment-id={commentId}
    >
      <header className="border-b border-border px-6 py-4">
        <h1 className="text-base font-sans font-semibold text-text-primary">
          @Mentions
        </h1>
        <p className="text-xs text-text-secondary mt-1">
          Comments where teammates have @mentioned you. Click a row to jump to
          its source thread.
        </p>
      </header>

      <div className="flex-1 grid grid-cols-1 lg:grid-cols-[18rem_1fr] gap-0 overflow-hidden">
        <aside
          className="border-r border-border bg-bg-elevated/40 overflow-y-auto"
          data-testid="mentions-inbox"
        >
          <h2 className="px-4 pt-4 pb-2 text-[11px] font-sans font-medium text-text-secondary uppercase tracking-wider">
            Recent
          </h2>
          {isLoading && (
            <p className="px-4 py-3 text-xs text-text-secondary">Loading…</p>
          )}
          {!isLoading && mentions.length === 0 && (
            <p
              className="px-4 py-3 text-xs text-text-secondary"
              data-testid="mentions-empty"
            >
              You have no @mentions yet.
            </p>
          )}
          <ul className="space-y-px">
            {mentions.map((n) => (
              <li key={n.id}>
                <MentionInboxRow
                  notification={n}
                  active={notificationFocus(n) === focusKey(rid, commentId)}
                />
              </li>
            ))}
          </ul>
        </aside>

        <main className="overflow-y-auto" data-testid="mentions-thread">
          {hasFocus ? (
            <div className="px-6 py-4">
              <p className="text-[11px] font-mono text-text-secondary mb-2">
                Source: <span data-testid="mentions-source-rid">{rid}</span>
              </p>
              <CommentsTab targetRid={rid} highlightCommentId={commentId} />
            </div>
          ) : (
            <p
              className="text-sm text-text-secondary py-12 text-center"
              data-testid="mentions-no-selection"
            >
              Select a mention from the inbox to see the source thread.
            </p>
          )}
        </main>
      </div>
    </div>
  );
}

interface MentionInboxRowProps {
  notification: Notification;
  active: boolean;
}

function MentionInboxRow({ notification, active }: MentionInboxRowProps) {
  const link = notification.link || '#';
  const className = [
    'block px-4 py-3 text-left transition-colors border-l-2',
    active
      ? 'border-accent-cyan bg-accent-cyan/10 text-text-primary'
      : 'border-transparent text-text-secondary hover:bg-bg-tertiary hover:text-text-primary',
  ].join(' ');
  return (
    <a
      href={link}
      data-testid={`mention-inbox-row-${notification.id}`}
      data-active={active ? 'true' : 'false'}
      data-unread={notification.read ? 'false' : 'true'}
      className={className}
    >
      <div className="flex items-center gap-2">
        {!notification.read && (
          <span
            className="inline-block w-1.5 h-1.5 rounded-full bg-accent-cyan flex-shrink-0"
            aria-hidden="true"
          />
        )}
        <span className="text-xs font-medium truncate">
          {notification.title}
        </span>
      </div>
      {notification.body && (
        <p className="text-[11px] mt-1 line-clamp-2">{notification.body}</p>
      )}
    </a>
  );
}

// notificationFocus pulls (rid, commentId) out of a mention notification's
// link so the inbox row can highlight when its target matches the current
// page focus. Returns an empty string for malformed links.
function notificationFocus(n: Notification): string {
  if (!n.link) return '';
  const idx = n.link.indexOf('?');
  if (idx < 0) return '';
  const params = new URLSearchParams(n.link.slice(idx + 1));
  return focusKey(params.get('rid') ?? '', params.get('commentId') ?? '');
}

function focusKey(rid: string, commentId: string): string {
  return `${rid}|${commentId}`;
}
