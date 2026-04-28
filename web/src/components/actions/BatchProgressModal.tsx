import { useState } from 'react';
import { Modal } from '../common/Modal';
import { useActionJobProgress } from '../../hooks/useActionJobProgress';
import { cancelActionJob } from '../../api/actions';
import type { ActionJobStatus } from '../../api/actions';

export interface BatchProgressModalProps {
  // ontologyApiName scopes both the WebSocket URL and the cancel REST call.
  ontologyApiName: string;
  // jobId is null when the modal is closed; non-null after the parent has
  // submitted the async batch and received a 202.
  jobId: string | null;
  // open controls visibility independently of jobId so the modal can be
  // shown with a "scheduling…" placeholder before the async POST returns.
  open: boolean;
  // onClose fires when the user dismisses the modal — either after a
  // terminal state via the close button, after Esc, or after clicking the
  // overlay. The parent owns the visibility state so it can also clear
  // selection / refetch the underlying list.
  onClose: () => void;
}

const STATUS_LABEL: Record<ActionJobStatus | 'CONNECTING', string> = {
  CONNECTING: 'Connecting…',
  PENDING: 'Pending',
  RUNNING: 'Running',
  SUCCEEDED: 'Succeeded',
  FAILED: 'Failed',
  CANCELED: 'Canceled',
};

const STATUS_COLOR: Record<ActionJobStatus | 'CONNECTING', string> = {
  CONNECTING: 'text-text-secondary',
  PENDING: 'text-text-secondary',
  RUNNING: 'text-accent-cyan',
  SUCCEEDED: 'text-accent-success',
  FAILED: 'text-accent-error',
  CANCELED: 'text-accent-warning',
};

const TERMINAL: ReadonlySet<ActionJobStatus | 'CONNECTING'> = new Set([
  'SUCCEEDED',
  'FAILED',
  'CANCELED',
]);

export function BatchProgressModal({
  ontologyApiName,
  jobId,
  open,
  onClose,
}: BatchProgressModalProps) {
  const [cancelError, setCancelError] = useState<string | null>(null);
  const [cancelInflight, setCancelInflight] = useState(false);

  const progress = useActionJobProgress({
    ontologyApiName,
    jobId,
    enabled: open && jobId !== null,
  });

  const isTerminal = TERMINAL.has(progress.status);
  const showCancel = open && jobId !== null && !isTerminal;
  const percent = clamp(progress.percent, 0, 100);

  const handleCancel = async () => {
    if (!jobId) return;
    setCancelError(null);
    setCancelInflight(true);
    try {
      await cancelActionJob(ontologyApiName, jobId);
    } catch (err) {
      setCancelError(err instanceof Error ? err.message : 'Cancel failed');
    } finally {
      setCancelInflight(false);
    }
  };

  return (
    <Modal open={open} onClose={onClose} title="Batch action progress">
      <div className="flex flex-col gap-4" data-testid="batch-progress-modal">
        {jobId === null ? (
          <p className="text-sm text-text-secondary" data-testid="batch-scheduling">
            Scheduling…
          </p>
        ) : (
          <>
            <div className="flex items-center justify-between">
              <span
                className={`text-xs font-mono uppercase tracking-wider ${STATUS_COLOR[progress.status]}`}
                data-testid="batch-status"
              >
                {STATUS_LABEL[progress.status]}
              </span>
              <span
                className="text-xs font-mono text-text-secondary"
                data-testid="batch-percent"
              >
                {percent}%
              </span>
            </div>
            <div className="w-full h-2 rounded-full bg-bg-secondary overflow-hidden">
              <div
                role="progressbar"
                aria-valuenow={percent}
                aria-valuemin={0}
                aria-valuemax={100}
                className="h-full bg-accent-cyan transition-all duration-200"
                style={{ width: `${percent}%` }}
                data-testid="batch-progress-bar"
              />
            </div>
            {progress.message && (
              <p
                className="text-xs font-mono text-text-secondary"
                data-testid="batch-message"
              >
                {progress.message}
              </p>
            )}
            {progress.status === 'FAILED' && (
              <p
                className="text-xs font-mono text-accent-error"
                role="alert"
                data-testid="batch-error"
              >
                Action failed.
              </p>
            )}
            {progress.status === 'CANCELED' && (
              <p
                className="text-xs font-mono text-accent-warning"
                data-testid="batch-canceled"
              >
                Canceled by user. Already-applied actions were not rolled back.
              </p>
            )}
            {cancelError && (
              <p
                className="text-xs font-mono text-accent-error"
                role="alert"
                data-testid="batch-cancel-error"
              >
                {cancelError}
              </p>
            )}
          </>
        )}
        <div className="flex justify-end gap-2 pt-2">
          {showCancel && (
            <button
              type="button"
              onClick={handleCancel}
              disabled={cancelInflight}
              className="px-3 py-2 rounded text-xs font-sans text-accent-warning border border-accent-warning/40 hover:bg-accent-warning/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
              data-testid="batch-cancel"
            >
              {cancelInflight ? 'Cancelling…' : 'Cancel'}
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            disabled={!isTerminal && jobId !== null}
            className="px-3 py-2 rounded text-xs font-sans text-text-secondary hover:text-text-primary border border-border disabled:opacity-40 disabled:cursor-not-allowed"
            data-testid="batch-close"
          >
            Close
          </button>
        </div>
      </div>
    </Modal>
  );
}

function clamp(value: number, min: number, max: number): number {
  if (Number.isNaN(value)) return min;
  return Math.max(min, Math.min(max, Math.round(value)));
}
