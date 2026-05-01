import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
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

const THEME_OPTIONS: ThemePreference[] = ['system', 'light', 'dark'];

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
        <div role="status" className="text-sm text-text-secondary">
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
                    : 'border border-border text-text-secondary hover:text-text-primary hover:bg-bg-tertiary'
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
    </div>
  );
}
