import { create } from 'zustand';

// Toast is a transient notification displayed at the bottom-right of the
// shell. US-319 introduces it primarily to host the post-action Undo
// affordance: every successful Apply pushes a toast carrying the
// `actionLogId` and a 5-second auto-dismiss timer; clicking Undo invokes
// the toast's onAction callback.
//
// Severity drives the border / icon styling. The default `info` is a calm
// neutral accent; `success` / `error` are emerald / rose to match the rest
// of the app's status palette.
export type ToastSeverity = 'info' | 'success' | 'error';

export interface Toast {
  id: string;
  message: string;
  severity?: ToastSeverity;
  // actionLabel + onAction render an inline button (e.g. "Undo") inside the
  // toast tile. Clicking does NOT auto-dismiss the toast — the caller is
  // expected to dismiss it once the async action completes (or to mutate
  // the toast text via dismiss + push).
  actionLabel?: string;
  onAction?: () => void;
  // ttlMs is the auto-dismiss timer in milliseconds. Zero disables the timer
  // (caller must dismiss manually). Default 5000ms — matches the PRD's
  // "5-second window" for action Undo.
  ttlMs?: number;
}

interface ToastState {
  toasts: Toast[];
  push: (toast: Omit<Toast, 'id'> & { id?: string }) => string;
  dismiss: (id: string) => void;
  clear: () => void;
}

let toastCounter = 0;
function nextToastId(): string {
  toastCounter += 1;
  return `toast-${Date.now()}-${toastCounter}`;
}

export const useToastStore = create<ToastState>()((set) => ({
  toasts: [],
  push: (toast) => {
    const id = toast.id ?? nextToastId();
    set((s) => ({ toasts: [...s.toasts, { ...toast, id }] }));
    return id;
  },
  dismiss: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
  clear: () => set({ toasts: [] }),
}));
