import { useCallback, useState } from 'react';
import {
  useCreateSavedSearch,
  useDeleteSavedSearch,
  useSavedSearches,
  useUpdateSavedSearch,
} from '../../hooks/useSavedSearches';
import type { SavedSearch, SavedSearchDefinition } from '../../api/savedSearches';
import { Modal } from '../common/Modal';

interface SavedSearchesPanelProps {
  ontology: string;
  objectType: string;
  // The current view's serialisable state — used for "Save current".
  currentDefinition: SavedSearchDefinition;
  // Whether the current view is non-default (search, filters, facets, sort).
  // When false the "Save current" button is disabled with a tooltip.
  hasCurrentState: boolean;
  // Fired when the user picks one of the saved entries — the consumer
  // is expected to apply the definition to its own state. The full row is
  // forwarded so the consumer can track which saved search is currently
  // applied (active-indicator UX); see BrowserPage.handleApplySavedSearch.
  onLoad: (row: SavedSearch) => void;
  // ID of the saved search the consumer considers "currently applied".
  // When set, the matching row renders with `aria-current="true"` and an
  // operator-visible Active badge so audit/Drift state is obvious.
  activeId?: string | null;
  // ID of the saved search the operator most recently loaded — separate
  // from `activeId` so it survives drift. When this row is present but
  // `activeId` no longer matches it, the panel exposes an "Update"
  // affordance that PUTs the current view back onto the saved search.
  lastLoadedId?: string | null;
  // Fired after a successful in-place update so the consumer can re-mark
  // the row as active (the operator's intent is "this is still my saved
  // view, now with the latest tweaks").
  onUpdated?: (row: SavedSearch) => void;
}

export function SavedSearchesPanel({
  ontology,
  objectType,
  currentDefinition,
  hasCurrentState,
  onLoad,
  activeId = null,
  lastLoadedId = null,
  onUpdated,
}: SavedSearchesPanelProps) {
  const { data: rows = [], isLoading } = useSavedSearches({ ontology, objectType });
  const createMutation = useCreateSavedSearch();
  const updateMutation = useUpdateSavedSearch();
  const deleteMutation = useDeleteSavedSearch();

  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState('');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [updateErrorId, setUpdateErrorId] = useState<string | null>(null);
  const [updatingId, setUpdatingId] = useState<string | null>(null);

  const openSaveDialog = useCallback(() => {
    setName('');
    setErrorMessage(null);
    setSaveOpen(true);
  }, []);

  const closeSaveDialog = useCallback(() => {
    setSaveOpen(false);
    setErrorMessage(null);
  }, []);

  const handleSave = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      const trimmed = name.trim();
      if (!trimmed) {
        setErrorMessage('Name is required');
        return;
      }
      try {
        await createMutation.mutateAsync({
          name: trimmed,
          ontology,
          objectType,
          definition: currentDefinition,
        });
        setSaveOpen(false);
      } catch (err) {
        const reason = err instanceof Error ? err.message : 'Failed to save';
        setErrorMessage(reason);
      }
    },
    [createMutation, currentDefinition, name, objectType, ontology],
  );

  const handleLoad = useCallback(
    (row: SavedSearch) => {
      onLoad(row);
    },
    [onLoad],
  );

  const handleDelete = useCallback(
    (row: SavedSearch) => {
      if (typeof window !== 'undefined' && !window.confirm(
        `Delete saved search "${row.name}"?`,
      )) {
        return;
      }
      deleteMutation.mutate(row.id);
    },
    [deleteMutation],
  );

  const handleUpdate = useCallback(
    async (row: SavedSearch) => {
      setUpdateErrorId(null);
      setUpdatingId(row.id);
      try {
        const saved = await updateMutation.mutateAsync({
          id: row.id,
          definition: currentDefinition,
        });
        onUpdated?.(saved);
      } catch {
        // We surface a per-row inline error rather than a modal — the
        // operator's next action is usually "tweak the view and retry",
        // and a modal would steal focus from the SearchBar/FilterBuilder.
        setUpdateErrorId(row.id);
      } finally {
        setUpdatingId(null);
      }
    },
    [currentDefinition, onUpdated, updateMutation],
  );

  return (
    <aside
      data-testid="saved-searches-panel"
      className="w-56 shrink-0 border-r border-border pr-4 self-stretch"
      aria-label="Saved searches"
    >
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-mono uppercase tracking-wider text-text-secondary">
          Saved Searches
        </h2>
        <button
          type="button"
          onClick={openSaveDialog}
          disabled={!hasCurrentState}
          data-testid="saved-searches-save"
          className="text-xs font-mono text-accent-cyan hover:underline disabled:text-text-muted disabled:no-underline disabled:cursor-not-allowed"
          title={
            hasCurrentState
              ? 'Save the current search/filters/facets'
              : 'Apply a search, filter, or facet to enable saving'
          }
        >
          + Save
        </button>
      </div>

      {isLoading ? (
        <p className="text-xs font-mono text-text-muted">Loading…</p>
      ) : rows.length === 0 ? (
        <p
          data-testid="saved-searches-empty"
          className="text-xs font-mono text-text-muted"
        >
          No saved searches yet.
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {rows.map((row) => {
            const isActive = activeId === row.id;
            // "Drifted" = the operator loaded this row but has since
            // changed the view. We only ever offer Update for the row
            // they originated from to avoid clobbering an unrelated
            // saved search with the current ad-hoc view.
            const isDrifted = !isActive && lastLoadedId === row.id;
            const isUpdating = updatingId === row.id;
            const showUpdateError = updateErrorId === row.id;
            return (
              <li
                key={row.id}
                data-testid={`saved-search-${row.id}`}
                aria-current={isActive ? 'true' : undefined}
                className={[
                  'group flex flex-col gap-0.5 px-1 py-0.5 rounded',
                  isActive
                    ? 'bg-accent-cyan/10'
                    : 'hover:bg-bg-secondary',
                ].join(' ')}
              >
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={() => handleLoad(row)}
                    data-testid={`saved-search-load-${row.id}`}
                    className={[
                      'flex-1 truncate text-left text-xs font-mono hover:text-accent-cyan',
                      isActive ? 'text-accent-cyan' : 'text-text-primary',
                    ].join(' ')}
                    title={row.name}
                  >
                    {row.name}
                  </button>
                  {isActive && (
                    <span
                      data-testid={`saved-search-active-badge-${row.id}`}
                      aria-label="Currently applied"
                      title="This saved search matches the current view"
                      className="text-[9px] font-mono uppercase tracking-wider px-1 py-0.5 rounded bg-accent-cyan/15 text-accent-cyan"
                    >
                      Active
                    </span>
                  )}
                  {isDrifted && (
                    <button
                      type="button"
                      onClick={() => handleUpdate(row)}
                      disabled={isUpdating}
                      data-testid={`saved-search-update-${row.id}`}
                      aria-label={`Update saved search ${row.name} with the current view`}
                      title="Save current view into this saved search"
                      className="text-[9px] font-mono uppercase tracking-wider px-1 py-0.5 rounded border border-accent-cyan/40 text-accent-cyan hover:bg-accent-cyan/10 disabled:opacity-50"
                    >
                      {isUpdating ? 'Saving…' : 'Update'}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => handleDelete(row)}
                    aria-label={`Delete saved search ${row.name}`}
                    data-testid={`saved-search-delete-${row.id}`}
                    className="opacity-0 group-hover:opacity-100 text-xs font-mono text-text-muted hover:text-accent-error"
                    title="Delete"
                  >
                    ×
                  </button>
                </div>
                {showUpdateError && (
                  <p
                    role="alert"
                    data-testid={`saved-search-update-error-${row.id}`}
                    className="text-[10px] font-mono text-accent-error pl-1"
                  >
                    Failed to update — retry or reload the saved view.
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <Modal open={saveOpen} onClose={closeSaveDialog} title="Save search">
        <form onSubmit={handleSave} className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-xs font-mono uppercase tracking-wider text-text-secondary">
              Name
            </span>
            <input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={128}
              data-testid="saved-searches-name-input"
              className="px-2 py-1 rounded border border-border bg-bg-secondary text-xs font-mono text-text-primary outline-none focus:border-accent-cyan"
            />
          </label>
          {errorMessage && (
            <p
              role="alert"
              data-testid="saved-searches-error"
              className="text-xs font-mono text-accent-error"
            >
              {errorMessage}
            </p>
          )}
          <div className="flex justify-end gap-2 mt-2">
            <button
              type="button"
              onClick={closeSaveDialog}
              className="px-3 py-1 rounded border border-border text-xs font-mono text-text-secondary hover:text-text-primary"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending}
              data-testid="saved-searches-confirm"
              className="px-3 py-1 rounded border border-accent-cyan text-xs font-mono text-accent-cyan hover:bg-accent-cyan/10 disabled:opacity-50"
            >
              {createMutation.isPending ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      </Modal>
    </aside>
  );
}
