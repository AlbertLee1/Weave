import {
  useState,
  useRef,
  useEffect,
  useCallback,
  type KeyboardEvent,
} from 'react';
import {
  exportObjects,
  type ExportFormat,
  type ExportQuery,
} from '../../lib/exportObjects';
import type { ObjectType } from '../../api/types';

interface ExportButtonProps {
  objectType: ObjectType;
  query: ExportQuery;
  disabled?: boolean;
  disabledReason?: string;
}

export function ExportButton({
  objectType,
  query,
  disabled = false,
  disabledReason,
}: ExportButtonProps) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const disabledDescriptionId =
    disabled && disabledReason ? 'export-disabled-reason' : undefined;

  const getMenuItems = useCallback(
    () =>
      Array.from(
        menuRef.current?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [],
      ),
    [],
  );

  const closeMenu = useCallback((restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) {
      triggerRef.current?.focus();
    }
  }, []);

  // When the menu opens, move focus to the first menu item (WAI-ARIA menu pattern).
  useEffect(() => {
    if (!open || busy) return;
    const items = getMenuItems();
    items[0]?.focus();
  }, [open, busy, getMenuItems]);

  const handleMenuKeyDown = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      const items = getMenuItems();
      if (items.length === 0) {
        if (e.key === 'Escape') {
          e.preventDefault();
          closeMenu(true);
        }
        return;
      }

      const activeIndex = items.findIndex(
        (item) => item === document.activeElement,
      );

      switch (e.key) {
        case 'ArrowDown': {
          e.preventDefault();
          const next = activeIndex < 0 ? 0 : (activeIndex + 1) % items.length;
          items[next]?.focus();
          break;
        }
        case 'ArrowUp': {
          e.preventDefault();
          const prev =
            activeIndex <= 0 ? items.length - 1 : activeIndex - 1;
          items[prev]?.focus();
          break;
        }
        case 'Home': {
          e.preventDefault();
          items[0]?.focus();
          break;
        }
        case 'End': {
          e.preventDefault();
          items[items.length - 1]?.focus();
          break;
        }
        case 'Escape': {
          e.preventDefault();
          closeMenu(true);
          break;
        }
        default:
          break;
      }
    },
    [getMenuItems, closeMenu],
  );

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open]);

  useEffect(() => {
    if (disabled) setOpen(false);
  }, [disabled]);

  const handleExport = useCallback(
    async (format: ExportFormat) => {
      if (disabled) return;
      setOpen(false);
      setError(null);
      setBusy(true);
      setProgress(0);
      try {
        await exportObjects(format, query, objectType, (count) => {
          setProgress(count);
        });
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Export failed');
      } finally {
        setBusy(false);
      }
    },
    [disabled, query, objectType],
  );

  return (
    <div ref={rootRef} className="relative inline-block">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => {
          if (disabled) return;
          setOpen((v) => !v);
        }}
        disabled={busy || disabled}
        title={disabled ? disabledReason : undefined}
        aria-describedby={disabledDescriptionId}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid="export-button"
        className="flex items-center gap-1.5 px-3 py-2 bg-bg-primary border border-border rounded text-xs font-sans text-text-secondary hover:text-text-primary hover:border-accent-cyan disabled:opacity-50 transition-colors"
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <path d="M12 3v12m0 0l-4-4m4 4l4-4M5 21h14" />
        </svg>
        {busy ? `Exporting${progress > 0 ? ` (${progress})` : '...'}` : 'Export'}
        <svg
          className="w-3 h-3 opacity-60"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>

      {disabledDescriptionId && (
        <span
          id={disabledDescriptionId}
          data-testid="export-disabled-reason"
          className="sr-only"
        >
          {disabledReason}
        </span>
      )}

      {open && !busy && (
        <div
          ref={menuRef}
          role="menu"
          aria-orientation="vertical"
          onKeyDown={handleMenuKeyDown}
          className="absolute right-0 mt-1 min-w-[160px] rounded border border-border bg-bg-primary shadow-lg z-10"
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => handleExport('csv')}
            data-testid="export-csv"
            className="block w-full text-left px-3 py-2 text-xs font-sans text-text-primary hover:bg-bg-secondary transition-colors"
          >
            Export as CSV
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => handleExport('json')}
            data-testid="export-json"
            className="block w-full text-left px-3 py-2 text-xs font-sans text-text-primary hover:bg-bg-secondary transition-colors"
          >
            Export as JSON
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => handleExport('xlsx')}
            data-testid="export-xlsx"
            className="block w-full text-left px-3 py-2 text-xs font-sans text-text-primary hover:bg-bg-secondary transition-colors"
          >
            Export as Excel
          </button>
        </div>
      )}

      {error && (
        <div
          role="alert"
          className="absolute right-0 mt-1 px-3 py-2 rounded border border-accent-error/30 bg-accent-error/5 text-xs font-mono text-accent-error whitespace-nowrap"
        >
          {error}
        </div>
      )}
    </div>
  );
}
