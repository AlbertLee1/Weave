import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import {
  useActionHistory,
  useActionHistoryEntry,
} from '../../hooks/useActionHistory';
import { useActionTypes, useRevertActionLog } from '../../hooks/useActions';
import { useOntologyStore } from '../../stores/ontologyStore';
import { SkeletonTable, SkeletonText } from '../common/Skeleton';
import { EmptyState } from '../common/EmptyState';
import { Modal } from '../common/Modal';
import { useToastStore } from '../../stores/toastStore';
import { ApiRequestError } from '../../api/client';
import type { ActionHistoryEntry } from '../../api/actionHistory';

type StatusFilter = 'ALL' | 'SUCCESS' | 'FAILED';

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'ALL', label: 'All' },
  { value: 'SUCCESS', label: 'Success' },
  { value: 'FAILED', label: 'Failed' },
];

const STATUS_BADGE_STYLE: Record<string, string> = {
  SUCCESS: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  FAILED: 'bg-rose-500/10 text-rose-400 border border-rose-500/30',
  REVERTED: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
};

export function ActionHistoryPage() {
  const { ontology } = useParams<{ ontology?: string }>();
  const selectedOntology = useOntologyStore((s) => s.selectedOntology);
  const activeOntology = ontology ?? selectedOntology ?? '';

  const [actionTypeFilter, setActionTypeFilter] = useState<string>('');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('ALL');
  const [userIdFilter, setUserIdFilter] = useState<string>('');
  const [detailLogId, setDetailLogId] = useState<number | null>(null);
  const [pendingUndoId, setPendingUndoId] = useState<number | null>(null);

  const actionTypesQuery = useActionTypes(activeOntology);
  const historyQuery = useActionHistory(activeOntology, {
    actionType: actionTypeFilter || undefined,
    status: statusFilter === 'ALL' ? undefined : statusFilter,
    userId: userIdFilter.trim() || undefined,
  });

  const detailQuery = useActionHistoryEntry(activeOntology, detailLogId);
  const revertMutation = useRevertActionLog(activeOntology);
  const pushToast = useToastStore((s) => s.push);

  function handleUndo(logId: number) {
    setPendingUndoId(logId);
    revertMutation.mutate(logId, {
      onSettled: () => setPendingUndoId(null),
      onSuccess: () => {
        pushToast({
          message: `Action #${logId} reverted`,
          severity: 'info',
          ttlMs: 4000,
        });
      },
      onError: (err) => {
        const message =
          err instanceof ApiRequestError && err.errorName === 'AlreadyReverted'
            ? `Action #${logId} was already reverted`
            : err instanceof Error
              ? `Undo failed: ${err.message}`
              : 'Undo failed';
        pushToast({ message, severity: 'error', ttlMs: 4000 });
      },
    });
  }

  const apiNameByRid = useMemo(() => {
    const map: Record<string, string> = {};
    (actionTypesQuery.data ?? []).forEach((at) => {
      if (at.rid) map[at.rid] = at.apiName;
    });
    return map;
  }, [actionTypesQuery.data]);

  const rows = historyQuery.data?.data ?? [];

  if (!activeOntology) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to view action execution history."
        />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
          Action History
        </h1>
        <p className="text-sm text-text-secondary">
          Past action executions for{' '}
          <span className="font-mono text-text-primary">{activeOntology}</span>.
        </p>
      </header>

      <section
        className="flex flex-wrap items-end gap-3 rounded-lg border border-border/50 bg-bg-secondary/60 p-3"
        data-testid="action-history-filters"
      >
        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Action type
          <select
            value={actionTypeFilter}
            onChange={(e) => setActionTypeFilter(e.target.value)}
            className="min-w-[10rem] rounded-md border border-border/60 bg-bg-primary px-2 py-1.5 font-mono text-xs text-text-primary outline-none focus:border-accent-cyan"
            data-testid="filter-action-type"
          >
            <option value="">All</option>
            {(actionTypesQuery.data ?? []).map((at) => (
              <option key={at.rid} value={at.apiName}>
                {at.apiName}
              </option>
            ))}
          </select>
        </label>
        <div className="flex flex-col gap-1 text-xs text-text-secondary">
          <span>Status</span>
          <div
            className="flex items-center gap-1"
            role="tablist"
            aria-label="Status filter"
          >
            {STATUS_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                type="button"
                role="tab"
                aria-selected={statusFilter === opt.value}
                onClick={() => setStatusFilter(opt.value)}
                className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                  statusFilter === opt.value
                    ? 'bg-bg-tertiary text-text-primary'
                    : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          User ID
          <input
            type="text"
            value={userIdFilter}
            onChange={(e) => setUserIdFilter(e.target.value)}
            placeholder="user:alice"
            className="min-w-[12rem] rounded-md border border-border/60 bg-bg-primary px-2 py-1.5 font-mono text-xs text-text-primary outline-none focus:border-accent-cyan"
            data-testid="filter-user-id"
          />
        </label>
      </section>

      {historyQuery.isLoading ? (
        <SkeletonTable rows={6} columns={5} aria-label="Loading action history" />
      ) : historyQuery.isError ? (
        <EmptyState
          title="Failed to load action history"
          description={
            historyQuery.error instanceof Error
              ? historyQuery.error.message
              : 'Unexpected error.'
          }
        />
      ) : rows.length === 0 ? (
        <EmptyState
          title="No action executions"
          description="Try a different filter or run an action from the Action Console."
        />
      ) : (
        <div
          className="overflow-hidden rounded-lg border border-border/50"
          data-testid="action-history-list"
        >
          <table className="min-w-full divide-y divide-border/40 text-sm">
            <thead className="bg-bg-secondary/60 text-left text-xs text-text-secondary">
              <tr>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 font-medium">Action Type</th>
                <th className="px-4 py-2 font-medium">User</th>
                <th className="px-4 py-2 font-medium">When</th>
                <th className="px-4 py-2 font-medium">ID</th>
                <th className="px-4 py-2"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/30">
              {rows.map((entry) => (
                <tr
                  key={entry.id}
                  className="hover:bg-bg-tertiary/40"
                  data-testid="action-history-row"
                  data-log-id={entry.id}
                >
                  <td className="px-4 py-2">
                    <span
                      className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${
                        STATUS_BADGE_STYLE[entry.status] ??
                        'bg-bg-tertiary text-text-secondary border border-border/50'
                      }`}
                    >
                      {entry.status}
                    </span>
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-text-primary">
                    {apiNameByRid[entry.actionTypeRid] ?? entry.actionTypeRid}
                  </td>
                  <td className="px-4 py-2 text-xs text-text-primary">
                    {entry.userId || '—'}
                  </td>
                  <td className="px-4 py-2 text-xs text-text-secondary">
                    {new Date(entry.createdAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                    {entry.id}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <div className="flex justify-end gap-2">
                      {entry.status === 'SUCCESS' && (
                        <button
                          type="button"
                          onClick={() => handleUndo(entry.id)}
                          disabled={
                            revertMutation.isPending && pendingUndoId === entry.id
                          }
                          className="rounded-md border border-amber-500/40 px-3 py-1 text-xs text-amber-300 hover:bg-amber-500/10 disabled:opacity-50"
                          data-testid="undo-btn"
                          data-log-id={entry.id}
                        >
                          {revertMutation.isPending && pendingUndoId === entry.id
                            ? 'Undoing…'
                            : 'Undo'}
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => setDetailLogId(entry.id)}
                        className="rounded-md border border-border/60 px-3 py-1 text-xs text-text-secondary hover:bg-bg-tertiary hover:text-text-primary"
                        data-testid="view-detail-btn"
                      >
                        View
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Modal
        open={detailLogId !== null}
        onClose={() => setDetailLogId(null)}
        title={
          detailLogId !== null
            ? `Execution #${detailLogId}`
            : 'Execution detail'
        }
        size="xl"
      >
        {detailQuery.isLoading ? (
          <SkeletonText lines={6} aria-label="Loading execution detail" />
        ) : detailQuery.isError ? (
          <div role="alert" className="text-sm text-rose-300">
            Failed to load:{' '}
            {detailQuery.error instanceof Error
              ? detailQuery.error.message
              : 'unknown error'}
          </div>
        ) : detailQuery.data ? (
          <ActionHistoryDetail
            entry={detailQuery.data}
            actionApiName={
              apiNameByRid[detailQuery.data.actionTypeRid] ??
              detailQuery.data.actionTypeRid
            }
          />
        ) : null}
      </Modal>
    </div>
  );
}

interface DetailProps {
  entry: ActionHistoryEntry;
  actionApiName: string;
}

function ActionHistoryDetail({ entry, actionApiName }: DetailProps) {
  return (
    <div className="space-y-4 text-sm" data-testid="action-history-detail">
      <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-xs text-text-secondary">
        <dt>Action Type</dt>
        <dd className="font-mono text-text-primary">{actionApiName}</dd>
        <dt>Status</dt>
        <dd className="text-text-primary">{entry.status}</dd>
        <dt>User</dt>
        <dd className="text-text-primary">{entry.userId || '—'}</dd>
        <dt>When</dt>
        <dd className="text-text-primary">
          {new Date(entry.createdAt).toLocaleString()}
        </dd>
        {entry.errorMessage && (
          <>
            <dt>Error</dt>
            <dd className="text-rose-300">{entry.errorMessage}</dd>
          </>
        )}
      </dl>
      <DetailJsonBlock
        title="Parameters"
        value={entry.parameters}
        testId="detail-parameters"
      />
      <DetailJsonBlock
        title="Edits"
        value={entry.edits}
        testId="detail-edits"
      />
      {entry.prevEdits !== undefined && entry.prevEdits !== null && (
        <DetailJsonBlock
          title="Previous State"
          value={entry.prevEdits}
          testId="detail-prev-edits"
        />
      )}
    </div>
  );
}

interface JsonBlockProps {
  title: string;
  value: unknown;
  testId: string;
}

function DetailJsonBlock({ title, value, testId }: JsonBlockProps) {
  const text = useMemo(() => {
    if (value === undefined || value === null) {
      return '(none)';
    }
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return String(value);
    }
  }, [value]);
  return (
    <details className="text-xs">
      <summary className="cursor-pointer text-text-secondary hover:text-text-primary">
        {title}
      </summary>
      <pre
        className="mt-2 max-h-72 overflow-auto rounded-md border border-border/40 bg-bg-primary/70 p-3 font-mono text-[11px] text-text-primary"
        data-testid={testId}
      >
        {text}
      </pre>
    </details>
  );
}
