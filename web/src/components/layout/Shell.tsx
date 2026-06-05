import { useCallback, useEffect, useRef, useState } from 'react';
import { Outlet, useNavigate, useParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';
import { CommandPalette } from '../common/CommandPalette';
import { HotkeyHelpModal } from '../common/HotkeyHelpModal';
import { Toaster } from '../common/Toaster';
import { OfflineIndicator } from '../common/OfflineIndicator';
import { RouteErrorBoundary } from '../common/ErrorBoundary';
import { useShortcut } from '../../hotkeys';
import { useOnlineStatus } from '../../hooks/useOnlineStatus';
import { useOntologyStore } from '../../stores/ontologyStore';

export function Shell() {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const togglePalette = useCallback(() => setPaletteOpen((v) => !v), []);
  const toggleHelp = useCallback(() => setHelpOpen((v) => !v), []);
  const navigate = useNavigate();

  const params = useParams();
  const selectedOntology = useOntologyStore((s) => s.selectedOntology);
  const activeOntology =
    (params.ontology as string | undefined) ?? selectedOntology ?? null;

  // Auto-sync on reconnect (US-354): TanStack Query's onlineManager already
  // resumes paused queries on `online`, but we ALSO invalidate every query
  // on transition from offline → online so cached-but-stale views (which
  // the user was browsing while disconnected) refetch immediately. The
  // ref guards against the initial mount counting as a recovery.
  const online = useOnlineStatus();
  const queryClient = useQueryClient();
  const wasOnlineRef = useRef<boolean>(online);
  useEffect(() => {
    if (!wasOnlineRef.current && online) {
      queryClient.invalidateQueries();
    }
    wasOnlineRef.current = online;
  }, [online, queryClient]);

  useShortcut('commandPalette', togglePalette);
  useShortcut('showHelp', toggleHelp);
  useShortcut('goDashboard', () => navigate('/'));
  useShortcut(
    'goObjectsets',
    () => {
      if (activeOntology) navigate(`/objectsets/${activeOntology}`);
    },
    { enabled: !!activeOntology },
  );
  useShortcut('goPipelines', () => navigate('/pipelines'));
  useShortcut('goApprovals', () => navigate('/approvals'));

  return (
    <div className="flex h-screen bg-bg-primary text-text-primary font-sans">
      {/* Skip to main content (WCAG 2.4.1 Bypass Blocks): first focusable
          element so keyboard / screen-reader users can jump past the
          Sidebar + Topbar straight to <main>. Visually hidden until focused. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:top-2 focus:left-2 focus:px-3 focus:py-2 focus:rounded focus:bg-bg-primary focus:text-text-primary focus:border focus:border-border"
      >
        Skip to main content
      </a>
      {/* Top accent line: 1px gradient amber → teal → transparent */}
      <div
        className="fixed top-0 left-0 right-0 z-50 h-px"
        style={{
          background:
            'linear-gradient(90deg, #F59E0B 0%, #14B8A6 50%, transparent 100%)',
        }}
      />
      <Sidebar />
      <div className="flex flex-col flex-1 min-w-0">
        <OfflineIndicator />
        <Topbar />
        <main
          id="main-content"
          tabIndex={-1}
          className="flex-1 overflow-auto p-6"
          style={{
            background:
              'linear-gradient(135deg, rgba(245,158,11,0.02) 0%, transparent 40%, rgba(20,184,166,0.02) 100%)',
          }}
        >
          <RouteErrorBoundary>
            <Outlet />
          </RouteErrorBoundary>
        </main>
      </div>
      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        activeOntology={activeOntology}
      />
      <HotkeyHelpModal open={helpOpen} onClose={() => setHelpOpen(false)} />
      <Toaster />
    </div>
  );
}
