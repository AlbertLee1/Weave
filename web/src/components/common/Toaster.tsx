import { useEffect } from 'react';
import { useToastStore, type Toast } from '../../stores/toastStore';

const SEVERITY_BORDER: Record<string, string> = {
  info: 'border-accent-cyan/50 bg-bg-secondary/95',
  success: 'border-emerald-500/40 bg-bg-secondary/95',
  error: 'border-rose-500/50 bg-bg-secondary/95',
};

// Toaster renders the queue from useToastStore as a stack of tiles in the
// bottom-right of the shell. Each tile auto-dismisses after `ttlMs` (default
// 5000ms) unless ttlMs is 0. Mount once in <Shell /> so every page can push
// without per-page wiring.
//
// Tiles render an optional inline action button (e.g. "Undo" for US-319);
// clicking the action invokes the supplied callback but does NOT auto-dismiss
// — the caller dismisses on success/failure of whatever the action triggered.
// The × button always dismisses immediately regardless of state.
export function Toaster() {
  const toasts = useToastStore((s) => s.toasts);

  if (toasts.length === 0) {
    return null;
  }

  return (
    <div
      role="region"
      aria-label="Notifications"
      data-testid="toaster"
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-96 max-w-[calc(100vw-2rem)] flex-col gap-2"
    >
      {toasts.map((t) => (
        <ToastTile key={t.id} toast={t} />
      ))}
    </div>
  );
}

function ToastTile({ toast }: { toast: Toast }) {
  const dismiss = useToastStore((s) => s.dismiss);
  const ttl = toast.ttlMs ?? 5000;
  const severity = toast.severity ?? 'info';

  useEffect(() => {
    if (ttl <= 0) return;
    const timer = window.setTimeout(() => dismiss(toast.id), ttl);
    return () => window.clearTimeout(timer);
  }, [toast.id, ttl, dismiss]);

  const borderClass = SEVERITY_BORDER[severity] ?? SEVERITY_BORDER.info;

  return (
    <div
      role="status"
      data-testid="toast"
      data-toast-id={toast.id}
      className={`pointer-events-auto flex items-start gap-3 rounded-lg border px-3 py-2.5 text-sm text-text-primary shadow-lg backdrop-blur ${borderClass}`}
    >
      <div className="flex-1 break-words">{toast.message}</div>
      {toast.actionLabel && toast.onAction && (
        <button
          type="button"
          onClick={toast.onAction}
          className="rounded-md border border-accent-cyan/60 px-2 py-1 text-xs font-medium text-accent-cyan hover:bg-accent-cyan/10"
          data-testid="toast-action"
        >
          {toast.actionLabel}
        </button>
      )}
      <button
        type="button"
        onClick={() => dismiss(toast.id)}
        aria-label="Dismiss notification"
        className="rounded p-1 text-text-secondary hover:bg-bg-tertiary hover:text-text-primary"
        data-testid="toast-dismiss"
      >
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" aria-hidden="true">
          <path
            d="M3 3 L13 13 M13 3 L3 13"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            fill="none"
          />
        </svg>
      </button>
    </div>
  );
}
