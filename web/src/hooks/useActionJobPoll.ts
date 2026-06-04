import { useEffect, useState } from 'react';
import { getActionJob } from '../api/actions';
import type { ActionJob, ActionJobStatus } from '../api/actions';

// Terminal statuses end the poll loop. PENDING / RUNNING keep it ticking.
const TERMINAL_STATUSES: ReadonlySet<ActionJobStatus> = new Set([
  'SUCCEEDED',
  'FAILED',
  'CANCELED',
]);

function isTerminal(status: ActionJobStatus): boolean {
  return TERMINAL_STATUSES.has(status);
}

export interface ActionJobPollState {
  // Latest persisted job row, or null before the first successful poll.
  job: ActionJob | null;
  // True while the poll loop is active (job not yet terminal AND not errored
  // AND the attempt cap has not been hit).
  polling: boolean;
  // Transport error from the last poll (e.g. job row not found, 5xx). The
  // loop stops once this is set so a broken endpoint can't spin forever.
  error: Error | null;
}

export interface UseActionJobPollOptions {
  // ontologyApiName scopes the GET /actions/jobs/{jobId} URL.
  ontologyApiName: string;
  // jobId names the action_jobs row to poll. null disables the loop (e.g.
  // before the async POST has returned a 202).
  jobId: string | null;
  // intervalMs is the gap between polls. Defaults to 1s — matches the
  // server's RUNNING-progress cadence closely enough for a smooth bar.
  intervalMs?: number;
  // maxAttempts hard-caps the number of polls so a job that never settles
  // (e.g. a wedged worker) cannot poll forever. Defaults to 600 (~10 minutes
  // at the 1s default), after which the loop stops and `polling` flips false.
  maxAttempts?: number;
}

/**
 * useActionJobPoll polls GET /api/v2/ontologies/{ontology}/actions/jobs/{jobId}
 * on a fixed interval until the job reaches a terminal status (SUCCEEDED /
 * FAILED / CANCELED), a transport error occurs, or the attempt cap is hit.
 *
 * This is the polling counterpart to the WebSocket-driven useActionJobProgress
 * used by the batch modal — chosen for the single-apply console because the
 * persisted GET endpoint is the authoritative source of the result payload and
 * needs no live socket. The loop is self-terminating on every exit condition
 * (terminal status, error, cap, jobId change, unmount) so it never runs away.
 */
export function useActionJobPoll(
  options: UseActionJobPollOptions,
): ActionJobPollState {
  const {
    ontologyApiName,
    jobId,
    intervalMs = 1000,
    maxAttempts = 600,
  } = options;

  const [state, setState] = useState<ActionJobPollState>({
    job: null,
    polling: false,
    error: null,
  });

  // Render-phase reset whenever the tracked (ontology, jobId) tuple changes so
  // a re-applied action does not show the previous job's terminal state while
  // the new poll spins up.
  const trackingKey = jobId ? `${ontologyApiName}::${jobId}` : '';
  const [prevKey, setPrevKey] = useState(trackingKey);
  if (trackingKey !== prevKey) {
    setPrevKey(trackingKey);
    setState({ job: null, polling: jobId !== null, error: null });
  }

  useEffect(() => {
    if (!jobId || !ontologyApiName) {
      return;
    }

    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let attempts = 0;

    setState((prev) => ({ ...prev, polling: true, error: null }));

    const poll = async () => {
      if (disposed) return;
      attempts += 1;
      try {
        const job = await getActionJob(ontologyApiName, jobId);
        if (disposed) return;
        if (isTerminal(job.status)) {
          // Terminal: record final row and stop the loop.
          setState({ job, polling: false, error: null });
          return;
        }
        // Non-terminal: record progress and schedule the next poll unless the
        // attempt cap has been reached (guards against a never-settling job).
        setState({ job, polling: true, error: null });
        if (attempts >= maxAttempts) {
          setState({ job, polling: false, error: null });
          return;
        }
        timer = setTimeout(() => void poll(), intervalMs);
      } catch (err) {
        if (disposed) return;
        // Transport error: surface it and stop polling. A broken / missing
        // endpoint must not spin forever.
        setState((prev) => ({
          ...prev,
          polling: false,
          error: err instanceof Error ? err : new Error('Job poll failed'),
        }));
      }
    };

    void poll();

    return () => {
      disposed = true;
      if (timer !== null) clearTimeout(timer);
    };
  }, [ontologyApiName, jobId, intervalMs, maxAttempts]);

  return state;
}
