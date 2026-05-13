import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type { ValueType } from '../../api/types';
import type {
  CreateValueTypeRequest,
  UpdateValueTypeRequest,
} from '../../api/ontologies';
import {
  useValueTypesAdmin,
  useCreateValueType,
  useUpdateValueType,
  useDeleteValueType,
  useValueTypeUsages,
} from '../../hooks/useValueTypes';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const BASE_TYPES = [
  'string',
  'integer',
  'long',
  'double',
  'boolean',
  'date',
  'timestamp',
] as const;

type ConstraintKind = 'none' | 'pattern' | 'range' | 'enum';

// classifyConstraint inspects a constraints object and reports which of the
// three editor modes it represents. The data attr on each row uses this
// label so BDD scenarios can lock contract without parsing JSON.
function classifyConstraint(c: unknown): ConstraintKind {
  if (!c || typeof c !== 'object') return 'none';
  const obj = c as Record<string, unknown>;
  if (typeof obj.pattern === 'string') return 'pattern';
  if (Array.isArray(obj.enum)) return 'enum';
  if (obj.min !== undefined || obj.max !== undefined) return 'range';
  return 'none';
}

interface ConstraintsObject {
  pattern?: string;
  min?: number;
  max?: number;
  enum?: string[];
}

function constraintToInputs(c: unknown): {
  kind: ConstraintKind;
  pattern: string;
  min: string;
  max: string;
  enumText: string;
} {
  const empty = { kind: 'none' as ConstraintKind, pattern: '', min: '', max: '', enumText: '' };
  if (!c || typeof c !== 'object') return empty;
  const obj = c as ConstraintsObject;
  if (typeof obj.pattern === 'string') {
    return { ...empty, kind: 'pattern', pattern: obj.pattern };
  }
  if (Array.isArray(obj.enum)) {
    return { ...empty, kind: 'enum', enumText: obj.enum.join(', ') };
  }
  if (obj.min !== undefined || obj.max !== undefined) {
    return {
      ...empty,
      kind: 'range',
      min: obj.min !== undefined ? String(obj.min) : '',
      max: obj.max !== undefined ? String(obj.max) : '',
    };
  }
  return empty;
}

// inputsToConstraints serializes the editor state back into the wire shape.
// Only the active kind's keys are emitted so the server doesn't have to
// discriminate between unset and intentionally empty values.
function inputsToConstraints(
  kind: ConstraintKind,
  pattern: string,
  min: string,
  max: string,
  enumText: string,
): Record<string, unknown> | undefined {
  if (kind === 'none') return undefined;
  if (kind === 'pattern') {
    if (!pattern.trim()) return undefined;
    return { pattern: pattern.trim() };
  }
  if (kind === 'range') {
    const out: Record<string, number> = {};
    if (min.trim() !== '') {
      const n = Number(min);
      if (!Number.isNaN(n)) out.min = n;
    }
    if (max.trim() !== '') {
      const n = Number(max);
      if (!Number.isNaN(n)) out.max = n;
    }
    return Object.keys(out).length ? out : undefined;
  }
  // enum
  const values = enumText
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '');
  if (values.length === 0) return undefined;
  return { enum: values };
}

export function ValueTypeAdminPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const {
    data: valueTypes,
    isLoading,
    error,
  } = useValueTypesAdmin(ontologyApiName);

  const [search, setSearch] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<ValueType | null>(null);
  const [deleting, setDeleting] = useState<ValueType | null>(null);
  const [usagesFor, setUsagesFor] = useState<ValueType | null>(null);

  const filtered = useMemo(() => {
    if (!valueTypes) return [];
    const q = search.trim().toLowerCase();
    const list = valueTypes.filter((vt) => {
      if (!q) return true;
      return (
        vt.apiName.toLowerCase().includes(q) ||
        vt.displayName.toLowerCase().includes(q)
      );
    });
    list.sort((a, b) => a.displayName.localeCompare(b.displayName));
    return list;
  }, [valueTypes, search]);

  if (!ontologyApiName) {
    return (
      <div
        data-testid="value-type-admin-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="value-type-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Value Types
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="value-type-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Value Type
        </button>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <input
          type="search"
          data-testid="value-type-search-input"
          aria-label="Search value types"
          placeholder="Search by name or apiName…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-[12rem] max-w-md px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="value-type-admin-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="value-type-admin-error"
            className="text-sm text-accent-error"
          >
            Failed to load value types: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            data-testid="value-type-admin-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No value types yet
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Create a Value Type to define a reusable typed primitive (e.g.
              EmailAddress, Currency) with optional pattern, range, or enum
              constraints.
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <ValueTypeTable
            rows={filtered}
            onEdit={setEditing}
            onDelete={setDeleting}
            onUsages={setUsagesFor}
          />
        )}
      </div>

      {createOpen && (
        <ValueTypeBuilderModal
          ontologyApiName={ontologyApiName}
          existing={valueTypes ?? []}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <ValueTypeBuilderModal
          ontologyApiName={ontologyApiName}
          existing={valueTypes ?? []}
          editing={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteValueTypeModal
          ontologyApiName={ontologyApiName}
          valueType={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
      {usagesFor && (
        <ValueTypeUsagesModal
          ontologyApiName={ontologyApiName}
          valueType={usagesFor}
          onClose={() => setUsagesFor(null)}
        />
      )}
    </div>
  );
}

function ValueTypeTable({
  rows,
  onEdit,
  onDelete,
  onUsages,
}: {
  rows: ValueType[];
  onEdit: (vt: ValueType) => void;
  onDelete: (vt: ValueType) => void;
  onUsages: (vt: ValueType) => void;
}) {
  return (
    <div
      data-testid="value-type-admin-table"
      className="rounded border overflow-hidden"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <table className="w-full text-sm">
        <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
          <tr className="border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
            <th className="text-left px-4 py-2 font-medium">Display Name</th>
            <th className="text-left px-4 py-2 font-medium">API Name</th>
            <th className="text-left px-4 py-2 font-medium">Base Type</th>
            <th className="text-left px-4 py-2 font-medium">Constraint</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((vt) => {
            const constraintKind = classifyConstraint(vt.constraints);
            return (
              <tr
                key={vt.rid}
                data-testid="value-type-row"
                data-value-type-api-name={vt.apiName}
                data-value-type-rid={vt.rid}
                data-value-type-base-type={vt.baseType}
                data-value-type-constraint-kind={constraintKind}
                className="border-b last:border-0 hover:bg-bg-tertiary/30"
                style={{ borderColor: 'rgba(31,41,55,0.5)' }}
              >
                <td className="px-4 py-2 text-text-primary">{vt.displayName}</td>
                <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                  {vt.apiName}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary font-mono">
                  {vt.baseType}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {constraintKind === 'none' ? '—' : constraintKind}
                </td>
                <td className="px-4 py-2 text-right whitespace-nowrap">
                  <button
                    type="button"
                    data-testid="value-type-usages-btn"
                    data-value-type-api-name={vt.apiName}
                    onClick={() => onUsages(vt)}
                    className="text-xs text-accent-cyan hover:underline mr-3"
                  >
                    Used by
                  </button>
                  <button
                    type="button"
                    data-testid="value-type-edit-btn"
                    data-value-type-api-name={vt.apiName}
                    onClick={() => onEdit(vt)}
                    className="text-xs text-accent-cyan hover:underline mr-3"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    data-testid="value-type-delete-btn"
                    data-value-type-api-name={vt.apiName}
                    onClick={() => onDelete(vt)}
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
  );
}

interface BuilderState {
  apiName: string;
  apiNameDirty: boolean;
  displayName: string;
  baseType: string;
  constraintKind: ConstraintKind;
  pattern: string;
  min: string;
  max: string;
  enumText: string;
}

function initialBuilderState(vt?: ValueType): BuilderState {
  if (!vt) {
    return {
      apiName: '',
      apiNameDirty: false,
      displayName: '',
      baseType: 'string',
      constraintKind: 'none',
      pattern: '',
      min: '',
      max: '',
      enumText: '',
    };
  }
  const inputs = constraintToInputs(vt.constraints);
  return {
    apiName: vt.apiName,
    apiNameDirty: true,
    displayName: vt.displayName,
    baseType: vt.baseType,
    constraintKind: inputs.kind,
    pattern: inputs.pattern,
    min: inputs.min,
    max: inputs.max,
    enumText: inputs.enumText,
  };
}

function ValueTypeBuilderModal({
  ontologyApiName,
  existing,
  editing,
  onClose,
}: {
  ontologyApiName: string;
  existing: ValueType[];
  editing?: ValueType;
  onClose: () => void;
}) {
  const isEdit = !!editing;
  const create = useCreateValueType(ontologyApiName);
  const update = useUpdateValueType(ontologyApiName);
  const [form, setForm] = useState<BuilderState>(() =>
    initialBuilderState(editing),
  );
  const [submitError, setSubmitError] = useState<string | null>(null);

  const apiNameTaken = useMemo(
    () =>
      new Set(
        existing
          .filter((vt) => !editing || vt.rid !== editing.rid)
          .map((vt) => vt.apiName),
      ),
    [existing, editing],
  );

  function updateDisplayName(next: string) {
    setForm((f) => ({
      ...f,
      displayName: next,
      apiName: f.apiNameDirty ? f.apiName : autoApiName(next),
    }));
  }

  const duplicateApiName =
    !isEdit && !!form.apiName && apiNameTaken.has(form.apiName);

  const canSubmit =
    !!form.apiName.trim() &&
    !!form.displayName.trim() &&
    !!form.baseType.trim() &&
    !duplicateApiName &&
    !create.isPending &&
    !update.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const constraints = inputsToConstraints(
      form.constraintKind,
      form.pattern,
      form.min,
      form.max,
      form.enumText,
    );
    try {
      if (editing) {
        const body: UpdateValueTypeRequest = {
          displayName: form.displayName.trim(),
          baseType: form.baseType.trim(),
          constraints,
        };
        await update.mutateAsync({ rid: editing.rid, body });
      } else {
        const body: CreateValueTypeRequest = {
          apiName: form.apiName.trim(),
          displayName: form.displayName.trim(),
          baseType: form.baseType.trim(),
          constraints,
        };
        await create.mutateAsync(body);
      }
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  const modePrefix = isEdit ? 'value-type-edit' : 'value-type-create';
  return (
    <Modal
      open
      onClose={onClose}
      title={isEdit ? `Edit: ${editing.displayName}` : 'New Value Type'}
      size="lg"
    >
      <form
        onSubmit={onSubmit}
        data-testid={`${modePrefix}-form`}
        className="flex flex-col gap-4"
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Display Name" required>
            <input
              type="text"
              data-testid="value-type-display-name"
              value={form.displayName}
              onChange={(e) => updateDisplayName(e.target.value)}
              required
              autoFocus
              className={inputClass}
            />
          </Field>
          <Field
            label="API Name"
            required
            error={
              duplicateApiName
                ? `A ValueType with apiName "${form.apiName}" already exists.`
                : undefined
            }
          >
            <input
              type="text"
              data-testid="value-type-api-name"
              value={form.apiName}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  apiName: e.target.value,
                  apiNameDirty: true,
                }))
              }
              disabled={isEdit}
              required
              className={inputClass + ' font-mono'}
            />
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Base Type" required>
            <select
              data-testid="value-type-base-type"
              value={form.baseType}
              onChange={(e) =>
                setForm((f) => ({ ...f, baseType: e.target.value }))
              }
              className={inputClass}
            >
              {BASE_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Constraint">
            <select
              data-testid="value-type-constraint-kind"
              value={form.constraintKind}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  constraintKind: e.target.value as ConstraintKind,
                }))
              }
              className={inputClass}
            >
              <option value="none">None</option>
              <option value="pattern">Pattern (regex)</option>
              <option value="range">Range (min / max)</option>
              <option value="enum">Enum (comma separated)</option>
            </select>
          </Field>
        </div>

        {form.constraintKind === 'pattern' && (
          <Field
            label="Pattern"
            hint="JavaScript / Go regular expression — values that don't match are rejected."
          >
            <input
              type="text"
              data-testid="value-type-constraint-pattern"
              value={form.pattern}
              onChange={(e) =>
                setForm((f) => ({ ...f, pattern: e.target.value }))
              }
              className={inputClass + ' font-mono'}
              placeholder="^[^@]+@[^@]+\\.[^@]+$"
            />
          </Field>
        )}

        {form.constraintKind === 'range' && (
          <div className="grid grid-cols-2 gap-3">
            <Field label="Min">
              <input
                type="number"
                data-testid="value-type-constraint-min"
                value={form.min}
                onChange={(e) =>
                  setForm((f) => ({ ...f, min: e.target.value }))
                }
                className={inputClass}
              />
            </Field>
            <Field label="Max">
              <input
                type="number"
                data-testid="value-type-constraint-max"
                value={form.max}
                onChange={(e) =>
                  setForm((f) => ({ ...f, max: e.target.value }))
                }
                className={inputClass}
              />
            </Field>
          </div>
        )}

        {form.constraintKind === 'enum' && (
          <Field
            label="Allowed Values"
            hint="Comma-separated list. Values are trimmed; empties are dropped."
          >
            <input
              type="text"
              data-testid="value-type-constraint-enum"
              value={form.enumText}
              onChange={(e) =>
                setForm((f) => ({ ...f, enumText: e.target.value }))
              }
              className={inputClass}
              placeholder="active, pending, archived"
            />
          </Field>
        )}

        {form.constraintKind !== 'none' && (
          <pre
            data-testid="value-type-constraint-preview"
            className="text-[11px] font-mono text-text-secondary bg-bg-tertiary/40 rounded p-2 overflow-x-auto"
          >
            {JSON.stringify(
              inputsToConstraints(
                form.constraintKind,
                form.pattern,
                form.min,
                form.max,
                form.enumText,
              ) ?? {},
              null,
              2,
            )}
          </pre>
        )}

        {submitError && (
          <p
            role="alert"
            data-testid={`${modePrefix}-error`}
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid={`${modePrefix}-cancel`}
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid={`${modePrefix}-submit`}
            disabled={!canSubmit}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {create.isPending || update.isPending
              ? isEdit
                ? 'Saving…'
                : 'Creating…'
              : isEdit
                ? 'Save changes'
                : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteValueTypeModal({
  ontologyApiName,
  valueType,
  onClose,
}: {
  ontologyApiName: string;
  valueType: ValueType;
  onClose: () => void;
}) {
  const del = useDeleteValueType(ontologyApiName);
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync(valueType.rid);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Value Type">
      <div
        data-testid="value-type-delete-modal"
        data-value-type-api-name={valueType.apiName}
        data-value-type-rid={valueType.rid}
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{valueType.displayName}</span>{' '}
          <span className="text-xs text-text-secondary font-mono">
            ({valueType.apiName})
          </span>
          ?
        </p>
        <p className="text-xs text-text-secondary">
          Properties whose <code>base_type</code> references this ValueType
          will keep the apiName string but lose its constraint metadata. Use
          "Used by" to inspect references before deleting.
        </p>
        {submitError && (
          <p
            role="alert"
            data-testid="value-type-delete-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="value-type-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="value-type-delete-confirm"
            onClick={onConfirm}
            disabled={del.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {del.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function ValueTypeUsagesModal({
  ontologyApiName,
  valueType,
  onClose,
}: {
  ontologyApiName: string;
  valueType: ValueType;
  onClose: () => void;
}) {
  const { data: usages, isLoading, error } = useValueTypeUsages(
    ontologyApiName,
    valueType.rid,
  );

  return (
    <Modal open onClose={onClose} title={`Used by — ${valueType.displayName}`} size="lg">
      <div
        data-testid="value-type-usages-modal"
        data-value-type-api-name={valueType.apiName}
        data-value-type-rid={valueType.rid}
        className="flex flex-col gap-4"
      >
        {isLoading && (
          <div className="flex items-center justify-center py-8">
            <LoadingSpinner size="md" />
          </div>
        )}
        {!isLoading && error && (
          <p className="text-xs text-accent-error" role="alert">
            Failed to load usages: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && (!usages || usages.length === 0) && (
          <p
            data-testid="value-type-usages-empty"
            className="text-xs text-text-muted italic"
          >
            No Property currently references this ValueType.
          </p>
        )}
        {!isLoading && !error && usages && usages.length > 0 && (
          <ul
            data-testid="value-type-usages-list"
            className="flex flex-col gap-1"
          >
            {usages.map((u) => (
              <li
                key={`${u.objectTypeRid}|${u.propertyRid}`}
                data-testid="value-type-usage-row"
                data-object-type-api-name={u.objectTypeApiName}
                data-object-type-rid={u.objectTypeRid}
                data-property-api-name={u.propertyApiName}
                data-property-rid={u.propertyRid}
                className="flex items-center gap-3 px-3 py-2 rounded border"
                style={{
                  borderColor: 'rgba(31,41,55,0.5)',
                  background: 'rgba(13,17,23,0.4)',
                }}
              >
                <span className="text-sm text-text-primary">
                  {u.objectTypeApiName}
                  <span className="text-text-secondary">.</span>
                  <span className="font-mono">{u.propertyApiName}</span>
                </span>
              </li>
            ))}
          </ul>
        )}
        <div className="flex justify-end">
          <button
            type="button"
            data-testid="value-type-usages-close"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Done
          </button>
        </div>
      </div>
    </Modal>
  );
}

function autoApiName(displayName: string): string {
  const words = displayName.trim().split(/\s+/);
  if (words.length === 0 || words[0] === '') return '';
  const first = words[0].toLowerCase();
  const rest = words
    .slice(1)
    .map((w) => (w ? w[0].toUpperCase() + w.slice(1).toLowerCase() : ''));
  return [first, ...rest].join('');
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-60';

function Field({
  label,
  required,
  hint,
  error,
  children,
}: {
  label: string;
  required?: boolean;
  hint?: string;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary">
      <span className="uppercase tracking-widest">
        {label}
        {required && <span className="text-accent-error"> *</span>}
      </span>
      {children}
      {hint && !error && (
        <span className="text-[11px] text-text-muted">{hint}</span>
      )}
      {error && (
        <span className="text-[11px] text-accent-error" role="alert">
          {error}
        </span>
      )}
    </label>
  );
}
