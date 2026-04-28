import { useState, useRef, useEffect, useCallback } from 'react';
import {
  exportObjects,
  type ExportFormat,
  type ExportQuery,
} from '../../lib/exportObjects';
import type { ObjectType } from '../../api/types';

interface ExportButtonProps {
  objectType: ObjectType;
  query: ExportQuery;
}

export function ExportButton({ objectType, query }: ExportButtonProps) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);

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

  const handleExport = useCallback(
    async (format: ExportFormat) => {
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
    [query, objectType],
  );

  return (
    <div ref={rootRef} className="relative inline-block">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        disabled={busy}
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

      {open && !busy && (
        <div
          role="menu"
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
