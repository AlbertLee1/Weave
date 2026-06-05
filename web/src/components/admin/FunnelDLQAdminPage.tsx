import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  discardFunnelDLQ,
  listFunnelDLQ,
  replayFunnelDLQ,
  type DLQEntry,
} from '../../api/funnelDLQ';
import { ApiRequestError } from '../../api/client';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';

const DLQ_KEY = ['admin', 'funnel-dlq'] as const;

type PushToast = ReturnType<typeof useToastStore.getState>['push'];

export function FunnelDLQAdminPage() {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const { data, isLoading, error } = useQuery({
    queryKey: DLQ_KEY,
    queryFn: () => listFunnelDLQ(),
    // The DLQ-not-configured 503 is a benign degraded state, not a transient
    // failure to retry — it is handled inline below. Keep retry off so the
    // friendly state appears immediately.
    retry: false,
  });

  const [discarding, setDiscarding] = useState<DLQEntry | null>(null);

  function invalidate() {
    queryClient.invalidateQueries({ queryKey: DLQ_KEY });
  }

  const replayMutation = useMutation({
    mutationFn: (id: string) => replayFunnelDLQ(id),
    onSuccess: (res) => {
      invalidate();
      pushToast({
        message: `Replayed ${res.id} → ${res.originalSubject}`,
        severity: 'success',
      });
    },
    onError: (err) =>
      pushToast({
        message: describeApiError(err, 'Failed to replay DLQ entry.'),
        severity: 'error',
      }),
  });

  const entries = data?.entries ?? [];
  const size = data?.size ?? 0;

  // The DLQ-not-configured signal is HTTP 503 (FunnelDLQNotConfigured). We
  // special-case it into a friendly informational state rather than a red
  // error so operators understand the feature simply isn't enabled.
  const notConfigured =
    error instanceof ApiRequestError && error.statusCode === 503;
  const genericError = error && !notConfigured;

  return (
    <div
      data-testid="funnel-dlq-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Funnel DLQ
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Dead-Lettered Edit Batches
        </span>
        <div className="flex-1" />
        {!notConfigured && !genericError && (
          <span
            data-testid="funnel-dlq-depth"
            className="px-2.5 py-1 text-xs font-mono rounded border border-amber-500/40 bg-amber-500/10 text-amber-300"
            title="Authoritative DLQ depth (may exceed rows shown)"
          >
            Depth: {size}
          </span>
        )}
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {isLoading && (
          <div data-testid="funnel-dlq-loading" className="flex flex-col gap-2">
            {[0, 1, 2].map((i) => (
              <div
                key={i}
                className="h-12 rounded animate-pulse"
                style={{ background: 'rgba(31,41,55,0.4)' }}
              />
            ))}
          </div>
        )}

        {!isLoading && notConfigured && (
          <div
            data-testid="funnel-dlq-not-configured"
            className="mx-auto max-w-xl rounded border px-6 py-8 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm font-semibold text-text-primary">
              Funnel DLQ is not enabled
            </p>
            <p className="text-xs text-text-secondary mt-2 leading-relaxed">
              The dead-letter queue is not configured on this server. It
              requires a NATS JetStream connection; in degraded / single-machine
              bootstraps without NATS there is no DLQ to inspect. Start the
              server with a reachable NATS to enable replay and discard
              operations.
            </p>
          </div>
        )}

        {!isLoading && genericError && (
          <p
            data-testid="funnel-dlq-error"
            role="alert"
            className="text-sm text-accent-error"
          >
            Failed to load the funnel DLQ: {(error as Error).message}
          </p>
        )}

        {!isLoading && !notConfigured && !genericError && entries.length === 0 && (
          <div
            data-testid="funnel-dlq-empty"
            className="mx-auto max-w-xl rounded border px-6 py-8 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm font-semibold text-text-primary">
              The DLQ is empty
            </p>
            <p className="text-xs text-text-secondary mt-2">
              No edit batches have been dead-lettered. Batches land here only
              after exhausting their delivery attempts.
            </p>
          </div>
        )}

        {!isLoading && !notConfigured && !genericError && entries.length > 0 && (
          <table data-testid="funnel-dlq-table" className="w-full text-sm">
            <thead className="text-[10px] uppercase tracking-widest text-text-secondary border-b border-border/30">
              <tr>
                <th className="text-left py-2 px-2 font-semibold">ID</th>
                <th className="text-left py-2 px-2 font-semibold">Subject</th>
                <th className="text-left py-2 px-2 font-semibold">Reason</th>
                <th className="text-right py-2 px-2 font-semibold">Deliveries</th>
                <th className="py-2 px-2 w-44"></th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => {
                const busy =
                  replayMutation.isPending &&
                  replayMutation.variables === entry.id;
                return (
                  <tr
                    key={entry.id}
                    data-testid={`funnel-dlq-row-${entry.id}`}
                    className="border-b border-border/10 hover:bg-white/[0.02] align-top"
                  >
                    <td className="py-2 px-2 font-mono text-text-primary break-all">
                      {entry.id}
                    </td>
                    <td className="py-2 px-2 font-mono text-text-secondary break-all">
                      <span className="block text-text-primary">
                        {entry.subject}
                      </span>
                      <span className="block text-[11px] text-text-secondary">
                        → {entry.message.originalSubject}
                      </span>
                    </td>
                    <td className="py-2 px-2 text-rose-300">
                      {entry.message.reason}
                    </td>
                    <td className="py-2 px-2 text-right font-mono text-text-primary">
                      {entry.message.maxDeliveries}
                    </td>
                    <td className="py-2 px-2 text-right whitespace-nowrap">
                      <button
                        type="button"
                        data-testid={`funnel-dlq-replay-btn-${entry.id}`}
                        onClick={() => replayMutation.mutate(entry.id)}
                        disabled={busy}
                        className="text-xs px-2 py-1 mr-2 rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
                      >
                        {busy ? 'Replaying…' : 'Replay'}
                      </button>
                      <button
                        type="button"
                        data-testid={`funnel-dlq-discard-btn-${entry.id}`}
                        onClick={() => setDiscarding(entry)}
                        className="text-xs px-2 py-1 rounded border border-accent-error/30 text-accent-error hover:bg-accent-error/10"
                      >
                        Discard
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {discarding && (
        <DiscardDLQModal
          entry={discarding}
          onClose={() => setDiscarding(null)}
          onDiscarded={() => {
            invalidate();
            setDiscarding(null);
          }}
          pushToast={pushToast}
        />
      )}
    </div>
  );
}

function DiscardDLQModal({
  entry,
  onClose,
  onDiscarded,
  pushToast,
}: {
  entry: DLQEntry;
  onClose: () => void;
  onDiscarded: () => void;
  pushToast: PushToast;
}) {
  const mutation = useMutation({
    mutationFn: () => discardFunnelDLQ(entry.id),
    onSuccess: () => {
      pushToast({
        message: `Discarded ${entry.id} from the DLQ.`,
        severity: 'success',
      });
      onDiscarded();
    },
    onError: (err) =>
      pushToast({
        message: describeApiError(err, 'Failed to discard DLQ entry.'),
        severity: 'error',
      }),
  });

  return (
    <Modal open onClose={onClose} title="Discard DLQ Entry">
      <div data-testid="funnel-dlq-discard-modal" className="flex flex-col gap-3">
        <p className="text-sm text-text-primary">
          Discard <span className="font-mono">{entry.id}</span>?
        </p>
        <p className="text-xs text-text-secondary">
          The dead-lettered edit batch will be permanently dropped without
          being re-applied. Its mutations will never reach the indexes. This
          cannot be undone — use{' '}
          <span className="text-accent-cyan">Replay</span> instead if you want
          to retry it.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="funnel-dlq-discard-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="funnel-dlq-discard-confirm"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {mutation.isPending ? 'Discarding…' : 'Discard'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
