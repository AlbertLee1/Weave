import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type { SagaDLQEntry, SagaDLQStatusFilter } from '../../api/sagaDLQ';
import { ApiRequestError } from '../../api/client';
import {
  useDropSagaDLQ,
  useRetrySagaDLQ,
  useSagaDLQ,
} from '../../hooks/useSagaDLQ';
import { SkeletonTable } from '../common/Skeleton';
import { EmptyState } from '../common/EmptyState';
import { useOntologyStore } from '../../stores/ontologyStore';

const STATUS_OPTIONS: { value: SagaDLQStatusFilter; label: string }[] = [
  { value: 'PENDING', label: 'Pending' },
  { value: 'RESOLVED', label: 'Resolved' },
  { value: 'DROPPED', label: 'Dropped' },
];

const STATUS_BADGE_STYLE: Record<SagaDLQEntry['status'], string> = {
  PENDING: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  RESOLVED: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  DROPPED: 'bg-slate-500/10 text-slate-400 border border-slate-500/30',
};

function describeApiError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    return `${err.errorName}: ${err.parameters?.error ?? err.message}`;
  }
  if (err instanceof Error) {
    return err.message;
  }
  return 'Operation failed.';
}

export function SagaDLQPage() {
  const { ontology } = useParams<{ ontology?: string }>();
  const selectedOntology = useOntologyStore((s) => s.selectedOntology);
  const activeOntology = ontology ?? selectedOntology ?? '';

  const [status, setStatus] = useState<SagaDLQStatusFilter>('PENDING');
  const [pendingDlqId, setPendingDlqId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const listQuery = useSagaDLQ(activeOntology, { status });
  const retryMutation = useRetrySagaDLQ(activeOntology);
  const dropMutation = useDropSagaDLQ(activeOntology);

  const entries = useMemo(
    () => listQuery.data?.entries ?? [],
    [listQuery.data],
  );

  if (!activeOntology) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to view its saga DLQ."
        />
      </div>
    );
  }

  const handleRetry = (entry: SagaDLQEntry) => {
    setActionError(null);
    setPendingDlqId(entry.dlqId);
    retryMutation.mutate(entry.dlqId, {
      onSettled: () => setPendingDlqId(null),
      onError: (err) => setActionError(describeApiError(err)),
    });
  };

  const handleDrop = (entry: SagaDLQEntry) => {
    setActionError(null);
    setPendingDlqId(entry.dlqId);
    dropMutation.mutate(entry.dlqId, {
      onSettled: () => setPendingDlqId(null),
      onError: (err) => setActionError(describeApiError(err)),
    });
  };

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
          Saga DLQ
        </h1>
        <p className="text-sm text-text-secondary">
          Failed compensations awaiting operator review on{' '}
          <span className="font-mono text-text-primary">{activeOntology}</span>.
        </p>
      </header>

      <section
        className="flex flex-wrap items-center gap-3 rounded-lg border border-border/50 bg-bg-secondary/60 p-3"
        data-testid="saga-dlq-filters"
      >
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
              aria-selected={status === opt.value}
              onClick={() => setStatus(opt.value)}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                status === opt.value
                  ? 'bg-bg-tertiary text-text-primary'
                  : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
              }`}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </section>

      {actionError && (
        <div
          role="alert"
          className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          data-testid="saga-dlq-error"
        >
          {actionError}
        </div>
      )}

      {listQuery.isLoading ? (
        <SkeletonTable rows={6} columns={4} aria-label="Loading saga DLQ" />
      ) : listQuery.isError ? (
        <EmptyState
          title="Failed to load saga DLQ"
          description={
            listQuery.error instanceof Error
              ? listQuery.error.message
              : 'Unexpected error.'
          }
        />
      ) : entries.length === 0 ? (
        <EmptyState
          title="No DLQ entries"
          description={
            status === 'PENDING'
              ? 'No failed compensations are awaiting retry.'
              : 'No entries match this filter.'
          }
        />
      ) : (
        <ul
          className="space-y-3"
          data-testid="saga-dlq-list"
          aria-label="Saga DLQ entries"
        >
          {entries.map((entry) => (
            <SagaDLQCard
              key={entry.dlqId}
              entry={entry}
              busy={pendingDlqId === entry.dlqId}
              onRetry={() => handleRetry(entry)}
              onDrop={() => handleDrop(entry)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

interface SagaDLQCardProps {
  entry: SagaDLQEntry;
  busy: boolean;
  onRetry: () => void;
  onDrop: () => void;
}

function SagaDLQCard({ entry, busy, onRetry, onDrop }: SagaDLQCardProps) {
  const editsText = useMemo(() => {
    if (entry.editsJson === undefined || entry.editsJson === null) {
      return '(no edits captured)';
    }
    try {
      return JSON.stringify(entry.editsJson, null, 2);
    } catch {
      return String(entry.editsJson);
    }
  }, [entry.editsJson]);

  const isPending = entry.status === 'PENDING';

  return (
    <li
      className="rounded-lg border border-border/50 bg-bg-secondary/60 p-4"
      data-testid="saga-dlq-card"
      data-dlq-id={entry.dlqId}
    >
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <span
              className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[entry.status]}`}
            >
              {entry.status}
            </span>
            <h2 className="font-mono text-sm text-text-primary">
              {entry.dlqId}
            </h2>
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-xs text-text-secondary">
            <dt>Saga</dt>
            <dd className="font-mono text-text-primary">{entry.sagaId}</dd>
            <dt>Step</dt>
            <dd className="font-mono text-text-primary">{entry.stepId}</dd>
            <dt>Attempts</dt>
            <dd className="text-text-primary">{entry.attempts}</dd>
            {entry.failureMessage && (
              <>
                <dt>Failure</dt>
                <dd className="text-rose-300">{entry.failureMessage}</dd>
              </>
            )}
            <dt>Created</dt>
            <dd className="text-text-primary">
              {new Date(entry.createdAt).toLocaleString()}
            </dd>
            {entry.lastAttemptAt && (
              <>
                <dt>Last attempt</dt>
                <dd className="text-text-primary">
                  {new Date(entry.lastAttemptAt).toLocaleString()}
                </dd>
              </>
            )}
          </dl>
        </div>
        {isPending && (
          <div className="flex shrink-0 gap-2">
            <button
              type="button"
              onClick={onRetry}
              disabled={busy}
              data-testid="saga-dlq-retry-btn"
              className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
            >
              {busy ? 'Working…' : 'Retry'}
            </button>
            <button
              type="button"
              onClick={onDrop}
              disabled={busy}
              data-testid="saga-dlq-drop-btn"
              className="rounded-md border border-rose-500/50 px-3 py-1.5 text-xs font-semibold text-rose-300 hover:bg-rose-500/10 disabled:opacity-60"
            >
              Drop
            </button>
          </div>
        )}
      </div>
      <details className="mt-3 text-xs">
        <summary className="cursor-pointer text-text-secondary hover:text-text-primary">
          Inverse edits
        </summary>
        <pre
          className="mt-2 max-h-64 overflow-auto rounded-md border border-border/40 bg-bg-primary/70 p-3 font-mono text-[11px] text-text-primary"
          data-testid="saga-dlq-edits"
        >
          {editsText}
        </pre>
      </details>
    </li>
  );
}
