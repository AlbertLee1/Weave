import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { useAuditEvents } from '../../hooks/useAudit';
import type { AuditEvent } from '../../api/audit';
import { LoadingSpinner } from '../common/LoadingSpinner';

const ENTITY_TYPES = [
  'ObjectType',
  'Property',
  'LinkType',
  'ActionType',
  'Interface',
  'SharedProperty',
  'ValueType',
  'TypeGroup',
  'Function',
  'QueryType',
  'Session',
];

// Action vocabulary mirrored from the ActionBadge / actionTone tones below
// (create / update / delete) plus the auth lifecycle events the backend
// records (login_success / login_failure). Serialized to the `action` query
// param that cmd/server/admin_audit.go reads.
const ACTIONS = [
  'create',
  'update',
  'delete',
  'login_success',
  'login_failure',
];

const PAGE_SIZE = 50;

export function AuditHistoryPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const [entityType, setEntityType] = useState('');
  const [actor, setActor] = useState('');
  const [resourceRid, setResourceRid] = useState('');
  const [action, setAction] = useState('');
  const [since, setSince] = useState('');
  const [until, setUntil] = useState('');

  const filters = useMemo(
    () => ({
      resource_type: entityType || undefined,
      actor: actor.trim() || undefined,
      resourceRid: resourceRid.trim() || undefined,
      action: action || undefined,
      since: since ? new Date(since).toISOString() : undefined,
      until: until ? new Date(until).toISOString() : undefined,
      pageSize: PAGE_SIZE,
    }),
    [entityType, actor, resourceRid, action, since, until],
  );

  const {
    data,
    error,
    isLoading,
    isFetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useAuditEvents(filters);

  const events: AuditEvent[] = useMemo(
    () => (data ? data.pages.flatMap((p) => p.data ?? []) : []),
    [data],
  );

  const clearFilters = () => {
    setEntityType('');
    setActor('');
    setResourceRid('');
    setAction('');
    setSince('');
    setUntil('');
  };

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm">
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto">
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Audit History
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        {isFetching && !isLoading && (
          <span className="text-xs text-text-secondary">Refreshing…</span>
        )}
      </header>

      <div
        className="px-6 py-3 border-b grid gap-3 items-end"
        style={{
          borderColor: 'rgba(31,41,55,0.5)',
          gridTemplateColumns: 'repeat(auto-fit, minmax(12rem, 1fr)) auto',
        }}
      >
        <label className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            Entity type
          </span>
          <select
            aria-label="Entity type"
            value={entityType}
            onChange={(e) => setEntityType(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="">All entity types</option>
            {ENTITY_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            Actor
          </span>
          <input
            type="text"
            aria-label="Actor"
            placeholder="e.g. user-1"
            value={actor}
            onChange={(e) => setActor(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            Resource RID
          </span>
          <input
            type="text"
            aria-label="Resource RID"
            placeholder="e.g. ri.ontology.main.object-type.x"
            value={resourceRid}
            onChange={(e) => setResourceRid(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            Action
          </span>
          <select
            aria-label="Action"
            value={action}
            onChange={(e) => setAction(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="">All actions</option>
            {ACTIONS.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            From
          </span>
          <input
            type="datetime-local"
            aria-label="From date"
            value={since}
            onChange={(e) => setSince(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            To
          </span>
          <input
            type="datetime-local"
            aria-label="To date"
            value={until}
            onChange={(e) => setUntil(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>
        <button
          type="button"
          onClick={clearFilters}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-bg-tertiary text-text-secondary border border-transparent hover:border-accent-cyan/40 transition-colors"
        >
          Clear
        </button>
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div className="flex items-center justify-center py-20">
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p className="text-sm text-accent-error">
            Failed to load audit events: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && events.length === 0 && (
          <div
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No audit events match
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Try clearing filters or widening the date range.
            </p>
          </div>
        )}
        {!isLoading && !error && events.length > 0 && (
          <>
            <TimelineList events={events} />
            <div className="flex justify-center mt-6">
              {hasNextPage ? (
                <button
                  type="button"
                  onClick={() => fetchNextPage()}
                  disabled={isFetchingNextPage}
                  className="px-4 py-2 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors disabled:opacity-50"
                >
                  {isFetchingNextPage ? 'Loading…' : 'Load more'}
                </button>
              ) : (
                <span className="text-xs text-text-secondary">
                  End of history
                </span>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function TimelineList({ events }: { events: AuditEvent[] }) {
  return (
    <div
      className="rounded border overflow-hidden"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <ul>
        {events.map((evt) => (
          <TimelineRow key={evt.id} event={evt} />
        ))}
      </ul>
    </div>
  );
}

function TimelineRow({ event }: { event: AuditEvent }) {
  const [expanded, setExpanded] = useState(false);
  const hasDiff =
    event.diff_json !== null &&
    event.diff_json !== undefined &&
    !(
      typeof event.diff_json === 'object' &&
      !Array.isArray(event.diff_json) &&
      Object.keys(event.diff_json as object).length === 0
    );

  const ts = formatTimestamp(event.ts);

  return (
    <li
      className="border-b last:border-0"
      style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      data-testid="audit-row"
    >
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        aria-label={`Audit event ${event.id}`}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-bg-tertiary/30"
      >
        <ActionBadge action={event.action} />
        <div className="flex-1 min-w-0">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <span className="text-sm text-text-primary font-semibold">
              {event.resource_type}
            </span>
            <span
              className="font-mono text-xs text-text-secondary truncate"
              title={event.resource_rid}
            >
              {event.resource_rid}
            </span>
          </div>
          <div className="text-xs text-text-secondary mt-0.5">
            <span>{event.actor_id || 'unknown'}</span>
            <span className="mx-1">·</span>
            <time dateTime={event.ts} title={event.ts}>
              {ts}
            </time>
          </div>
        </div>
        <span className="text-xs text-text-secondary shrink-0">
          {hasDiff ? (expanded ? '▾ Hide diff' : '▸ Show diff') : 'No diff'}
        </span>
      </button>
      {expanded && hasDiff && (
        <div
          data-testid="audit-diff"
          className="px-4 pb-3 pt-0 border-t"
          style={{ borderColor: 'rgba(31,41,55,0.5)' }}
        >
          <DiffView diff={event.diff_json} />
        </div>
      )}
    </li>
  );
}

function ActionBadge({ action }: { action: string }) {
  const tone = actionTone(action);
  return (
    <span
      className={`inline-flex items-center shrink-0 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest rounded border ${tone}`}
    >
      {action}
    </span>
  );
}

function actionTone(action: string): string {
  const a = action.toLowerCase();
  if (a === 'create' || a.endsWith('_success')) {
    return 'bg-accent-success/10 text-accent-success border-accent-success/40';
  }
  if (a === 'delete' || a.endsWith('_failure')) {
    return 'bg-accent-error/10 text-accent-error border-accent-error/40';
  }
  if (a === 'update' || a === 'modify') {
    return 'bg-accent-warning/10 text-accent-warning border-accent-warning/40';
  }
  return 'bg-bg-tertiary text-text-secondary border-transparent';
}

function DiffView({ diff }: { diff: unknown }) {
  if (
    typeof diff === 'object' &&
    diff !== null &&
    !Array.isArray(diff) &&
    ('before' in diff || 'after' in diff)
  ) {
    const d = diff as { before?: unknown; after?: unknown };
    return (
      <div className="grid gap-3 md:grid-cols-2 mt-3">
        <DiffPane title="Before" value={d.before} tone="error" />
        <DiffPane title="After" value={d.after} tone="success" />
      </div>
    );
  }
  return (
    <pre
      className="mt-3 p-3 text-xs font-mono whitespace-pre-wrap rounded bg-bg-primary text-text-primary overflow-x-auto"
      style={{ border: '1px solid rgba(31,41,55,0.5)' }}
    >
      {prettyJSON(diff)}
    </pre>
  );
}

function DiffPane({
  title,
  value,
  tone,
}: {
  title: string;
  value: unknown;
  tone: 'error' | 'success';
}) {
  const borderClass =
    tone === 'error' ? 'border-accent-error/40' : 'border-accent-success/40';
  const titleClass =
    tone === 'error' ? 'text-accent-error' : 'text-accent-success';
  return (
    <div className={`rounded border ${borderClass}`}>
      <div
        className={`px-3 py-1 text-[10px] font-semibold uppercase tracking-widest ${titleClass}`}
      >
        {title}
      </div>
      <pre className="p-3 text-xs font-mono whitespace-pre-wrap text-text-primary overflow-x-auto">
        {prettyJSON(value)}
      </pre>
    </div>
  );
}

function prettyJSON(v: unknown): string {
  if (v === undefined) return '—';
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

function formatTimestamp(raw: string): string {
  if (!raw) return '';
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return date.toLocaleString();
}
