import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  DEFAULT_LOCALE,
  DEFAULT_NAMESPACE,
  FALLBACK_LOCALE,
  LOCALE_STORAGE_KEY,
  SUPPORTED_LOCALES,
  changeLocale,
  detectInitialLocale,
  i18n,
  isSupportedLocale,
  normaliseLocale,
  persistLocale,
  readPersistedLocale,
  resources,
} from '../index';

describe('i18n bootstrap', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(async () => {
    window.localStorage.clear();
    // Reset to the canonical default so tests run in any order.
    if (i18n.isInitialized && i18n.language !== DEFAULT_LOCALE) {
      await i18n.changeLanguage(DEFAULT_LOCALE);
    }
  });

  it('initialises synchronously with both locales registered', () => {
    expect(i18n.isInitialized).toBe(true);
    expect(i18n.options.fallbackLng).toEqual([FALLBACK_LOCALE]);
    for (const lng of SUPPORTED_LOCALES) {
      expect(resources[lng][DEFAULT_NAMESPACE]).toBeTruthy();
    }
  });

  it('translates a known key in the active language', async () => {
    await changeLocale('zh-CN');
    expect(i18n.t('common.cancel')).toBe('取消');
    await changeLocale('en');
    expect(i18n.t('common.cancel')).toBe('Cancel');
  });

  it('falls back to zh-CN when the active locale is missing the key', async () => {
    // Simulate a key that exists in zh-CN but not yet translated to en.
    i18n.addResource('zh-CN', DEFAULT_NAMESPACE, 'temp.placeholder', '占位');
    await changeLocale('en');
    try {
      expect(i18n.t('temp.placeholder')).toBe('占位');
    } finally {
      // Remove the test-only key so it doesn't leak into other tests.
      const bundle = i18n.getResourceBundle('zh-CN', DEFAULT_NAMESPACE) as Record<string, unknown>;
      if (bundle && typeof bundle === 'object' && 'temp' in bundle) {
        delete (bundle as { temp?: unknown }).temp;
      }
    }
  });

  it('interpolates placeholders', async () => {
    await changeLocale('en');
    expect(i18n.t('auth.tooManyAttempts', { seconds: 30 })).toContain('30');
  });

  it('honours plural forms via _one / _other', async () => {
    await changeLocale('zh-CN');
    expect(i18n.t('dashboard.ontologyCount', { count: 1 })).toBe('1 个本体');
    expect(i18n.t('dashboard.ontologyCount', { count: 5 })).toBe('5 个本体');
    await changeLocale('en');
    expect(i18n.t('dashboard.ontologyCount', { count: 1 })).toBe('1 ontology');
    expect(i18n.t('dashboard.ontologyCount', { count: 5 })).toBe('5 ontologies');
  });

  it('changeLocale persists into localStorage', async () => {
    await changeLocale('en');
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en');
    await changeLocale('zh-CN');
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-CN');
  });
});

describe('normaliseLocale', () => {
  it('passes supported locales through unchanged', () => {
    expect(normaliseLocale('zh-CN')).toBe('zh-CN');
    expect(normaliseLocale('en')).toBe('en');
  });

  it('collapses regional variants to supported locales', () => {
    expect(normaliseLocale('zh-TW')).toBe('zh-CN');
    expect(normaliseLocale('zh-Hans')).toBe('zh-CN');
    expect(normaliseLocale('en-US')).toBe('en');
    expect(normaliseLocale('en-GB')).toBe('en');
    expect(normaliseLocale('en_US')).toBe('en');
  });

  it('returns null for unsupported / empty input', () => {
    expect(normaliseLocale('fr')).toBeNull();
    expect(normaliseLocale('')).toBeNull();
    expect(normaliseLocale(null)).toBeNull();
    expect(normaliseLocale(undefined)).toBeNull();
  });
});

describe('isSupportedLocale', () => {
  it('returns true only for SUPPORTED_LOCALES members', () => {
    for (const lng of SUPPORTED_LOCALES) {
      expect(isSupportedLocale(lng)).toBe(true);
    }
    expect(isSupportedLocale('fr')).toBe(false);
    expect(isSupportedLocale(null)).toBe(false);
    expect(isSupportedLocale(undefined)).toBe(false);
  });
});

describe('locale detection', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('readPersistedLocale returns null when nothing is stored', () => {
    expect(readPersistedLocale()).toBeNull();
  });

  it('readPersistedLocale returns the stored locale when valid', () => {
    persistLocale('en');
    expect(readPersistedLocale()).toBe('en');
  });

  it('readPersistedLocale ignores garbage values', () => {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, 'fr-FR');
    expect(readPersistedLocale()).toBeNull();
  });

  it('detectInitialLocale prefers persisted value over navigator.language', () => {
    persistLocale('en');
    const original = Object.getOwnPropertyDescriptor(navigator, 'language');
    Object.defineProperty(navigator, 'language', { value: 'zh-CN', configurable: true });
    try {
      expect(detectInitialLocale()).toBe('en');
    } finally {
      if (original) Object.defineProperty(navigator, 'language', original);
    }
  });

  it('detectInitialLocale falls back to navigator.language', () => {
    const original = Object.getOwnPropertyDescriptor(navigator, 'language');
    Object.defineProperty(navigator, 'language', { value: 'en-GB', configurable: true });
    try {
      expect(detectInitialLocale()).toBe('en');
    } finally {
      if (original) Object.defineProperty(navigator, 'language', original);
    }
  });

  it('detectInitialLocale uses DEFAULT_LOCALE when no signal is available', () => {
    const original = Object.getOwnPropertyDescriptor(navigator, 'language');
    Object.defineProperty(navigator, 'language', { value: 'fr', configurable: true });
    try {
      expect(detectInitialLocale()).toBe(DEFAULT_LOCALE);
    } finally {
      if (original) Object.defineProperty(navigator, 'language', original);
    }
  });
});

describe('persistLocale', () => {
  it('silently ignores localStorage failures', () => {
    const setItem = window.localStorage.setItem;
    window.localStorage.setItem = vi.fn(() => {
      throw new Error('quota');
    });
    try {
      expect(() => persistLocale('en')).not.toThrow();
    } finally {
      window.localStorage.setItem = setItem;
    }
  });
});

describe('resource parity', () => {
  it('zh-CN and en define the same conceptual key set', () => {
    const PLURAL_SUFFIX = /_(zero|one|two|few|many|other)$/;
    const flatten = (
      obj: Record<string, unknown>,
      prefix = '',
      acc = new Set<string>(),
    ): Set<string> => {
      for (const [k, v] of Object.entries(obj)) {
        const next = prefix ? `${prefix}.${k}` : k;
        if (v && typeof v === 'object' && !Array.isArray(v)) {
          flatten(v as Record<string, unknown>, next, acc);
        } else {
          // Strip plural suffix so zh-CN (only `_other`) matches en (`_one`
          // + `_other`) at the conceptual key level.
          acc.add(next.replace(PLURAL_SUFFIX, ''));
        }
      }
      return acc;
    };
    const zh = flatten(resources['zh-CN'][DEFAULT_NAMESPACE] as Record<string, unknown>);
    const en = flatten(resources.en[DEFAULT_NAMESPACE] as Record<string, unknown>);
    const onlyZh = [...zh].filter((k) => !en.has(k));
    const onlyEn = [...en].filter((k) => !zh.has(k));
    expect(onlyZh).toEqual([]);
    expect(onlyEn).toEqual([]);
  });
});
