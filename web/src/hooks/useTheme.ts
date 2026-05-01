import { useCallback, useEffect, useState } from 'react';

export type Theme = 'dark' | 'light';
export type ThemePreference = Theme | 'system';

export const THEME_STORAGE_KEY = 'weave:theme';

const VALID_PREFERENCES = new Set<ThemePreference>(['dark', 'light', 'system']);

const DARK_QUERY = '(prefers-color-scheme: dark)';

export function readPersistedPreference(): ThemePreference {
  if (typeof window === 'undefined') return 'dark';
  try {
    const raw = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (raw && VALID_PREFERENCES.has(raw as ThemePreference)) {
      return raw as ThemePreference;
    }
  } catch {
    // localStorage may throw in privacy modes / SSR — fall through to default
  }
  return 'dark';
}

export function readSystemTheme(): Theme {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return 'dark';
  }
  try {
    return window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light';
  } catch {
    return 'dark';
  }
}

export function resolveTheme(pref: ThemePreference): Theme {
  if (pref === 'system') return readSystemTheme();
  return pref;
}

export function readPersistedTheme(): Theme {
  return resolveTheme(readPersistedPreference());
}

export function applyThemeToDocument(theme: Theme): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (theme === 'dark') {
    root.classList.add('dark');
    root.classList.remove('light');
  } else {
    root.classList.add('light');
    root.classList.remove('dark');
  }
}

function persistPreference(pref: ThemePreference): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, pref);
  } catch {
    // ignore quota / privacy-mode failures
  }
}

export interface UseThemeResult {
  theme: Theme;
  preference: ThemePreference;
  setPreference: (pref: ThemePreference) => void;
  setTheme: (theme: Theme) => void;
  toggleTheme: () => void;
}

export function useTheme(): UseThemeResult {
  const [preference, setPreferenceState] = useState<ThemePreference>(() =>
    readPersistedPreference(),
  );
  const [systemTheme, setSystemTheme] = useState<Theme>(() => readSystemTheme());

  // Subscribe to system theme changes ONLY while preference === 'system'.
  // Resync on subscribe so we pick up changes that happened while watching was
  // paused (e.g. user toggled the OS theme while preference was concrete).
  useEffect(() => {
    if (preference !== 'system') return;
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const mql = window.matchMedia(DARK_QUERY);
    setSystemTheme(mql.matches ? 'dark' : 'light');
    const handler = (ev: MediaQueryListEvent) => {
      setSystemTheme(ev.matches ? 'dark' : 'light');
    };
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', handler);
      return () => mql.removeEventListener('change', handler);
    }
    // Older Safari / jsdom fallback
    mql.addListener?.(handler);
    return () => mql.removeListener?.(handler);
  }, [preference]);

  const theme: Theme = preference === 'system' ? systemTheme : preference;

  useEffect(() => {
    applyThemeToDocument(theme);
  }, [theme]);

  const setPreference = useCallback((next: ThemePreference) => {
    setPreferenceState(next);
    persistPreference(next);
  }, []);

  const setTheme = useCallback((next: Theme) => {
    setPreferenceState(next);
    persistPreference(next);
  }, []);

  const toggleTheme = useCallback(() => {
    setPreferenceState((prev) => {
      const currentEffective: Theme = prev === 'system' ? readSystemTheme() : prev;
      const next: Theme = currentEffective === 'dark' ? 'light' : 'dark';
      persistPreference(next);
      return next;
    });
  }, []);

  return { theme, preference, setPreference, setTheme, toggleTheme };
}
