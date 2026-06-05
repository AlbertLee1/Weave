import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  abandonSideEffectDLQ,
  listSideEffectDLQ,
  replaySideEffectDLQ,
  type SideEffectDLQReplayStatus,
  type SideEffectDLQRow,
} from '../../api/sideEffectDLQ';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const DLQ_QUERY_KEY = ['admin', 'side-effect-dlq'] as const;

// Per-status badge styling. Pending rows are the only actionable state
// (amber, "needs attention"); replayed is terminal-success (emerald);
// abandoned is terminal-dismissed (slate).
const STATUS_BADGE_STYLE: Record<SideEffectDLQReplayStatus, string> = {
  pending: 'bg-accent-amber/15 text-accent-amber border border-accent-amber/30',
  replayed:
    'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  abandoned: 'bg-slate-500/10 text-slate-400 border border-slate-500/30',
};

function formatCreatedAt(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

export function SideEffectDLQAdminPage() {
  const {
    data: entries,
    isLoading,
    error,
  } = useQuery({ queryKey: DLQ_QUERY_KEY, queryFn: listSideEffectDLQ });

  const [abandoning, setAbandoning] = useState<SideEffectDLQRow | null>(null);

  const sorted = useMemo(() => {
    if (!entries) return [];
    // Newest-first by id (server already orders newest-first, but be stable).
    return [...entries].sort((a, b) => b.id - a.id);
  }, [entries]);

  return (
    <div
      data-testid="side-effect-dlq-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Side-Effect DLQ
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Failed Action Side-Effects
        </span>
      </header>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="side-effect-dlq-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="side-effect-dlq-error"
            className="text-sm text-accent-error"
          >
            Failed to load side-effect DLQ: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && sorted.length === 0 && (
          <div
            data-testid="side-effect-dlq-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No dead-lettered side-effects
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Action side-effects (webhooks, etc.) that exhaust their retry
              budget land here for inspection. Nothing is pending right now.
            </p>
          </div>
        )}
        {!isLoading && !error && sorted.length > 0 && (
          <DLQTable rows={sorted} onAbandon={setAbandoning} />
        )}
      </div>

      {abandoning && (
        <AbandonModal
          row={abandoning}
          onClose={() => setAbandoning(null)}
        />
      )}
    </div>
  );
}

function DLQTable({
  rows,
  onAbandon,
}: {
  rows: SideEffectDLQRow[];
  onAbandon: (row: SideEffectDLQRow) => void;
}) {
  return (
    <div
      data-testid="side-effect-dlq-table"
      className="rounded border overflow-hidden"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <table className="w-full text-sm">
        <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
          <tr className="border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
            <th className="text-left px-4 py-2 font-medium">ID</th>
            <th className="text-left px-4 py-2 font-medium">Action Log</th>
            <th className="text-left px-4 py-2 font-medium">Effect Type</th>
            <th className="text-left px-4 py-2 font-medium">Created</th>
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-right px-4 py-2 font-medium">Replays</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <DLQRow key={row.id} row={row} onAbandon={() => onAbandon(row)} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function DLQRow({
  row,
  onAbandon,
}: {
  row: SideEffectDLQRow;
  onAbandon: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const [showOutcome, setShowOutcome] = useState(false);

  const isPending = row.replayStatus === 'pending';

  const replay = useMutation({
    mutationFn: () => replaySideEffectDLQ(row.id),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: DLQ_QUERY_KEY });
      pushToast({
        message: resp.replayed
          ? `Side-effect #${row.id} replayed successfully.`
          : `Side-effect #${row.id} replay ran but the effect still failed.`,
        severity: resp.replayed ? 'success' : 'error',
      });
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to replay side-effect.'),
        severity: 'error',
      });
    },
  });

  const outcomeText = useMemo(() => {
    if (row.outcome === undefined || row.outcome === null) {
      return '(no outcome captured)';
    }
    try {
      return JSON.stringify(row.outcome, null, 2);
    } catch {
      return String(row.outcome);
    }
  }, [row.outcome]);

  return (
    <>
      <tr
        data-testid={`side-effect-dlq-row-${row.id}`}
        data-replay-status={row.replayStatus}
        className="border-b last:border-0 hover:bg-bg-tertiary/30"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <td className="px-4 py-2 font-mono text-xs text-text-primary">
          {row.id}
        </td>
        <td className="px-4 py-2 font-mono text-xs text-text-secondary">
          {row.actionLogId}
        </td>
        <td className="px-4 py-2 text-xs text-text-secondary">
          {row.effectType}
        </td>
        <td className="px-4 py-2 text-xs text-text-secondary">
          {formatCreatedAt(row.createdAt)}
        </td>
        <td className="px-4 py-2">
          <span
            data-testid="side-effect-dlq-status-badge"
            className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[row.replayStatus]}`}
          >
            {row.replayStatus}
          </span>
        </td>
        <td className="px-4 py-2 text-right text-xs text-text-secondary font-mono">
          {row.replayCount}
        </td>
        <td className="px-4 py-2 text-right whitespace-nowrap">
          <button
            type="button"
            data-testid="side-effect-dlq-outcome-btn"
            onClick={() => setShowOutcome((v) => !v)}
            aria-expanded={showOutcome}
            className="text-xs text-accent-cyan hover:underline mr-3"
          >
            {showOutcome ? 'Hide' : 'Outcome'}
          </button>
          <button
            type="button"
            data-testid="side-effect-dlq-replay-btn"
            onClick={() => replay.mutate()}
            disabled={!isPending || replay.isPending}
            title={
              isPending
                ? undefined
                : 'Only pending entries can be replayed'
            }
            className="text-xs text-accent-cyan hover:underline mr-3 disabled:opacity-40 disabled:cursor-not-allowed disabled:no-underline"
          >
            {replay.isPending ? 'Replaying…' : 'Replay'}
          </button>
          <button
            type="button"
            data-testid="side-effect-dlq-abandon-btn"
            onClick={onAbandon}
            disabled={!isPending}
            title={
              isPending ? undefined : 'Only pending entries can be abandoned'
            }
            className="text-xs text-accent-error hover:underline disabled:opacity-40 disabled:cursor-not-allowed disabled:no-underline"
          >
            Abandon
          </button>
        </td>
      </tr>
      {showOutcome && (
        <tr
          data-testid={`side-effect-dlq-outcome-${row.id}`}
          className="border-b last:border-0"
          style={{ borderColor: 'rgba(31,41,55,0.5)' }}
        >
          <td colSpan={7} className="px-4 py-2">
            <pre
              className="max-h-64 overflow-auto rounded border border-border/40 bg-bg-primary/70 p-3 font-mono text-[11px] text-text-primary"
              style={{ borderColor: 'rgba(31,41,55,0.5)' }}
            >
              {outcomeText}
            </pre>
          </td>
        </tr>
      )}
    </>
  );
}

function AbandonModal({
  row,
  onClose,
}: {
  row: SideEffectDLQRow;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const abandon = useMutation({
    mutationFn: () => abandonSideEffectDLQ(row.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: DLQ_QUERY_KEY });
      pushToast({
        message: `Side-effect #${row.id} abandoned.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to abandon side-effect.'),
        severity: 'error',
      });
    },
  });

  return (
    <Modal open onClose={onClose} title="Abandon Side-Effect">
      <div
        data-testid="side-effect-dlq-abandon-modal"
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Abandon dead-lettered side-effect{' '}
          <span className="font-semibold font-mono">#{row.id}</span>{' '}
          <span className="text-xs text-text-secondary">
            ({row.effectType}, action log {row.actionLogId})
          </span>
          ?
        </p>
        <p className="text-xs text-text-secondary">
          Abandoning marks this entry as permanently dismissed — it will no
          longer be replayable. Use this when the effect is obsolete or the
          target has been retired. This cannot be undone.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="side-effect-dlq-abandon-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="side-effect-dlq-abandon-confirm"
            onClick={() => abandon.mutate()}
            disabled={abandon.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {abandon.isPending ? 'Abandoning…' : 'Abandon'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
