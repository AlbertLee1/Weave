import { useMemo } from 'react';
import { useObjectActivity } from '../../hooks/useObjects';
import type { ObjectActivityEntry } from '../../api/types';
import { LoadingSpinner } from '../common/LoadingSpinner';

interface ObjectActivityPanelProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
}

export function ObjectActivityPanel({
  ontologyApiName,
  objectType,
  primaryKey,
}: ObjectActivityPanelProps) {
  const {
    data,
    error,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useObjectActivity({
    ontologyApiName,
    objectType,
    primaryKey,
    pageSize: 25,
  });

  const events = useMemo<ObjectActivityEntry[]>(
    () => (data ? data.pages.flatMap((p) => p.data ?? []) : []),
    [data],
  );

  if (isLoading) {
    return (
      <div
        className="flex items-center justify-center py-12"
        data-testid="activity-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (error) {
    return (
      <p className="text-xs text-accent-error" data-testid="activity-error">
        Failed to load activity: {(error as Error).message}
      </p>
    );
  }

  if (events.length === 0) {
    return (
      <p
        className="text-xs text-text-secondary py-6 text-center"
        data-testid="activity-empty"
      >
        No activity recorded for this object yet.
      </p>
    );
  }

  return (
    <div className="space-y-3" data-testid="activity-list">
      <ol className="space-y-2 border-l border-border pl-4">
        {events.map((evt) => (
          <ActivityRow key={evt.id} event={evt} />
        ))}
      </ol>
      <div className="flex justify-center pt-2">
        {hasNextPage ? (
          <button
            type="button"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="px-3 py-1.5 text-xs font-mono rounded bg-accent-cyan/15 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/25 disabled:opacity-50"
            data-testid="activity-load-more"
          >
            {isFetchingNextPage ? 'Loading…' : 'Load more'}
          </button>
        ) : (
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            End of timeline
          </span>
        )}
      </div>
    </div>
  );
}

function ActivityRow({ event }: { event: ObjectActivityEntry }) {
  return (
    <li
      className="relative flex flex-col gap-1 -ml-[1.05rem] pl-4"
      data-testid="activity-row"
    >
      <span
        className={`absolute top-1.5 -left-[5px] w-2 h-2 rounded-full ${editTypeDot(event.editType)}`}
        aria-hidden
      />
      <div className="flex items-center gap-2 text-[11px] font-mono">
        <EditTypeBadge editType={event.editType} />
        <span className="text-text-secondary">v{event.version}</span>
        <span className="text-text-secondary">·</span>
        <time className="text-text-secondary" dateTime={event.recordedAt}>
          {formatTimestamp(event.recordedAt)}
        </time>
      </div>
      <div className="flex flex-wrap items-center gap-2 text-[11px] text-text-secondary font-mono">
        {event.userId && (
          <span data-testid="activity-row-user">by {event.userId}</span>
        )}
        {event.source && (
          <span data-testid="activity-row-source">source: {event.source}</span>
        )}
        {event.actionLogRid && (
          <span className="truncate" title={event.actionLogRid}>
            action: {event.actionLogRid}
          </span>
        )}
      </div>
    </li>
  );
}

function EditTypeBadge({ editType }: { editType: ObjectActivityEntry['editType'] }) {
  const cls = editTypeBadgeClass(editType);
  return (
    <span
      className={`px-1.5 py-0.5 rounded text-[10px] font-semibold tracking-wider ${cls}`}
    >
      {editType}
    </span>
  );
}

function editTypeBadgeClass(editType: ObjectActivityEntry['editType']): string {
  switch (editType) {
    case 'CREATE':
      return 'bg-accent-success/20 text-accent-success';
    case 'DELETE':
      return 'bg-accent-error/20 text-accent-error';
    case 'MODIFY':
    default:
      return 'bg-accent-cyan/20 text-accent-cyan';
  }
}

function editTypeDot(editType: ObjectActivityEntry['editType']): string {
  switch (editType) {
    case 'CREATE':
      return 'bg-accent-success';
    case 'DELETE':
      return 'bg-accent-error';
    case 'MODIFY':
    default:
      return 'bg-accent-cyan';
  }
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
