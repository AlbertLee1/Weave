import { useMemo, useState } from 'react';
import { useLocation } from 'react-router';
import { NotificationCenter } from '../common/NotificationCenter';
import { useNotifications } from '../../hooks/useNotifications';

function pathToBreadcrumbs(pathname: string): string[] {
  const segments = pathname.split('/').filter(Boolean);
  if (segments.length === 0) return ['Dashboard'];
  return segments.map((s) => s.charAt(0).toUpperCase() + s.slice(1));
}

export function Topbar() {
  const location = useLocation();
  const breadcrumbs = pathToBreadcrumbs(location.pathname);
  const [panelOpen, setPanelOpen] = useState(false);

  const { data } = useNotifications({ unreadOnly: true });
  const unreadCount = useMemo(
    () => (data?.data ?? []).filter((n) => !n.read).length,
    [data],
  );
  const badgeLabel = unreadCount > 9 ? '9+' : String(unreadCount);

  return (
    <header
      data-testid="topbar"
      className="relative flex items-center h-12 px-6 border-b"
      style={{
        background: 'rgba(13, 17, 23, 0.60)',
        backdropFilter: 'blur(12px)',
        WebkitBackdropFilter: 'blur(12px)',
        borderColor: 'rgba(31, 41, 55, 0.30)',
      }}
    >
      <div className="flex items-center gap-1.5 text-sm font-sans">
        {breadcrumbs.map((crumb, i) => (
          <span key={i} className="flex items-center gap-1.5">
            {i > 0 && (
              <span
                className="select-none"
                style={{ color: 'rgba(75, 85, 99, 0.6)', fontSize: '0.75rem' }}
              >
                /
              </span>
            )}
            <span
              className={
                i === breadcrumbs.length - 1
                  ? 'text-text-primary font-medium'
                  : 'text-text-secondary'
              }
            >
              {crumb}
            </span>
          </span>
        ))}
      </div>

      <div className="ml-auto flex items-center">
        <button
          type="button"
          aria-label="Notifications"
          onClick={() => setPanelOpen(true)}
          className="relative p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-white/5 transition-colors"
        >
          <svg
            className="w-5 h-5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.73 21a2 2 0 0 1-3.46 0" />
          </svg>
          {unreadCount > 0 && (
            <span
              data-testid="notification-badge"
              className="absolute -top-0.5 -right-0.5 min-w-[16px] h-4 px-1 rounded-full bg-accent-cyan text-[10px] font-semibold text-bg-primary flex items-center justify-center"
            >
              {badgeLabel}
            </span>
          )}
        </button>
      </div>

      {/* Subtle gradient line below */}
      <div
        className="absolute bottom-0 left-0 right-0 h-px"
        style={{
          background:
            'linear-gradient(90deg, transparent 0%, rgba(245,158,11,0.15) 30%, rgba(20,184,166,0.15) 70%, transparent 100%)',
        }}
      />

      <NotificationCenter open={panelOpen} onClose={() => setPanelOpen(false)} />
    </header>
  );
}
