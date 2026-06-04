import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { getBuildFeatures, getBuildInfo } from '../../api/buildInfo';
import { getServerConnections, getServerInfo } from '../../api/serverInfo';
import {
  changeLocale,
  isSupportedLocale,
  SUPPORTED_LOCALES,
  type SupportedLocale,
} from '../../i18n';
import {
  useTheme,
  type ThemePreference,
} from '../../hooks/useTheme';
import {
  useUpdateUserPreferences,
  useUserPreferences,
} from '../../hooks/useUserPreferences';
import type {
  HotkeyPreferences,
  NotificationPreferences,
  ThemePreferenceValue,
} from '../../api/userPreferences';
import { HOTKEYS } from '../../hotkeys/registry';
import {
  useRevokeOtherSessions,
  useRevokeSession,
  useSessions,
} from '../../hooks/useSessions';
import type { Session } from '../../api/sessions';

const THEME_OPTIONS: ThemePreference[] = ['system', 'light', 'dark'];

// formatSessionTimestamp renders an ISO timestamp as the user's locale
// date-time, falling back to the raw string when the backend sends an
// unparseable value so the row never shows "Invalid Date".
function formatSessionTimestamp(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

// formatUptime humanises a seconds count into "Nd HHh MMm" (dropping
// leading zero units) for the System status row.
function formatUptime(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return '—';
  const d = Math.floor(totalSeconds / 86400);
  const h = Math.floor((totalSeconds % 86400) / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const parts: string[] = [];
  if (d) parts.push(`${d}d`);
  if (d || h) parts.push(`${h}h`);
  parts.push(`${m}m`);
  return parts.join(' ');
}

// SettingsPage is the user preference center (US-350). It surfaces four
// independent sections — theme, language, notifications, hotkeys —
// and persists every change to the backend `user_preferences` table.
// While the row is loading the page falls back to the existing local
// (localStorage / OS) defaults so the controls remain interactive.
export function SettingsPage() {
  const { t } = useTranslation();
  const { preference: localTheme, setPreference: setLocalTheme } = useTheme();
  const { i18n } = useTranslation();
  const localLocale: SupportedLocale = isSupportedLocale(i18n.language)
    ? (i18n.language as SupportedLocale)
    : 'zh-CN';

  const { data: prefs, isLoading, unavailable } = useUserPreferences();
  const update = useUpdateUserPreferences();

  // Build info / feature flags are read-only server diagnostics (US: parity
  // with /api/v2/build-info). A degraded deployment may 404; we render the
  // section only when the build info resolves so the page never crashes.
  const buildInfoQuery = useQuery({
    queryKey: ['build-info'],
    queryFn: getBuildInfo,
    retry: false,
    staleTime: 5 * 60_000,
  });
  const featuresQuery = useQuery({
    queryKey: ['build-info', 'features'],
    queryFn: getBuildFeatures,
    retry: false,
    staleTime: 5 * 60_000,
  });
  const buildInfo = buildInfoQuery.data;
  const features = featuresQuery.data?.features ?? [];

  // Runtime status (uptime / connection pools) — same diagnostic surface
  // as build-info, refreshed on a short interval so the snapshot stays
  // roughly live while the page is open.
  const serverInfoQuery = useQuery({
    queryKey: ['server-info'],
    queryFn: getServerInfo,
    retry: false,
    refetchInterval: 15_000,
  });
  const connectionsQuery = useQuery({
    queryKey: ['server-info', 'connections'],
    queryFn: getServerConnections,
    retry: false,
    refetchInterval: 15_000,
  });
  const serverInfo = serverInfoQuery.data;
  const connections = connectionsQuery.data;

  // Active login sessions (US: session management parity). A degraded
  // deployment (dev auth / no session store) may 404/501 — we render the
  // section only when the list resolves so the page never crashes.
  const sessionsQuery = useSessions();
  const sessions = sessionsQuery.data ?? [];
  const revokeSession = useRevokeSession();
  const revokeOthers = useRevokeOtherSessions();

  // Working copies seeded from the persisted row when it loads. Local
  // edits flow through `apply()` so the optimistic UI stays in sync
  // with the backend round-trip.
  const notif: NotificationPreferences = useMemo(
    () => prefs.notifications ?? {},
    [prefs.notifications],
  );
  const hotkeyPrefs: HotkeyPreferences = useMemo(
    () => prefs.hotkeys ?? {},
    [prefs.hotkeys],
  );

  const [savedFlash, setSavedFlash] = useState(false);
  useEffect(() => {
    if (!savedFlash) return;
    const id = window.setTimeout(() => setSavedFlash(false), 1500);
    return () => window.clearTimeout(id);
  }, [savedFlash]);

  // applyTheme: update the local theme (which writes localStorage +
  // toggles the html class) AND persist to the backend so the choice
  // follows the user across devices. We swallow backend errors silently
  // — the local change still landed.
  const applyTheme = async (next: ThemePreference) => {
    setLocalTheme(next);
    try {
      await update.mutateAsync({ theme: next as ThemePreferenceValue });
      setSavedFlash(true);
    } catch {
      // Backend persistence failure — local change is intact.
    }
  };

  const applyLanguage = async (next: SupportedLocale) => {
    await changeLocale(next);
    try {
      await update.mutateAsync({ language: next });
      setSavedFlash(true);
    } catch {
      // local i18next change still landed.
    }
  };

  const applyNotifications = async (patch: Partial<NotificationPreferences>) => {
    const merged = { ...notif, ...patch };
    try {
      await update.mutateAsync({ notifications: merged });
      setSavedFlash(true);
    } catch {
      // ignore — surfaced via update.error in the saveFailed banner
    }
  };

  const applyHotkeys = async (patch: Partial<HotkeyPreferences>) => {
    const merged: HotkeyPreferences = { ...hotkeyPrefs, ...patch };
    try {
      await update.mutateAsync({ hotkeys: merged });
      setSavedFlash(true);
    } catch {
      // ignore — surfaced via update.error in the saveFailed banner
    }
  };

  const notificationsEnabled = notif.enabled !== false;
  const hotkeysEnabled = hotkeyPrefs.enabled !== false;

  return (
    <div
      className="flex flex-col gap-6 p-6 max-w-3xl mx-auto"
      data-testid="settings-page"
    >
      <header>
        <h1 className="text-2xl font-semibold text-text-primary">
          {t('settings.title')}
        </h1>
        <p className="mt-1 text-sm text-text-secondary">
          {t('settings.subtitle')}
        </p>
      </header>

      {isLoading && (
        <div
          role="status"
          aria-live="polite"
          aria-atomic="true"
          className="text-sm text-text-secondary"
        >
          {t('settings.loading')}
        </div>
      )}

      {unavailable && (
        <div
          role="alert"
          data-testid="settings-unavailable-banner"
          className="rounded border border-border bg-bg-secondary p-3 text-sm text-text-secondary"
        >
          {t('settings.unavailable')}
        </div>
      )}

      {update.isError && !unavailable && (
        <div
          role="alert"
          data-testid="settings-save-error"
          className="rounded border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-400"
        >
          {t('settings.saveFailed', {
            message: (update.error as Error)?.message ?? '',
          })}
        </div>
      )}

      {savedFlash && (
        <div
          role="status"
          aria-live="polite"
          aria-atomic="true"
          data-testid="settings-saved-flash"
          className="rounded border border-emerald-500/40 bg-emerald-500/10 p-3 text-sm text-emerald-400"
        >
          {t('settings.saved')}
        </div>
      )}

      <section
        data-testid="settings-section-theme"
        aria-labelledby="settings-theme-heading"
        className="rounded border border-border bg-bg-secondary p-4"
      >
        <h2
          id="settings-theme-heading"
          className="text-sm font-semibold text-text-primary mb-3"
        >
          {t('settings.sectionTheme')}
        </h2>
        <div className="flex gap-2" role="radiogroup" aria-label={t('theme.label')}>
          {THEME_OPTIONS.map((opt) => {
            const active = (prefs.theme || localTheme) === opt;
            return (
              <button
                key={opt}
                type="button"
                role="radio"
                aria-checked={active}
                data-testid={`settings-theme-${opt}`}
                onClick={() => applyTheme(opt)}
                className={`px-3 py-1.5 rounded text-sm transition-colors ${
                  active
                    ? 'bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40'
                    : 'border border-border text-text-primary opacity-80 hover:opacity-100 hover:bg-bg-tertiary'
                }`}
              >
                {t(`theme.${opt}`)}
              </button>
            );
          })}
        </div>
      </section>

      <section
        data-testid="settings-section-language"
        aria-labelledby="settings-language-heading"
        className="rounded border border-border bg-bg-secondary p-4"
      >
        <h2
          id="settings-language-heading"
          className="text-sm font-semibold text-text-primary mb-3"
        >
          {t('settings.sectionLanguage')}
        </h2>
        <div
          className="flex gap-2"
          role="radiogroup"
          aria-label={t('language.label')}
        >
          {SUPPORTED_LOCALES.map((loc) => {
            const active = (prefs.language || localLocale) === loc;
            return (
              <button
                key={loc}
                type="button"
                role="radio"
                aria-checked={active}
                data-testid={`settings-language-${loc}`}
                onClick={() => applyLanguage(loc)}
                className={`px-3 py-1.5 rounded text-sm transition-colors ${
                  active
                    ? 'bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40'
                    : 'border border-border text-text-secondary hover:text-text-primary hover:bg-bg-tertiary'
                }`}
              >
                {t(`language.${loc}`)}
              </button>
            );
          })}
        </div>
      </section>

      <section
        data-testid="settings-section-notifications"
        aria-labelledby="settings-notifications-heading"
        className="rounded border border-border bg-bg-secondary p-4"
      >
        <h2
          id="settings-notifications-heading"
          className="text-sm font-semibold text-text-primary mb-3"
        >
          {t('settings.sectionNotifications')}
        </h2>
        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={notificationsEnabled}
            onChange={(e) => applyNotifications({ enabled: e.target.checked })}
            data-testid="settings-notifications-enabled"
            className="mt-0.5"
          />
          <span>
            <span className="block text-sm text-text-primary">
              {t('settings.notificationsEnabled')}
            </span>
            <span className="block text-xs text-text-secondary mt-0.5">
              {t('settings.notificationsEnabledHint')}
            </span>
          </span>
        </label>
        <div
          className={`mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3 transition-opacity ${
            notificationsEnabled ? 'opacity-100' : 'opacity-50 pointer-events-none'
          }`}
        >
          {(
            [
              ['mentions', 'settings.notificationChannelMentions'],
              ['approvals', 'settings.notificationChannelApprovals'],
              ['watches', 'settings.notificationChannelWatches'],
            ] as const
          ).map(([key, labelKey]) => {
            const enabled = notif[key] !== false;
            return (
              <label
                key={key}
                className="flex items-center gap-2 text-sm text-text-secondary"
              >
                <input
                  type="checkbox"
                  checked={enabled}
                  onChange={(e) => applyNotifications({ [key]: e.target.checked })}
                  data-testid={`settings-notifications-${key}`}
                />
                {t(labelKey)}
              </label>
            );
          })}
        </div>
      </section>

      <section
        data-testid="settings-section-hotkeys"
        aria-labelledby="settings-hotkeys-heading"
        className="rounded border border-border bg-bg-secondary p-4"
      >
        <h2
          id="settings-hotkeys-heading"
          className="text-sm font-semibold text-text-primary mb-3"
        >
          {t('settings.sectionHotkeys')}
        </h2>
        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={hotkeysEnabled}
            onChange={(e) => applyHotkeys({ enabled: e.target.checked })}
            data-testid="settings-hotkeys-enabled"
            className="mt-0.5"
          />
          <span>
            <span className="block text-sm text-text-primary">
              {t('settings.hotkeysEnabled')}
            </span>
            <span className="block text-xs text-text-secondary mt-0.5">
              {t('settings.hotkeysEnabledHint')}
            </span>
          </span>
        </label>
        <ul
          className={`mt-3 divide-y divide-border text-sm transition-opacity ${
            hotkeysEnabled ? 'opacity-100' : 'opacity-50'
          }`}
        >
          {HOTKEYS.map((def) => (
            <li
              key={def.id}
              data-testid={`settings-hotkey-${def.id}`}
              className="flex items-center justify-between py-2"
            >
              <span className="text-text-secondary">{t(def.i18nKey)}</span>
              <kbd className="px-2 py-0.5 rounded bg-bg-tertiary text-text-primary font-mono text-xs">
                {def.keys}
              </kbd>
            </li>
          ))}
        </ul>
      </section>

      {sessionsQuery.isSuccess && (
        <section
          data-testid="settings-section-sessions"
          aria-labelledby="settings-sessions-heading"
          className="rounded border border-border bg-bg-secondary p-4"
        >
          <div className="mb-3 flex items-center justify-between gap-3">
            <h2
              id="settings-sessions-heading"
              className="text-sm font-semibold text-text-primary"
            >
              Active sessions
            </h2>
            <button
              type="button"
              data-testid="session-revoke-others"
              onClick={() => revokeOthers.mutate()}
              disabled={revokeOthers.isPending}
              className="rounded border border-border px-2.5 py-1 text-xs text-text-secondary transition-colors hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-400 disabled:opacity-50"
            >
              Log out other devices
            </button>
          </div>

          {revokeSession.isError || revokeOthers.isError ? (
            <div
              role="alert"
              data-testid="session-action-error"
              className="mb-3 rounded border border-red-500/40 bg-red-500/10 p-2 text-xs text-red-400"
            >
              {((revokeSession.error || revokeOthers.error) as Error)?.message ??
                'Session action failed'}
            </div>
          ) : null}

          {sessions.length === 0 ? (
            <p data-testid="session-empty" className="text-sm text-text-secondary">
              No active sessions.
            </p>
          ) : (
            <ul className="divide-y divide-border text-sm">
              {sessions.map((s: Session) => (
                <li
                  key={s.id}
                  data-testid={`session-row-${s.id}`}
                  className="flex items-start justify-between gap-3 py-3"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-text-primary break-all">
                        {s.ip || 'unknown IP'}
                      </span>
                      {s.current && (
                        <span
                          data-testid="session-current-badge"
                          className="inline-flex items-center rounded border border-accent-cyan/40 bg-accent-cyan/20 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent-cyan"
                        >
                          This device
                        </span>
                      )}
                    </div>
                    <div className="mt-0.5 truncate text-xs text-text-secondary">
                      {s.user_agent || 'Unknown device'}
                    </div>
                    <div className="mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-[11px] text-text-secondary">
                      <span>Created {formatSessionTimestamp(s.created_at)}</span>
                      <span>
                        Last active {formatSessionTimestamp(s.last_seen)}
                      </span>
                    </div>
                  </div>
                  <button
                    type="button"
                    data-testid={`session-revoke-${s.id}`}
                    onClick={() => revokeSession.mutate(s.id)}
                    disabled={revokeSession.isPending}
                    className="shrink-0 rounded border border-border px-2.5 py-1 text-xs text-text-secondary transition-colors hover:border-rose-500/40 hover:bg-rose-500/10 hover:text-rose-400 disabled:opacity-50"
                  >
                    Revoke
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {buildInfo && (
        <section
          data-testid="settings-section-system"
          aria-labelledby="settings-system-heading"
          className="rounded border border-border bg-bg-secondary p-4"
        >
          <h2
            id="settings-system-heading"
            className="text-sm font-semibold text-text-primary mb-3"
          >
            System
          </h2>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-sm">
            {(
              [
                ['Version', buildInfo.version],
                ['Commit', buildInfo.commit],
                ['Go version', buildInfo.goVersion],
                ['Build time', buildInfo.buildTime],
              ] as const
            ).map(([label, value]) =>
              value ? (
                <div key={label} className="contents">
                  <dt className="text-text-secondary">{label}</dt>
                  <dd className="font-mono text-xs text-text-primary break-all">
                    {value}
                  </dd>
                </div>
              ) : null,
            )}
          </dl>

          {(serverInfo || connections) && (
            <dl className="mt-3 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 border-t border-border/60 pt-3 text-sm">
              {serverInfo && (
                <>
                  <dt className="text-text-secondary">Uptime</dt>
                  <dd
                    data-testid="server-uptime"
                    className="font-mono text-xs text-text-primary"
                  >
                    {formatUptime(serverInfo.uptimeSeconds)}
                  </dd>
                  <dt className="text-text-secondary">Goroutines</dt>
                  <dd className="font-mono text-xs text-text-primary">
                    {serverInfo.goroutineCount}
                  </dd>
                </>
              )}
              {connections?.nats && (
                <>
                  <dt className="text-text-secondary">NATS</dt>
                  <dd
                    data-testid="server-nats-status"
                    data-status={connections.nats.status}
                    className="flex items-center gap-1.5 font-mono text-xs text-text-primary"
                  >
                    <span
                      aria-hidden
                      className={`h-1.5 w-1.5 rounded-full ${
                        connections.nats.status === 'connected'
                          ? 'bg-emerald-400'
                          : 'bg-rose-400'
                      }`}
                    />
                    {connections.nats.status}
                    {connections.nats.serverUrl && (
                      <span className="text-text-secondary">
                        · {connections.nats.serverUrl}
                      </span>
                    )}
                  </dd>
                </>
              )}
              {connections?.postgres && (
                <>
                  <dt className="text-text-secondary">Postgres pool</dt>
                  <dd
                    data-testid="server-pg-pool"
                    className="font-mono text-xs text-text-primary"
                  >
                    {connections.postgres.acquiredConns} in use ·{' '}
                    {connections.postgres.idleConns} idle ·{' '}
                    {connections.postgres.totalConns}/{connections.postgres.maxConns} total
                  </dd>
                </>
              )}
            </dl>
          )}

          {features.length > 0 && (
            <div className="mt-4">
              <h3 className="text-xs font-semibold text-text-secondary uppercase tracking-wide mb-2">
                Feature flags
              </h3>
              <ul className="flex flex-wrap gap-2">
                {features.map((f) => (
                  <li
                    key={f.name}
                    data-testid={`settings-feature-${f.name}`}
                    data-enabled={f.enabled}
                    title={f.reason || f.description || undefined}
                    className={`inline-flex items-center gap-1.5 rounded border px-2 py-1 text-xs font-mono ${
                      f.enabled
                        ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                        : 'border-border bg-bg-tertiary text-text-secondary'
                    }`}
                  >
                    <span
                      aria-hidden
                      className={`h-1.5 w-1.5 rounded-full ${
                        f.enabled ? 'bg-emerald-400' : 'bg-text-secondary/50'
                      }`}
                    />
                    {f.name}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
