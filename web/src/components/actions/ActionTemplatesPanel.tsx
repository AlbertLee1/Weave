import { useCallback, useState } from 'react';
import {
  useActionTemplates,
  useCreateActionTemplate,
  useDeleteActionTemplate,
} from '../../hooks/useActionTemplates';
import type {
  ActionParameters,
  ActionTemplate,
} from '../../api/actionTemplates';
import { Modal } from '../common/Modal';
import { useToastStore } from '../../stores/toastStore';

interface ActionTemplatesPanelProps {
  ontology: string;
  actionType: string;
  // Currently-edited parameter values, used for "Save as template".
  currentParameters: ActionParameters;
  // True when the user has touched at least one parameter (empty
  // {} disables the save button so the user doesn't accidentally
  // persist an empty template).
  hasCurrentState: boolean;
  // Fired when the user picks one of the saved entries — the consumer
  // is expected to apply the parameters to its own form state.
  onLoad: (parameters: ActionParameters) => void;
  // Optional currently-authenticated user id; rows whose createdBy
  // matches gain a delete affordance, others render as read-only
  // shared-by-someone-else entries.
  currentUserId?: string;
}

export function ActionTemplatesPanel({
  ontology,
  actionType,
  currentParameters,
  hasCurrentState,
  onLoad,
  currentUserId,
}: ActionTemplatesPanelProps) {
  const { data: rows = [], isLoading } = useActionTemplates({ ontology, actionType });
  const createMutation = useCreateActionTemplate();
  const deleteMutation = useDeleteActionTemplate();
  const pushToast = useToastStore((s) => s.push);

  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState('');
  const [shared, setShared] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const openSaveDialog = useCallback(() => {
    setName('');
    setShared(false);
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
          actionType,
          shared,
          parameters: currentParameters,
        });
        setSaveOpen(false);
        pushToast({
          message: `Template "${trimmed}" saved`,
          severity: 'success',
          ttlMs: 4000,
        });
      } catch (err) {
        const reason = err instanceof Error ? err.message : 'Failed to save';
        setErrorMessage(reason);
      }
    },
    [actionType, createMutation, currentParameters, name, ontology, pushToast, shared],
  );

  const handleLoad = useCallback(
    (row: ActionTemplate) => {
      onLoad(row.parameters ?? {});
      pushToast({
        message: `Loaded template "${row.name}"`,
        severity: 'info',
        ttlMs: 3000,
      });
    },
    [onLoad, pushToast],
  );

  const handleDelete = useCallback(
    (row: ActionTemplate) => {
      if (typeof window !== 'undefined' && !window.confirm(
        `Delete template "${row.name}"?`,
      )) {
        return;
      }
      deleteMutation.mutate(row.id, {
        onSuccess: () => {
          pushToast({
            message: `Template "${row.name}" deleted`,
            severity: 'info',
            ttlMs: 3000,
          });
        },
      });
    },
    [deleteMutation, pushToast],
  );

  return (
    <aside
      data-testid="action-templates-panel"
      className="border border-border rounded p-3 bg-bg-primary"
      aria-label="Action parameter templates"
    >
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-xs font-medium text-text-primary">Templates</h3>
        <button
          type="button"
          onClick={openSaveDialog}
          disabled={!hasCurrentState}
          data-testid="action-template-save"
          className="text-xs font-mono text-accent-cyan hover:underline disabled:text-text-muted disabled:no-underline disabled:cursor-not-allowed"
          title={
            hasCurrentState
              ? 'Save current parameters as a template'
              : 'Fill in at least one parameter to save a template'
          }
        >
          + Save current
        </button>
      </div>

      {isLoading ? (
        <p className="text-xs font-mono text-text-muted">Loading…</p>
      ) : rows.length === 0 ? (
        <p
          data-testid="action-templates-empty"
          className="text-xs font-mono text-text-muted"
        >
          No templates saved yet.
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {rows.map((row) => {
            const isOwner = !!currentUserId && row.createdBy === currentUserId;
            return (
              <li
                key={row.id}
                data-testid={`action-template-${row.id}`}
                className="group flex items-center gap-1 px-1 py-0.5 rounded hover:bg-bg-secondary"
              >
                <button
                  type="button"
                  onClick={() => handleLoad(row)}
                  data-testid={`action-template-load-${row.id}`}
                  className="flex-1 truncate text-left text-xs font-mono text-text-primary hover:text-accent-cyan"
                  title={
                    isOwner
                      ? row.name
                      : `${row.name} (shared by ${row.createdBy})`
                  }
                >
                  {row.name}
                  {row.shared && (
                    <span
                      data-testid={`action-template-shared-badge-${row.id}`}
                      className="ml-2 text-[10px] uppercase tracking-wider text-accent-cyan"
                    >
                      shared
                    </span>
                  )}
                  {!isOwner && (
                    <span className="ml-2 text-[10px] text-text-muted truncate">
                      by {row.createdBy}
                    </span>
                  )}
                </button>
                {isOwner && (
                  <button
                    type="button"
                    onClick={() => handleDelete(row)}
                    aria-label={`Delete template ${row.name}`}
                    data-testid={`action-template-delete-${row.id}`}
                    className="opacity-0 group-hover:opacity-100 text-xs font-mono text-text-muted hover:text-accent-error"
                    title="Delete"
                  >
                    ×
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <Modal open={saveOpen} onClose={closeSaveDialog} title="Save action template">
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
              data-testid="action-template-name-input"
              className="px-2 py-1 rounded border border-border bg-bg-secondary text-xs font-mono text-text-primary outline-none focus:border-accent-cyan"
            />
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={shared}
              onChange={(e) => setShared(e.target.checked)}
              data-testid="action-template-shared-input"
            />
            <span className="text-xs font-mono text-text-secondary">
              Share with other users (read-only)
            </span>
          </label>
          {errorMessage && (
            <p
              role="alert"
              data-testid="action-template-error"
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
              data-testid="action-template-confirm"
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
