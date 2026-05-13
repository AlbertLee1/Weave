import { useEffect, useMemo, useState } from 'react';
import type { ObjectType, DatasourceBinding } from '../../api/types';
import {
  useDatasourceBindings,
  useCreateDatasourceBinding,
  useUpdateDatasourceBinding,
  useDeleteDatasourceBinding,
} from '../../hooks/useDatasourceBindings';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

// MappingRow is the array-shape the form edits; the wire-shape is an
// object map (apiName → source column). The double rep is intentional:
// arrays let users add / remove / reorder rows without juggling map keys
// in React state.
interface MappingRow {
  property: string;
  column: string;
}

function mappingToRows(mapping: unknown): MappingRow[] {
  if (!mapping || typeof mapping !== 'object') return [];
  const obj = mapping as Record<string, unknown>;
  return Object.entries(obj).map(([property, raw]) => ({
    property,
    column: typeof raw === 'string' ? raw : String(raw ?? ''),
  }));
}

function rowsToMapping(rows: MappingRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of rows) {
    const k = r.property.trim();
    if (!k) continue;
    out[k] = r.column.trim();
  }
  return out;
}

// derive a short table label from datasetRid for compact display in the
// table column. We keep the dataset RID authoritative — the "table" view
// is a UX affordance (last RID segment) that matches the AC literally
// without inventing a separate backend field.
function deriveTableLabel(datasetRid: string): string {
  if (!datasetRid) return '';
  const parts = datasetRid.split('.');
  return parts.length > 0 ? parts[parts.length - 1] : datasetRid;
}

export function BindingsEditor({
  ontologyApiName,
  objectType,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
}) {
  const { data, isLoading, error } = useDatasourceBindings(
    ontologyApiName,
    objectType.rid,
  );
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<DatasourceBinding | null>(null);
  const [deleting, setDeleting] = useState<DatasourceBinding | null>(null);

  const rows = useMemo(() => data ?? [], [data]);

  return (
    <div data-testid="bindings-editor" className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <p
          className="text-[11px] uppercase tracking-widest text-text-secondary"
          data-testid="bindings-count"
          data-bindings-count={rows.length}
        >
          {rows.length} {rows.length === 1 ? 'binding' : 'bindings'}
        </p>
        <button
          type="button"
          data-testid="bindings-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30"
        >
          + Add Binding
        </button>
      </div>

      {isLoading && (
        <div
          className="flex items-center justify-center py-6"
          data-testid="bindings-loading"
        >
          <LoadingSpinner size="md" />
        </div>
      )}
      {!isLoading && error && (
        <p data-testid="bindings-error" className="text-xs text-accent-error">
          Failed to load bindings: {(error as Error).message}
        </p>
      )}
      {!isLoading && !error && rows.length === 0 && (
        <div
          data-testid="bindings-empty"
          className="rounded border px-4 py-6 text-center text-xs text-text-secondary"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          No datasource bindings yet. Click “Add Binding” to map this
          ObjectType to an upstream dataset.
        </div>
      )}

      {!isLoading && !error && rows.length > 0 && (
        <div
          className="rounded border overflow-hidden"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <table className="w-full text-xs" data-testid="bindings-table">
            <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
              <tr
                className="border-b"
                style={{ borderColor: 'rgba(31,41,55,0.5)' }}
              >
                <th className="text-left px-3 py-2 font-medium">Dataset</th>
                <th className="text-left px-3 py-2 font-medium">Table</th>
                <th className="text-left px-3 py-2 font-medium">Column Mapping</th>
                <th className="text-left px-3 py-2 font-medium">Lineage</th>
                <th className="text-left px-3 py-2 font-medium">Primary</th>
                <th className="text-right px-3 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((b) => {
                const mappingRows = mappingToRows(b.columnMapping);
                const mappingCount = mappingRows.length;
                const tableLabel = deriveTableLabel(b.datasetRid);
                // Lineage derivation is automatic & unconditional whenever
                // columnMapping has entries; an empty mapping yields no
                // edges. Surfacing that as a discrete badge column matches
                // the AC's "lineage 触发标记" requirement.
                const lineageTriggers = mappingCount > 0;
                return (
                  <tr
                    key={b.rid}
                    data-testid="bindings-row"
                    data-binding-rid={b.rid}
                    data-binding-dataset-rid={b.datasetRid}
                    data-binding-branch={b.branch}
                    data-binding-is-primary={b.isPrimary ? 'true' : 'false'}
                    data-binding-mapping-count={mappingCount}
                    data-binding-lineage-triggers={lineageTriggers ? 'true' : 'false'}
                    className="border-b last:border-0 hover:bg-bg-tertiary/30"
                    style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                  >
                    <td className="px-3 py-2 font-mono text-text-primary">
                      <div>{b.datasetRid}</div>
                      {b.branch && b.branch !== 'main' && (
                        <div className="text-[10px] text-text-secondary">
                          branch: {b.branch}
                        </div>
                      )}
                    </td>
                    <td className="px-3 py-2 font-mono text-text-secondary">
                      {tableLabel}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">
                      {mappingCount > 0
                        ? `${mappingCount} column${mappingCount === 1 ? '' : 's'}`
                        : '—'}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">
                      {lineageTriggers ? (
                        <span
                          data-testid="bindings-lineage-badge"
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold bg-accent-magenta/20 text-accent-magenta border border-accent-magenta/40"
                        >
                          on
                        </span>
                      ) : (
                        <span className="text-[10px] text-text-secondary">
                          off
                        </span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">
                      {b.isPrimary ? (
                        <span
                          data-testid="bindings-primary-badge"
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40"
                        >
                          primary
                        </span>
                      ) : (
                        <span className="text-[10px] text-text-secondary">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right whitespace-nowrap">
                      <button
                        type="button"
                        data-testid="bindings-edit-btn"
                        data-binding-rid={b.rid}
                        onClick={() => setEditing(b)}
                        className="text-xs text-accent-cyan hover:underline mr-3"
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        data-testid="bindings-delete-btn"
                        data-binding-rid={b.rid}
                        onClick={() => setDeleting(b)}
                        className="text-xs text-accent-error hover:underline"
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {createOpen && (
        <CreateBindingModal
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <EditBindingModal
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          binding={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteBindingModal
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          binding={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

const inputClass =
  'w-full bg-bg-tertiary border border-border-subtle rounded px-2 py-1.5 text-xs text-text-primary placeholder:text-text-secondary focus:outline-none focus:ring-1 focus:ring-accent-cyan/60';

function FormField({
  label,
  required,
  children,
}: {
  label: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-[10px] uppercase tracking-widest text-text-secondary">
        {label}
        {required && <span className="text-accent-error">*</span>}
      </span>
      {children}
    </label>
  );
}

interface FormState {
  datasetRid: string;
  branch: string;
  isPrimary: boolean;
  rows: MappingRow[];
}

function MappingEditor({
  rows,
  setRows,
}: {
  rows: MappingRow[];
  setRows: (next: MappingRow[]) => void;
}) {
  return (
    <div className="flex flex-col gap-2" data-testid="bindings-mapping-editor">
      {rows.length === 0 && (
        <p className="text-[11px] text-text-secondary">
          No column mappings yet. Add one below to wire properties to
          upstream columns.
        </p>
      )}
      {rows.map((r, i) => (
        <div
          key={i}
          data-testid="bindings-mapping-row"
          data-mapping-index={i}
          className="grid grid-cols-[1fr_1fr_auto] gap-2 items-center"
        >
          <input
            type="text"
            data-testid="bindings-mapping-property"
            data-mapping-index={i}
            value={r.property}
            placeholder="property apiName"
            onChange={(e) => {
              const next = [...rows];
              next[i] = { ...next[i], property: e.target.value };
              setRows(next);
            }}
            className={inputClass}
          />
          <input
            type="text"
            data-testid="bindings-mapping-column"
            data-mapping-index={i}
            value={r.column}
            placeholder="source column"
            onChange={(e) => {
              const next = [...rows];
              next[i] = { ...next[i], column: e.target.value };
              setRows(next);
            }}
            className={inputClass}
          />
          <button
            type="button"
            data-testid="bindings-mapping-remove"
            data-mapping-index={i}
            onClick={() => setRows(rows.filter((_, j) => j !== i))}
            className="px-2 py-1.5 text-[10px] rounded text-accent-error hover:bg-accent-error/10"
          >
            Remove
          </button>
        </div>
      ))}
      <div>
        <button
          type="button"
          data-testid="bindings-mapping-add"
          onClick={() => setRows([...rows, { property: '', column: '' }])}
          className="text-xs text-accent-cyan hover:underline"
        >
          + Add column
        </button>
      </div>
    </div>
  );
}

function CreateBindingModal({
  ontologyApiName,
  objectType,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  onClose: () => void;
}) {
  const create = useCreateDatasourceBinding(ontologyApiName, objectType.rid);
  const [form, setForm] = useState<FormState>({
    datasetRid: '',
    branch: 'main',
    isPrimary: false,
    rows: [{ property: '', column: '' }],
  });
  const [submitError, setSubmitError] = useState<string | null>(null);
  const canSubmit = form.datasetRid.trim().length > 0 && !create.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    try {
      await create.mutateAsync({
        datasetRid: form.datasetRid.trim(),
        branch: form.branch.trim() || undefined,
        columnMapping: rowsToMapping(form.rows),
        isPrimary: form.isPrimary,
      });
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Add Datasource Binding" size="lg">
      <form
        data-testid="bindings-create-form"
        onSubmit={onSubmit}
        className="flex flex-col gap-3"
      >
        <FormField label="Dataset RID" required>
          <input
            type="text"
            data-testid="bindings-create-dataset-rid"
            value={form.datasetRid}
            onChange={(e) =>
              setForm((f) => ({ ...f, datasetRid: e.target.value }))
            }
            placeholder="ri.dataset.main.dataset.northwind-customers"
            required
            className={inputClass}
          />
        </FormField>
        <FormField label="Branch">
          <input
            type="text"
            data-testid="bindings-create-branch"
            value={form.branch}
            onChange={(e) =>
              setForm((f) => ({ ...f, branch: e.target.value }))
            }
            placeholder="main"
            className={inputClass}
          />
        </FormField>
        <label className="flex items-center gap-2 text-xs text-text-primary">
          <input
            type="checkbox"
            data-testid="bindings-create-is-primary"
            checked={form.isPrimary}
            onChange={(e) =>
              setForm((f) => ({ ...f, isPrimary: e.target.checked }))
            }
          />
          Mark as primary binding
        </label>
        <div>
          <p className="text-[10px] uppercase tracking-widest text-text-secondary mb-1">
            Column Mapping
          </p>
          <MappingEditor
            rows={form.rows}
            setRows={(next) => setForm((f) => ({ ...f, rows: next }))}
          />
        </div>
        {submitError && (
          <p
            data-testid="bindings-create-error"
            role="alert"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="bindings-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="bindings-create-submit"
            disabled={!canSubmit}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function EditBindingModal({
  ontologyApiName,
  objectType,
  binding,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  binding: DatasourceBinding;
  onClose: () => void;
}) {
  const update = useUpdateDatasourceBinding(ontologyApiName, objectType.rid);
  const [form, setForm] = useState<FormState>(() => ({
    datasetRid: binding.datasetRid,
    branch: binding.branch || 'main',
    isPrimary: binding.isPrimary,
    rows: mappingToRows(binding.columnMapping),
  }));
  const [submitError, setSubmitError] = useState<string | null>(null);
  // Reset form when a different binding row opens the modal.
  useEffect(() => {
    setForm({
      datasetRid: binding.datasetRid,
      branch: binding.branch || 'main',
      isPrimary: binding.isPrimary,
      rows: mappingToRows(binding.columnMapping),
    });
  }, [binding]);

  const canSubmit = form.datasetRid.trim().length > 0 && !update.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    try {
      await update.mutateAsync({
        rid: binding.rid,
        body: {
          datasetRid: form.datasetRid.trim(),
          branch: form.branch.trim() || undefined,
          columnMapping: rowsToMapping(form.rows),
          isPrimary: form.isPrimary,
        },
      });
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Edit Datasource Binding" size="lg">
      <form
        data-testid="bindings-edit-form"
        data-binding-rid={binding.rid}
        onSubmit={onSubmit}
        className="flex flex-col gap-3"
      >
        <div
          className="text-xs text-text-secondary font-mono"
          data-testid="bindings-edit-rid"
        >
          {binding.rid}
        </div>
        <FormField label="Dataset RID" required>
          <input
            type="text"
            data-testid="bindings-edit-dataset-rid"
            value={form.datasetRid}
            onChange={(e) =>
              setForm((f) => ({ ...f, datasetRid: e.target.value }))
            }
            required
            className={inputClass}
          />
        </FormField>
        <FormField label="Branch">
          <input
            type="text"
            data-testid="bindings-edit-branch"
            value={form.branch}
            onChange={(e) =>
              setForm((f) => ({ ...f, branch: e.target.value }))
            }
            className={inputClass}
          />
        </FormField>
        <label className="flex items-center gap-2 text-xs text-text-primary">
          <input
            type="checkbox"
            data-testid="bindings-edit-is-primary"
            checked={form.isPrimary}
            onChange={(e) =>
              setForm((f) => ({ ...f, isPrimary: e.target.checked }))
            }
          />
          Mark as primary binding
        </label>
        <div>
          <p className="text-[10px] uppercase tracking-widest text-text-secondary mb-1">
            Column Mapping
          </p>
          <MappingEditor
            rows={form.rows}
            setRows={(next) => setForm((f) => ({ ...f, rows: next }))}
          />
        </div>
        {submitError && (
          <p
            data-testid="bindings-edit-error"
            role="alert"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="bindings-edit-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="bindings-edit-submit"
            disabled={!canSubmit}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {update.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteBindingModal({
  ontologyApiName,
  objectType,
  binding,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  binding: DatasourceBinding;
  onClose: () => void;
}) {
  const remove = useDeleteDatasourceBinding(ontologyApiName, objectType.rid);
  const [error, setError] = useState<string | null>(null);

  async function onConfirm() {
    setError(null);
    try {
      await remove.mutateAsync(binding.rid);
      onClose();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Binding" size="md">
      <div
        data-testid="bindings-delete-modal"
        data-binding-rid={binding.rid}
        className="flex flex-col gap-3"
      >
        <p className="text-xs text-text-primary">
          Delete the binding for dataset{' '}
          <span className="font-mono">{binding.datasetRid}</span>?
        </p>
        <p className="text-[11px] text-text-secondary">
          Column-level lineage edges derived from this binding will be
          removed. The upstream dataset itself is not affected.
        </p>
        {error && (
          <p
            data-testid="bindings-delete-error"
            role="alert"
            className="text-xs text-accent-error"
          >
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="bindings-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="bindings-delete-submit"
            onClick={onConfirm}
            disabled={remove.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {remove.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
