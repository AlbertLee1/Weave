import { useMemo, useState } from 'react';
import type {
  ActionType,
  ObjectType,
  WireObject,
} from '../../api/types';
import { useActionTypes, useApplyBatch } from '../../hooks/useActions';
import { Modal } from '../common/Modal';
import { toCsv, toJsonEnvelope, triggerDownload } from '../../lib/exportObjects';

interface BulkActionToolbarProps {
  ontologyApiName: string;
  objectType: ObjectType;
  selectedRows: WireObject[];
  onClear: () => void;
  onDeleted?: () => void;
}

// Looks up an ActionType whose rules include a deleteObject rule targeting
// the given objectType apiName. Rules JSON is loosely typed; we walk any
// nested shape for { type: 'deleteObject', objectType }.
function findDeleteAction(
  actions: ActionType[],
  objectTypeApiName: string,
): ActionType | null {
  for (const action of actions) {
    if (rulesDeletingObjectType(action.rules, objectTypeApiName)) return action;
  }
  return null;
}

function rulesDeletingObjectType(v: unknown, objectTypeApiName: string): boolean {
  if (v == null) return false;
  if (Array.isArray(v)) {
    return v.some((item) => rulesDeletingObjectType(item, objectTypeApiName));
  }
  if (typeof v === 'object') {
    const obj = v as Record<string, unknown>;
    if (
      obj.type === 'deleteObject' &&
      typeof obj.objectType === 'string' &&
      obj.objectType === objectTypeApiName
    ) {
      return true;
    }
    for (const key of Object.keys(obj)) {
      if (rulesDeletingObjectType(obj[key], objectTypeApiName)) return true;
    }
  }
  return false;
}

function findPrimaryKeyParam(action: ActionType): string | null {
  const params = action.parameters ?? {};
  const names = Object.keys(params);
  if (names.length === 0) return null;
  const preferred = names.find((n) => /primaryKey|primary_key|pk/i.test(n));
  return preferred ?? names[0];
}

export function BulkActionToolbar({
  ontologyApiName,
  objectType,
  selectedRows,
  onClear,
  onDeleted,
}: BulkActionToolbarProps) {
  const { data: actions } = useActionTypes(ontologyApiName);
  const applyBatch = useApplyBatch(ontologyApiName);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [exportOpen, setExportOpen] = useState(false);

  const deleteAction = useMemo(() => {
    if (!actions) return null;
    return findDeleteAction(actions, objectType.apiName);
  }, [actions, objectType.apiName]);

  const deletePkParam = useMemo(
    () => (deleteAction ? findPrimaryKeyParam(deleteAction) : null),
    [deleteAction],
  );

  const count = selectedRows.length;
  if (count === 0) return null;

  const handleConfirmDelete = async () => {
    if (!deleteAction || !deletePkParam) return;
    setErrorMsg(null);
    try {
      await applyBatch.mutateAsync({
        action: deleteAction.apiName,
        requests: selectedRows.map((row) => ({
          parameters: { [deletePkParam]: row.__primaryKey },
        })),
      });
      setConfirmOpen(false);
      onDeleted?.();
      onClear();
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : 'Delete failed');
    }
  };

  const handleExport = (format: 'csv' | 'json') => {
    setExportOpen(false);
    const columns = Object.keys(objectType.properties ?? {});
    if (format === 'csv') {
      const csv = toCsv(selectedRows, columns);
      triggerDownload(
        csv,
        `${objectType.apiName}-selected.csv`,
        'text/csv;charset=utf-8;',
      );
    } else {
      const envelope = toJsonEnvelope(selectedRows, objectType.apiName);
      triggerDownload(
        JSON.stringify(envelope, null, 2),
        `${objectType.apiName}-selected.json`,
        'application/json;charset=utf-8;',
      );
    }
  };

  return (
    <>
      <div
        data-testid="bulk-action-toolbar"
        className="fixed bottom-6 left-1/2 -translate-x-1/2 z-40 flex items-center gap-3 px-4 py-3 rounded-full border border-accent-cyan/30 bg-bg-elevated shadow-lg"
        style={{ backdropFilter: 'blur(12px)' }}
      >
        <span className="text-xs font-mono text-text-primary" data-testid="selected-count">
          {count} selected
        </span>
        <div className="h-4 w-px bg-border" />
        <button
          type="button"
          onClick={() => {
            setErrorMsg(null);
            setConfirmOpen(true);
          }}
          disabled={applyBatch.isPending}
          title={deleteAction ? 'Delete selected' : 'No deleteObject action configured'}
          data-testid="bulk-delete"
          className="px-3 py-1.5 rounded text-xs font-sans text-accent-error border border-accent-error/40 hover:bg-accent-error/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
        >
          Delete
        </button>
        <div className="relative">
          <button
            type="button"
            onClick={() => setExportOpen((v) => !v)}
            data-testid="bulk-export"
            className="px-3 py-1.5 rounded text-xs font-sans text-text-secondary hover:text-text-primary border border-border hover:border-accent-cyan transition-colors"
          >
            Export Selected
          </button>
          {exportOpen && (
            <div
              role="menu"
              className="absolute bottom-full mb-2 left-0 min-w-[160px] rounded border border-border bg-bg-primary shadow-lg"
            >
              <button
                type="button"
                role="menuitem"
                onClick={() => handleExport('csv')}
                data-testid="bulk-export-csv"
                className="block w-full text-left px-3 py-2 text-xs font-sans text-text-primary hover:bg-bg-secondary"
              >
                Export as CSV
              </button>
              <button
                type="button"
                role="menuitem"
                onClick={() => handleExport('json')}
                data-testid="bulk-export-json"
                className="block w-full text-left px-3 py-2 text-xs font-sans text-text-primary hover:bg-bg-secondary"
              >
                Export as JSON
              </button>
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={onClear}
          data-testid="bulk-clear"
          className="px-3 py-1.5 rounded text-xs font-sans text-text-secondary hover:text-text-primary transition-colors"
        >
          Clear
        </button>
      </div>

      <Modal
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        title="Delete selected objects?"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-primary">
            You are about to delete <strong>{count}</strong> {objectType.displayName}
            {count === 1 ? '' : 's'}. This action cannot be undone here.
          </p>
          {!deleteAction && (
            <p className="text-xs font-mono text-accent-error" data-testid="no-delete-action">
              No ActionType with a deleteObject rule targeting "{objectType.apiName}" is
              defined for this ontology. Configure one in the Ontology Manager first.
            </p>
          )}
          {errorMsg && (
            <p className="text-xs font-mono text-accent-error" role="alert">
              {errorMsg}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => setConfirmOpen(false)}
              className="px-3 py-2 rounded text-xs font-sans text-text-secondary hover:text-text-primary border border-border"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleConfirmDelete}
              disabled={!deleteAction || applyBatch.isPending}
              data-testid="bulk-delete-confirm"
              className="px-3 py-2 rounded text-xs font-sans text-white bg-accent-error hover:opacity-90 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {applyBatch.isPending ? 'Deleting...' : `Delete ${count}`}
            </button>
          </div>
        </div>
      </Modal>
    </>
  );
}
