import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';
import { Link, useLocation, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { LanguageSwitcher } from '../common/LanguageSwitcher';
import { BranchPicker } from './BranchPicker';
import { TimeRangePicker } from './TimeRangePicker';
import { useNotificationsUnreadCount } from '../../hooks/useNotifications';
import { useTheme, type ThemePreference } from '../../hooks/useTheme';
import { useOntologyStore } from '../../stores/ontologyStore';
import { splitCamelCase } from '../../lib/breadcrumb';

const THEME_OPTION_VALUES: ReadonlyArray<ThemePreference> = ['light', 'dark', 'system'];

function pathToBreadcrumbs(pathname: string): string[] {
  const segments = pathname.split('/').filter(Boolean);
  if (segments.length === 0) return ['Dashboard'];
  return segments.map((s) => splitCamelCase(s));
}

export function Topbar() {
  const location = useLocation();
  const breadcrumbs = pathToBreadcrumbs(location.pathname);
  // Dogfood Round 3 #1 / #3: the bell is a single-source-of-truth entry
  // into the dedicated `/notifications` full page. The old Topbar slide
  // drawer was removed entirely — having two notification surfaces
  // (drawer + page) violated single-source-of-truth and surveyors
  // routinely flagged the drawer as "always open" / "duplicates the page".
  const { t } = useTranslation();

  const params = useParams();
  const selectedOntology = useOntologyStore((s) => s.selectedOntology);
  const activeOntology =
    (params.ontology as string | undefined) ??
    (params.dataset as string | undefined) ??
    selectedOntology ??
    null;

  // Read the badge number from the dedicated O(1) `/notifications/unread-count`
  // endpoint rather than pulling the entire unread list and counting it
  // client-side — the backend handler is partial-index-backed for exactly
  // this "lightweight badge" use case.
  const { data } = useNotificationsUnreadCount();
  const unreadCount = data?.count ?? 0;
  const badgeLabel = unreadCount > 9 ? '9+' : String(unreadCount);

  const { theme, preference, setPreference } = useTheme();
  const [themeMenuOpen, setThemeMenuOpen] = useState(false);
  // Wraps the trigger + popup so outside-click detection covers both. The
  // popup panel and trigger have their own refs for roving focus / refocus.
  const themeMenuRef = useRef<HTMLDivElement | null>(null);
  // The role="menu" panel — scope for the menuitemradio roving-focus query.
  const themeMenuPanelRef = useRef<HTMLDivElement | null>(null);
  // The toggle button — focus is returned here when the menu closes via Escape.
  const themeTriggerRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!themeMenuOpen) return;
    const onDocClick = (e: MouseEvent) => {
      if (!themeMenuRef.current?.contains(e.target as Node)) {
        setThemeMenuOpen(false);
      }
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [themeMenuOpen]);

  // The theme options are the only roving-focus targets inside the panel.
  // Scoped to the theme menu panel only — never touches BranchPicker or any
  // other Topbar child.
  const themeOptionEls = useCallback((): HTMLElement[] => {
    const panel = themeMenuPanelRef.current;
    if (!panel) return [];
    return Array.from(
      panel.querySelectorAll<HTMLElement>('[role="menuitemradio"]'),
    );
  }, []);

  // Move roving focus to the next/previous option, wrapping at the ends.
  const moveThemeFocus = useCallback(
    (delta: 1 | -1) => {
      const els = themeOptionEls();
      if (els.length === 0) return;
      const activeEl = document.activeElement as HTMLElement | null;
      const current = activeEl ? els.indexOf(activeEl) : -1;
      // From outside the list (current === -1) ArrowDown lands on the first
      // option and ArrowUp on the last.
      const base = current === -1 ? (delta === 1 ? -1 : 0) : current;
      const next = (base + delta + els.length) % els.length;
      els[next]?.focus();
    },
    [themeOptionEls],
  );

  const onThemeMenuKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        setThemeMenuOpen(false);
        themeTriggerRef.current?.focus();
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        moveThemeFocus(1);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        moveThemeFocus(-1);
        return;
      }
      if (e.key === 'Home') {
        e.preventDefault();
        themeOptionEls()[0]?.focus();
        return;
      }
      if (e.key === 'End') {
        e.preventDefault();
        const els = themeOptionEls();
        els[els.length - 1]?.focus();
      }
    },
    [moveThemeFocus, themeOptionEls],
  );

  // On open, move focus onto the active (aria-checked) option, or the first
  // one, so Arrow keys have a starting anchor and screen readers announce the
  // menu. Don't yank focus once the user has started navigating.
  useEffect(() => {
    if (!themeMenuOpen) return;
    const els = themeOptionEls();
    if (els.length === 0) return;
    const activeEl = document.activeElement as HTMLElement | null;
    if (activeEl && els.includes(activeEl)) return;
    const target =
      els.find((el) => el.getAttribute('aria-checked') === 'true') ?? els[0];
    target.focus();
  }, [themeMenuOpen, themeOptionEls]);

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

      <div className="ml-auto flex items-center gap-1">
        <TimeRangePicker />
        <BranchPicker ontologyApiName={activeOntology} />
        <div ref={themeMenuRef} className="relative">
          <button
            ref={themeTriggerRef}
            type="button"
            aria-label={t('theme.label')}
            aria-haspopup="menu"
            aria-expanded={themeMenuOpen}
            data-testid="theme-menu-trigger"
            data-theme={theme}
            data-preference={preference}
            onClick={() => setThemeMenuOpen((v) => !v)}
            className="p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-white/5 transition-colors"
            title={t('theme.label')}
          >
            {theme === 'dark' ? (
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
                <circle cx="12" cy="12" r="4" />
                <path d="M12 2v2" />
                <path d="M12 20v2" />
                <path d="m4.93 4.93 1.41 1.41" />
                <path d="m17.66 17.66 1.41 1.41" />
                <path d="M2 12h2" />
                <path d="M20 12h2" />
                <path d="m6.34 17.66-1.41 1.41" />
                <path d="m19.07 4.93-1.41 1.41" />
              </svg>
            ) : (
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
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
              </svg>
            )}
          </button>
          {themeMenuOpen && (
            <div
              ref={themeMenuPanelRef}
              role="menu"
              aria-label={t('theme.label')}
              data-testid="theme-menu"
              onKeyDown={onThemeMenuKeyDown}
              className="absolute right-0 mt-1 min-w-[160px] rounded border border-border bg-bg-primary shadow-lg z-10"
            >
              {THEME_OPTION_VALUES.map((value) => {
                const active = preference === value;
                return (
                  <button
                    key={value}
                    type="button"
                    role="menuitemradio"
                    aria-checked={active}
                    data-testid={`theme-option-${value}`}
                    onClick={() => {
                      setPreference(value);
                      setThemeMenuOpen(false);
                    }}
                    className={`flex w-full items-center justify-between px-3 py-2 text-xs font-sans transition-colors ${
                      active
                        ? 'text-accent-cyan'
                        : 'text-text-primary hover:bg-bg-secondary'
                    }`}
                  >
                    <span>{t(`theme.${value}`)}</span>
                  </button>
                );
              })}
            </div>
          )}
        </div>
        <LanguageSwitcher />
        <Link
          to="/notifications"
          aria-label="Notifications"
          data-testid="notification-bell"
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
        </Link>
      </div>

      {/* Subtle gradient line below */}
      <div
        className="absolute bottom-0 left-0 right-0 h-px"
        style={{
          background:
            'linear-gradient(90deg, transparent 0%, rgba(245,158,11,0.15) 30%, rgba(20,184,166,0.15) 70%, transparent 100%)',
        }}
      />
    </header>
  );
}
