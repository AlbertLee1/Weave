import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import type { ObjectType, LinkType, ActionType } from '../../api/types';
import {
  listOutgoingLinkTypes,
  listActionTypes,
  type CreateObjectTypeRequest,
  type UpdateObjectTypeRequest,
} from '../../api/ontologies';
import {
  useCreateObjectType,
  useDeleteObjectType,
  useObjectTypes,
  useUpdateObjectType,
} from '../../hooks/useObjectTypes';
import { toApiName, toPluralName } from '../../utils/naming';
import { Modal } from '../common/Modal';
import { Badge, statusVariant } from '../common/Badge';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { PropertiesEditor } from './PropertiesEditor';

type StatusFilter =
  | 'ALL'
  | 'ACTIVE'
  | 'ENDORSED'
  | 'EXPERIMENTAL'
  | 'DEPRECATED';
type SortOrder = 'asc' | 'desc';

const STATUS_OPTIONS: Array<{ value: StatusFilter; label: string }> = [
  { value: 'ALL', label: 'All statuses' },
  { value: 'ACTIVE', label: 'Active' },
  { value: 'ENDORSED', label: 'Endorsed' },
  { value: 'EXPERIMENTAL', label: 'Experimental' },
  { value: 'DEPRECATED', label: 'Deprecated' },
];

const STATUS_VALUES = [
  'ACTIVE',
  'ENDORSED',
  'EXPERIMENTAL',
  'DEPRECATED',
] as const;
const VISIBILITY_VALUES = ['PROMINENT', 'NORMAL', 'HIDDEN'] as const;

export function ObjectTypeAdminPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { data: objectTypes, isLoading, error } = useObjectTypes(
    ontologyApiName,
  );

  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<StatusFilter>('ALL');
  const [sort, setSort] = useState<SortOrder>('asc');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<ObjectType | null>(null);
  const [deleting, setDeleting] = useState<ObjectType | null>(null);

  const filtered = useMemo(() => {
    if (!objectTypes) return [];
    const q = search.trim().toLowerCase();
    const list = objectTypes.filter((ot) => {
      if (status !== 'ALL' && ot.status !== status) return false;
      if (!q) return true;
      return (
        ot.apiName.toLowerCase().includes(q) ||
        ot.displayName.toLowerCase().includes(q)
      );
    });
    list.sort((a, b) => {
      const cmp = a.displayName.localeCompare(b.displayName);
      return sort === 'asc' ? cmp : -cmp;
    });
    return list;
  }, [objectTypes, search, status, sort]);

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm">
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto">
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Object Types
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Object Type
        </button>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <input
          type="search"
          aria-label="Search object types"
          placeholder="Search by name or apiName…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-[12rem] max-w-md px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Status</span>
          <select
            aria-label="Filter by status"
            value={status}
            onChange={(e) => setStatus(e.target.value as StatusFilter)}
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            {STATUS_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Sort</span>
          <select
            aria-label="Sort by name"
            value={sort}
            onChange={(e) => setSort(e.target.value as SortOrder)}
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="asc">Name (A → Z)</option>
            <option value="desc">Name (Z → A)</option>
          </select>
        </label>
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div className="flex items-center justify-center py-20">
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p className="text-sm text-accent-error">
            Failed to load object types: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No object types match the filters
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Adjust the search or create a new Object Type.
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <ObjectTypeTable
            rows={filtered}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>

      {createOpen && (
        <CreateObjectTypeModal
          ontologyApiName={ontologyApiName}
          existing={objectTypes ?? []}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <EditObjectTypeModal
          ontologyApiName={ontologyApiName}
          objectType={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteObjectTypeModal
          ontologyApiName={ontologyApiName}
          objectType={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function ObjectTypeTable({
  rows,
  onEdit,
  onDelete,
}: {
  rows: ObjectType[];
  onEdit: (ot: ObjectType) => void;
  onDelete: (ot: ObjectType) => void;
}) {
  return (
    <div
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
            <th className="text-left px-4 py-2 font-medium">Primary Key</th>
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-left px-4 py-2 font-medium">Visibility</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((ot) => (
            <tr
              key={ot.rid}
              className="border-b last:border-0 hover:bg-bg-tertiary/30"
              style={{ borderColor: 'rgba(31,41,55,0.5)' }}
            >
              <td className="px-4 py-2 text-text-primary">{ot.displayName}</td>
              <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                {ot.apiName}
              </td>
              <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                {ot.primaryKey}
              </td>
              <td className="px-4 py-2">
                <Badge variant={statusVariant(ot.status)}>{ot.status}</Badge>
              </td>
              <td className="px-4 py-2 text-xs text-text-secondary">
                {ot.visibility}
              </td>
              <td className="px-4 py-2 text-right whitespace-nowrap">
                <button
                  type="button"
                  onClick={() => onEdit(ot)}
                  className="text-xs text-accent-cyan hover:underline mr-3"
                >
                  Edit
                </button>
                <button
                  type="button"
                  onClick={() => onDelete(ot)}
                  className="text-xs text-accent-error hover:underline"
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface CreateFormState {
  displayName: string;
  apiName: string;
  pluralDisplayName: string;
  description: string;
  primaryKey: string;
  titleProperty: string;
  status: (typeof STATUS_VALUES)[number];
  visibility: (typeof VISIBILITY_VALUES)[number];
  apiNameDirty: boolean;
  pluralDirty: boolean;
}

function CreateObjectTypeModal({
  ontologyApiName,
  existing,
  onClose,
}: {
  ontologyApiName: string;
  existing: ObjectType[];
  onClose: () => void;
}) {
  const create = useCreateObjectType(ontologyApiName);
  const [form, setForm] = useState<CreateFormState>({
    displayName: '',
    apiName: '',
    pluralDisplayName: '',
    description: '',
    primaryKey: '',
    titleProperty: '',
    status: 'ACTIVE',
    visibility: 'NORMAL',
    apiNameDirty: false,
    pluralDirty: false,
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  const apiNameTaken = useMemo(
    () => new Set(existing.map((ot) => ot.apiName)),
    [existing],
  );

  function updateDisplayName(next: string) {
    setForm((f) => ({
      ...f,
      displayName: next,
      apiName: f.apiNameDirty ? f.apiName : toApiName(next),
      pluralDisplayName: f.pluralDirty ? f.pluralDisplayName : toPluralName(next),
    }));
  }

  const duplicateApiName =
    !!form.apiName && apiNameTaken.has(form.apiName);
  const canSubmit =
    !!form.displayName.trim() &&
    !!form.apiName.trim() &&
    !!form.primaryKey.trim() &&
    !duplicateApiName &&
    !create.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const body: CreateObjectTypeRequest = {
      apiName: form.apiName.trim(),
      displayName: form.displayName.trim(),
      pluralDisplayName: form.pluralDisplayName.trim() || undefined,
      description: form.description.trim() || undefined,
      primaryKey: form.primaryKey.trim(),
      titleProperty: form.titleProperty.trim() || undefined,
      status: form.status,
      visibility: form.visibility,
    };
    try {
      await create.mutateAsync(body);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="New Object Type">
      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        <Field label="Display Name" required>
          <input
            type="text"
            value={form.displayName}
            onChange={(e) => updateDisplayName(e.target.value)}
            required
            aria-required
            autoFocus
            className={inputClass}
          />
        </Field>
        <Field
          label="API Name"
          required
          hint="Auto-generated from display name; edit to override."
          error={
            duplicateApiName
              ? `An ObjectType with apiName "${form.apiName}" already exists.`
              : undefined
          }
        >
          <input
            type="text"
            value={form.apiName}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                apiName: e.target.value,
                apiNameDirty: true,
              }))
            }
            required
            className={inputClass + ' font-mono'}
          />
        </Field>
        <Field
          label="Plural Display Name"
          hint="Auto-generated from display name; edit to override."
        >
          <input
            type="text"
            value={form.pluralDisplayName}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                pluralDisplayName: e.target.value,
                pluralDirty: true,
              }))
            }
            className={inputClass}
          />
        </Field>
        <Field label="Primary Key" required hint="apiName of the PK property.">
          <input
            type="text"
            value={form.primaryKey}
            onChange={(e) =>
              setForm((f) => ({ ...f, primaryKey: e.target.value }))
            }
            required
            className={inputClass + ' font-mono'}
          />
        </Field>
        <Field label="Title Property" hint="Optional — apiName of the property used as a title.">
          <input
            type="text"
            value={form.titleProperty}
            onChange={(e) =>
              setForm((f) => ({ ...f, titleProperty: e.target.value }))
            }
            className={inputClass + ' font-mono'}
          />
        </Field>
        <Field label="Description">
          <textarea
            value={form.description}
            onChange={(e) =>
              setForm((f) => ({ ...f, description: e.target.value }))
            }
            rows={3}
            className={inputClass}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Status">
            <select
              value={form.status}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  status: e.target.value as CreateFormState['status'],
                }))
              }
              className={inputClass}
            >
              {STATUS_VALUES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Visibility">
            <select
              value={form.visibility}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  visibility: e.target.value as CreateFormState['visibility'],
                }))
              }
              className={inputClass}
            >
              {VISIBILITY_VALUES.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </Field>
        </div>
        {submitError && (
          <p role="alert" className="text-xs text-accent-error">
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
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

interface EditFormState {
  displayName: string;
  pluralDisplayName: string;
  description: string;
  titleProperty: string;
  status: (typeof STATUS_VALUES)[number];
  visibility: (typeof VISIBILITY_VALUES)[number];
  iconName: string;
  color: string;
}

function EditObjectTypeModal({
  ontologyApiName,
  objectType,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  onClose: () => void;
}) {
  const update = useUpdateObjectType(ontologyApiName);
  const [tab, setTab] = useState<'details' | 'properties'>('details');
  const [form, setForm] = useState<EditFormState>({
    displayName: objectType.displayName,
    pluralDisplayName: objectType.pluralDisplayName ?? '',
    description: objectType.description ?? '',
    titleProperty: objectType.titleProperty ?? '',
    status: objectType.status,
    visibility: objectType.visibility,
    iconName: objectType.icon ?? '',
    color: objectType.color ?? '',
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const body: UpdateObjectTypeRequest = {
      displayName: form.displayName.trim(),
      pluralDisplayName: form.pluralDisplayName.trim() || undefined,
      description: form.description.trim() || undefined,
      titleProperty: form.titleProperty.trim() || undefined,
      status: form.status,
      visibility: form.visibility,
      iconName: form.iconName.trim() || undefined,
      color: form.color.trim() || undefined,
    };
    try {
      await update.mutateAsync({ rid: objectType.rid, body });
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title={`Edit: ${objectType.displayName}`} size="xl">
      <div className="flex gap-2 border-b mb-4" style={{ borderColor: 'rgba(31,41,55,0.5)' }} role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'details'}
          onClick={() => setTab('details')}
          className={`px-3 py-1.5 text-xs font-semibold border-b-2 -mb-px ${
            tab === 'details'
              ? 'border-accent-cyan text-accent-cyan'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          Details
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'properties'}
          onClick={() => setTab('properties')}
          className={`px-3 py-1.5 text-xs font-semibold border-b-2 -mb-px ${
            tab === 'properties'
              ? 'border-accent-cyan text-accent-cyan'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          Properties
        </button>
      </div>
      {tab === 'properties' ? (
        <PropertiesEditor
          ontologyApiName={ontologyApiName}
          objectType={objectType}
        />
      ) : (
      <form onSubmit={onSubmit} className="flex flex-col gap-3">
        <div className="text-xs text-text-secondary font-mono">
          {objectType.apiName}
        </div>
        <Field label="Display Name" required>
          <input
            type="text"
            value={form.displayName}
            onChange={(e) =>
              setForm((f) => ({ ...f, displayName: e.target.value }))
            }
            required
            className={inputClass}
          />
        </Field>
        <Field label="Plural Display Name">
          <input
            type="text"
            value={form.pluralDisplayName}
            onChange={(e) =>
              setForm((f) => ({ ...f, pluralDisplayName: e.target.value }))
            }
            className={inputClass}
          />
        </Field>
        <Field label="Description">
          <textarea
            value={form.description}
            onChange={(e) =>
              setForm((f) => ({ ...f, description: e.target.value }))
            }
            rows={3}
            className={inputClass}
          />
        </Field>
        <Field label="Title Property">
          <input
            type="text"
            value={form.titleProperty}
            onChange={(e) =>
              setForm((f) => ({ ...f, titleProperty: e.target.value }))
            }
            className={inputClass + ' font-mono'}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Status">
            <select
              value={form.status}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  status: e.target.value as EditFormState['status'],
                }))
              }
              className={inputClass}
            >
              {STATUS_VALUES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Visibility">
            <select
              value={form.visibility}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  visibility: e.target.value as EditFormState['visibility'],
                }))
              }
              className={inputClass}
            >
              {VISIBILITY_VALUES.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Icon" hint="Free-form icon identifier.">
            <input
              type="text"
              value={form.iconName}
              onChange={(e) =>
                setForm((f) => ({ ...f, iconName: e.target.value }))
              }
              className={inputClass}
            />
          </Field>
          <Field label="Color" hint="e.g. #F59E0B">
            <input
              type="text"
              value={form.color}
              onChange={(e) => setForm((f) => ({ ...f, color: e.target.value }))}
              className={inputClass + ' font-mono'}
            />
          </Field>
        </div>
        {submitError && (
          <p role="alert" className="text-xs text-accent-error">
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={update.isPending || !form.displayName.trim()}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {update.isPending ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </form>
      )}
    </Modal>
  );
}

function DeleteObjectTypeModal({
  ontologyApiName,
  objectType,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  onClose: () => void;
}) {
  const del = useDeleteObjectType(ontologyApiName);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const { data: outgoingLinks } = useQuery({
    queryKey: [
      'linkTypes',
      ontologyApiName,
      objectType.apiName,
      'admin-delete-impact',
    ],
    queryFn: () => listOutgoingLinkTypes(ontologyApiName, objectType.apiName),
  });

  const { data: actionTypes } = useQuery<ActionType[]>({
    queryKey: ['actionTypes', ontologyApiName, 'admin-delete-impact'],
    queryFn: () => listActionTypes(ontologyApiName),
  });

  const impactedActionTypes = useMemo(() => {
    if (!actionTypes) return [] as ActionType[];
    return actionTypes.filter((at) =>
      actionReferencesObjectType(at, objectType.apiName),
    );
  }, [actionTypes, objectType.apiName]);

  const outgoingCount = (outgoingLinks as LinkType[] | undefined)?.length ?? 0;
  const actionCount = impactedActionTypes.length;

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync(objectType.rid);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Object Type">
      <div className="flex flex-col gap-3">
        <p className="text-sm text-text-primary">
          Delete{' '}
          <span className="font-semibold">{objectType.displayName}</span>{' '}
          <span className="text-xs text-text-secondary font-mono">
            ({objectType.apiName})
          </span>
          ?
        </p>
        <p className="text-xs text-text-secondary">
          ACTIVE and PROMOTED object types cannot be deleted on main — deprecate
          first, or delete from a branch.
        </p>
        <div
          className="rounded border p-3 flex flex-col gap-1.5"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <p className="text-[10px] uppercase tracking-widest text-text-secondary">
            Impact
          </p>
          <p
            className="text-sm text-text-primary"
            data-testid="delete-impact-links"
          >
            <span className="font-semibold">{outgoingCount}</span> outgoing
            LinkType{outgoingCount === 1 ? '' : 's'}
          </p>
          <p
            className="text-sm text-text-primary"
            data-testid="delete-impact-actions"
          >
            <span className="font-semibold">{actionCount}</span> ActionType
            {actionCount === 1 ? '' : 's'} reference this type
          </p>
        </div>
        {submitError && (
          <p role="alert" className="text-xs text-accent-error">
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
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
            {del.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40';

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
      {hint && !error && <span className="text-[11px] text-text-muted">{hint}</span>}
      {error && (
        <span className="text-[11px] text-accent-error" role="alert">
          {error}
        </span>
      )}
    </label>
  );
}

// Checks whether an ActionType's parameters reference the given ObjectType
// apiName. Frontend ActionParameterV2 types are loosely typed (dataType is
// { type: string; ... }), so we walk the dataType tree looking for fields
// that name the objectType by string.
function actionReferencesObjectType(
  action: ActionType,
  objectTypeApiName: string,
): boolean {
  const params = action.parameters ?? {};
  for (const paramName of Object.keys(params)) {
    const p = params[paramName];
    if (!p) continue;
    if (dataTypeReferences(p.dataType as unknown, objectTypeApiName)) return true;
  }
  return false;
}

function dataTypeReferences(
  dt: unknown,
  objectTypeApiName: string,
): boolean {
  if (!dt || typeof dt !== 'object') return false;
  const obj = dt as Record<string, unknown>;
  if (
    typeof obj.objectTypeApiName === 'string' &&
    obj.objectTypeApiName === objectTypeApiName
  ) {
    return true;
  }
  if (
    typeof obj.objectType === 'string' &&
    obj.objectType === objectTypeApiName
  ) {
    return true;
  }
  if (obj.itemType && dataTypeReferences(obj.itemType, objectTypeApiName)) {
    return true;
  }
  return false;
}
