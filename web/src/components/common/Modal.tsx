import { useEffect, useId, useRef } from 'react';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  size?: 'md' | 'lg' | 'xl';
}

const SIZE_CLASS: Record<NonNullable<ModalProps['size']>, string> = {
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
};

// Selector for elements that can receive keyboard focus, used by the focus trap.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

export function Modal({ open, onClose, title, children, size = 'md' }: ModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  // Per-instance unique id wiring this dialog's aria-labelledby to its own <h2>
  // title. A module-level constant would collide when two Modals are stacked,
  // making both <h2> share one id and mislabelling the top dialog.
  const titleId = useId();

  useEffect(() => {
    if (!open) return;
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open, onClose]);

  // Move focus into the dialog when it opens, and restore focus to whatever was
  // focused before (typically the trigger) when it closes.
  useEffect(() => {
    if (!open) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (panel) {
      const focusables = panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
      const first = focusables[0];
      // Prefer the first focusable child; fall back to the panel itself
      // (made programmatically focusable via tabIndex={-1}) when none exist,
      // so screen-reader/keyboard focus never sits on the background page.
      if (first) first.focus();
      else panel.focus();
    }
    return () => {
      if (previouslyFocused && typeof previouslyFocused.focus === 'function') {
        previouslyFocused.focus();
      }
    };
  }, [open]);

  // Focus trap: keep Tab / Shift+Tab cycling among the dialog's focusable
  // elements instead of escaping to the background page.
  function handleTrapKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Tab') return;
    const panel = panelRef.current;
    if (!panel) return;
    const focusables = Array.from(
      panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    );

    // Degenerate case: nothing focusable inside — keep focus on the panel.
    if (focusables.length === 0) {
      e.preventDefault();
      panel.focus();
      return;
    }

    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = document.activeElement;

    if (e.shiftKey) {
      // Shift+Tab on the first element (or focus already outside) wraps to last.
      if (active === first || !panel.contains(active)) {
        e.preventDefault();
        last.focus();
      }
    } else {
      // Tab on the last element (or focus already outside) wraps to first.
      if (active === last || !panel.contains(active)) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  if (!open) return null;

  return (
    <div
      ref={overlayRef}
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{
        background: 'linear-gradient(to bottom, rgba(0,0,0,0.85), rgba(8,11,22,0.9))',
        animation: 'fadeIn 150ms ease-out both',
      }}
      onClick={(e) => {
        if (e.target === overlayRef.current) onClose();
      }}
      data-testid="modal-overlay"
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        aria-label={title ? undefined : 'Dialog'}
        tabIndex={-1}
        onKeyDown={handleTrapKeyDown}
        className={`w-full ${SIZE_CLASS[size]} mx-4 rounded-xl border border-border/50 overflow-hidden`}
        style={{
          background: 'rgba(30,36,51,0.92)',
          backdropFilter: 'blur(24px)',
          WebkitBackdropFilter: 'blur(24px)',
          boxShadow: '0 25px 60px rgba(0,0,0,0.6), 0 0 0 1px rgba(245,158,11,0.06)',
          animation: 'modalEnter 180ms cubic-bezier(0.34,1.56,0.64,1) both',
        }}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-border/50">
          <h2
            id={titleId}
            className="text-xl font-semibold text-text-primary tracking-tight"
            style={{ fontFamily: 'var(--font-sans)' }}
          >
            {title}
          </h2>
          <button
            onClick={onClose}
            className="flex items-center justify-center w-7 h-7 rounded-full text-text-muted hover:text-text-primary hover:bg-bg-elevated transition-all duration-150"
            aria-label="Close"
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="p-6">{children}</div>
      </div>

      <style>{`
        @keyframes modalEnter {
          from { opacity: 0; transform: scale(0.95) translateY(4px); }
          to   { opacity: 1; transform: scale(1)    translateY(0);    }
        }
      `}</style>
    </div>
  );
}
