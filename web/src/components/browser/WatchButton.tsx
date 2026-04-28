import {
  useCreateWatch,
  useDeleteWatch,
  useWatchStatus,
} from '../../hooks/useWatches';

interface WatchButtonProps {
  targetRid: string | null | undefined;
}

// WatchButton toggles the caller's follow state for a single object RID
// (US-337). Renders nothing when the target is unset (no row currently
// open) or when the watches endpoint is unmounted in degraded-mode
// deployments — same hide-on-404 contract as the saved-searches panel.
export function WatchButton({ targetRid }: WatchButtonProps) {
  const status = useWatchStatus(targetRid);
  const create = useCreateWatch();
  const remove = useDeleteWatch();

  if (!targetRid) return null;
  if (status.data && status.data.available === false) return null;

  const watching = status.data?.watching ?? false;
  const pending = status.isLoading || create.isPending || remove.isPending;

  const onToggle = () => {
    if (!targetRid || pending) return;
    if (watching) {
      remove.mutate(targetRid);
    } else {
      create.mutate(targetRid);
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
