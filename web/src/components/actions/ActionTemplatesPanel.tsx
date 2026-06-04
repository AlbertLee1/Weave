import { useCallback, useState } from 'react';
import {
  useActionTemplates,
  useCreateActionTemplate,
  useDeleteActionTemplate,
  useUpdateActionTemplate,
} from '../../hooks/useActionTemplates';
import type {
  ActionParameters,
  ActionTemplate,
  ActionTemplateScope,
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
  // matches gain a delete + scope-edit affordance, others render as
  // read-only entries shared by another user.
  currentUserId?: string;
}

const SCOPE_OPTIONS: Array<{ value: ActionTemplateScope; label: string; help: string }> = [
  {
    value: 'PRIVATE',
    label: 'Private',
    help: 'Only you can see this template.',
  },
  {
    value: 'TEAM',
    label: 'Team',
    help: 'Anyone sharing a group with you can see this template.',
  },
  {
    value: 'PUBLIC',
    label: 'Public',
    help: 'Anyone signed in can see this template.',
  },
];

const SCOPE_BADGE_CLASS: Record<ActionTemplateScope, string> = {
  PRIVATE: 'text-text-muted',
  TEAM: 'text-accent-amber',
  PUBLIC: 'text-accent-cyan',
};

const SCOPE_BADGE_LABEL: Record<ActionTemplateScope, string> = {
  PRIVATE: 'private',
  TEAM: 'team',
  PUBLIC: 'public',
};

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
  const updateMutation = useUpdateActionTemplate();
  const deleteMutation = useDeleteActionTemplate();
  const pushToast = useToastStore((s) => s.push);

  const [saveOpen, setSaveOpen] = useState(false);
  const [name, setName] = useState('');
  const [scope, setScope] = useState<ActionTemplateScope>('PRIVATE');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  // Deletion is gated by the shared, styled `Modal` rather than a native
  // `window.confirm` (which can't be themed and clashes with the rest of
  // the app's dialogs) — see DashboardEditorPage's note. The pending
  // template (id + name) drives the confirm Modal; the destructive Delete
  // button inside it is the only path that fires the mutation.
  const [pendingDelete, setPendingDelete] = useState<{
    id: string;
    name: string;
  } | null>(null);

  const openSaveDialog = useCallback(() => {
    setName('');
    setScope('PRIVATE');
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
          scope,
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
    [actionType, createMutation, currentParameters, name, ontology, pushToast, scope],
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

  const handleDelete = useCallback((row: ActionTemplate) => {
    // Open the styled confirmation Modal instead of a native window.confirm.
    setPendingDelete({ id: row.id, name: row.name });
  }, []);

  const cancelDelete = useCallback(() => setPendingDelete(null), []);

  const confirmDelete = useCallback(() => {
    const target = pendingDelete;
    if (!target) return;
    setPendingDelete(null);
    deleteMutation.mutate(target.id, {
      onSuccess: () => {
        pushToast({
          message: `Template "${target.name}" deleted`,
          severity: 'info',
          ttlMs: 3000,
        });
      },
    });
  }, [deleteMutation, pendingDelete, pushToast]);

  const handleScopeChange = useCallback(
    (row: ActionTemplate, nextScope: ActionTemplateScope) => {
      if (row.scope === nextScope) {
        return;
      }
      updateMutation.mutate(
        { id: row.id, scope: nextScope },
        {
          onSuccess: (saved) => {
            pushToast({
              message: `Scope set to ${SCOPE_BADGE_LABEL[saved.scope]}`,
              severity: 'success',
              ttlMs: 3000,
            });
          },
          onError: (err) => {
            const reason = err instanceof Error ? err.message : 'Failed to update scope';
            pushToast({ message: reason, severity: 'error', ttlMs: 4000 });
          },
        },
      );
    },
    [pushToast, updateMutation],
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
            const scopeLabel = SCOPE_BADGE_LABEL[row.scope] ?? 'private';
            const scopeClass = SCOPE_BADGE_CLASS[row.scope] ?? '';
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
                  {row.scope !== 'PRIVATE' && (
                    <span
                      data-testid={`action-template-scope-badge-${row.id}`}
                      className={`ml-2 text-[10px] uppercase tracking-wider ${scopeClass}`}
                    >
                      {scopeLabel}
                    </span>
                  )}
                  {row.scope === 'PUBLIC' && (
                    <span
                      data-testid={`action-template-shared-badge-${row.id}`}
                      className="hidden"
                      aria-hidden="true"
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
                  <select
                    value={row.scope}
                    onChange={(e) =>
                      handleScopeChange(row, e.target.value as ActionTemplateScope)
                    }
                    aria-label={`Scope for ${row.name}`}
                    data-testid={`action-template-scope-select-${row.id}`}
                    className="text-[10px] font-mono bg-transparent border border-border rounded px-1 py-0.5 text-text-secondary hover:text-text-primary focus:outline-none focus:border-accent-cyan"
                  >
                    {SCOPE_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                )}
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
          <fieldset
            className="flex flex-col gap-1"
            data-testid="action-template-scope-fieldset"
          >
            <legend className="text-xs font-mono uppercase tracking-wider text-text-secondary">
              Visibility
            </legend>
            {SCOPE_OPTIONS.map((opt) => (
              <label
                key={opt.value}
                className="flex items-start gap-2 px-2 py-1 rounded border border-transparent hover:border-border cursor-pointer"
              >
                <input
                  type="radio"
                  name="action-template-scope"
                  value={opt.value}
                  checked={scope === opt.value}
                  onChange={() => setScope(opt.value)}
                  data-testid={`action-template-scope-input-${opt.value}`}
                  className="mt-0.5"
                />
                <span className="flex flex-col gap-0.5">
                  <span className="text-xs font-mono text-text-primary">{opt.label}</span>
                  <span className="text-[10px] font-mono text-text-muted">{opt.help}</span>
                </span>
              </label>
            ))}
          </fieldset>
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

      <Modal
        open={pendingDelete !== null}
        onClose={cancelDelete}
        title="Delete action template"
      >
        <div data-testid="action-template-delete-confirm" className="space-y-4">
          <p className="text-sm text-text-secondary">
            Delete template{' '}
            <span className="font-medium text-text-primary">
              "{pendingDelete?.name}"
            </span>
            ? This cannot be undone.
          </p>
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={cancelDelete}
              data-testid="action-template-delete-cancel-btn"
              className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={confirmDelete}
              disabled={deleteMutation.isPending}
              data-testid="action-template-delete-confirm-btn"
              className="rounded-md border border-rose-500/50 bg-rose-500/10 px-3 py-1.5 text-xs font-semibold text-rose-300 hover:bg-rose-500/20 disabled:opacity-50"
            >
              {deleteMutation.isPending ? 'Deleting…' : 'Delete'}
            </button>
          </div>
        </div>
      </Modal>
    </aside>
  );
}
