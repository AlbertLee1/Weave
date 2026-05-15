import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import type { ActionType, LinkType, ObjectType } from '../../api/types';
import {
  listActionTypes,
  type CreateLinkTypeRequest,
  type Cardinality,
  type UpdateLinkTypeRequest,
} from '../../api/ontologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import {
  useCreateLinkType,
  useDeleteLinkType,
  useLinkTypes,
  useUpdateLinkType,
} from '../../hooks/useLinkTypes';
import { toApiName } from '../../utils/naming';
import { VERTEX_LINK_TYPE_CLASSES } from '../../features/vertex/links/edgeArrowStyle';
import { Modal } from '../common/Modal';
import { Badge } from '../common/Badge';
import { LoadingSpinner } from '../common/LoadingSpinner';

type CardinalityFilter = 'ALL' | Cardinality;

const CARDINALITIES: Cardinality[] = [
  'ONE_TO_ONE',
  'ONE_TO_MANY',
  'MANY_TO_MANY',
];

const CARDINALITY_OPTIONS: Array<{ value: CardinalityFilter; label: string }> = [
  { value: 'ALL', label: 'All cardinalities' },
  { value: 'ONE_TO_ONE', label: 'One to one' },
  { value: 'ONE_TO_MANY', label: 'One to many' },
  { value: 'MANY_TO_MANY', label: 'Many to many' },
];

export function LinkTypeAdminPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { data: linkTypes, isLoading, error } = useLinkTypes(ontologyApiName);
  const { data: objectTypes } = useObjectTypes(ontologyApiName);

  const [search, setSearch] = useState('');
  const [cardinality, setCardinality] = useState<CardinalityFilter>('ALL');
  const [sourceFilter, setSourceFilter] = useState<string>('ALL');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<LinkType | null>(null);
  const [deleting, setDeleting] = useState<LinkType | null>(null);

  const filtered = useMemo(() => {
    if (!linkTypes) return [];
    const q = search.trim().toLowerCase();
    const list = linkTypes.filter((lt) => {
      if (cardinality !== 'ALL' && lt.cardinality !== cardinality) return false;
      if (sourceFilter !== 'ALL' && lt.objectTypeApiName !== sourceFilter)
        return false;
      if (!q) return true;
      return (
        lt.apiName.toLowerCase().includes(q) ||
        lt.displayName.toLowerCase().includes(q) ||
        lt.objectTypeApiName.toLowerCase().includes(q) ||
        lt.linkedObjectTypeApiName.toLowerCase().includes(q)
      );
    });
    list.sort((a, b) => a.displayName.localeCompare(b.displayName));
    return list;
  }, [linkTypes, search, cardinality, sourceFilter]);

  if (!ontologyApiName) {
    return (
      <div
        data-testid="link-type-admin-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="link-type-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Link Types
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="link-type-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Link Type
        </button>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <input
          type="search"
          aria-label="Search link types"
          placeholder="Search by name, apiName, source, or target…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-[12rem] max-w-md px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Cardinality</span>
          <select
            aria-label="Filter by cardinality"
            value={cardinality}
            onChange={(e) =>
              setCardinality(e.target.value as CardinalityFilter)
            }
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            {CARDINALITY_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Source</span>
          <select
            aria-label="Filter by source object type"
            value={sourceFilter}
            onChange={(e) => setSourceFilter(e.target.value)}
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="ALL">All sources</option>
            {(objectTypes ?? []).map((ot) => (
              <option key={ot.rid} value={ot.apiName}>
                {ot.displayName}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="link-type-admin-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="link-type-admin-error"
            className="text-sm text-accent-error"
          >
            Failed to load link types: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            data-testid="link-type-admin-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No link types match the filters
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Adjust the search or create a new Link Type.
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <LinkTypeTable
            rows={filtered}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>

      {createOpen && (
        <CreateLinkTypeModal
          ontologyApiName={ontologyApiName}
          objectTypes={objectTypes ?? []}
          existing={linkTypes ?? []}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <EditLinkTypeModal
          ontologyApiName={ontologyApiName}
          linkType={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteLinkTypeModal
          ontologyApiName={ontologyApiName}
          linkType={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function cardinalityVariant(c: Cardinality) {
  if (c === 'ONE_TO_ONE') return 'info' as const;
  if (c === 'ONE_TO_MANY') return 'success' as const;
  return 'warning' as const;
}

function cardinalityLabel(c: Cardinality) {
  if (c === 'ONE_TO_ONE') return '1 : 1';
  if (c === 'ONE_TO_MANY') return '1 : N';
  return 'N : N';
}

function LinkTypeTable({
  rows,
  onEdit,
  onDelete,
}: {
  rows: LinkType[];
  onEdit: (lt: LinkType) => void;
  onDelete: (lt: LinkType) => void;
}) {
  return (
    <div
      data-testid="link-type-admin-table"
      className="rounded border overflow-hidden"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <table className="w-full text-sm">
        <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
          <tr
            className="border-b"
            style={{ borderColor: 'rgba(31,41,55,0.5)' }}
          >
            <th className="text-left px-4 py-2 font-medium">Display Name</th>
            <th className="text-left px-4 py-2 font-medium">API Name</th>
            <th className="text-left px-4 py-2 font-medium">Relationship</th>
            <th className="text-left px-4 py-2 font-medium">Required</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((lt) => (
            <tr
              key={lt.rid}
              data-testid="link-type-row"
              data-link-type-api-name={lt.apiName}
              data-link-type-rid={lt.rid}
              data-link-type-cardinality={lt.cardinality}
              className="border-b last:border-0 hover:bg-bg-tertiary/30"
              style={{ borderColor: 'rgba(31,41,55,0.5)' }}
            >
              <td className="px-4 py-2 text-text-primary">{lt.displayName}</td>
              <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                {lt.apiName}
              </td>
              <td className="px-4 py-2">
                <span className="flex flex-wrap items-center gap-2 text-xs text-text-primary">
                  <span className="font-mono">{lt.objectTypeApiName}</span>
                  <span className="text-text-muted">→</span>
                  <span className="font-mono">
                    {lt.linkedObjectTypeApiName}
                  </span>
                  <Badge variant={cardinalityVariant(lt.cardinality)}>
                    {cardinalityLabel(lt.cardinality)}
                  </Badge>
                </span>
              </td>
              <td className="px-4 py-2 text-xs text-text-secondary">
                {lt.required ? 'Yes' : 'No'}
              </td>
              <td className="px-4 py-2 text-right whitespace-nowrap">
                <button
                  type="button"
                  data-testid="link-type-edit-btn"
                  data-link-type-api-name={lt.apiName}
                  onClick={() => onEdit(lt)}
                  className="text-xs text-accent-cyan hover:underline mr-3"
                >
                  Edit
                </button>
                <button
                  type="button"
                  data-testid="link-type-delete-btn"
                  data-link-type-api-name={lt.apiName}
                  onClick={() => onDelete(lt)}
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
  description: string;
  source: string;
  target: string;
  cardinality: Cardinality;
  required: boolean;
  foreignKeyConfig: string;
  apiNameDirty: boolean;
  typeClasses: string[];
}

function CreateLinkTypeModal({
  ontologyApiName,
  objectTypes,
  existing,
  onClose,
}: {
  ontologyApiName: string;
  objectTypes: ObjectType[];
  existing: LinkType[];
  onClose: () => void;
}) {
  const create = useCreateLinkType(ontologyApiName);
  const [form, setForm] = useState<CreateFormState>({
    displayName: '',
    apiName: '',
    description: '',
    source: objectTypes[0]?.apiName ?? '',
    target: objectTypes[0]?.apiName ?? '',
    cardinality: 'ONE_TO_MANY',
    required: false,
    foreignKeyConfig: '',
    apiNameDirty: false,
    typeClasses: [],
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  const apiNameTaken = useMemo(
    () => new Set(existing.map((lt) => lt.apiName)),
    [existing],
  );

  function updateDisplayName(next: string) {
    setForm((f) => ({
      ...f,
      displayName: next,
      apiName: f.apiNameDirty ? f.apiName : toApiName(next),
    }));
  }

  const duplicateApiName =
    !!form.apiName && apiNameTaken.has(form.apiName);

  const needsForeignKey = form.cardinality !== 'MANY_TO_MANY';

  let parsedForeignKey: unknown | undefined;
  let foreignKeyError: string | null = null;
  if (needsForeignKey && form.foreignKeyConfig.trim()) {
    try {
      parsedForeignKey = JSON.parse(form.foreignKeyConfig);
    } catch (err) {
      foreignKeyError = `Invalid JSON: ${(err as Error).message}`;
    }
  }

  const canSubmit =
    !!form.displayName.trim() &&
    !!form.apiName.trim() &&
    !!form.source &&
    !!form.target &&
    !duplicateApiName &&
    !foreignKeyError &&
    !create.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const body: CreateLinkTypeRequest = {
      apiName: form.apiName.trim(),
      displayName: form.displayName.trim(),
      description: form.description.trim() || undefined,
      objectTypeApiName: form.source,
      linkedObjectTypeApiName: form.target,
      cardinality: form.cardinality,
      required: form.required,
      foreignKeyConfig:
        needsForeignKey && parsedForeignKey !== undefined
          ? parsedForeignKey
          : undefined,
      typeClasses:
        form.typeClasses.length > 0 ? [...form.typeClasses] : undefined,
    };
    try {
      await create.mutateAsync(body);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="New Link Type">
      <form
        onSubmit={onSubmit}
        data-testid="link-type-create-form"
        className="flex flex-col gap-3"
      >
        <Field label="Display Name" required>
          <input
            type="text"
            data-testid="link-type-create-display-name"
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
              ? `A LinkType with apiName "${form.apiName}" already exists.`
              : undefined
          }
        >
          <input
            type="text"
            data-testid="link-type-create-api-name"
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
        <Field label="Description">
          <textarea
            data-testid="link-type-create-description"
            value={form.description}
            onChange={(e) =>
              setForm((f) => ({ ...f, description: e.target.value }))
            }
            rows={2}
            className={inputClass}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Source Object Type" required>
            <select
              aria-label="Source object type"
              data-testid="link-type-create-source"
              value={form.source}
              onChange={(e) =>
                setForm((f) => ({ ...f, source: e.target.value }))
              }
              required
              className={inputClass}
            >
              {objectTypes.map((ot) => (
                <option key={ot.rid} value={ot.apiName}>
                  {ot.displayName} ({ot.apiName})
                </option>
              ))}
            </select>
          </Field>
          <Field label="Target Object Type" required>
            <select
              aria-label="Target object type"
              data-testid="link-type-create-target"
              value={form.target}
              onChange={(e) =>
                setForm((f) => ({ ...f, target: e.target.value }))
              }
              required
              className={inputClass}
            >
              {objectTypes.map((ot) => (
                <option key={ot.rid} value={ot.apiName}>
                  {ot.displayName} ({ot.apiName})
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field label="Cardinality" required>
          <select
            aria-label="Cardinality"
            data-testid="link-type-create-cardinality"
            value={form.cardinality}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                cardinality: e.target.value as Cardinality,
              }))
            }
            className={inputClass}
          >
            {CARDINALITIES.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </Field>
        {needsForeignKey && (
          <Field
            label="Foreign Key Config (JSON)"
            hint='Optional. Example: {"sourceProperty": "departmentId", "targetProperty": "id"}'
            error={foreignKeyError ?? undefined}
          >
            <textarea
              aria-label="Foreign key config"
              data-testid="link-type-create-foreign-key"
              value={form.foreignKeyConfig}
              onChange={(e) =>
                setForm((f) => ({ ...f, foreignKeyConfig: e.target.value }))
              }
              rows={3}
              className={inputClass + ' font-mono text-xs'}
              placeholder='{"sourceProperty":"...","targetProperty":"..."}'
            />
          </Field>
        )}
        <label className="flex items-center gap-2 text-xs text-text-secondary">
          <input
            type="checkbox"
            data-testid="link-type-create-required"
            checked={form.required}
            onChange={(e) =>
              setForm((f) => ({ ...f, required: e.target.checked }))
            }
          />
          <span>Required link (non-nullable)</span>
        </label>
        <TypeClassCheckboxes
          testIdPrefix="link-type-create"
          selected={form.typeClasses}
          onChange={(next) => setForm((f) => ({ ...f, typeClasses: next }))}
        />
        {submitError && (
          <p
            role="alert"
            data-testid="link-type-create-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="link-type-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="link-type-create-submit"
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
  description: string;
  required: boolean;
  typeClasses: string[];
  typeClassesDirty: boolean;
}

function EditLinkTypeModal({
  ontologyApiName,
  linkType,
  onClose,
}: {
  ontologyApiName: string;
  linkType: LinkType;
  onClose: () => void;
}) {
  const update = useUpdateLinkType(ontologyApiName);
  const [form, setForm] = useState<EditFormState>({
    displayName: linkType.displayName,
    description: linkType.description ?? '',
    required: linkType.required,
    typeClasses: linkType.typeClasses ?? [],
    typeClassesDirty: false,
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const body: UpdateLinkTypeRequest = {
      displayName: form.displayName.trim(),
      description: form.description.trim() || undefined,
      required: form.required,
      // Only send typeClasses when the user actually toggled a box. Mirrors
      // the backend tri-state: omit = preserve.
      typeClasses: form.typeClassesDirty ? [...form.typeClasses] : undefined,
    };
    try {
      await update.mutateAsync({ rid: linkType.rid, body });
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title={`Edit: ${linkType.displayName}`}>
      <form
        onSubmit={onSubmit}
        data-testid="link-type-edit-form"
        className="flex flex-col gap-3"
      >
        <div
          data-testid="link-type-edit-api-name"
          className="text-xs text-text-secondary font-mono"
        >
          {linkType.apiName}
        </div>
        <div
          data-testid="link-type-edit-relationship"
          data-link-type-source={linkType.objectTypeApiName}
          data-link-type-target={linkType.linkedObjectTypeApiName}
          data-link-type-cardinality={linkType.cardinality}
          className="rounded border p-3 flex flex-col gap-1 text-xs"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <p className="uppercase tracking-widest text-text-secondary">
            Relationship (immutable)
          </p>
          <p className="text-text-primary">
            <span className="font-mono">{linkType.objectTypeApiName}</span>{' '}
            <span className="text-text-muted">→</span>{' '}
            <span className="font-mono">{linkType.linkedObjectTypeApiName}</span>{' '}
            <Badge variant={cardinalityVariant(linkType.cardinality)}>
              {cardinalityLabel(linkType.cardinality)}
            </Badge>
          </p>
          <p className="text-[11px] text-text-muted">
            Source, target, and cardinality cannot be changed. Delete and
            recreate to modify the relationship.
          </p>
        </div>
        <Field label="Display Name" required>
          <input
            type="text"
            data-testid="link-type-edit-display-name"
            value={form.displayName}
            onChange={(e) =>
              setForm((f) => ({ ...f, displayName: e.target.value }))
            }
            required
            className={inputClass}
          />
        </Field>
        <Field label="Description">
          <textarea
            data-testid="link-type-edit-description"
            value={form.description}
            onChange={(e) =>
              setForm((f) => ({ ...f, description: e.target.value }))
            }
            rows={3}
            className={inputClass}
          />
        </Field>
        <label className="flex items-center gap-2 text-xs text-text-secondary">
          <input
            type="checkbox"
            data-testid="link-type-edit-required"
            checked={form.required}
            onChange={(e) =>
              setForm((f) => ({ ...f, required: e.target.checked }))
            }
          />
          <span>Required link (non-nullable)</span>
        </label>
        <TypeClassCheckboxes
          testIdPrefix="link-type-edit"
          selected={form.typeClasses}
          onChange={(next) =>
            setForm((f) => ({
              ...f,
              typeClasses: next,
              typeClassesDirty: true,
            }))
          }
        />
        {submitError && (
          <p
            role="alert"
            data-testid="link-type-edit-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="link-type-edit-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="link-type-edit-submit"
            disabled={update.isPending || !form.displayName.trim()}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {update.isPending ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteLinkTypeModal({
  ontologyApiName,
  linkType,
  onClose,
}: {
  ontologyApiName: string;
  linkType: LinkType;
  onClose: () => void;
}) {
  const del = useDeleteLinkType(ontologyApiName);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const { data: actionTypes } = useQuery<ActionType[]>({
    queryKey: ['actionTypes', ontologyApiName, 'linkType-delete-impact'],
    queryFn: () => listActionTypes(ontologyApiName),
  });

  const impactedActionTypes = useMemo(() => {
    if (!actionTypes) return [] as ActionType[];
    return actionTypes.filter((at) =>
      actionReferencesLinkType(at, linkType.apiName),
    );
  }, [actionTypes, linkType.apiName]);

  const actionCount = impactedActionTypes.length;

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync(linkType.rid);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Link Type">
      <div
        data-testid="link-type-delete-modal"
        data-link-type-api-name={linkType.apiName}
        data-link-type-rid={linkType.rid}
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{linkType.displayName}</span>{' '}
          <span className="text-xs text-text-secondary font-mono">
            ({linkType.apiName})
          </span>
          ?
        </p>
        <p className="text-xs text-text-secondary">
          <span className="font-mono">{linkType.objectTypeApiName}</span>{' '}
          <span className="text-text-muted">→</span>{' '}
          <span className="font-mono">{linkType.linkedObjectTypeApiName}</span>{' '}
          <Badge variant={cardinalityVariant(linkType.cardinality)}>
            {cardinalityLabel(linkType.cardinality)}
          </Badge>
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
            data-testid="delete-impact-actions"
          >
            <span className="font-semibold">{actionCount}</span> ActionType
            {actionCount === 1 ? '' : 's'} reference this link
          </p>
          <p
            className="text-sm text-text-primary"
            data-testid="delete-impact-search-around"
          >
            <span className="font-semibold">searchAround</span> queries using{' '}
            <span className="font-mono text-xs">{linkType.apiName}</span> will
            stop returning results.
          </p>
        </div>
        {submitError && (
          <p
            role="alert"
            data-testid="link-type-delete-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="link-type-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="link-type-delete-confirm"
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

function TypeClassCheckboxes({
  testIdPrefix,
  selected,
  onChange,
}: {
  testIdPrefix: string;
  selected: string[];
  onChange: (next: string[]) => void;
}) {
  function toggle(tag: string, on: boolean) {
    if (on) {
      if (!selected.includes(tag)) onChange([...selected, tag]);
    } else {
      onChange(selected.filter((t) => t !== tag));
    }
  }
  return (
    <fieldset className="flex flex-col gap-1 text-xs text-text-secondary">
      <legend className="uppercase tracking-widest pb-1">
        Vertex Type Classes
      </legend>
      {VERTEX_LINK_TYPE_CLASSES.map((tag) => (
        <label key={tag} className="flex items-center gap-2">
          <input
            type="checkbox"
            data-testid={`${testIdPrefix}-type-class-${tag}`}
            checked={selected.includes(tag)}
            onChange={(e) => toggle(tag, e.target.checked)}
          />
          <span className="font-mono">{tag}</span>
        </label>
      ))}
    </fieldset>
  );
}

// Walks an ActionType's rules JSON looking for references to the given
// linkType apiName. Rules are loosely typed (createLink / deleteLink rules
// carry linkTypeApiName strings); this tolerates any nested object/array
// shape.
function actionReferencesLinkType(
  action: ActionType,
  linkTypeApiName: string,
): boolean {
  return containsLinkTypeRef(action.rules, linkTypeApiName);
}

function containsLinkTypeRef(v: unknown, linkTypeApiName: string): boolean {
  if (v == null) return false;
  if (Array.isArray(v)) {
    return v.some((item) => containsLinkTypeRef(item, linkTypeApiName));
  }
  if (typeof v === 'object') {
    const obj = v as Record<string, unknown>;
    if (
      typeof obj.linkTypeApiName === 'string' &&
      obj.linkTypeApiName === linkTypeApiName
    ) {
      return true;
    }
    for (const key of Object.keys(obj)) {
      if (containsLinkTypeRef(obj[key], linkTypeApiName)) return true;
    }
  }
  return false;
}
