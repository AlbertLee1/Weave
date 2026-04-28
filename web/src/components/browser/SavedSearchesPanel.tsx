import { useCallback, useState } from 'react';
import {
  useCreateSavedSearch,
  useDeleteSavedSearch,
  useSavedSearches,
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
  // is expected to apply the definition to its own state.
  onLoad: (definition: SavedSearchDefinition) => void;
}

export function SavedSearchesPanel({
  ontology,
  objectType,
  currentDefinition,
  hasCurrentState,
  onLoad,
}: SavedSearchesPanelProps) {
  const { data: rows = [], isLoading } = useSavedSearches({ ontology, objectType });
  const createMutation = useCreateSavedSearch();
  const deleteMutation = useDeleteSavedSearch();

  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState('');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

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
      onLoad(row.definition ?? {});
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
          {rows.map((row) => (
            <li
              key={row.id}
              data-testid={`saved-search-${row.id}`}
              className="group flex items-center gap-1 px-1 py-0.5 rounded hover:bg-bg-secondary"
            >
              <button
                type="button"
                onClick={() => handleLoad(row)}
                data-testid={`saved-search-load-${row.id}`}
                className="flex-1 truncate text-left text-xs font-mono text-text-primary hover:text-accent-cyan"
                title={row.name}
              >
                {row.name}
              </button>
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
            </li>
          ))}
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
