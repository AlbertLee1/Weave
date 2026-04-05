interface EmptyStateProps {
  title: string;
  description?: string;
  action?: React.ReactNode;
}

export function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-20 text-center">
      {/* Icon container with gradient background */}
      <div
        className="relative mb-6"
        style={{ animation: 'fadeInUp 400ms ease-out both' }}
      >
        <div
          className="w-16 h-16 rounded-2xl flex items-center justify-center"
          style={{
            background: 'linear-gradient(135deg, rgba(245,158,11,0.12) 0%, rgba(20,184,166,0.08) 100%)',
            border: '1px solid rgba(245,158,11,0.2)',
            boxShadow: '0 0 24px rgba(245,158,11,0.08), inset 0 1px 0 rgba(245,158,11,0.1)',
          }}
        >
          <svg
            className="w-7 h-7"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            style={{ color: 'rgba(245,158,11,0.7)' }}
          >
            <path d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
          </svg>
        </div>
        {/* Subtle corner sparkle */}
        <span
          className="absolute -top-1 -right-1 w-2 h-2 rounded-full"
          style={{
            background: '#14B8A6',
            boxShadow: '0 0 6px rgba(20,184,166,0.8)',
            animation: 'spinnerPulse 2.4s ease-in-out infinite',
          }}
        />
      </div>

      {/* Title */}
      <h3
        className="text-base font-semibold text-text-primary mb-2 tracking-tight"
        style={{
          fontFamily: 'var(--font-sans)',
          animation: 'fadeInUp 400ms 80ms ease-out both',
        }}
      >
        {title}
      </h3>

      {/* Description */}
      {description && (
        <p
          className="text-sm text-text-secondary max-w-xs leading-relaxed"
          style={{ animation: 'fadeInUp 400ms 160ms ease-out both' }}
        >
          {description}
        </p>
      )}

      {/* Action */}
      {action && (
        <div
          className="mt-6"
          style={{ animation: 'fadeInUp 400ms 240ms ease-out both' }}
        >
          {action}
        </div>
      )}

      <style>{`
        @keyframes spinnerPulse {
          0%, 100% { opacity: 0.5; transform: scale(0.9); }
          50%       { opacity: 1;   transform: scale(1.1); }
        }
      `}</style>
    </div>
  );
}
