import { useEffect, useRef } from 'react';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
}

export function Modal({ open, onClose, title, children }: ModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open, onClose]);

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
        className="w-full max-w-lg mx-4 rounded-xl border border-border/50 overflow-hidden"
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
