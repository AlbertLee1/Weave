import { useMemo, useState } from 'react';
import { useAuditEvents } from '../../hooks/useAudit';
import type { AuditEvent } from '../../api/audit';
import { EmptyState } from '../common/EmptyState';
import { SkeletonTable } from '../common/Skeleton';

// US-045 (PC-A11): Global Audit Report.
//
// Operators with `user.manage` (the same gate the backend enforces on
// `/api/v2/admin/auditEvents`) reach this page from the Admin sidebar
// and can:
//   - filter by time range (since / until), actor, resource type, action
//   - expand each row to inspect the diff_json (request/response payload)
//   - load more pages via the cursor-based pagination
//   - export the currently-loaded events to CSV or JSON
//
// Honest mapping: the PRD AC also lists a "status" filter, but the
// `audit_events` table (migration 000020 + 000062) has no status
// column — the lifecycle status of the underlying action is captured
// inside `diff_json` when applicable. The page therefore omits a
// status control and the BDD spec locks the wire-shape negative: a
// `status` query param is never sent to the backend. When/if the
// backend grows an explicit status field, this page adds a select
// next to "Action" and the negative assertion is promoted to
// positive.

const PAGE_SIZE = 50;

interface FilterDraft {
  actor: string;
  action: string;
  resourceType: string;
  since: string;
  until: string;
}

const EMPTY_DRAFT: FilterDraft = {
  actor: '',
  action: '',
  resourceType: '',
  since: '',
  until: '',
};

function widenInstant(local: string, edge: 'from' | 'to'): string | undefined {
  if (!local) return undefined;
  const t = new Date(local);
  if (Number.isNaN(t.getTime())) return undefined;
  // datetime-local has no zone — interpret as UTC for predictability.
  if (edge === 'to') {
    // `until` is inclusive in user mental model; align with end-of-minute.
    t.setSeconds(59, 999);
  }
  return t.toISOString();
}

function formatTimestamp(raw: string): string {
  if (!raw) return '';
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return date.toLocaleString();
}

function prettyJSON(v: unknown): string {
  if (v === undefined || v === null) return '(none)';
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

function escapeCsvField(value: string): string {
  if (value === '') return '';
  if (/[",\n\r]/.test(value)) {
    return `"${value.replace(/"/g, '""')}"`;
  }
  return value;
}

function eventsToCsv(events: AuditEvent[]): string {
  const headers = [
    'id',
    'ts',
    'actor_id',
    'action',
    'resource_type',
    'resource_rid',
    'ip',
    'user_agent',
    'diff_json',
  ];
  const rows = events.map((e) =>
    [
      e.id,
      e.ts,
      e.actor_id,
      e.action,
      e.resource_type,
      e.resource_rid,
      e.ip,
      e.user_agent,
      e.diff_json === undefined || e.diff_json === null
        ? ''
        : JSON.stringify(e.diff_json),
    ]
      .map((c) => escapeCsvField(String(c ?? '')))
      .join(','),
  );
  return [headers.join(','), ...rows].join('\n');
}

function triggerDownload(content: string, filename: string, mime: string): void {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

export function AuditReportPage() {
  const [draft, setDraft] = useState<FilterDraft>(EMPTY_DRAFT);
  const [applied, setApplied] = useState<FilterDraft>(EMPTY_DRAFT);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [lastExport, setLastExport] = useState<{
    format: 'csv' | 'json';
    filename: string;
    rowCount: number;
  } | null>(null);

  const filters = useMemo(
    () => ({
      actor: applied.actor.trim() || undefined,
      action: applied.action.trim() || undefined,
      resource_type: applied.resourceType.trim() || undefined,
      since: widenInstant(applied.since, 'from'),
      until: widenInstant(applied.until, 'to'),
      pageSize: PAGE_SIZE,
    }),
    [applied],
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

  const events = useMemo<AuditEvent[]>(
    () => (data ? data.pages.flatMap((p) => p.data ?? []) : []),
    [data],
  );

  const apply = () => {
    setApplied(draft);
    setLastExport(null);
  };

  const clear = () => {
    setDraft(EMPTY_DRAFT);
    setApplied(EMPTY_DRAFT);
    setLastExport(null);
  };

  const toggle = (id: string) =>
    setExpanded((m) => ({ ...m, [id]: !m[id] }));

  const exportFormat = (format: 'csv' | 'json') => {
    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = `audit-report-${stamp}.${format}`;
    const body =
      format === 'csv' ? eventsToCsv(events) : JSON.stringify(events, null, 2);
    const mime = format === 'csv' ? 'text/csv' : 'application/json';
    triggerDownload(body, filename, mime);
    setLastExport({ format, filename, rowCount: events.length });
  };

  return (
    <div
      data-testid="audit-report-page"
      className="mx-auto max-w-6xl space-y-6 p-6"
    >
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            Audit Report
          </h1>
          <p className="text-sm text-text-secondary">
            Cross-ontology audit events from{' '}
            <span className="font-mono text-text-primary">
              /api/v2/admin/auditEvents
            </span>
            . Filter, expand a row to inspect the payload, export to CSV or
            JSON.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            data-testid="audit-report-export-csv-btn"
            onClick={() => exportFormat('csv')}
            disabled={events.length === 0}
            className="rounded-md border border-accent-cyan/50 px-3 py-1.5 text-sm font-medium text-accent-cyan hover:bg-accent-cyan/10 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            Export CSV
          </button>
          <button
            type="button"
            data-testid="audit-report-export-json-btn"
            onClick={() => exportFormat('json')}
            disabled={events.length === 0}
            className="rounded-md border border-accent-cyan/50 px-3 py-1.5 text-sm font-medium text-accent-cyan hover:bg-accent-cyan/10 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            Export JSON
          </button>
        </div>
      </header>

      {lastExport && (
        <div
          data-testid="audit-report-export-status"
          data-format={lastExport.format}
          data-filename={lastExport.filename}
          data-row-count={lastExport.rowCount}
          role="status"
          className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-200"
        >
          Exported {lastExport.rowCount} row
          {lastExport.rowCount === 1 ? '' : 's'} as{' '}
          <span className="font-mono">{lastExport.filename}</span>.
        </div>
      )}

      <section
        data-testid="audit-report-filters"
        className="rounded-lg border border-border/50 bg-bg-secondary/60 p-3"
      >
        <div
          className="grid gap-3"
          style={{
            gridTemplateColumns: 'repeat(auto-fit, minmax(10rem, 1fr))',
          }}
        >
          <label className="flex flex-col gap-1">
            <span className="text-[10px] uppercase tracking-widest text-text-secondary">
              Actor
            </span>
            <input
              data-testid="audit-report-filter-actor"
              type="text"
              placeholder="e.g. user-1"
              value={draft.actor}
              onChange={(e) =>
                setDraft((d) => ({ ...d, actor: e.target.value }))
              }
              className="rounded bg-bg-tertiary px-3 py-1.5 text-sm text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[10px] uppercase tracking-widest text-text-secondary">
              Resource type
            </span>
            <input
              data-testid="audit-report-filter-resource-type"
              type="text"
              placeholder="e.g. ObjectType"
              value={draft.resourceType}
              onChange={(e) =>
                setDraft((d) => ({ ...d, resourceType: e.target.value }))
              }
              className="rounded bg-bg-tertiary px-3 py-1.5 text-sm text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[10px] uppercase tracking-widest text-text-secondary">
              Action
            </span>
            <input
              data-testid="audit-report-filter-action"
              type="text"
              placeholder="e.g. create"
              value={draft.action}
              onChange={(e) =>
                setDraft((d) => ({ ...d, action: e.target.value }))
              }
              className="rounded bg-bg-tertiary px-3 py-1.5 text-sm text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[10px] uppercase tracking-widest text-text-secondary">
              From
            </span>
            <input
              data-testid="audit-report-filter-since"
              type="datetime-local"
              value={draft.since}
              onChange={(e) =>
                setDraft((d) => ({ ...d, since: e.target.value }))
              }
              className="rounded bg-bg-tertiary px-3 py-1.5 text-sm text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-[10px] uppercase tracking-widest text-text-secondary">
              To
            </span>
            <input
              data-testid="audit-report-filter-until"
              type="datetime-local"
              value={draft.until}
              onChange={(e) =>
                setDraft((d) => ({ ...d, until: e.target.value }))
              }
              className="rounded bg-bg-tertiary px-3 py-1.5 text-sm text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
            />
          </label>
        </div>
        <div className="mt-3 flex items-center gap-2">
          <button
            type="button"
            data-testid="audit-report-filter-apply-btn"
            onClick={apply}
            className="rounded-md bg-accent-cyan/20 px-3 py-1.5 text-xs font-semibold text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30"
          >
            Apply
          </button>
          <button
            type="button"
            data-testid="audit-report-filter-clear-btn"
            onClick={clear}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-secondary hover:bg-bg-tertiary"
          >
            Clear
          </button>
          {isFetching && !isLoading && (
            <span className="text-xs text-text-secondary">Refreshing…</span>
          )}
        </div>
      </section>

      {isLoading ? (
        <div data-testid="audit-report-loading">
          <SkeletonTable
            rows={6}
            columns={5}
            aria-label="Loading audit events"
          />
        </div>
      ) : error ? (
        <div data-testid="audit-report-error">
          <EmptyState
            title="Failed to load audit events"
            description={(error as Error).message}
          />
        </div>
      ) : events.length === 0 ? (
        <div data-testid="audit-report-empty">
          <EmptyState
            title="No audit events match"
            description="Try clearing filters or widening the date range."
          />
        </div>
      ) : (
        <>
          <div
            className="overflow-hidden rounded-lg border border-border/50"
            data-testid="audit-report-list"
          >
            <table className="w-full text-sm">
              <thead className="bg-bg-secondary/60 text-xs uppercase tracking-wider text-text-secondary">
                <tr>
                  <th className="w-8 px-3 py-2 text-left" />
                  <th className="px-3 py-2 text-left">Timestamp</th>
                  <th className="px-3 py-2 text-left">Actor</th>
                  <th className="px-3 py-2 text-left">Action</th>
                  <th className="px-3 py-2 text-left">Resource type</th>
                  <th className="px-3 py-2 text-left">Resource RID</th>
                </tr>
              </thead>
              <tbody>
                {events.map((evt) => (
                  <AuditRow
                    key={evt.id}
                    event={evt}
                    expanded={!!expanded[evt.id]}
                    onToggle={() => toggle(evt.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex justify-center">
            {hasNextPage ? (
              <button
                type="button"
                data-testid="audit-report-load-more-btn"
                onClick={() => fetchNextPage()}
                disabled={isFetchingNextPage}
                className="rounded-md border border-accent-cyan/40 bg-accent-cyan/10 px-4 py-1.5 text-xs font-semibold text-accent-cyan hover:bg-accent-cyan/20 disabled:opacity-50"
              >
                {isFetchingNextPage ? 'Loading…' : 'Load more'}
              </button>
            ) : (
              <span
                className="text-xs text-text-secondary"
                data-testid="audit-report-end-marker"
              >
                End of history
              </span>
            )}
          </div>
        </>
      )}
    </div>
  );
}

interface AuditRowProps {
  event: AuditEvent;
  expanded: boolean;
  onToggle: () => void;
}

function AuditRow({ event, expanded, onToggle }: AuditRowProps) {
  return (
    <>
      <tr
        data-testid="audit-report-row"
        data-audit-id={event.id}
        data-audit-action={event.action}
        data-audit-resource-type={event.resource_type}
        data-audit-expanded={expanded ? 'true' : 'false'}
        className="border-t border-border/40 hover:bg-bg-tertiary/30"
      >
        <td className="px-3 py-2 align-top">
          <button
            type="button"
            data-testid="audit-report-row-expand-btn"
            data-audit-id={event.id}
            aria-expanded={expanded}
            aria-label={`Toggle audit event ${event.id} payload`}
            onClick={onToggle}
            className="text-text-secondary hover:text-text-primary"
          >
            {expanded ? '▾' : '▸'}
          </button>
        </td>
        <td className="px-3 py-2 align-top text-xs text-text-primary">
          <time dateTime={event.ts} title={event.ts}>
            {formatTimestamp(event.ts)}
          </time>
        </td>
        <td className="px-3 py-2 align-top text-xs text-text-primary">
          {event.actor_id || '(unknown)'}
        </td>
        <td className="px-3 py-2 align-top text-xs">
          <span
            data-testid="audit-report-row-action-badge"
            className="inline-flex rounded-full bg-bg-tertiary px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-text-primary"
          >
            {event.action}
          </span>
        </td>
        <td className="px-3 py-2 align-top text-xs text-text-primary">
          {event.resource_type}
        </td>
        <td
          className="px-3 py-2 align-top font-mono text-[11px] text-text-secondary truncate max-w-xs"
          title={event.resource_rid}
        >
          {event.resource_rid}
        </td>
      </tr>
      {expanded && (
        <tr
          data-testid="audit-report-row-payload"
          data-audit-id={event.id}
          className="border-t border-border/40 bg-bg-primary/40"
        >
          <td colSpan={6} className="px-3 py-2">
            <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-xs text-text-secondary">
              <dt>IP</dt>
              <dd className="text-text-primary">{event.ip || '—'}</dd>
              <dt>User-Agent</dt>
              <dd className="text-text-primary truncate">
                {event.user_agent || '—'}
              </dd>
            </dl>
            <pre
              data-testid="audit-report-row-payload-json"
              className="mt-2 max-h-64 overflow-auto rounded-md border border-border/40 bg-bg-primary/70 p-2 font-mono text-[11px] text-text-primary"
            >
              {prettyJSON(event.diff_json)}
            </pre>
          </td>
        </tr>
      )}
    </>
  );
}
