import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { listBranches } from '../../api/ontologies';
import type { OntologyBranch } from '../../api/types';
import { DEFAULT_BRANCH, useBranchStore } from '../../stores/branchStore';

interface BranchPickerProps {
  ontologyApiName: string | null;
}

export function BranchPicker({ ontologyApiName }: BranchPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const queryClient = useQueryClient();

  const activeBranch = useBranchStore((s) =>
    ontologyApiName ? s.selections[ontologyApiName] || DEFAULT_BRANCH : DEFAULT_BRANCH,
  );
  const setBranch = useBranchStore((s) => s.setBranch);

  const { data: branches = [], isLoading } = useQuery<OntologyBranch[]>({
    queryKey: ['branches', ontologyApiName],
    queryFn: () => listBranches(ontologyApiName as string),
    enabled: !!ontologyApiName && open,
    staleTime: 30_000,
  });

  const items = useMemo(() => {
    const sorted = [...branches].sort((a, b) => a.name.localeCompare(b.name));
    return [{ id: DEFAULT_BRANCH, name: DEFAULT_BRANCH }, ...sorted];
  }, [branches]);

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open]);

  if (!ontologyApiName) return null;

  const onSelect = (branchId: string) => {
    if (branchId !== activeBranch) {
      setBranch(ontologyApiName, branchId);
      queryClient.invalidateQueries();
    }
    setOpen(false);
  };

  return (
    <div ref={menuRef} className="relative">
      <button
        type="button"
        aria-label={t('branch.label')}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid="branch-picker-trigger"
        data-branch={activeBranch}
        data-ontology={ontologyApiName}
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 px-2 py-1 rounded-md text-text-secondary hover:text-text-primary hover:bg-white/5 transition-colors text-xs font-mono"
        title={t('branch.label')}
      >
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <line x1="6" y1="3" x2="6" y2="15" />
          <circle cx="18" cy="6" r="3" />
          <circle cx="6" cy="18" r="3" />
          <path d="M18 9a9 9 0 0 1-9 9" />
        </svg>
        <span data-testid="branch-picker-active">{activeBranch}</span>
        <svg
          className="w-3 h-3 opacity-60"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          aria-hidden="true"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>
      {open && (
        <div
          role="menu"
          aria-label={t('branch.label')}
          data-testid="branch-picker-menu"
          className="absolute right-0 mt-1 min-w-[220px] max-h-72 overflow-y-auto rounded border border-border bg-bg-primary shadow-lg z-10"
        >
          {isLoading && items.length <= 1 ? (
            <div
              data-testid="branch-picker-loading"
              className="px-3 py-2 text-xs font-sans text-text-muted"
            >
              {t('branch.loading')}
            </div>
          ) : (
            items.map((b) => {
              const active = activeBranch === b.id;
              return (
                <button
                  key={b.id}
                  type="button"
                  role="menuitemradio"
                  aria-checked={active}
                  data-testid={`branch-picker-option-${b.id}`}
                  onClick={() => onSelect(b.id)}
                  className={`flex w-full items-center justify-between px-3 py-2 text-xs font-sans transition-colors ${
                    active
                      ? 'text-accent-cyan'
                      : 'text-text-primary hover:bg-bg-secondary'
                  }`}
                >
                  <span className="font-mono truncate">{b.name}</span>
                  {b.id === DEFAULT_BRANCH ? (
                    <span className="ml-3 text-text-muted text-[10px] uppercase">
                      {t('branch.default')}
                    </span>
                  ) : null}
                </button>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
