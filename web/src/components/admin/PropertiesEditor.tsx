import { useMemo, useState } from 'react';
import type { ObjectType, Property } from '../../api/types';
import type {
  CreatePropertyRequest,
  UpdatePropertyRequest,
} from '../../api/ontologies';
import {
  useCreateProperty,
  useDeleteProperty,
  useProperties,
  useUpdateProperty,
} from '../../hooks/useProperties';
import { useUpdateObjectType } from '../../hooks/useObjectTypes';
import { LoadingSpinner } from '../common/LoadingSpinner';

export const BASE_TYPES = [
  'string',
  'integer',
  'short',
  'long',
  'float',
  'double',
  'boolean',
  'byte',
  'date',
  'timestamp',
  'decimal',
  'struct',
  'vector',
  'geopoint',
  'geoshape',
  'attachment',
  'timeseries',
  'mediaReference',
  'marking',
  'cipher',
] as const;

type BaseType = (typeof BASE_TYPES)[number];

interface StructField {
  name: string;
  type: BaseType;
}

export interface StructTypeConfig {
  fields: StructField[];
}

function parseStructConfig(tc: unknown): StructField[] {
  if (!tc || typeof tc !== 'object') return [];
  const obj = tc as Record<string, unknown>;
  const fields = obj.fields;
  if (!Array.isArray(fields)) return [];
  return fields
    .map((f) => {
      if (!f || typeof f !== 'object') return null;
      const rec = f as Record<string, unknown>;
      const name = typeof rec.name === 'string' ? rec.name : '';
      const type =
        typeof rec.type === 'string' &&
        (BASE_TYPES as readonly string[]).includes(rec.type)
          ? (rec.type as BaseType)
          : 'string';
      if (!name) return null;
      return { name, type };
    })
    .filter((f): f is StructField => f !== null);
}

export function PropertiesEditor({
  ontologyApiName,
  objectType,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
}) {
  const {
    data: properties,
    isLoading,
    error,
  } = useProperties(ontologyApiName, objectType.rid);
  const updateOT = useUpdateObjectType(ontologyApiName);
  const [addOpen, setAddOpen] = useState(false);
  const [editing, setEditing] = useState<Property | null>(null);
  const [deleting, setDeleting] = useState<Property | null>(null);
  const [otError, setOtError] = useState<string | null>(null);

  const sorted = useMemo(() => {
    if (!properties) return [] as Property[];
    return [...properties].sort((a, b) =>
      a.apiName.localeCompare(b.apiName),
    );
  }, [properties]);

  async function setTitleProperty(apiName: string) {
    setOtError(null);
    try {
      await updateOT.mutateAsync({
        rid: objectType.rid,
        body: {
          displayName: objectType.displayName,
          pluralDisplayName: objectType.pluralDisplayName ?? undefined,
          description: objectType.description ?? undefined,
          titleProperty: apiName,
          status: objectType.status,
          visibility: objectType.visibility,
          iconName: objectType.icon ?? undefined,
          color: objectType.color ?? undefined,
        },
      });
    } catch (err) {
      setOtError((err as Error).message);
    }
  }

  return (
    <div className="flex flex-col gap-3" data-testid="properties-editor">
      <div className="flex items-center justify-between">
        <p className="text-[11px] uppercase tracking-widest text-text-secondary">
          {sorted.length} {sorted.length === 1 ? 'property' : 'properties'}
        </p>
        <button
          type="button"
          onClick={() => setAddOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30"
        >
          + Add Property
        </button>
      </div>

      {otError && (
        <p role="alert" className="text-xs text-accent-error">
          {otError}
        </p>
      )}

      {isLoading && (
        <div className="flex items-center justify-center py-6">
          <LoadingSpinner size="md" />
        </div>
      )}
      {!isLoading && error && (
        <p className="text-xs text-accent-error">
          Failed to load properties: {(error as Error).message}
        </p>
      )}
      {!isLoading && !error && sorted.length === 0 && !addOpen && (
        <div
          className="rounded border px-4 py-6 text-center text-xs text-text-secondary"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          No properties yet. Click “Add Property” to create one.
        </div>
      )}

      {!isLoading && !error && sorted.length > 0 && (
        <div
          className="rounded border overflow-hidden"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <table className="w-full text-xs">
            <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
              <tr
                className="border-b"
                style={{ borderColor: 'rgba(31,41,55,0.5)' }}
              >
                <th className="text-left px-3 py-2 font-medium">PK</th>
                <th className="text-left px-3 py-2 font-medium">Title</th>
                <th className="text-left px-3 py-2 font-medium">API Name</th>
                <th className="text-left px-3 py-2 font-medium">
                  Display Name
                </th>
                <th className="text-left px-3 py-2 font-medium">Type</th>
                <th className="text-left px-3 py-2 font-medium">Flags</th>
                <th className="text-right px-3 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((p) => {
                const isPK = p.apiName === objectType.primaryKey;
                const isTitle = p.apiName === objectType.titleProperty;
                return (
                  <tr
                    key={p.rid}
                    className="border-b last:border-0 hover:bg-bg-tertiary/30"
                    style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                  >
                    <td className="px-3 py-2">
                      <input
                        type="radio"
                        name="property-primaryKey"
                        aria-label={`primary key ${p.apiName}`}
                        checked={isPK}
                        readOnly
                        disabled
                        title="Primary key is set at object type creation"
                      />
                    </td>
                    <td className="px-3 py-2">
                      <input
                        type="radio"
                        name="property-titleProperty"
                        aria-label={`title property ${p.apiName}`}
                        checked={isTitle}
                        onChange={() => setTitleProperty(p.apiName)}
                        disabled={updateOT.isPending}
                      />
                    </td>
                    <td className="px-3 py-2 font-mono text-text-primary">
                      {p.apiName}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">
                      {p.displayName ?? ''}
                    </td>
                    <td className="px-3 py-2 font-mono text-text-secondary">
                      {p.baseType}
                      {p.isArray ? '[]' : ''}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">
                      <PropertyFlags p={p} />
                    </td>
                    <td className="px-3 py-2 text-right whitespace-nowrap">
                      <button
                        type="button"
                        onClick={() => setEditing(p)}
                        className="text-xs text-accent-cyan hover:underline mr-3"
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeleting(p)}
                        disabled={isPK}
                        title={
                          isPK ? 'Primary key property cannot be deleted' : undefined
                        }
                        className="text-xs text-accent-error hover:underline disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:no-underline"
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

      {addOpen && (
        <AddPropertyForm
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          existing={sorted}
          onClose={() => setAddOpen(false)}
        />
      )}
      {editing && (
        <EditPropertyForm
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          property={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeletePropertyForm
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          property={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function PropertyFlags({ p }: { p: Property }) {
  const flags: string[] = [];
  if (p.isNullable) flags.push('nullable');
  if (p.isSearchable) flags.push('searchable');
  if (p.isSortable) flags.push('sortable');
  if (p.editOnly) flags.push('editOnly');
  if (flags.length === 0) return <span>—</span>;
  return <span>{flags.join(', ')}</span>;
}

interface AddFormState {
  apiName: string;
  displayName: string;
  description: string;
  baseType: BaseType;
  isArray: boolean;
  isNullable: boolean;
  isSearchable: boolean;
  isSortable: boolean;
  structFields: StructField[];
}

function AddPropertyForm({
  ontologyApiName,
  objectType,
  existing,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  existing: Property[];
  onClose: () => void;
}) {
  const create = useCreateProperty(ontologyApiName, objectType.rid);
  const [form, setForm] = useState<AddFormState>({
    apiName: '',
    displayName: '',
    description: '',
    baseType: 'string',
    isArray: false,
    isNullable: true,
    isSearchable: false,
    isSortable: false,
    structFields: [],
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  const taken = useMemo(
    () => new Set(existing.map((p) => p.apiName)),
    [existing],
  );
  const duplicate = !!form.apiName && taken.has(form.apiName);
  const structInvalid =
    form.baseType === 'struct' &&
    (form.structFields.length === 0 ||
      form.structFields.some((f) => !f.name.trim()));
  const canSubmit =
    !!form.apiName.trim() && !duplicate && !structInvalid && !create.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const body: CreatePropertyRequest = {
      apiName: form.apiName.trim(),
      displayName: form.displayName.trim() || undefined,
      description: form.description.trim() || undefined,
      baseType: form.baseType,
      isArray: form.isArray,
      isNullable: form.isNullable,
      isSearchable: form.isSearchable,
      isSortable: form.isSortable,
    };
    if (form.baseType === 'struct') {
      body.typeConfig = { fields: form.structFields };
    }
    try {
      await create.mutateAsync(body);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded border p-4"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
      data-testid="add-property-form"
    >
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-text-primary">
          Add property
        </h3>
        <button
          type="button"
          onClick={onClose}
          className="text-xs text-text-secondary hover:text-text-primary"
        >
          Cancel
        </button>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <LabeledField
          label="API Name"
          required
          error={duplicate ? 'apiName already used on this object type' : undefined}
        >
          <input
            type="text"
            value={form.apiName}
            onChange={(e) =>
              setForm((f) => ({ ...f, apiName: e.target.value }))
            }
            required
            className={inputClass + ' font-mono'}
          />
        </LabeledField>
        <LabeledField label="Display Name">
          <input
            type="text"
            value={form.displayName}
            onChange={(e) =>
              setForm((f) => ({ ...f, displayName: e.target.value }))
            }
            className={inputClass}
          />
        </LabeledField>
      </div>
      <LabeledField label="Description">
        <input
          type="text"
          value={form.description}
          onChange={(e) =>
            setForm((f) => ({ ...f, description: e.target.value }))
          }
          className={inputClass}
        />
      </LabeledField>
      <div className="grid grid-cols-2 gap-3">
        <LabeledField label="Base Type" required>
          <select
            value={form.baseType}
            aria-label="Base Type"
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                baseType: e.target.value as BaseType,
                structFields:
                  e.target.value === 'struct' ? f.structFields : [],
              }))
            }
            className={inputClass}
          >
            {BASE_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </LabeledField>
        <div className="flex flex-col gap-2 justify-end">
          <Toggle
            label="Array"
            checked={form.isArray}
            onChange={(v) => setForm((f) => ({ ...f, isArray: v }))}
          />
          <Toggle
            label="Nullable"
            checked={form.isNullable}
            onChange={(v) => setForm((f) => ({ ...f, isNullable: v }))}
          />
          <Toggle
            label="Searchable"
            checked={form.isSearchable}
            onChange={(v) => setForm((f) => ({ ...f, isSearchable: v }))}
          />
          <Toggle
            label="Sortable"
            checked={form.isSortable}
            onChange={(v) => setForm((f) => ({ ...f, isSortable: v }))}
          />
        </div>
      </div>
      {form.baseType === 'struct' && (
        <StructFieldsEditor
          fields={form.structFields}
          onChange={(fields) => setForm((f) => ({ ...f, structFields: fields }))}
        />
      )}
      {submitError && (
        <p role="alert" className="text-xs text-accent-error">
          {submitError}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button
          type="submit"
          disabled={!canSubmit}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {create.isPending ? 'Creating…' : 'Create property'}
        </button>
      </div>
    </form>
  );
}

interface EditFormState {
  displayName: string;
  description: string;
  isNullable: boolean;
  isSearchable: boolean;
  isSortable: boolean;
  editOnly: boolean;
  status: string;
  deprecatedReason: string;
}

function EditPropertyForm({
  ontologyApiName,
  objectType,
  property,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  property: Property;
  onClose: () => void;
}) {
  const update = useUpdateProperty(ontologyApiName, objectType.rid);
  const [form, setForm] = useState<EditFormState>({
    displayName: property.displayName ?? '',
    description: property.description ?? '',
    isNullable: property.isNullable,
    isSearchable: property.isSearchable,
    isSortable: property.isSortable,
    editOnly: property.editOnly ?? false,
    status: property.status ?? '',
    deprecatedReason: property.deprecatedReason ?? '',
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const body: UpdatePropertyRequest = {
      displayName: form.displayName.trim() || undefined,
      description: form.description.trim() || undefined,
      isNullable: form.isNullable,
      isSearchable: form.isSearchable,
      isSortable: form.isSortable,
      editOnly: form.editOnly,
      status: form.status.trim() || undefined,
      deprecatedReason: form.deprecatedReason.trim() || undefined,
    };
    try {
      await update.mutateAsync({ rid: property.rid, body });
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <form
      onSubmit={onSubmit}
      className="flex flex-col gap-3 rounded border p-4"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
      data-testid="edit-property-form"
    >
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-text-primary">
          Edit: {property.apiName}
          <span className="ml-2 text-[10px] font-mono text-text-secondary">
            {property.baseType}
            {property.isArray ? '[]' : ''}
          </span>
        </h3>
        <button
          type="button"
          onClick={onClose}
          className="text-xs text-text-secondary hover:text-text-primary"
        >
          Cancel
        </button>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <LabeledField label="Display Name">
          <input
            type="text"
            value={form.displayName}
            onChange={(e) =>
              setForm((f) => ({ ...f, displayName: e.target.value }))
            }
            className={inputClass}
          />
        </LabeledField>
        <LabeledField label="Status">
          <input
            type="text"
            value={form.status}
            onChange={(e) =>
              setForm((f) => ({ ...f, status: e.target.value }))
            }
            className={inputClass + ' font-mono'}
          />
        </LabeledField>
      </div>
      <LabeledField label="Description">
        <input
          type="text"
          value={form.description}
          onChange={(e) =>
            setForm((f) => ({ ...f, description: e.target.value }))
          }
          className={inputClass}
        />
      </LabeledField>
      <div className="flex flex-col gap-2">
        <Toggle
          label="Nullable"
          checked={form.isNullable}
          onChange={(v) => setForm((f) => ({ ...f, isNullable: v }))}
        />
        <Toggle
          label="Searchable"
          checked={form.isSearchable}
          onChange={(v) => setForm((f) => ({ ...f, isSearchable: v }))}
        />
        <Toggle
          label="Sortable"
          checked={form.isSortable}
          onChange={(v) => setForm((f) => ({ ...f, isSortable: v }))}
        />
        <Toggle
          label="Edit-only"
          checked={form.editOnly}
          onChange={(v) => setForm((f) => ({ ...f, editOnly: v }))}
        />
      </div>
      {submitError && (
        <p role="alert" className="text-xs text-accent-error">
          {submitError}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button
          type="submit"
          disabled={update.isPending}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {update.isPending ? 'Saving…' : 'Save property'}
        </button>
      </div>
    </form>
  );
}

function DeletePropertyForm({
  ontologyApiName,
  objectType,
  property,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  property: Property;
  onClose: () => void;
}) {
  const del = useDeleteProperty(ontologyApiName, objectType.rid);
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync(property.rid);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <div
      className="flex flex-col gap-3 rounded border p-4"
      style={{
        borderColor: 'rgba(239,68,68,0.4)',
        background: 'rgba(239,68,68,0.06)',
      }}
      data-testid="delete-property-confirm"
    >
      <p className="text-sm text-text-primary">
        Delete property{' '}
        <span className="font-mono font-semibold">{property.apiName}</span>{' '}
        from{' '}
        <span className="font-semibold">{objectType.displayName}</span>?
      </p>
      <p className="text-xs text-text-secondary">
        Existing objects will keep the stored value, but new edits to this
        property will be rejected. This cannot be undone outside a branch.
      </p>
      {submitError && (
        <p role="alert" className="text-xs text-accent-error">
          {submitError}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onClose}
          className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={onConfirm}
          disabled={del.isPending}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {del.isPending ? 'Deleting…' : 'Delete property'}
        </button>
      </div>
    </div>
  );
}

function StructFieldsEditor({
  fields,
  onChange,
}: {
  fields: StructField[];
  onChange: (fields: StructField[]) => void;
}) {
  function setField(idx: number, next: StructField) {
    const copy = fields.slice();
    copy[idx] = next;
    onChange(copy);
  }
  function removeField(idx: number) {
    const copy = fields.slice();
    copy.splice(idx, 1);
    onChange(copy);
  }
  function addField() {
    onChange([...fields, { name: '', type: 'string' }]);
  }
  return (
    <div className="flex flex-col gap-2" data-testid="struct-fields">
      <div className="flex items-center justify-between">
        <span className="text-[10px] uppercase tracking-widest text-text-secondary">
          Struct fields
        </span>
        <button
          type="button"
          onClick={addField}
          className="text-xs text-accent-cyan hover:underline"
        >
          + Add field
        </button>
      </div>
      {fields.length === 0 && (
        <p className="text-[11px] text-text-muted">
          Struct type requires at least one field.
        </p>
      )}
      {fields.map((f, idx) => (
        <div key={idx} className="grid grid-cols-[1fr_10rem_auto] gap-2">
          <input
            type="text"
            aria-label={`struct field name ${idx}`}
            placeholder="field name"
            value={f.name}
            onChange={(e) =>
              setField(idx, { ...f, name: e.target.value })
            }
            className={inputClass + ' font-mono'}
          />
          <select
            aria-label={`struct field type ${idx}`}
            value={f.type}
            onChange={(e) =>
              setField(idx, {
                ...f,
                type: e.target.value as BaseType,
              })
            }
            className={inputClass}
          >
            {BASE_TYPES.filter((t) => t !== 'struct').map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => removeField(idx)}
            aria-label={`remove struct field ${idx}`}
            className="px-2 text-xs text-accent-error hover:underline"
          >
            ✕
          </button>
        </div>
      ))}
    </div>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-xs text-text-secondary">
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span>{label}</span>
    </label>
  );
}

function LabeledField({
  label,
  required,
  error,
  children,
}: {
  label: string;
  required?: boolean;
  error?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-[11px] uppercase tracking-widest text-text-secondary">
      <span>
        {label}
        {required && <span className="text-accent-error"> *</span>}
      </span>
      {children}
      {error && (
        <span className="text-[11px] text-accent-error normal-case tracking-normal" role="alert">
          {error}
        </span>
      )}
    </label>
  );
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 normal-case tracking-normal';

export { parseStructConfig };
