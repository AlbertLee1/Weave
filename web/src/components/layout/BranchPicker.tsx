import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { getBranch, listBranches } from '../../api/ontologies';
import { useCreateBranch, useDeleteBranch } from '../../hooks/useBranches';
import type { BranchDetailResponse, OntologyBranch } from '../../api/types';
import { DEFAULT_BRANCH, useBranchStore } from '../../stores/branchStore';

interface BranchPickerProps {
  ontologyApiName: string | null;
}

interface BranchChangeCountBadgeProps {
  ontologyApiName: string;
  branchId: string;
}

// Renders a "N pending" badge for a single non-default branch row. Fetches the
// branch detail (BranchDetailResponse.changeCount) lazily and renders nothing
// until the count is known or when there are zero pending changes.
function BranchChangeCountBadge({
  ontologyApiName,
  branchId,
}: BranchChangeCountBadgeProps) {
  const { t } = useTranslation();
  const { data } = useQuery<BranchDetailResponse>({
    queryKey: ['branch-detail', ontologyApiName, branchId],
    queryFn: () => getBranch(ontologyApiName, branchId),
    staleTime: 30_000,
  });

  const count = data?.changeCount ?? 0;
  if (count <= 0) return null;

  return (
    <span
      data-testid={`branch-picker-change-count-${branchId}`}
      title={t('branch.pendingChangesTitle', { n: count })}
      className="ml-2 inline-flex items-center rounded-full bg-bg-tertiary px-1.5 py-0.5 text-[10px] font-medium text-text-secondary"
    >
      {t('branch.pendingChanges', { n: count })}
    </span>
  );
}

export function BranchPicker({ ontologyApiName }: BranchPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const menuRef = useRef<HTMLDivElement | null>(null);
  const queryClient = useQueryClient();

  const activeBranch = useBranchStore((s) =>
    ontologyApiName ? s.selections[ontologyApiName] || DEFAULT_BRANCH : DEFAULT_BRANCH,
  );
  const setBranch = useBranchStore((s) => s.setBranch);
  const clearBranch = useBranchStore((s) => s.clearBranch);

  const createMutation = useCreateBranch(ontologyApiName ?? '');
  const deleteMutation = useDeleteBranch(ontologyApiName ?? '');
  // `reset` is a stable callback from TanStack Query; destructure it so the
  // close-menu effect below doesn't re-run on every render (the mutation
  // object identity changes each render, the reset reference does not).
  const resetCreate = createMutation.reset;

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

  // Close the menu and collapse the inline create form together so reopening
  // always lands on the plain branch list. Routed through every close path
  // (outside-click, select, toggle) rather than a setState-in-effect.
  const closeMenu = useCallback(() => {
    setOpen(false);
    setCreating(false);
    setNewName('');
    resetCreate();
  }, [resetCreate]);

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) closeMenu();
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open, closeMenu]);

  if (!ontologyApiName) return null;

  const onSelect = (branchId: string) => {
    if (branchId !== activeBranch) {
      setBranch(ontologyApiName, branchId);
      queryClient.invalidateQueries();
    }
    closeMenu();
  };

  const trimmedName = newName.trim();

  const onCreateSubmit = () => {
    if (!trimmedName || createMutation.isPending) return;
    createMutation.mutate(
      { name: trimmedName },
      {
        onSuccess: (created) => {
          setCreating(false);
          setNewName('');
          // Switch the active branch to the freshly-created one so the user
          // immediately works inside it.
          setBranch(ontologyApiName, created.id);
          queryClient.invalidateQueries();
        },
      },
    );
  };

  // Accepts the id/name pair the row already has — the synthetic "main" entry
  // in `items` is not a full OntologyBranch, and the delete button only ever
  // renders for non-default rows anyway.
  const onDelete = (branchId: string, branchName: string) => {
    if (deleteMutation.isPending) return;
    if (!window.confirm(t('branch.deleteConfirm', { name: branchName }))) return;
    deleteMutation.mutate(branchId, {
      onSuccess: () => {
        // If the closed branch was the active selection, fall back to main.
        if (activeBranch === branchId) {
          clearBranch(ontologyApiName);
        }
        queryClient.invalidateQueries();
      },
    });
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
        onClick={() => (open ? closeMenu() : setOpen(true))}
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
              const isDefault = b.id === DEFAULT_BRANCH;
              return (
                <div
                  key={b.id}
                  className={`group flex items-center ${
                    active ? '' : 'hover:bg-bg-secondary'
                  }`}
                >
                  <button
                    type="button"
                    role="menuitemradio"
                    aria-checked={active}
                    data-testid={`branch-picker-option-${b.id}`}
                    onClick={() => onSelect(b.id)}
                    className={`flex flex-1 items-center justify-between px-3 py-2 text-xs font-sans transition-colors ${
                      active ? 'text-accent-cyan' : 'text-text-primary'
                    }`}
                  >
                    <span className="font-mono truncate">{b.name}</span>
                    {isDefault ? (
                      <span className="ml-3 text-text-muted text-[10px] uppercase">
                        {t('branch.default')}
                      </span>
                    ) : (
                      <BranchChangeCountBadge
                        ontologyApiName={ontologyApiName}
                        branchId={b.id}
                      />
                    )}
                  </button>
                  {!isDefault ? (
                    <button
                      type="button"
                      data-testid={`branch-picker-delete-${b.id}`}
                      aria-label={t('branch.deleteLabel')}
                      title={t('branch.deleteLabel')}
                      disabled={deleteMutation.isPending}
                      onClick={() => onDelete(b.id, b.name)}
                      className="px-2 py-2 text-text-muted hover:text-red-400 opacity-60 hover:opacity-100 transition-colors disabled:cursor-not-allowed"
                    >
                      <svg
                        className="w-3.5 h-3.5"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        aria-hidden="true"
                      >
                        <polyline points="3 6 5 6 21 6" />
                        <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                        <path d="M10 11v6M14 11v6" />
                        <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                      </svg>
                    </button>
                  ) : null}
                </div>
              );
            })
          )}

          {/* US-113 create-branch affordance */}
          <div className="border-t border-border">
            {creating ? (
              <form
                data-testid="branch-picker-create-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  onCreateSubmit();
                }}
                className="px-3 py-2.5 space-y-2"
              >
                <input
                  autoFocus
                  data-testid="branch-picker-create-name"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder={t('branch.createNamePlaceholder')}
                  className="w-full px-2 py-1 rounded border border-border bg-bg-secondary text-xs font-mono text-text-primary focus:outline-none focus:border-accent-cyan"
                />
                {createMutation.isError ? (
                  <p
                    data-testid="branch-picker-create-error"
                    className="text-[10px] text-red-400"
                  >
                    {t('branch.createError', {
                      message: String(createMutation.error),
                    })}
                  </p>
                ) : null}
                <div className="flex items-center gap-2">
                  <button
                    type="submit"
                    data-testid="branch-picker-create-submit"
                    disabled={!trimmedName || createMutation.isPending}
                    className="px-2.5 py-1 rounded text-[11px] font-medium bg-accent-cyan text-bg-primary disabled:bg-bg-tertiary disabled:text-text-muted disabled:cursor-not-allowed"
                  >
                    {createMutation.isPending
                      ? t('branch.creating')
                      : t('branch.createSubmit')}
                  </button>
                  <button
                    type="button"
                    data-testid="branch-picker-create-cancel"
                    onClick={() => {
                      setCreating(false);
                      setNewName('');
                      createMutation.reset();
                    }}
                    className="px-2.5 py-1 rounded text-[11px] text-text-secondary hover:text-text-primary"
                  >
                    {t('branch.cancel')}
                  </button>
                </div>
              </form>
            ) : (
              <button
                type="button"
                data-testid="branch-picker-create-toggle"
                onClick={() => setCreating(true)}
                className="flex w-full items-center gap-2 px-3 py-2 text-xs font-sans text-text-secondary hover:text-accent-cyan hover:bg-bg-secondary transition-colors"
              >
                <svg
                  className="w-3.5 h-3.5"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <line x1="12" y1="5" x2="12" y2="19" />
                  <line x1="5" y1="12" x2="19" y2="12" />
                </svg>
                {t('branch.createNew')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
