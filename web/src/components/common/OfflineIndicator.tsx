import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useOnlineStatus } from '../../hooks/useOnlineStatus';

// OfflineIndicator (US-354). A persistent banner pinned to the top of the
// shell that surfaces network state. Three visual states:
//   - online (default)        → not rendered
//   - offline                 → amber banner with `离线` copy
//   - just reconnected        → teal "已恢复" pulse for 2.5s, then unmounts
//
// The transient "reconnected" pulse is what makes auto-sync visible —
// TanStack Query's onlineManager kicks in invisibly behind the scenes,
// but the banner gives the user a "your data is being refreshed" beat.
//
// Stays at the very top of the layout above the breadcrumbs so it never
// competes with content scroll. Pure presentational + a single
// useOnlineStatus subscription; safe to mount once inside <Shell>.
export function OfflineIndicator() {
  const online = useOnlineStatus();
  const { t } = useTranslation();
  const [showReconnected, setShowReconnected] = useState(false);
  const [prevOnline, setPrevOnline] = useState(online);

  // Render-phase setState comparison (progress.txt:119, US-315 prior art):
  // detect the offline → online transition and arm the "reconnected" pulse
  // without an effect-driven setState, which the React 19 lint rule
  // (`react-hooks/set-state-in-effect`) rejects.
  if (online !== prevOnline) {
    setPrevOnline(online);
    if (!prevOnline && online) {
      setShowReconnected(true);
    }
  }

  useEffect(() => {
    if (!showReconnected) return;
    const id = window.setTimeout(() => setShowReconnected(false), 2500);
    return () => window.clearTimeout(id);
  }, [showReconnected]);

  if (online && !showReconnected) return null;

  if (!online) {
    return (
      <div
        role="status"
        aria-live="polite"
        data-testid="offline-indicator"
        data-state="offline"
        className="flex items-center justify-center gap-2 px-4 py-1.5 text-xs font-medium"
        style={{
          background: 'rgba(245, 158, 11, 0.15)',
          color: '#F59E0B',
          borderBottom: '1px solid rgba(245, 158, 11, 0.3)',
        }}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
          focusable="false"
        >
          <line x1="1" y1="1" x2="23" y2="23" />
          <path d="M16.72 11.06A10.94 10.94 0 0 1 19 12.55" />
          <path d="M5 12.55a10.94 10.94 0 0 1 5.17-2.39" />
          <path d="M10.71 5.05A16 16 0 0 1 22.58 9" />
          <path d="M1.42 9a15.91 15.91 0 0 1 4.7-2.88" />
          <path d="M8.53 16.11a6 6 0 0 1 6.95 0" />
          <line x1="12" y1="20" x2="12.01" y2="20" />
        </svg>
        <span>{t('offline.banner')}</span>
      </div>
    );
  }

  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="offline-indicator"
      data-state="reconnected"
      className="flex items-center justify-center gap-2 px-4 py-1.5 text-xs font-medium"
      style={{
        background: 'rgba(20, 184, 166, 0.15)',
        color: '#14B8A6',
        borderBottom: '1px solid rgba(20, 184, 166, 0.3)',
      }}
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        focusable="false"
      >
        <path d="M5 12.55a11 11 0 0 1 14.08 0" />
        <path d="M1.42 9a16 16 0 0 1 21.16 0" />
        <path d="M8.53 16.11a6 6 0 0 1 6.95 0" />
        <line x1="12" y1="20" x2="12.01" y2="20" />
      </svg>
      <span>{t('offline.reconnected')}</span>
    </div>
  );
}
