import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type {
  InterfaceOutgoingLinkType,
  InterfaceSharedProperty,
  ObjectType,
  ObjectTypeInterface,
  OntologyInterface,
} from '../../api/types';
import type {
  CreateInterfaceRequest,
  UpdateInterfaceRequest,
} from '../../api/ontologies';
import { listObjectTypeInterfaces } from '../../api/ontologies';
import {
  useInterfacesAdmin,
  useCreateInterface,
  useUpdateInterface,
  useDeleteInterface,
  useAttachInterface,
  useDetachInterface,
} from '../../hooks/useInterfaces';
import { useObjectTypes } from '../../hooks/useObjectTypes';
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
  'geopoint',
  'geoshape',
] as const;

const LINK_CARDINALITIES = ['ONE', 'MANY'] as const;

export function InterfaceAdminPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const {
    data: interfaces,
    isLoading,
    error,
  } = useInterfacesAdmin(ontologyApiName);
  const { data: objectTypes } = useObjectTypes(ontologyApiName);

  const [search, setSearch] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<OntologyInterface | null>(null);
  const [deleting, setDeleting] = useState<OntologyInterface | null>(null);

  const filtered = useMemo(() => {
    if (!interfaces) return [];
    const q = search.trim().toLowerCase();
    const list = interfaces.filter((iface) => {
      if (!q) return true;
      return (
        iface.apiName.toLowerCase().includes(q) ||
        iface.displayName.toLowerCase().includes(q)
      );
    });
    list.sort((a, b) => a.displayName.localeCompare(b.displayName));
    return list;
  }, [interfaces, search]);

  if (!ontologyApiName) {
    return (
      <div
        data-testid="interface-admin-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="interface-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Interfaces
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="interface-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Interface
        </button>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <input
          type="search"
          data-testid="interface-search-input"
          aria-label="Search interfaces"
          placeholder="Search by name or apiName…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-[12rem] max-w-md px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="interface-admin-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="interface-admin-error"
            className="text-sm text-accent-error"
          >
            Failed to load interfaces: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            data-testid="interface-admin-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No interfaces yet
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Create an Interface to share properties and link types across
              multiple ObjectTypes.
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <InterfaceTable
            rows={filtered}
            ontologyApiName={ontologyApiName}
            objectTypes={objectTypes ?? []}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>

      {createOpen && (
        <InterfaceBuilderModal
          ontologyApiName={ontologyApiName}
          existing={interfaces ?? []}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <InterfaceBuilderModal
          ontologyApiName={ontologyApiName}
          existing={interfaces ?? []}
          editing={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteInterfaceModal
          ontologyApiName={ontologyApiName}
          iface={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function InterfaceTable({
  rows,
  ontologyApiName,
  objectTypes,
  onEdit,
  onDelete,
}: {
  rows: OntologyInterface[];
  ontologyApiName: string;
  objectTypes: ObjectType[];
  onEdit: (iface: OntologyInterface) => void;
  onDelete: (iface: OntologyInterface) => void;
}) {
  return (
    <div
      data-testid="interface-admin-table"
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
            <th className="text-left px-4 py-2 font-medium">Extends</th>
            <th className="text-left px-4 py-2 font-medium">Shared Props</th>
            <th className="text-left px-4 py-2 font-medium">Link Types</th>
            <th className="text-left px-4 py-2 font-medium">Implementing</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((iface) => {
            const sharedCount = (iface.sharedProperties ?? []).length;
            const linkCount = (iface.outgoingLinkTypes ?? []).length;
            const parent = rows.find((r) => r.rid === iface.extendsRid);
            return (
              <tr
                key={iface.rid}
                data-testid="interface-row"
                data-interface-api-name={iface.apiName}
                data-interface-rid={iface.rid}
                data-interface-shared-property-count={sharedCount}
                data-interface-link-type-count={linkCount}
                data-interface-extends-api-name={parent ? parent.apiName : ''}
                className="border-b last:border-0 hover:bg-bg-tertiary/30"
                style={{ borderColor: 'rgba(31,41,55,0.5)' }}
              >
                <td className="px-4 py-2 text-text-primary">
                  {iface.displayName}
                </td>
                <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                  {iface.apiName}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {parent ? parent.apiName : '—'}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {sharedCount}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {linkCount}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  <ImplementingObjectTypesCell
                    ontologyApiName={ontologyApiName}
                    iface={iface}
                    objectTypes={objectTypes}
                  />
                </td>
                <td className="px-4 py-2 text-right whitespace-nowrap">
                  <button
                    type="button"
                    data-testid="interface-edit-btn"
                    data-interface-api-name={iface.apiName}
                    onClick={() => onEdit(iface)}
                    className="text-xs text-accent-cyan hover:underline mr-3"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    data-testid="interface-delete-btn"
                    data-interface-api-name={iface.apiName}
                    onClick={() => onDelete(iface)}
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

function ImplementingObjectTypesCell({
  ontologyApiName,
  iface,
  objectTypes,
}: {
  ontologyApiName: string;
  iface: OntologyInterface;
  objectTypes: ObjectType[];
}) {
  const [open, setOpen] = useState(false);
  const attached = useAttachmentsByInterface(
    ontologyApiName,
    iface.rid,
    objectTypes,
  );
  return (
    <>
      <button
        type="button"
        data-testid="interface-manage-btn"
        data-interface-api-name={iface.apiName}
        data-interface-implementing-count={attached.length}
        onClick={() => setOpen(true)}
        className="text-xs text-accent-cyan hover:underline"
      >
        Manage ({attached.length})
      </button>
      {open && (
        <ImplementingObjectTypesModal
          ontologyApiName={ontologyApiName}
          iface={iface}
          objectTypes={objectTypes}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}

// Builder form state
interface BuilderState {
  apiName: string;
  apiNameDirty: boolean;
  displayName: string;
  description: string;
  extendsRid: string;
  sharedProperties: InterfaceSharedProperty[];
  outgoingLinkTypes: InterfaceOutgoingLinkType[];
}

function initialState(iface?: OntologyInterface): BuilderState {
  if (!iface) {
    return {
      apiName: '',
      apiNameDirty: false,
      displayName: '',
      description: '',
      extendsRid: '',
      sharedProperties: [],
      outgoingLinkTypes: [],
    };
  }
  return {
    apiName: iface.apiName,
    apiNameDirty: true,
    displayName: iface.displayName,
    description: '',
    extendsRid: iface.extendsRid ?? '',
    sharedProperties: Array.isArray(iface.sharedProperties)
      ? iface.sharedProperties
      : [],
    outgoingLinkTypes: Array.isArray(iface.outgoingLinkTypes)
      ? iface.outgoingLinkTypes
      : [],
  };
}

function InterfaceBuilderModal({
  ontologyApiName,
  existing,
  editing,
  onClose,
}: {
  ontologyApiName: string;
  existing: OntologyInterface[];
  editing?: OntologyInterface;
  onClose: () => void;
}) {
  const isEdit = !!editing;
  const create = useCreateInterface(ontologyApiName);
  const update = useUpdateInterface(ontologyApiName);
  const [form, setForm] = useState<BuilderState>(() => initialState(editing));
  const [submitError, setSubmitError] = useState<string | null>(null);

  const apiNameTaken = useMemo(
    () =>
      new Set(
        existing
          .filter((i) => !editing || i.rid !== editing.rid)
          .map((i) => i.apiName),
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

  const parentOptions = useMemo(
    () => existing.filter((i) => !editing || i.rid !== editing.rid),
    [existing, editing],
  );

  const canSubmit =
    !!form.apiName.trim() &&
    !!form.displayName.trim() &&
    !duplicateApiName &&
    !create.isPending &&
    !update.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    try {
      if (editing) {
        const body: UpdateInterfaceRequest = {
          displayName: form.displayName.trim(),
          extendsRid: form.extendsRid || undefined,
          sharedProperties: form.sharedProperties.filter(
            (sp) => sp.apiName.trim() !== '',
          ),
          outgoingLinkTypes: form.outgoingLinkTypes.filter(
            (lt) => lt.apiName.trim() !== '',
          ),
        };
        await update.mutateAsync({ rid: editing.rid, body });
      } else {
        const body: CreateInterfaceRequest = {
          apiName: form.apiName.trim(),
          displayName: form.displayName.trim(),
          description: form.description.trim() || undefined,
          extendsRid: form.extendsRid || undefined,
          sharedProperties: form.sharedProperties.filter(
            (sp) => sp.apiName.trim() !== '',
          ),
          outgoingLinkTypes: form.outgoingLinkTypes.filter(
            (lt) => lt.apiName.trim() !== '',
          ),
        };
        await create.mutateAsync(body);
      }
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  const modePrefix = isEdit ? 'interface-edit' : 'interface-create';
  return (
    <Modal
      open
      onClose={onClose}
      title={isEdit ? `Edit: ${editing.displayName}` : 'New Interface'}
      size="xl"
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
              data-testid="interface-display-name"
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
                ? `An Interface with apiName "${form.apiName}" already exists.`
                : undefined
            }
          >
            <input
              type="text"
              data-testid="interface-api-name"
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
        <div className="grid grid-cols-[1fr_16rem] gap-3">
          <Field label="Description">
            <textarea
              data-testid="interface-description"
              value={form.description}
              onChange={(e) =>
                setForm((f) => ({ ...f, description: e.target.value }))
              }
              rows={2}
              disabled={isEdit}
              className={inputClass}
            />
          </Field>
          <Field label="Extends">
            <select
              aria-label="Parent interface"
              data-testid="interface-extends"
              value={form.extendsRid}
              onChange={(e) =>
                setForm((f) => ({ ...f, extendsRid: e.target.value }))
              }
              className={inputClass}
            >
              <option value="">None</option>
              {parentOptions.map((p) => (
                <option key={p.rid} value={p.rid}>
                  {p.displayName} ({p.apiName})
                </option>
              ))}
            </select>
          </Field>
        </div>

        <Section
          title="Shared Properties"
          testId="interface-shared-properties-section"
          action={
            <button
              type="button"
              data-testid="interface-add-shared-property"
              onClick={() =>
                setForm((f) => ({
                  ...f,
                  sharedProperties: [
                    ...f.sharedProperties,
                    { apiName: '', baseType: 'string', isArray: false },
                  ],
                }))
              }
              className="text-xs text-accent-cyan hover:underline"
            >
              + Add shared property
            </button>
          }
        >
          <SharedPropertiesEditor
            items={form.sharedProperties}
            onChange={(sharedProperties) =>
              setForm((f) => ({ ...f, sharedProperties }))
            }
          />
        </Section>

        <Section
          title="Outgoing Link Types"
          testId="interface-link-types-section"
          action={
            <button
              type="button"
              data-testid="interface-add-link-type"
              onClick={() =>
                setForm((f) => ({
                  ...f,
                  outgoingLinkTypes: [
                    ...f.outgoingLinkTypes,
                    {
                      apiName: '',
                      displayName: '',
                      linkedEntityTypeApiName: '',
                      cardinality: 'ONE',
                      required: false,
                    },
                  ],
                }))
              }
              className="text-xs text-accent-cyan hover:underline"
            >
              + Add link type
            </button>
          }
        >
          <OutgoingLinkTypesEditor
            items={form.outgoingLinkTypes}
            onChange={(outgoingLinkTypes) =>
              setForm((f) => ({ ...f, outgoingLinkTypes }))
            }
          />
        </Section>

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

function SharedPropertiesEditor({
  items,
  onChange,
}: {
  items: InterfaceSharedProperty[];
  onChange: (next: InterfaceSharedProperty[]) => void;
}) {
  if (items.length === 0) {
    return (
      <p className="text-[11px] text-text-muted italic">
        No shared properties. Add at least one to expose required fields on
        every implementing object type.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {items.map((sp, idx) => (
        <div
          key={idx}
          className="grid grid-cols-[1fr_10rem_6rem_auto] gap-2 items-center"
        >
          <input
            aria-label={`Shared property ${idx + 1} api name`}
            type="text"
            placeholder="apiName"
            value={sp.apiName}
            onChange={(e) => {
              const next = [...items];
              next[idx] = { ...sp, apiName: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' font-mono text-xs'}
          />
          <select
            aria-label={`Shared property ${idx + 1} base type`}
            value={sp.baseType}
            onChange={(e) => {
              const next = [...items];
              next[idx] = { ...sp, baseType: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' text-xs'}
          >
            {BASE_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          <label className="flex items-center gap-1 text-[11px] text-text-secondary">
            <input
              type="checkbox"
              checked={!!sp.isArray}
              onChange={(e) => {
                const next = [...items];
                next[idx] = { ...sp, isArray: e.target.checked };
                onChange(next);
              }}
            />
            Array
          </label>
          <button
            type="button"
            aria-label={`Remove shared property ${idx + 1}`}
            onClick={() => onChange(items.filter((_, i) => i !== idx))}
            className="px-2 py-1 text-[11px] rounded text-accent-error hover:bg-accent-error/10"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}

function OutgoingLinkTypesEditor({
  items,
  onChange,
}: {
  items: InterfaceOutgoingLinkType[];
  onChange: (next: InterfaceOutgoingLinkType[]) => void;
}) {
  if (items.length === 0) {
    return (
      <p className="text-[11px] text-text-muted italic">
        No outgoing link types. Add one to expose a link from every
        implementing object type to a target type.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      {items.map((lt, idx) => (
        <div
          key={idx}
          className="grid grid-cols-[1fr_1fr_10rem_8rem_auto] gap-2 items-center"
        >
          <input
            aria-label={`Link type ${idx + 1} api name`}
            type="text"
            placeholder="apiName"
            value={lt.apiName}
            onChange={(e) => {
              const next = [...items];
              next[idx] = { ...lt, apiName: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' font-mono text-xs'}
          />
          <input
            aria-label={`Link type ${idx + 1} target type`}
            type="text"
            placeholder="Target object type"
            value={lt.linkedEntityTypeApiName}
            onChange={(e) => {
              const next = [...items];
              next[idx] = {
                ...lt,
                linkedEntityTypeApiName: e.target.value,
              };
              onChange(next);
            }}
            className={inputClass + ' font-mono text-xs'}
          />
          <input
            aria-label={`Link type ${idx + 1} display name`}
            type="text"
            placeholder="Display name"
            value={lt.displayName}
            onChange={(e) => {
              const next = [...items];
              next[idx] = { ...lt, displayName: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' text-xs'}
          />
          <select
            aria-label={`Link type ${idx + 1} cardinality`}
            value={lt.cardinality}
            onChange={(e) => {
              const next = [...items];
              next[idx] = {
                ...lt,
                cardinality: e.target.value as 'ONE' | 'MANY',
              };
              onChange(next);
            }}
            className={inputClass + ' text-xs'}
          >
            {LINK_CARDINALITIES.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
          <button
            type="button"
            aria-label={`Remove link type ${idx + 1}`}
            onClick={() => onChange(items.filter((_, i) => i !== idx))}
            className="px-2 py-1 text-[11px] rounded text-accent-error hover:bg-accent-error/10"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}

function DeleteInterfaceModal({
  ontologyApiName,
  iface,
  onClose,
}: {
  ontologyApiName: string;
  iface: OntologyInterface;
  onClose: () => void;
}) {
  const del = useDeleteInterface(ontologyApiName);
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync(iface.rid);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Interface">
      <div
        data-testid="interface-delete-modal"
        data-interface-api-name={iface.apiName}
        data-interface-rid={iface.rid}
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{iface.displayName}</span>{' '}
          <span className="text-xs text-text-secondary font-mono">
            ({iface.apiName})
          </span>
          ?
        </p>
        <p className="text-xs text-text-secondary">
          ObjectTypes that implement this interface will lose its shared
          properties and outgoing link types. This cannot be undone.
        </p>
        {submitError && (
          <p
            role="alert"
            data-testid="interface-delete-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="interface-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="interface-delete-confirm"
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

function ImplementingObjectTypesModal({
  ontologyApiName,
  iface,
  objectTypes,
  onClose,
}: {
  ontologyApiName: string;
  iface: OntologyInterface;
  objectTypes: ObjectType[];
  onClose: () => void;
}) {
  const attached = useAttachmentsByInterface(
    ontologyApiName,
    iface.rid,
    objectTypes,
  );
  const attach = useAttachInterface(ontologyApiName);
  const detach = useDetachInterface(ontologyApiName);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const attachedSet = useMemo(
    () => new Set(attached.map((a) => a.objectTypeRid)),
    [attached],
  );
  const available = objectTypes.filter((ot) => !attachedSet.has(ot.rid));
  const [selected, setSelected] = useState<string>('');

  async function onAttach() {
    if (!selected) return;
    setSubmitError(null);
    try {
      await attach.mutateAsync({
        objectTypeRid: selected,
        interfaceRid: iface.rid,
      });
      setSelected('');
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  async function onDetach(objectTypeRid: string) {
    setSubmitError(null);
    try {
      await detach.mutateAsync({
        objectTypeRid,
        interfaceRid: iface.rid,
      });
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title={`Implementing — ${iface.displayName}`} size="lg">
      <div
        data-testid="interface-implementing-modal"
        data-interface-api-name={iface.apiName}
        data-interface-rid={iface.rid}
        className="flex flex-col gap-4"
      >
        <section
          data-testid="interface-implementing-attached-section"
          className="flex flex-col gap-2"
        >
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            Currently implementing
          </span>
          {attached.length === 0 ? (
            <p
              data-testid="interface-implementing-empty"
              className="text-xs text-text-muted italic"
            >
              No ObjectType implements this interface yet.
            </p>
          ) : (
            <ul
              data-testid="interface-implementing-list"
              className="flex flex-col gap-1"
            >
              {attached.map((a) => {
                const ot = objectTypes.find((o) => o.rid === a.objectTypeRid);
                return (
                  <li
                    key={a.objectTypeRid}
                    data-testid="interface-implementing-row"
                    data-object-type-rid={a.objectTypeRid}
                    data-object-type-api-name={ot?.apiName ?? ''}
                    className="flex items-center gap-3 px-3 py-2 rounded border"
                    style={{
                      borderColor: 'rgba(31,41,55,0.5)',
                      background: 'rgba(13,17,23,0.4)',
                    }}
                  >
                    <span className="flex-1 text-sm text-text-primary">
                      {ot?.displayName ?? a.objectTypeRid}
                      <span className="ml-2 text-[11px] font-mono text-text-secondary">
                        ({ot?.apiName ?? ''})
                      </span>
                    </span>
                    <button
                      type="button"
                      data-testid="interface-implementing-detach-btn"
                      data-object-type-api-name={ot?.apiName ?? ''}
                      aria-label={`Detach ${ot?.apiName ?? a.objectTypeRid}`}
                      onClick={() => onDetach(a.objectTypeRid)}
                      disabled={detach.isPending}
                      className="px-2 py-1 text-[11px] rounded text-accent-error hover:bg-accent-error/10 disabled:opacity-40"
                    >
                      Detach
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
        <section className="flex flex-col gap-2">
          <span className="text-[10px] uppercase tracking-widest text-text-secondary">
            Attach an ObjectType
          </span>
          <div className="flex items-center gap-2">
            <select
              aria-label="Object type to attach"
              data-testid="interface-implementing-attach-select"
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
              className={inputClass + ' text-xs flex-1'}
            >
              <option value="">Select object type…</option>
              {available.map((ot) => (
                <option key={ot.rid} value={ot.rid}>
                  {ot.displayName} ({ot.apiName})
                </option>
              ))}
            </select>
            <button
              type="button"
              data-testid="interface-implementing-attach-btn"
              onClick={onAttach}
              disabled={!selected || attach.isPending}
              className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {attach.isPending ? 'Attaching…' : 'Attach'}
            </button>
          </div>
          {available.length === 0 && (
            <p className="text-[11px] text-text-muted italic">
              Every ObjectType in the ontology already implements this
              interface.
            </p>
          )}
        </section>
        {submitError && (
          <p
            role="alert"
            data-testid="interface-implementing-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end">
          <button
            type="button"
            data-testid="interface-implementing-close"
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

// useAttachmentsByInterface loads attachments across all ObjectTypes and
// filters to the ones attached to this interface. The backend route is
// objectType → interfaces, so we fan out per object type.
function useAttachmentsByInterface(
  ontologyApiName: string,
  interfaceRid: string,
  objectTypes: ObjectType[],
): ObjectTypeInterface[] {
  const [state, setState] = useState<ObjectTypeInterface[]>([]);
  const key = objectTypes.map((o) => o.rid).join('|');

  useEffect(() => {
    let cancelled = false;
    if (!ontologyApiName || objectTypes.length === 0) {
      setState([]);
      return () => {
        cancelled = true;
      };
    }
    Promise.all(
      objectTypes.map((ot) =>
        listObjectTypeInterfaces(ontologyApiName, ot.rid)
          .then((rows) => rows.filter((a) => a.interfaceRid === interfaceRid))
          .catch(() => [] as ObjectTypeInterface[]),
      ),
    ).then((rows) => {
      if (cancelled) return;
      setState(rows.flat());
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ontologyApiName, interfaceRid, key]);

  return state;
}

function autoApiName(displayName: string): string {
  const words = displayName.trim().split(/\s+/);
  if (words.length === 0 || words[0] === '') return '';
  // Interfaces conventionally use PascalCase, but keep tolerance with the
  // other admin pages which use camelCase. Prefer the simple camelCase path
  // to match the rest of the repo.
  const first = words[0].toLowerCase();
  const rest = words.slice(1).map(
    (w) => (w ? w[0].toUpperCase() + w.slice(1).toLowerCase() : ''),
  );
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

function Section({
  title,
  action,
  testId,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  testId?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2" data-testid={testId}>
      <div className="flex items-center gap-2">
        <span className="text-[10px] uppercase tracking-widest text-text-secondary">
          {title}
        </span>
        <div className="flex-1" />
        {action}
      </div>
      {children}
    </div>
  );
}
