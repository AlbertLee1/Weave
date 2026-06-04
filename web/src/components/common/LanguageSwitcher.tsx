import { type KeyboardEvent, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  SUPPORTED_LOCALES,
  changeLocale,
  isSupportedLocale,
  type SupportedLocale,
} from '../../i18n';

export function LanguageSwitcher() {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  const current: SupportedLocale = isSupportedLocale(i18n.language)
    ? (i18n.language as SupportedLocale)
    : 'zh-CN';

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open]);

  // Collect the focusable menu items, in DOM order.
  const getItems = (): HTMLButtonElement[] =>
    listRef.current
      ? Array.from(
          listRef.current.querySelectorAll<HTMLButtonElement>(
            '[role="menuitemradio"]',
          ),
        )
      : [];

  // When the menu opens, move focus to the active item, falling back to the
  // first item. Degrades gracefully when there are no focusable items.
  useEffect(() => {
    if (!open) return;
    const items = getItems();
    if (items.length === 0) return;
    const activeIndex = items.findIndex(
      (item) => item.getAttribute('aria-checked') === 'true',
    );
    (items[activeIndex >= 0 ? activeIndex : 0] ?? items[0]).focus();
  }, [open]);

  const closeAndRefocusTrigger = () => {
    setOpen(false);
    triggerRef.current?.focus();
  };

  const focusItemAt = (index: number) => {
    const items = getItems();
    if (items.length === 0) return;
    const wrapped = ((index % items.length) + items.length) % items.length;
    items[wrapped].focus();
  };

  const handleMenuKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    switch (e.key) {
      case 'Escape': {
        e.preventDefault();
        closeAndRefocusTrigger();
        break;
      }
      case 'ArrowDown': {
        e.preventDefault();
        const items = getItems();
        const idx = items.indexOf(document.activeElement as HTMLButtonElement);
        focusItemAt(idx + 1);
        break;
      }
      case 'ArrowUp': {
        e.preventDefault();
        const items = getItems();
        const idx = items.indexOf(document.activeElement as HTMLButtonElement);
        focusItemAt(idx <= 0 ? items.length - 1 : idx - 1);
        break;
      }
      case 'Home': {
        e.preventDefault();
        focusItemAt(0);
        break;
      }
      case 'End': {
        e.preventDefault();
        focusItemAt(getItems().length - 1);
        break;
      }
      default:
        break;
    }
  };

  return (
    <div ref={menuRef} className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={t('language.label')}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid="language-menu-trigger"
        data-locale={current}
        onClick={() => setOpen((v) => !v)}
        className="p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-white/5 transition-colors flex items-center gap-1"
        title={t('language.label')}
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
          <circle cx="12" cy="12" r="10" />
          <path d="M2 12h20" />
          <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
        </svg>
        <span className="text-[10px] font-mono uppercase">
          {current === 'zh-CN' ? '中' : 'EN'}
        </span>
      </button>
      {open && (
        <div
          ref={listRef}
          role="menu"
          aria-label={t('language.label')}
          data-testid="language-menu"
          onKeyDown={handleMenuKeyDown}
          className="absolute right-0 mt-1 min-w-[160px] rounded border border-border bg-bg-primary shadow-lg z-10"
        >
          {SUPPORTED_LOCALES.map((loc) => {
            const active = current === loc;
            return (
              <button
                key={loc}
                type="button"
                role="menuitemradio"
                aria-checked={active}
                data-testid={`language-option-${loc}`}
                onClick={async () => {
                  await changeLocale(loc);
                  setOpen(false);
                }}
                className={`flex w-full items-center justify-between px-3 py-2 text-xs font-sans transition-colors ${
                  active
                    ? 'text-accent-cyan'
                    : 'text-text-primary hover:bg-bg-secondary'
                }`}
              >
                <span>{t(`language.${loc}`)}</span>
                <span className="ml-3 text-text-muted font-mono">{loc}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
