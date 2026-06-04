import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import type {
  ObjectType,
  LinkType,
  ActionType,
  ActionLog,
  Classification,
} from '../../api/types';
import { CLASSIFICATION_VALUES } from '../../api/types';
import {
  listOutgoingLinkTypes,
  listActionTypes,
  getResolvedObjectType,
  postObjectTypeEditsHistory,
  type CreateObjectTypeRequest,
  type UpdateObjectTypeRequest,
  type ResolvedObjectType,
  type ResolvedProperty,
  type ResolvedOutgoingLink,
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
import { MarkdownEditor } from '../common/MarkdownEditor';
import { PropertiesEditor } from './PropertiesEditor';
import { BindingsEditor } from './BindingsEditor';

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

// rfc3339ToDatetimeLocal converts a stored RFC3339 deprecation deadline into
// the "YYYY-MM-DDTHH:mm" shape an `<input type="datetime-local">` displays.
// The control speaks local wall-clock time, so we derive the components from
// the parsed Date in local time — symmetric with datetimeLocalToRFC3339, which
// reads that same control's value back out via `new Date(local).toISOString()`.
// Returns '' for empty/unparseable input so the control renders blank.
function rfc3339ToDatetimeLocal(value: string | null | undefined): string {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  );
}

// datetimeLocalToRFC3339 turns the control's local "YYYY-MM-DDTHH:mm" value
// into the RFC3339 instant the backend's time.Parse(time.RFC3339) accepts
// (pkg/oms/admin_handlers.go). `new Date(local)` interprets the value in the
// browser's local timezone; `.toISOString()` yields a UTC `...Z` string such
// as "2026-06-03T12:00:00.000Z", which is valid RFC3339. Returns null for a
// blank/unparseable control so the PUT body clears the stored deadline.
function datetimeLocalToRFC3339(local: string): string | null {
  if (!local) return null;
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return null;
  return d.toISOString();
}

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
      <div
        data-testid="object-type-admin-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="object-type-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
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
          data-testid="object-type-new-btn"
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
          <div
            data-testid="object-type-admin-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="object-type-admin-error"
            className="text-sm text-accent-error"
          >
            Failed to load object types: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            data-testid="object-type-admin-empty"
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
          existing={objectTypes ?? []}
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
      data-testid="object-type-admin-table"
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
              data-testid="object-type-row"
              data-object-type-api-name={ot.apiName}
              data-object-type-rid={ot.rid}
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
                  data-testid="object-type-edit-btn"
                  data-object-type-api-name={ot.apiName}
                  onClick={() => onEdit(ot)}
                  className="text-xs text-accent-cyan hover:underline mr-3"
                >
                  Edit
                </button>
                <button
                  type="button"
                  data-testid="object-type-delete-btn"
                  data-object-type-api-name={ot.apiName}
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
  // Comma- / whitespace-separated PK property apiNames. A single entry maps
  // to the legacy `primaryKey`; more than one becomes a composite `primaryKeys`.
  primaryKey: string;
  titleProperty: string;
  status: (typeof STATUS_VALUES)[number];
  visibility: (typeof VISIBILITY_VALUES)[number];
  classification: '' | Classification;
  extendsRid: string;
  auditDataAccess: boolean;
  apiNameDirty: boolean;
  pluralDirty: boolean;
}

// US-211: split a free-form primary-key entry ("orderId, lineNumber" or
// "orderId lineNumber") into an ordered, de-duplicated list of apiNames.
function parsePrimaryKeys(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const tok of raw.split(/[\s,]+/)) {
    const t = tok.trim();
    if (t && !seen.has(t)) {
      seen.add(t);
      out.push(t);
    }
  }
  return out;
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
    classification: '',
    extendsRid: '',
    auditDataAccess: false,
    apiNameDirty: false,
    pluralDirty: false,
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  const apiNameTaken = useMemo(
    () => new Set(existing.map((ot) => ot.apiName)),
    [existing],
  );

  // US-212: inheritance candidates are every existing ObjectType in the
  // ontology (a brand-new type can't be its own parent yet, so nothing is
  // excluded here). Sorted by display name for a stable dropdown.
  const extendsCandidates = useMemo(
    () =>
      [...existing].sort((a, b) =>
        a.displayName.localeCompare(b.displayName),
      ),
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
    // US-211: a single-element key stays on the legacy `primaryKey`; only a
    // genuine composite key (>1 field) sends `primaryKeys`. The backend
    // prefers `primaryKeys` and rewrites `primaryKey` to its first element,
    // so we keep `primaryKey` populated as the single-key fallback.
    const pkList = parsePrimaryKeys(form.primaryKey);
    const body: CreateObjectTypeRequest = {
      apiName: form.apiName.trim(),
      displayName: form.displayName.trim(),
      pluralDisplayName: form.pluralDisplayName.trim() || undefined,
      description: form.description.trim() || undefined,
      primaryKey: pkList[0] ?? form.primaryKey.trim(),
      ...(pkList.length > 1 ? { primaryKeys: pkList } : {}),
      titleProperty: form.titleProperty.trim() || undefined,
      status: form.status,
      visibility: form.visibility,
      classification: form.classification || undefined,
      ...(form.extendsRid ? { extendsRid: form.extendsRid } : {}),
      ...(form.auditDataAccess ? { auditDataAccess: true } : {}),
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
      <form
        onSubmit={onSubmit}
        data-testid="object-type-create-form"
        className="flex flex-col gap-3"
      >
        <Field label="Display Name" required>
          <input
            type="text"
            data-testid="object-type-create-display-name"
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
            data-testid="object-type-create-api-name"
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
        <Field
          label="Primary Key"
          required
          hint="apiName of the PK property. For a composite key, list multiple apiNames separated by commas."
        >
          <input
            type="text"
            data-testid="object-type-create-primary-key"
            value={form.primaryKey}
            onChange={(e) =>
              setForm((f) => ({ ...f, primaryKey: e.target.value }))
            }
            required
            className={inputClass + ' font-mono'}
          />
        </Field>
        <Field
          label="Extends"
          hint="Optional — parent ObjectType to inherit properties and links from."
        >
          <select
            aria-label="Extends"
            data-testid="object-type-create-extends"
            value={form.extendsRid}
            onChange={(e) =>
              setForm((f) => ({ ...f, extendsRid: e.target.value }))
            }
            className={inputClass}
          >
            <option value="">— None —</option>
            {extendsCandidates.map((ot) => (
              <option key={ot.rid} value={ot.rid}>
                {ot.displayName} ({ot.apiName})
              </option>
            ))}
          </select>
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
          <MarkdownEditor
            value={form.description}
            onChange={(next) =>
              setForm((f) => ({ ...f, description: next }))
            }
            placeholder="Describe this object type — Markdown supported"
            ariaLabel="Description (Markdown)"
            testId="object-type-description-editor"
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
        <Field
          label="Classification"
          hint="Data classification label (optional)."
        >
          <select
            aria-label="Classification"
            value={form.classification}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                classification: e.target.value as CreateFormState['classification'],
              }))
            }
            className={inputClass}
          >
            <option value="">— Unspecified —</option>
            {CLASSIFICATION_VALUES.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </Field>
        <ToggleField
          label="Audit data access"
          checked={form.auditDataAccess}
          onChange={(checked) =>
            setForm((f) => ({ ...f, auditDataAccess: checked }))
          }
          testId="object-type-create-audit-data-access"
          hint="Emit an audit event for every successful read of objects of this type."
        />
        {submitError && (
          <p
            role="alert"
            data-testid="object-type-create-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="object-type-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="object-type-create-submit"
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
  deprecatedReason: string;
  // Local "YYYY-MM-DDTHH:mm" value backing the datetime-local control; '' when
  // no deadline is set. Serialized back to RFC3339 on submit.
  deprecatedDeadline: string;
  iconName: string;
  color: string;
  classification: '' | Classification;
  extendsRid: string;
  auditDataAccess: boolean;
}

function EditObjectTypeModal({
  ontologyApiName,
  objectType,
  existing,
  onClose,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
  existing: ObjectType[];
  onClose: () => void;
}) {
  const update = useUpdateObjectType(ontologyApiName);
  const [tab, setTab] = useState<
    'details' | 'properties' | 'bindings' | 'resolved' | 'history'
  >('details');
  const [form, setForm] = useState<EditFormState>({
    displayName: objectType.displayName,
    pluralDisplayName: objectType.pluralDisplayName ?? '',
    description: objectType.description ?? '',
    titleProperty: objectType.titleProperty ?? '',
    status: objectType.status,
    visibility: objectType.visibility,
    deprecatedReason: objectType.deprecatedReason ?? '',
    // Convert the stored RFC3339 deadline back to the datetime-local shape so
    // the control preloads the existing value.
    deprecatedDeadline: rfc3339ToDatetimeLocal(objectType.deprecatedDeadline),
    iconName: objectType.icon ?? '',
    color: objectType.color ?? '',
    classification: objectType.classification ?? '',
    extendsRid: objectType.extendsRid ?? '',
    auditDataAccess: objectType.auditDataAccess ?? false,
  });
  const [submitError, setSubmitError] = useState<string | null>(null);

  // US-212: a type cannot extend itself, so the current ObjectType is
  // excluded from the candidate list. (The backend additionally rejects
  // cycles; this filter just prevents the most obvious self-loop in the UI.)
  const extendsCandidates = useMemo(
    () =>
      existing
        .filter((ot) => ot.rid !== objectType.rid)
        .sort((a, b) => a.displayName.localeCompare(b.displayName)),
    [existing, objectType.rid],
  );

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
      // Records *why* a type was deprecated. Backend persists this verbatim
      // (updated.DeprecatedReason = req.DeprecatedReason); send the trimmed
      // value, or undefined when blank so the omitempty field is dropped.
      deprecatedReason: form.deprecatedReason.trim() || undefined,
      // Records *when* a deprecated type should be retired. The backend parses
      // a non-empty value with time.Parse(time.RFC3339, ...) and clears the
      // stored deadline on null/empty. Send the control's local value as an
      // RFC3339 instant, or null when blank so the backend wipes it.
      deprecatedDeadline: datetimeLocalToRFC3339(form.deprecatedDeadline),
      // Backend reads the icon under the `icon` JSON key (IconName
      // `json:"icon"`); sending `iconName` was silently dropped server-side.
      icon: form.iconName.trim() || undefined,
      color: form.color.trim() || undefined,
      // US-262: always send the current form value so the tri-state
      // ("" = clear, known label = assign) lands on the backend. Omitting
      // the field would preserve the existing value on the server, but
      // that's indistinguishable from "user left the dropdown alone" in
      // this UI, where any change the admin makes in the form should be
      // the new authoritative value.
      classification: form.classification,
      // US-212: same rationale as classification — the dropdown's current
      // value is authoritative. "" clears the parent pointer on the backend.
      extendsRid: form.extendsRid,
      // US-264: always send the toggle's current state so the backend
      // tri-state pointer receives an explicit bool.
      auditDataAccess: form.auditDataAccess,
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
      <div
        data-testid="object-type-edit-tabs"
        className="flex gap-2 border-b mb-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
        role="tablist"
      >
        <button
          type="button"
          role="tab"
          data-testid="object-type-edit-tab-details"
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
          data-testid="object-type-edit-tab-properties"
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
        <button
          type="button"
          role="tab"
          data-testid="object-type-edit-tab-bindings"
          aria-selected={tab === 'bindings'}
          onClick={() => setTab('bindings')}
          className={`px-3 py-1.5 text-xs font-semibold border-b-2 -mb-px ${
            tab === 'bindings'
              ? 'border-accent-cyan text-accent-cyan'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          Bindings
        </button>
        <button
          type="button"
          role="tab"
          data-testid="object-type-edit-tab-resolved"
          aria-selected={tab === 'resolved'}
          onClick={() => setTab('resolved')}
          className={`px-3 py-1.5 text-xs font-semibold border-b-2 -mb-px ${
            tab === 'resolved'
              ? 'border-accent-cyan text-accent-cyan'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          Resolved
        </button>
        <button
          type="button"
          role="tab"
          data-testid="object-type-edit-tab-history"
          aria-selected={tab === 'history'}
          onClick={() => setTab('history')}
          className={`px-3 py-1.5 text-xs font-semibold border-b-2 -mb-px ${
            tab === 'history'
              ? 'border-accent-cyan text-accent-cyan'
              : 'border-transparent text-text-secondary hover:text-text-primary'
          }`}
        >
          History
        </button>
      </div>
      {tab === 'properties' ? (
        <PropertiesEditor
          ontologyApiName={ontologyApiName}
          objectType={objectType}
        />
      ) : tab === 'bindings' ? (
        <BindingsEditor
          ontologyApiName={ontologyApiName}
          objectType={objectType}
        />
      ) : tab === 'resolved' ? (
        <ResolvedView
          ontologyApiName={ontologyApiName}
          objectType={objectType}
        />
      ) : tab === 'history' ? (
        <HistoryView
          ontologyApiName={ontologyApiName}
          objectType={objectType}
        />
      ) : (
      <form
        onSubmit={onSubmit}
        data-testid="object-type-edit-form"
        className="flex flex-col gap-3"
      >
        <div
          data-testid="object-type-edit-api-name"
          className="text-xs text-text-secondary font-mono"
        >
          {objectType.apiName}
        </div>
        <Field label="Display Name" required>
          <input
            type="text"
            data-testid="object-type-edit-display-name"
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
          <MarkdownEditor
            value={form.description}
            onChange={(next) =>
              setForm((f) => ({ ...f, description: next }))
            }
            placeholder="Describe this object type — Markdown supported"
            ariaLabel="Description (Markdown)"
            testId="object-type-description-editor-edit"
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
        <Field
          label="Deprecation reason"
          hint="Recorded when deprecating a type — explain why and what replaces it."
        >
          <textarea
            aria-label="Deprecation reason"
            data-testid="object-type-edit-deprecated-reason"
            value={form.deprecatedReason}
            onChange={(e) =>
              setForm((f) => ({ ...f, deprecatedReason: e.target.value }))
            }
            rows={2}
            className={inputClass}
          />
        </Field>
        <Field
          label="Deprecation deadline"
          hint="Optional date/time by which this type should be retired. Leave blank to clear."
        >
          <input
            type="datetime-local"
            aria-label="Deprecation deadline"
            data-testid="object-type-edit-deprecated-deadline"
            value={form.deprecatedDeadline}
            onChange={(e) =>
              setForm((f) => ({ ...f, deprecatedDeadline: e.target.value }))
            }
            className={inputClass}
          />
        </Field>
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
        <Field
          label="Classification"
          hint="Data classification label (optional)."
        >
          <select
            aria-label="Classification"
            value={form.classification}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                classification: e.target.value as EditFormState['classification'],
              }))
            }
            className={inputClass}
          >
            <option value="">— Unspecified —</option>
            {CLASSIFICATION_VALUES.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </Field>
        <Field
          label="Extends"
          hint="Optional — parent ObjectType to inherit properties and links from."
        >
          <select
            aria-label="Extends"
            data-testid="object-type-edit-extends"
            value={form.extendsRid}
            onChange={(e) =>
              setForm((f) => ({ ...f, extendsRid: e.target.value }))
            }
            className={inputClass}
          >
            <option value="">— None —</option>
            {extendsCandidates.map((ot) => (
              <option key={ot.rid} value={ot.rid}>
                {ot.displayName} ({ot.apiName})
              </option>
            ))}
          </select>
        </Field>
        <ToggleField
          label="Audit data access"
          checked={form.auditDataAccess}
          onChange={(checked) =>
            setForm((f) => ({ ...f, auditDataAccess: checked }))
          }
          testId="object-type-edit-audit-data-access"
          hint="Emit an audit event for every successful read of objects of this type."
        />
        {submitError && (
          <p
            role="alert"
            data-testid="object-type-edit-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="object-type-edit-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="object-type-edit-submit"
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
      <div
        data-testid="object-type-delete-modal"
        data-object-type-api-name={objectType.apiName}
        className="flex flex-col gap-3"
      >
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
          <p
            role="alert"
            data-testid="object-type-delete-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="object-type-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="object-type-delete-confirm"
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

// ToggleField renders a labeled checkbox styled to match the admin form. The
// label text is associated with the input via the wrapping <label> so RTL's
// getByLabelText resolves the checkbox.
function ToggleField({
  label,
  checked,
  onChange,
  hint,
  testId,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  hint?: string;
  testId?: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary">
      <span className="flex items-center gap-2">
        <input
          type="checkbox"
          data-testid={testId}
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          className="h-3.5 w-3.5 rounded border-transparent bg-bg-tertiary accent-accent-cyan"
        />
        <span className="uppercase tracking-widest">{label}</span>
      </span>
      {hint && <span className="text-[11px] text-text-muted">{hint}</span>}
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

// US-499: Resolved view — render properties + outgoing links from the
// backend `/resolved` endpoint with `inheritedFrom` provenance surfaced
// on rows that originated in an ancestor ObjectType.
function ResolvedView({
  ontologyApiName,
  objectType,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
}) {
  const { data, isLoading, error } = useQuery<ResolvedObjectType>({
    queryKey: ['objectType', 'resolved', ontologyApiName, objectType.apiName],
    queryFn: () => getResolvedObjectType(ontologyApiName, objectType.apiName),
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-10">
        <LoadingSpinner size="md" />
      </div>
    );
  }
  if (error) {
    return (
      <p
        role="alert"
        data-testid="object-type-resolved-error"
        className="text-xs text-accent-error"
      >
        Failed to load resolved view: {(error as Error).message}
      </p>
    );
  }
  if (!data) return null;

  const properties = Object.entries(data.properties).sort(([a], [b]) =>
    a.localeCompare(b),
  );

  return (
    <div
      data-testid="object-type-resolved-panel"
      className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto"
    >
      {data.extendsChain && data.extendsChain.length > 0 && (
        <div
          data-testid="resolved-extends-chain"
          className="text-[11px] text-text-secondary"
        >
          <span className="uppercase tracking-widest">Extends:</span>{' '}
          <span className="font-mono">{data.extendsChain.join(' → ')}</span>
        </div>
      )}

      <section>
        <h3 className="text-[10px] uppercase tracking-widest text-text-secondary mb-2">
          Resolved Properties ({properties.length})
        </h3>
        <div
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
                <th className="text-left px-3 py-2 font-medium">API Name</th>
                <th className="text-left px-3 py-2 font-medium">Type</th>
                <th className="text-left px-3 py-2 font-medium">Source</th>
              </tr>
            </thead>
            <tbody>
              {properties.map(([apiName, p]) => (
                <PropertyRow key={apiName} apiName={apiName} property={p} />
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h3 className="text-[10px] uppercase tracking-widest text-text-secondary mb-2">
          Resolved Outgoing Links ({data.outgoingLinkTypes.length})
        </h3>
        <div
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
                <th className="text-left px-3 py-2 font-medium">API Name</th>
                <th className="text-left px-3 py-2 font-medium">Target</th>
                <th className="text-left px-3 py-2 font-medium">Cardinality</th>
                <th className="text-left px-3 py-2 font-medium">Source</th>
              </tr>
            </thead>
            <tbody>
              {data.outgoingLinkTypes.map((lt) => (
                <LinkRow key={lt.rid} link={lt} />
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function PropertyRow({
  apiName,
  property,
}: {
  apiName: string;
  property: ResolvedProperty;
}) {
  const dt = property.dataType as { type?: string } | undefined;
  return (
    <tr
      data-testid="resolved-property-row"
      data-property-api-name={apiName}
      className="border-b last:border-0"
      style={{ borderColor: 'rgba(31,41,55,0.5)' }}
    >
      <td className="px-3 py-2 font-mono text-xs text-text-primary">
        {apiName}
      </td>
      <td className="px-3 py-2 font-mono text-xs text-text-secondary">
        {dt?.type ?? '—'}
      </td>
      <td className="px-3 py-2 text-xs">
        {property.inheritedFrom ? (
          <span
            data-testid="resolved-property-inherited-from"
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-accent-cyan/10 text-accent-cyan text-[10px] font-mono"
            title={property.inheritedFrom}
          >
            inherited
          </span>
        ) : (
          <span className="text-text-muted text-[10px]">own</span>
        )}
      </td>
    </tr>
  );
}

function LinkRow({ link }: { link: ResolvedOutgoingLink }) {
  return (
    <tr
      data-testid="resolved-link-row"
      data-link-api-name={link.apiName}
      className="border-b last:border-0"
      style={{ borderColor: 'rgba(31,41,55,0.5)' }}
    >
      <td className="px-3 py-2 font-mono text-xs text-text-primary">
        {link.apiName}
      </td>
      <td className="px-3 py-2 font-mono text-xs text-text-secondary">
        {link.linkedObjectTypeApiName}
      </td>
      <td className="px-3 py-2 text-xs text-text-secondary">
        {link.cardinality}
      </td>
      <td className="px-3 py-2 text-xs">
        {link.inheritedFrom ? (
          <span
            data-testid="resolved-link-inherited-from"
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-accent-cyan/10 text-accent-cyan text-[10px] font-mono"
            title={link.inheritedFrom}
          >
            inherited
          </span>
        ) : (
          <span className="text-text-muted text-[10px]">own</span>
        )}
      </td>
    </tr>
  );
}

// US-499: History view — list action_logs for this ObjectType sorted by
// createdAt descending (most-recent-first). Backend POSTs the rows in
// repository default order, so the UI applies the time-sort contract
// here. Failed entries surface an error indicator on the status cell.
function HistoryView({
  ontologyApiName,
  objectType,
}: {
  ontologyApiName: string;
  objectType: ObjectType;
}) {
  const { data, isLoading, error } = useQuery<ActionLog[]>({
    queryKey: [
      'objectType',
      'editsHistory',
      ontologyApiName,
      objectType.apiName,
    ],
    queryFn: () =>
      postObjectTypeEditsHistory(ontologyApiName, objectType.apiName),
  });

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort((a, b) => {
      const at = Date.parse(a.createdAt);
      const bt = Date.parse(b.createdAt);
      if (Number.isNaN(at) && Number.isNaN(bt)) return 0;
      if (Number.isNaN(at)) return 1;
      if (Number.isNaN(bt)) return -1;
      return bt - at;
    });
  }, [data]);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-10">
        <LoadingSpinner size="md" />
      </div>
    );
  }
  if (error) {
    return (
      <p
        role="alert"
        data-testid="object-type-history-error"
        className="text-xs text-accent-error"
      >
        Failed to load edit history: {(error as Error).message}
      </p>
    );
  }

  return (
    <div
      data-testid="object-type-history-panel"
      className="flex flex-col gap-2 max-h-[60vh] overflow-y-auto"
    >
      {sorted.length === 0 ? (
        <p
          data-testid="object-type-history-empty"
          className="text-xs text-text-secondary"
        >
          No edit history recorded for this ObjectType.
        </p>
      ) : (
        <div
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
                <th className="text-left px-3 py-2 font-medium">When</th>
                <th className="text-left px-3 py-2 font-medium">User</th>
                <th className="text-left px-3 py-2 font-medium">Action</th>
                <th className="text-left px-3 py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((log) => (
                <tr
                  key={log.id}
                  data-testid="history-row"
                  data-action-log-id={String(log.id)}
                  className="border-b last:border-0"
                  style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                >
                  <td className="px-3 py-2 text-xs text-text-secondary font-mono whitespace-nowrap">
                    {log.createdAt}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-primary font-mono">
                    {log.userId || '—'}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary font-mono truncate max-w-xs">
                    {log.actionTypeRid}
                  </td>
                  <td className="px-3 py-2 text-xs">
                    <span
                      data-testid="history-row-status"
                      className={
                        log.status === 'FAILED'
                          ? 'text-accent-error font-semibold'
                          : 'text-text-primary'
                      }
                    >
                      {log.status}
                    </span>
                    {log.status === 'FAILED' && log.errorMessage && (
                      <span
                        className="block text-[10px] text-accent-error/80 mt-0.5"
                        data-testid="history-row-error"
                      >
                        {log.errorMessage}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
