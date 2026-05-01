// i18n bootstrap. Initialises a SINGLETON `i18next` instance synchronously
// at module-import time so callers can `import { t } from './i18n'` and
// invoke it before React mounts (e.g. error boundaries, top-level fetch
// helpers). React tree consumers prefer `useTranslation()` from
// `react-i18next` for re-render-on-language-change semantics.
//
// Locale precedence (first match wins):
//   1. `localStorage[weave:locale]` — user explicit choice (US-347 picker)
//   2. `navigator.language` (collapsed: `zh-*` → `zh-CN`, `en-*` → `en`)
//   3. `DEFAULT_LOCALE` ('zh-CN')
//
// Resources are bundled at build-time (no async http loader) so the first
// paint never shows untranslated keys.

import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import en from './resources/en';
import zhCN from './resources/zh-CN';

export const SUPPORTED_LOCALES = ['zh-CN', 'en'] as const;
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number];

export const DEFAULT_LOCALE: SupportedLocale = 'zh-CN';
export const FALLBACK_LOCALE: SupportedLocale = 'zh-CN';
export const LOCALE_STORAGE_KEY = 'weave:locale';

export const DEFAULT_NAMESPACE = 'translation';

const VALID_LOCALES = new Set<SupportedLocale>(SUPPORTED_LOCALES);

export function isSupportedLocale(value: string | null | undefined): value is SupportedLocale {
  return !!value && VALID_LOCALES.has(value as SupportedLocale);
}

export function normaliseLocale(raw: string | null | undefined): SupportedLocale | null {
  if (!raw) return null;
  if (isSupportedLocale(raw)) return raw;
  const lower = raw.toLowerCase();
  if (lower === 'zh' || lower.startsWith('zh-') || lower.startsWith('zh_')) return 'zh-CN';
  if (lower === 'en' || lower.startsWith('en-') || lower.startsWith('en_')) return 'en';
  return null;
}

export function readPersistedLocale(): SupportedLocale | null {
  if (typeof window === 'undefined') return null;
  try {
    return normaliseLocale(window.localStorage.getItem(LOCALE_STORAGE_KEY));
  } catch {
    return null;
  }
}

export function detectInitialLocale(): SupportedLocale {
  const persisted = readPersistedLocale();
  if (persisted) return persisted;
  if (typeof navigator !== 'undefined' && navigator.language) {
    const nav = normaliseLocale(navigator.language);
    if (nav) return nav;
  }
  return DEFAULT_LOCALE;
}

export function persistLocale(locale: SupportedLocale): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    // ignore quota / privacy-mode failures
  }
}

// Resources keyed under the default namespace so callers reference keys
// without namespace prefixes (e.g. `t('common.cancel')`).
export const resources = {
  'zh-CN': { [DEFAULT_NAMESPACE]: zhCN },
  en: { [DEFAULT_NAMESPACE]: en },
} as const;

if (!i18n.isInitialized) {
  i18n.use(initReactI18next).init({
    resources,
    lng: detectInitialLocale(),
    fallbackLng: FALLBACK_LOCALE,
    defaultNS: DEFAULT_NAMESPACE,
    ns: [DEFAULT_NAMESPACE],
    supportedLngs: [...SUPPORTED_LOCALES],
    interpolation: {
      // React already escapes by default; double-escaping breaks user-visible
      // characters like `<` in markup-free body strings.
      escapeValue: false,
    },
    returnNull: false,
    react: {
      // Suspense-free mode: resources are pre-bundled, so no async load
      // boundary is needed and Suspense triggers spurious fallbacks during
      // tests where translations are already on hand.
      useSuspense: false,
    },
  });
}

export async function changeLocale(locale: SupportedLocale): Promise<void> {
  await i18n.changeLanguage(locale);
  persistLocale(locale);
}

export default i18n;
export { i18n };
