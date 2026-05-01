import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import {
  THEME_STORAGE_KEY,
  applyThemeToDocument,
  readPersistedPreference,
  readPersistedTheme,
  resolveTheme,
  useTheme,
} from '../useTheme';

interface MQLStub {
  matches: boolean;
  media: string;
  onchange: null;
  addEventListener: ReturnType<typeof vi.fn>;
  removeEventListener: ReturnType<typeof vi.fn>;
  addListener: ReturnType<typeof vi.fn>;
  removeListener: ReturnType<typeof vi.fn>;
  dispatchEvent: () => boolean;
  _fire: (matches: boolean) => void;
}

function installMatchMedia(initialMatches: boolean): {
  mql: MQLStub;
  cleanup: () => void;
} {
  const listeners: Array<(ev: MediaQueryListEvent) => void> = [];
  const mql: MQLStub = {
    matches: initialMatches,
    media: '(prefers-color-scheme: dark)',
    onchange: null,
    addEventListener: vi.fn(
      (_evt: string, cb: (ev: MediaQueryListEvent) => void) => {
        listeners.push(cb);
      },
    ),
    removeEventListener: vi.fn(
      (_evt: string, cb: (ev: MediaQueryListEvent) => void) => {
        const i = listeners.indexOf(cb);
        if (i >= 0) listeners.splice(i, 1);
      },
    ),
    addListener: vi.fn((cb: (ev: MediaQueryListEvent) => void) => {
      listeners.push(cb);
    }),
    removeListener: vi.fn((cb: (ev: MediaQueryListEvent) => void) => {
      const i = listeners.indexOf(cb);
      if (i >= 0) listeners.splice(i, 1);
    }),
    dispatchEvent: () => true,
    _fire(matches: boolean) {
      mql.matches = matches;
      for (const cb of [...listeners]) {
        cb({ matches } as MediaQueryListEvent);
      }
    },
  };
  const original = window.matchMedia;
  window.matchMedia = vi.fn().mockReturnValue(mql) as unknown as typeof window.matchMedia;
  return {
    mql,
    cleanup: () => {
      if (original) {
        window.matchMedia = original;
      } else {
        // delete in jsdom — stub-only environment
        // @ts-expect-error allow for cleanup in environments without native matchMedia
        delete window.matchMedia;
      }
    },
  };
}

describe('useTheme', () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark', 'light');
  });

  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove('dark', 'light');
    vi.restoreAllMocks();
  });

  it('defaults to dark when nothing is persisted', () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('dark');
    expect(result.current.preference).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(document.documentElement.classList.contains('light')).toBe(false);
  });

  it('reads the persisted theme on mount', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'light');
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('light');
    expect(result.current.preference).toBe('light');
    expect(document.documentElement.classList.contains('light')).toBe(true);
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('toggleTheme flips dark↔light and persists to localStorage', () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('dark');
    act(() => {
      result.current.toggleTheme();
    });
    expect(result.current.theme).toBe('light');
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.classList.contains('light')).toBe(true);
    act(() => {
      result.current.toggleTheme();
    });
    expect(result.current.theme).toBe('dark');
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('setTheme writes the explicit value', () => {
    const { result } = renderHook(() => useTheme());
    act(() => {
      result.current.setTheme('light');
    });
    expect(result.current.theme).toBe('light');
    expect(result.current.preference).toBe('light');
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
  });

  it('ignores unknown persisted values and falls back to dark', () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, 'pink-and-purple');
    expect(readPersistedTheme()).toBe('dark');
    expect(readPersistedPreference()).toBe('dark');
  });

  it('applyThemeToDocument toggles the class on <html>', () => {
    applyThemeToDocument('light');
    expect(document.documentElement.classList.contains('light')).toBe(true);
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    applyThemeToDocument('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(document.documentElement.classList.contains('light')).toBe(false);
  });

  describe('system preference', () => {
    it('setPreference("system") persists "system" to localStorage', () => {
      const { cleanup } = installMatchMedia(true);
      try {
        const { result } = renderHook(() => useTheme());
        act(() => {
          result.current.setPreference('system');
        });
        expect(result.current.preference).toBe('system');
        expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe('system');
      } finally {
        cleanup();
      }
    });

    it('resolves to dark when prefers-color-scheme: dark', () => {
      const { cleanup } = installMatchMedia(true);
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, 'system');
        const { result } = renderHook(() => useTheme());
        expect(result.current.preference).toBe('system');
        expect(result.current.theme).toBe('dark');
        expect(document.documentElement.classList.contains('dark')).toBe(true);
      } finally {
        cleanup();
      }
    });

    it('resolves to light when prefers-color-scheme: light', () => {
      const { cleanup } = installMatchMedia(false);
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, 'system');
        const { result } = renderHook(() => useTheme());
        expect(result.current.preference).toBe('system');
        expect(result.current.theme).toBe('light');
        expect(document.documentElement.classList.contains('light')).toBe(true);
      } finally {
        cleanup();
      }
    });

    it('reacts to live system theme changes while preference is "system"', () => {
      const { mql, cleanup } = installMatchMedia(true);
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, 'system');
        const { result } = renderHook(() => useTheme());
        expect(result.current.theme).toBe('dark');
        act(() => {
          mql._fire(false);
        });
        expect(result.current.theme).toBe('light');
        expect(document.documentElement.classList.contains('light')).toBe(true);
      } finally {
        cleanup();
      }
    });

    it('does not react to system changes when preference is concrete', () => {
      const { mql, cleanup } = installMatchMedia(true);
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, 'light');
        const { result } = renderHook(() => useTheme());
        expect(result.current.theme).toBe('light');
        act(() => {
          mql._fire(true);
        });
        expect(result.current.theme).toBe('light');
      } finally {
        cleanup();
      }
    });

    it('resolveTheme returns the concrete preference unchanged', () => {
      expect(resolveTheme('dark')).toBe('dark');
      expect(resolveTheme('light')).toBe('light');
    });

    it('falls back to dark when matchMedia is unavailable and preference is system', () => {
      // No matchMedia stub installed.
      // @ts-expect-error mimic a runtime where matchMedia is missing
      delete window.matchMedia;
      window.localStorage.setItem(THEME_STORAGE_KEY, 'system');
      const { result } = renderHook(() => useTheme());
      expect(result.current.preference).toBe('system');
      expect(result.current.theme).toBe('dark');
    });
  });
});
