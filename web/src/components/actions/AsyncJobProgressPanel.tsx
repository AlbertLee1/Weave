import { useActionJobPoll } from '../../hooks/useActionJobPoll';
import { ActionResult } from './ActionResult';
import type { ActionJobStatus } from '../../api/actions';
import type { ActionApplyResponse } from '../../api/types';

export interface AsyncJobProgressPanelProps {
  // ontologyApiName scopes the GET /actions/jobs/{jobId} poll URL.
  ontologyApiName: string;
  // jobId names the action_jobs row to track. null renders a "scheduling…"
  // placeholder (the async POST 202 hasn't returned yet).
  jobId: string | null;
}

const STATUS_LABEL: Record<ActionJobStatus, string> = {
  PENDING: 'Pending',
  RUNNING: 'Running',
  SUCCEEDED: 'Succeeded',
  FAILED: 'Failed',
  CANCELED: 'Canceled',
};

const STATUS_COLOR: Record<ActionJobStatus, string> = {
  PENDING: 'text-text-secondary',
  RUNNING: 'text-accent-cyan',
  SUCCEEDED: 'text-accent-success',
  FAILED: 'text-accent-error',
  CANCELED: 'text-accent-warning',
};

function clamp(value: number, min: number, max: number): number {
  if (Number.isNaN(value)) return min;
  return Math.max(min, Math.min(max, Math.round(value)));
}

/**
 * AsyncJobProgressPanel renders the live status of an async single-apply job.
 * It owns a polling subscription to GET /actions/jobs/{jobId} via
 * useActionJobPoll and surfaces the progress bar, status pill, and — on
 * SUCCEEDED — the edit summary stored in the job's result payload (the same
 * SyncApplyActionResponseV2 envelope the sync path would have returned).
 */
export function AsyncJobProgressPanel({
  ontologyApiName,
  jobId,
}: AsyncJobProgressPanelProps) {
  const { job, error } = useActionJobPoll({ ontologyApiName, jobId });

  if (jobId === null) {
    return null;
  }

  // Before the first successful poll the row is unknown; show "Pending" so the
  // panel never renders an empty shell.
  const status: ActionJobStatus = job?.status ?? 'PENDING';
  const percent = clamp(job?.progress ?? 0, 0, 100);
  const result = (job?.status === 'SUCCEEDED'
    ? (job.result as ActionApplyResponse | undefined) ?? null
    : null);

  return (
    <div
      className="border border-border rounded p-4 bg-bg-tertiary flex flex-col gap-3"
      data-testid="async-job-progress"
    >
      <div className="flex items-center justify-between">
        <span
          className={`text-xs font-mono uppercase tracking-wider ${STATUS_COLOR[status]}`}
          data-testid="async-job-status"
        >
          {STATUS_LABEL[status]}
        </span>
        <span
          className="text-xs font-mono text-text-secondary"
          data-testid="async-job-percent"
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
          data-testid="async-job-progress-bar"
        />
      </div>

      {status === 'FAILED' && (
        <p
          className="text-xs font-mono text-accent-error"
          role="alert"
          data-testid="async-job-error"
        >
          {job?.errorMessage || 'Action failed.'}
        </p>
      )}

      {status === 'CANCELED' && (
        <p
          className="text-xs font-mono text-accent-warning"
          data-testid="async-job-canceled"
        >
          Canceled. Already-applied edits were not rolled back.
        </p>
      )}

      {error && (
        <p
          className="text-xs font-mono text-accent-error"
          role="alert"
          data-testid="async-job-poll-error"
        >
          Could not load job status: {error.message}
        </p>
      )}

      {status === 'SUCCEEDED' && <ActionResult result={result} />}
    </div>
  );
}
