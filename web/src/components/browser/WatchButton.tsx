import {
  useCreateWatch,
  useDeleteWatch,
  useWatchStatus,
} from '../../hooks/useWatches';
import { ApiRequestError } from '../../api/client';
import { useToastStore } from '../../stores/toastStore';

interface WatchButtonProps {
  targetRid: string | null | undefined;
}

// describeWatchError turns a failed watch toggle into a short user-facing
// string. Mirrors the convention used by AutomationRulesPage so the toast
// reads "<ErrorName>: <reason>" for structured API errors and falls back to
// the raw message otherwise.
function describeWatchError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason ?? err.parameters?.error;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return 'Watch update failed.';
}

// WatchButton toggles the caller's follow state for a single object RID
// (US-337). Renders nothing when the target is unset (no row currently
// open) or when the watches endpoint is unmounted in degraded-mode
// deployments — same hide-on-404 contract as the saved-searches panel.
export function WatchButton({ targetRid }: WatchButtonProps) {
  const status = useWatchStatus(targetRid);
  const create = useCreateWatch();
  const remove = useDeleteWatch();
  const pushToast = useToastStore((s) => s.push);

  if (!targetRid) return null;
  if (status.data && status.data.available === false) return null;

  const watching = status.data?.watching ?? false;
  const pending = status.isLoading || create.isPending || remove.isPending;

  const onToggle = () => {
    if (!targetRid || pending) return;
    if (watching) {
      // Surface unwatch failures: the hook's onSuccess flips the cached
      // status, but without an onError a 5xx/timeout left the button stuck
      // with zero feedback. Push a user-visible error toast instead.
      remove.mutate(targetRid, {
        onError: (err) =>
          pushToast({
            message: `Failed to stop watching: ${describeWatchError(err)}`,
            severity: 'error',
          }),
      });
    } else {
      create.mutate(targetRid, {
        onError: (err) =>
          pushToast({
            message: `Failed to watch object: ${describeWatchError(err)}`,
            severity: 'error',
          }),
      });
    }
  };

  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={watching}
      disabled={pending}
      data-testid="watch-button"
      data-watching={watching ? 'true' : 'false'}
      className={[
        'px-2 py-1 text-xs font-mono rounded border transition-colors',
        watching
          ? 'border-accent-cyan/50 bg-accent-cyan/10 text-accent-cyan hover:bg-accent-cyan/20'
          : 'border-border text-text-secondary hover:text-text-primary hover:border-text-secondary',
        pending ? 'opacity-60 cursor-progress' : '',
      ].join(' ')}
      title={watching ? 'Stop watching this object' : 'Watch this object for changes'}
    >
      <span aria-hidden="true" className="mr-1">
        {watching ? '★' : '☆'}
      </span>
      {watching ? 'Watching' : 'Watch'}
    </button>
  );
}
