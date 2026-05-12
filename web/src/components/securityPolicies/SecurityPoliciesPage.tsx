import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import {
  isMaskExempt,
  isPolicyApplicable,
  KNOWN_MASK_RULES,
  type AppliesTo,
  type ColumnMask,
  type MaskRule,
  type RowPolicy,
  type SimulatedUser,
} from '../../api/securityPolicies';
import {
  useColumnMasks,
  useCreateColumnMask,
  useCreateRowPolicy,
  useDeleteColumnMask,
  useDeleteRowPolicy,
  useRowPolicies,
  useUpdateColumnMask,
  useUpdateRowPolicy,
} from '../../hooks/useSecurityPolicies';
import { useObjectType, useObjectTypes } from '../../hooks/useObjectTypes';
import { ApiRequestError } from '../../api/client';
import { useToastStore } from '../../stores/toastStore';
import { EmptyState } from '../common/EmptyState';
import { SkeletonTable } from '../common/Skeleton';

// US-041 (PC-A07a): Security Policies — Row Policies tab.
//
// `/admin/:ontology/security` renders the Security Policies shell with three
// tabs (Row Policies / Column Masks / Cell Masks). US-041 implements the
// Row Policies tab; the other two tabs are gated by US-042 / US-043 and
// surface an explicit placeholder so the route exists today and the future
// implementation can drop into the established slot.
//
// The PRD AC mentions a "CEL editor (语法高亮 + lint)" but the backend
// stores the predicate as a JSON-serialised pkg/oss/where.WhereClause —
// not a CEL expression. Honest mapping (per the US-025 / US-029 / US-033
// progress.txt patterns): the editor is a JSON editor with monospace font
// and JSON.parse-driven lint, mirroring the actual wire contract. If the
// repo ever swaps the predicate format to CEL, the editor swaps in place
// and the BDD spec keeps working — only the lint backend changes.

type TabId = 'row' | 'column' | 'cell';

const TABS: Array<{ id: TabId; label: string }> = [
  { id: 'row', label: 'Row Policies' },
  { id: 'column', label: 'Column Masks' },
  { id: 'cell', label: 'Cell Masks' },
];

export function SecurityPoliciesPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const activeOntology = ontology ?? '';
  const [tab, setTab] = useState<TabId>('row');

  if (!activeOntology) {
    return (
      <div
        data-testid="security-policies-empty-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to manage security policies."
        />
      </div>
    );
  }

  return (
    <div
      data-testid="security-policies-page"
      className="mx-auto max-w-7xl space-y-6"
    >
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
          Security Policies
        </h1>
        <p className="text-sm text-text-secondary">
          Manage row-level, column-level, and cell-level access controls for{' '}
          <span className="font-mono text-text-primary">{activeOntology}</span>.
        </p>
      </header>

      <div
        data-testid="security-policies-tabs"
        role="tablist"
        aria-label="Security Policies tabs"
        className="flex gap-1 border-b border-border/40"
      >
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            data-testid="security-policies-tab"
            data-tab-id={t.id}
            data-active={tab === t.id ? 'true' : 'false'}
            onClick={() => setTab(t.id)}
            className={`relative -mb-px rounded-t-md border border-b-0 px-4 py-2 text-sm font-medium transition-colors ${
              tab === t.id
                ? 'border-amber-500/40 bg-bg-tertiary text-amber-200'
                : 'border-transparent text-text-secondary hover:bg-bg-tertiary hover:text-text-primary'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div data-testid="security-policies-tab-panel" data-active-tab={tab}>
        {tab === 'row' && <RowPoliciesTab activeOntology={activeOntology} />}
        {tab === 'column' && <ColumnMasksTab activeOntology={activeOntology} />}
        {tab === 'cell' && <PlaceholderTab id="cell" />}
      </div>
    </div>
  );
}

function PlaceholderTab({ id }: { id: 'cell' }) {
  // Honest mapping: US-043 (Cell Masks) ships the remaining tab — surface
  // an explicit placeholder so the shell is usable now and the BDD spec
  // can lock the tab-routing contract end-to-end.
  const story = 'US-043';
  const label = 'Cell Masks';
  return (
    <div
      data-testid={`security-policies-${id}-placeholder`}
      className="rounded-lg border border-border/40 bg-bg-secondary/40 p-6 text-sm text-text-secondary"
    >
      <p>
        <span className="font-semibold text-text-primary">{label}</span>{' '}
        management is tracked under{' '}
        <span className="font-mono text-amber-300">{story}</span>.
      </p>
    </div>
  );
}

interface RowPoliciesTabProps {
  activeOntology: string;
}

function RowPoliciesTab({ activeOntology }: RowPoliciesTabProps) {
  const policiesQuery = useRowPolicies();
  const objectTypesQuery = useObjectTypes(activeOntology);

  const policies = useMemo(
    () => policiesQuery.data ?? [],
    [policiesQuery.data],
  );
  const objectTypes = useMemo(
    () => objectTypesQuery.data ?? [],
    [objectTypesQuery.data],
  );
  const objectTypeByRid = useMemo(() => {
    const m = new Map<string, { apiName: string; displayName: string }>();
    for (const ot of objectTypes) {
      m.set(ot.rid, { apiName: ot.apiName, displayName: ot.displayName });
    }
    return m;
  }, [objectTypes]);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<RowPolicy | null>(null);
  const [simulatorOpen, setSimulatorOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<RowPolicy | null>(null);

  const deleteMutation = useDeleteRowPolicy();
  const pushToast = useToastStore((s) => s.push);

  const onCreate = () => {
    setEditing(null);
    setEditorOpen(true);
  };
  const onEdit = (p: RowPolicy) => {
    setEditing(p);
    setEditorOpen(true);
  };
  const onConfirmDelete = () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    deleteMutation.mutate(target.rid, {
      onSuccess: () => {
        pushToast({
          message: `Deleted row policy ${target.rid}.`,
          severity: 'success',
        });
        setPendingDelete(null);
      },
      onError: (err) =>
        pushToast({ message: describeApiError(err), severity: 'error' }),
    });
  };

  return (
    <div
      data-testid="row-policies-tab"
      className="space-y-4 rounded-lg border border-border/40 bg-bg-secondary/40 p-5"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-text-primary">
            Row Policies
          </h2>
          <p className="text-xs text-text-secondary">
            Predicate filters that hide rows from non-applicable callers.
            Multiple applicable policies on the same ObjectType are
            OR-combined.
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          <button
            type="button"
            data-testid="row-policies-simulator-toggle"
            onClick={() => setSimulatorOpen((v) => !v)}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            {simulatorOpen ? 'Hide simulator' : 'Test as user'}
          </button>
          <button
            type="button"
            data-testid="row-policies-create-btn"
            onClick={onCreate}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500"
          >
            Create row policy
          </button>
        </div>
      </div>

      {simulatorOpen && (
        <SimulatorPanel
          policies={policies}
          objectTypeByRid={objectTypeByRid}
        />
      )}

      <RowPoliciesList
        query={policiesQuery}
        policies={policies}
        objectTypeByRid={objectTypeByRid}
        onEdit={onEdit}
        onDelete={(p) => setPendingDelete(p)}
      />

      {editorOpen && (
        <RowPolicyEditor
          existing={editing}
          objectTypes={objectTypes}
          onClose={() => {
            setEditorOpen(false);
            setEditing(null);
          }}
        />
      )}

      {pendingDelete && (
        <DeleteConfirmDialog
          target={pendingDelete}
          pending={deleteMutation.isPending}
          onCancel={() => setPendingDelete(null)}
          onConfirm={onConfirmDelete}
        />
      )}
    </div>
  );
}

function RowPoliciesList({
  query,
  policies,
  objectTypeByRid,
  onEdit,
  onDelete,
}: {
  query: ReturnType<typeof useRowPolicies>;
  policies: RowPolicy[];
  objectTypeByRid: Map<string, { apiName: string; displayName: string }>;
  onEdit: (p: RowPolicy) => void;
  onDelete: (p: RowPolicy) => void;
}) {
  if (query.isLoading) {
    return (
      <div data-testid="row-policies-loading">
        <SkeletonTable
          rows={3}
          columns={3}
          aria-label="Loading row policies"
        />
      </div>
    );
  }
  if (query.isError) {
    return (
      <div data-testid="row-policies-error">
        <EmptyState
          title="Failed to load row policies"
          description={
            query.error instanceof Error
              ? query.error.message
              : 'Unexpected error.'
          }
        />
      </div>
    );
  }
  if (policies.length === 0) {
    return (
      <div data-testid="row-policies-empty">
        <EmptyState
          title="No row policies yet"
          description="Create a row policy to restrict which rows callers can see."
        />
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-md border border-border/40">
      <table
        data-testid="row-policies-list"
        className="w-full text-sm"
      >
        <thead className="bg-bg-tertiary/60 text-left text-xs uppercase tracking-wider text-text-secondary">
          <tr>
            <th className="px-3 py-2">Object Type</th>
            <th className="px-3 py-2">Applies To</th>
            <th className="px-3 py-2">Description</th>
            <th className="px-3 py-2 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {policies.map((p) => {
            const ot = objectTypeByRid.get(p.objectTypeRid);
            return (
              <tr
                key={p.rid}
                data-testid="row-policies-row"
                data-policy-rid={p.rid}
                data-object-type-rid={p.objectTypeRid}
                data-object-type-api-name={ot?.apiName ?? ''}
                className="border-t border-border/30"
              >
                <td className="px-3 py-2 align-top">
                  <div className="font-mono text-text-primary">
                    {ot?.apiName ?? p.objectTypeRid}
                  </div>
                  {ot?.displayName && (
                    <div className="text-xs text-text-secondary">
                      {ot.displayName}
                    </div>
                  )}
                </td>
                <td className="px-3 py-2 align-top">
                  <AppliesToBadges applies={p.appliesTo} />
                </td>
                <td className="px-3 py-2 align-top text-xs text-text-secondary">
                  {p.description ? p.description : <em>—</em>}
                </td>
                <td className="px-3 py-2 align-top text-right">
                  <div className="flex justify-end gap-2">
                    <button
                      type="button"
                      data-testid="row-policies-edit-btn"
                      data-policy-rid={p.rid}
                      onClick={() => onEdit(p)}
                      className="rounded-md border border-border/60 px-2 py-1 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      data-testid="row-policies-delete-btn"
                      data-policy-rid={p.rid}
                      onClick={() => onDelete(p)}
                      className="rounded-md border border-rose-500/40 bg-rose-500/10 px-2 py-1 text-xs font-medium text-rose-300 hover:bg-rose-500/20"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function AppliesToBadges({
  applies,
  testidPrefix = 'row-policies',
}: {
  applies: AppliesTo;
  // Each surface (row-policies, column-masks) reuses this component but
  // expects its own testid prefix so BDD specs can scope per-tab without
  // cross-contamination. Defaults to row-policies for the original caller.
  testidPrefix?: 'row-policies' | 'column-masks';
}) {
  const roles = applies.roles ?? [];
  const groups = applies.groups ?? [];
  const users = applies.users ?? [];
  if (roles.length === 0 && groups.length === 0 && users.length === 0) {
    return (
      <span
        data-testid={`${testidPrefix}-applies-empty`}
        className="text-xs italic text-text-secondary"
      >
        {testidPrefix === 'column-masks'
          ? 'no allow list (mask applies to everyone)'
          : 'no scope (matches nobody)'}
      </span>
    );
  }
  return (
    <div className="flex flex-wrap gap-1">
      {roles.map((r) => (
        <span
          key={`r-${r}`}
          data-testid={`${testidPrefix}-applies-badge`}
          data-kind="role"
          data-value={r}
          className="inline-flex rounded-full border border-sky-500/40 bg-sky-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-sky-300"
        >
          role:{r}
        </span>
      ))}
      {groups.map((g) => (
        <span
          key={`g-${g}`}
          data-testid={`${testidPrefix}-applies-badge`}
          data-kind="group"
          data-value={g}
          className="inline-flex rounded-full border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-emerald-300"
        >
          group:{g}
        </span>
      ))}
      {users.map((u) => (
        <span
          key={`u-${u}`}
          data-testid={`${testidPrefix}-applies-badge`}
          data-kind="user"
          data-value={u}
          className="inline-flex rounded-full border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-amber-300"
        >
          user:{u}
        </span>
      ))}
    </div>
  );
}

interface RowPolicyEditorProps {
  existing: RowPolicy | null;
  objectTypes: Array<{ rid: string; apiName: string; displayName: string }>;
  onClose: () => void;
}

function RowPolicyEditor({
  existing,
  objectTypes,
  onClose,
}: RowPolicyEditorProps) {
  const isEdit = existing !== null;
  const createMutation = useCreateRowPolicy();
  const updateMutation = useUpdateRowPolicy();
  const pushToast = useToastStore((s) => s.push);

  const [objectTypeRid, setObjectTypeRid] = useState(
    existing?.objectTypeRid ?? objectTypes[0]?.rid ?? '',
  );
  const [predicateText, setPredicateText] = useState(
    formatPredicate(existing?.predicate),
  );
  const [predicateError, setPredicateError] = useState<string | null>(null);
  const [rolesText, setRolesText] = useState(
    (existing?.appliesTo.roles ?? []).join(', '),
  );
  const [groupsText, setGroupsText] = useState(
    (existing?.appliesTo.groups ?? []).join(', '),
  );
  const [usersText, setUsersText] = useState(
    (existing?.appliesTo.users ?? []).join(', '),
  );
  const [description, setDescription] = useState(existing?.description ?? '');
  const [submitError, setSubmitError] = useState<string | null>(null);

  const lintPredicate = (text: string): { value: unknown; error: string | null } => {
    const trimmed = text.trim();
    if (trimmed === '') {
      return { value: null, error: 'Predicate is required.' };
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed === null || typeof parsed !== 'object') {
        return {
          value: null,
          error: 'Predicate must be a JSON object (a where-clause).',
        };
      }
      return { value: parsed, error: null };
    } catch (err) {
      return {
        value: null,
        error:
          err instanceof Error
            ? `Invalid JSON: ${err.message}`
            : 'Invalid JSON.',
      };
    }
  };

  const onPredicateChange = (text: string) => {
    setPredicateText(text);
    const lint = lintPredicate(text);
    setPredicateError(lint.error);
  };

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const lint = lintPredicate(predicateText);
    if (lint.error) {
      setPredicateError(lint.error);
      return;
    }
    if (!objectTypeRid) {
      setSubmitError('Object Type is required.');
      return;
    }
    setSubmitError(null);
    const appliesTo: AppliesTo = {
      roles: parseList(rolesText),
      groups: parseList(groupsText),
      users: parseList(usersText),
    };
    const trimmedDesc = description.trim();

    if (isEdit && existing) {
      updateMutation.mutate(
        {
          rid: existing.rid,
          body: {
            predicate: lint.value,
            appliesTo,
            description: trimmedDesc,
          },
        },
        {
          onSuccess: () => {
            pushToast({
              message: 'Row policy updated.',
              severity: 'success',
            });
            onClose();
          },
          onError: (err) => {
            const msg = describeApiError(err);
            setSubmitError(msg);
            pushToast({ message: msg, severity: 'error' });
          },
        },
      );
    } else {
      createMutation.mutate(
        {
          objectTypeRid,
          predicate: lint.value,
          appliesTo,
          description: trimmedDesc,
        },
        {
          onSuccess: () => {
            pushToast({
              message: 'Row policy created.',
              severity: 'success',
            });
            onClose();
          },
          onError: (err) => {
            const msg = describeApiError(err);
            setSubmitError(msg);
            pushToast({ message: msg, severity: 'error' });
          },
        },
      );
    }
  };

  const submitting = createMutation.isPending || updateMutation.isPending;
  const canSubmit = !predicateError && (!isEdit ? !!objectTypeRid : true);

  return (
    <div
      data-testid="row-policy-editor"
      data-mode={isEdit ? 'edit' : 'create'}
      role="dialog"
      aria-modal="true"
      aria-label={isEdit ? 'Edit row policy' : 'Create row policy'}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/60"
    >
      <form
        data-testid="row-policy-editor-form"
        onSubmit={onSubmit}
        className="w-[36rem] max-w-full space-y-4 rounded-lg border border-border/60 bg-bg-primary p-5 shadow-xl"
      >
        <header className="space-y-1">
          <h2 className="text-base font-semibold text-text-primary">
            {isEdit ? 'Edit row policy' : 'Create row policy'}
          </h2>
          <p className="text-xs text-text-secondary">
            Predicates are JSON where-clauses (mirrors{' '}
            <span className="font-mono">pkg/oss/where.WhereClause</span>).
            They are AND-combined into reads so denied rows never
            materialise.
          </p>
        </header>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Object Type
          </span>
          <select
            data-testid="row-policy-editor-objectType"
            value={objectTypeRid}
            onChange={(e) => setObjectTypeRid(e.target.value)}
            disabled={isEdit}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary disabled:opacity-50"
          >
            {!objectTypeRid && <option value="">Select an object type…</option>}
            {objectTypes.map((ot) => (
              <option key={ot.rid} value={ot.rid}>
                {ot.apiName} ({ot.displayName})
              </option>
            ))}
          </select>
        </label>

        <label className="block">
          <span className="flex items-baseline justify-between text-xs font-medium text-text-secondary mb-1">
            <span>Predicate (JSON where-clause)</span>
            {predicateError ? (
              <span
                data-testid="row-policy-editor-predicate-error"
                className="text-rose-400"
              >
                {predicateError}
              </span>
            ) : (
              <span
                data-testid="row-policy-editor-predicate-ok"
                className="text-emerald-400"
              >
                ✓ valid
              </span>
            )}
          </span>
          <textarea
            data-testid="row-policy-editor-predicate"
            value={predicateText}
            onChange={(e) => onPredicateChange(e.target.value)}
            spellCheck={false}
            rows={6}
            className={`block w-full rounded-md border bg-bg-secondary/60 px-3 py-2 font-mono text-xs text-text-primary ${
              predicateError ? 'border-rose-500/60' : 'border-border/60'
            }`}
            placeholder='{"type":"eq","field":"status","value":"active"}'
          />
        </label>

        <fieldset className="grid grid-cols-1 gap-2 rounded-md border border-border/40 bg-bg-tertiary/30 p-3">
          <legend className="px-1 text-xs font-semibold uppercase tracking-wider text-text-secondary">
            Applies To (OR across roles, groups, users)
          </legend>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Roles (comma-separated)
            </span>
            <input
              type="text"
              data-testid="row-policy-editor-roles"
              value={rolesText}
              onChange={(e) => setRolesText(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
              placeholder="viewer, ontology-owner"
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Groups (comma-separated)
            </span>
            <input
              type="text"
              data-testid="row-policy-editor-groups"
              value={groupsText}
              onChange={(e) => setGroupsText(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
              placeholder="finance, eng"
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Users (id or email, comma-separated)
            </span>
            <input
              type="text"
              data-testid="row-policy-editor-users"
              value={usersText}
              onChange={(e) => setUsersText(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
              placeholder="alice@test, bob@test"
            />
          </label>
        </fieldset>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Description (optional)
          </span>
          <input
            type="text"
            data-testid="row-policy-editor-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
          />
        </label>

        {submitError && (
          <div
            data-testid="row-policy-editor-error"
            role="alert"
            className="rounded-md border border-rose-500/40 bg-rose-500/10 p-2 text-xs text-rose-200"
          >
            {submitError}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="row-policy-editor-cancel-btn"
            onClick={onClose}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="row-policy-editor-submit-btn"
            disabled={!canSubmit || submitting}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Create policy'}
          </button>
        </div>
      </form>
    </div>
  );
}

function DeleteConfirmDialog({
  target,
  pending,
  onCancel,
  onConfirm,
}: {
  target: RowPolicy;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div
      data-testid="row-policy-delete-dialog"
      role="dialog"
      aria-modal="true"
      aria-label="Confirm delete"
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/60"
    >
      <div className="w-[26rem] max-w-full space-y-4 rounded-lg border border-border/60 bg-bg-primary p-5 shadow-xl">
        <header className="space-y-1">
          <h2 className="text-base font-semibold text-text-primary">
            Delete row policy
          </h2>
          <p className="text-xs text-text-secondary">
            Removing{' '}
            <span
              data-testid="row-policy-delete-target-rid"
              className="font-mono text-text-primary"
            >
              {target.rid}
            </span>{' '}
            takes effect on the next read after the cache reload.
          </p>
        </header>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="row-policy-delete-cancel-btn"
            onClick={onCancel}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="row-policy-delete-confirm-btn"
            disabled={pending}
            onClick={onConfirm}
            className="rounded-md bg-rose-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-50"
          >
            {pending ? 'Deleting…' : 'Delete policy'}
          </button>
        </div>
      </div>
    </div>
  );
}

interface SimulatorPanelProps {
  policies: RowPolicy[];
  objectTypeByRid: Map<string, { apiName: string; displayName: string }>;
}

function SimulatorPanel({ policies, objectTypeByRid }: SimulatorPanelProps) {
  // Test-as-user simulator: there is no backend endpoint that takes a
  // hypothetical user and returns a policy decision (Engine.Compile()
  // evaluates internally at read time and never exposes per-policy hits).
  // Honest mapping: replicate AppliesTo.IsApplicable client-side and surface
  // which policies WOULD govern the simulated user. This keeps the gesture
  // useful for writing scopes while the backend keeps its authority over
  // actual reads.
  const [userId, setUserId] = useState('alice');
  const [email, setEmail] = useState('alice@test');
  const [rolesText, setRolesText] = useState('viewer');
  const [groupsText, setGroupsText] = useState('');

  const simulated: SimulatedUser = useMemo(
    () => ({
      id: userId.trim(),
      email: email.trim() || undefined,
      roles: parseList(rolesText),
      groups: parseList(groupsText),
    }),
    [userId, email, rolesText, groupsText],
  );

  const decisions = useMemo(
    () =>
      policies.map((p) => ({
        policy: p,
        applies: isPolicyApplicable(p.appliesTo, simulated),
      })),
    [policies, simulated],
  );
  const matchCount = decisions.filter((d) => d.applies).length;

  return (
    <div
      data-testid="row-policies-simulator"
      data-match-count={matchCount}
      className="space-y-3 rounded-md border border-sky-500/30 bg-sky-500/5 p-3"
    >
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-sky-200">
          Test as user — simulator
        </h3>
        <span className="text-[10px] text-text-secondary">
          Replicates pkg/rls AppliesTo.IsApplicable client-side. Backend
          reads remain authoritative.
        </span>
      </header>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            User ID
          </span>
          <input
            type="text"
            data-testid="row-policies-simulator-user-id"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            Email
          </span>
          <input
            type="text"
            data-testid="row-policies-simulator-email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            Roles (comma-separated)
          </span>
          <input
            type="text"
            data-testid="row-policies-simulator-roles"
            value={rolesText}
            onChange={(e) => setRolesText(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            Groups (comma-separated)
          </span>
          <input
            type="text"
            data-testid="row-policies-simulator-groups"
            value={groupsText}
            onChange={(e) => setGroupsText(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
      </div>
      <div
        data-testid="row-policies-simulator-summary"
        className="text-xs text-text-secondary"
      >
        <span
          data-testid="row-policies-simulator-match-count"
          className="font-mono text-text-primary"
        >
          {matchCount}
        </span>{' '}
        of {decisions.length} polic
        {decisions.length === 1 ? 'y applies' : 'ies apply'} to this user.
      </div>
      {decisions.length > 0 && (
        <ul
          data-testid="row-policies-simulator-decisions"
          className="space-y-1 text-xs"
        >
          {decisions.map((d) => {
            const ot = objectTypeByRid.get(d.policy.objectTypeRid);
            return (
              <li
                key={d.policy.rid}
                data-testid="row-policies-simulator-decision-row"
                data-policy-rid={d.policy.rid}
                data-applies={d.applies ? 'true' : 'false'}
                className={`flex items-center gap-2 rounded-md border px-2 py-1 ${
                  d.applies
                    ? 'border-emerald-500/40 bg-emerald-500/5'
                    : 'border-border/40 bg-bg-secondary/30'
                }`}
              >
                <span
                  data-testid="row-policies-simulator-decision-badge"
                  className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${
                    d.applies
                      ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/40'
                      : 'bg-bg-tertiary text-text-secondary border border-border/60'
                  }`}
                >
                  {d.applies ? 'applies' : 'no match'}
                </span>
                <span className="font-mono text-text-primary">
                  {ot?.apiName ?? d.policy.objectTypeRid}
                </span>
                {d.policy.description && (
                  <span className="truncate text-text-secondary">
                    — {d.policy.description}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function parseList(text: string): string[] {
  return text
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

function formatPredicate(predicate: unknown): string {
  if (predicate === undefined || predicate === null) {
    return '{\n  "type": "eq",\n  "field": "status",\n  "value": "active"\n}';
  }
  try {
    return JSON.stringify(predicate, null, 2);
  } catch {
    return '';
  }
}

function describeApiError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason ?? err.parameters?.error;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return 'Operation failed.';
}

// ===========================================================================
// US-042 (PC-A07b): Column Masks tab.
//
// Same 5-piece shape as Row Policies: list + simulator + create/edit
// editor modal + delete confirm dialog. Semantic flip vs row-policies
// (allow-list vs governance) is surfaced in the simulator panel header
// and the per-row decision badges so operators don't conflate the two.
// ===========================================================================

interface ColumnMasksTabProps {
  activeOntology: string;
}

function ColumnMasksTab({ activeOntology }: ColumnMasksTabProps) {
  const masksQuery = useColumnMasks();
  const objectTypesQuery = useObjectTypes(activeOntology);

  const masks = useMemo(() => masksQuery.data ?? [], [masksQuery.data]);
  const objectTypes = useMemo(
    () => objectTypesQuery.data ?? [],
    [objectTypesQuery.data],
  );
  const objectTypeByRid = useMemo(() => {
    const m = new Map<string, { apiName: string; displayName: string }>();
    for (const ot of objectTypes) {
      m.set(ot.rid, { apiName: ot.apiName, displayName: ot.displayName });
    }
    return m;
  }, [objectTypes]);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<ColumnMask | null>(null);
  const [simulatorOpen, setSimulatorOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<ColumnMask | null>(null);

  const deleteMutation = useDeleteColumnMask();
  const pushToast = useToastStore((s) => s.push);

  const onCreate = () => {
    setEditing(null);
    setEditorOpen(true);
  };
  const onEdit = (m: ColumnMask) => {
    setEditing(m);
    setEditorOpen(true);
  };
  const onConfirmDelete = () => {
    if (!pendingDelete) return;
    const target = pendingDelete;
    deleteMutation.mutate(target.rid, {
      onSuccess: () => {
        pushToast({
          message: `Deleted column mask ${target.rid}.`,
          severity: 'success',
        });
        setPendingDelete(null);
      },
      onError: (err) =>
        pushToast({ message: describeApiError(err), severity: 'error' }),
    });
  };

  return (
    <div
      data-testid="column-masks-tab"
      className="space-y-4 rounded-lg border border-border/40 bg-bg-secondary/40 p-5"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold text-text-primary">
            Column Masks
          </h2>
          <p className="text-xs text-text-secondary">
            Per-property value rewrites applied at response time. AppliesTo
            identifies callers ALLOWED to see the clear value; every other
            caller receives the masked value. Admins always see clear data.
          </p>
        </div>
        <div className="flex shrink-0 gap-2">
          <button
            type="button"
            data-testid="column-masks-simulator-toggle"
            onClick={() => setSimulatorOpen((v) => !v)}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            {simulatorOpen ? 'Hide simulator' : 'Test as user'}
          </button>
          <button
            type="button"
            data-testid="column-masks-create-btn"
            onClick={onCreate}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500"
          >
            Create column mask
          </button>
        </div>
      </div>

      {simulatorOpen && (
        <ColumnMasksSimulatorPanel
          masks={masks}
          objectTypeByRid={objectTypeByRid}
        />
      )}

      <ColumnMasksList
        query={masksQuery}
        masks={masks}
        objectTypeByRid={objectTypeByRid}
        onEdit={onEdit}
        onDelete={(m) => setPendingDelete(m)}
      />

      {editorOpen && (
        <ColumnMaskEditor
          existing={editing}
          objectTypes={objectTypes}
          activeOntology={activeOntology}
          onClose={() => {
            setEditorOpen(false);
            setEditing(null);
          }}
        />
      )}

      {pendingDelete && (
        <ColumnMaskDeleteDialog
          target={pendingDelete}
          pending={deleteMutation.isPending}
          onCancel={() => setPendingDelete(null)}
          onConfirm={onConfirmDelete}
        />
      )}
    </div>
  );
}

function ColumnMasksList({
  query,
  masks,
  objectTypeByRid,
  onEdit,
  onDelete,
}: {
  query: ReturnType<typeof useColumnMasks>;
  masks: ColumnMask[];
  objectTypeByRid: Map<string, { apiName: string; displayName: string }>;
  onEdit: (m: ColumnMask) => void;
  onDelete: (m: ColumnMask) => void;
}) {
  if (query.isLoading) {
    return (
      <div data-testid="column-masks-loading">
        <SkeletonTable
          rows={3}
          columns={4}
          aria-label="Loading column masks"
        />
      </div>
    );
  }
  if (query.isError) {
    return (
      <div data-testid="column-masks-error">
        <EmptyState
          title="Failed to load column masks"
          description={
            query.error instanceof Error
              ? query.error.message
              : 'Unexpected error.'
          }
        />
      </div>
    );
  }
  if (masks.length === 0) {
    return (
      <div data-testid="column-masks-empty">
        <EmptyState
          title="No column masks yet"
          description="Create a column mask to rewrite a property's value for non-allow-listed callers."
        />
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-md border border-border/40">
      <table data-testid="column-masks-list" className="w-full text-sm">
        <thead className="bg-bg-tertiary/60 text-left text-xs uppercase tracking-wider text-text-secondary">
          <tr>
            <th className="px-3 py-2">Object Type</th>
            <th className="px-3 py-2">Property</th>
            <th className="px-3 py-2">Mask Rule</th>
            <th className="px-3 py-2">Applies To (clear list)</th>
            <th className="px-3 py-2">Description</th>
            <th className="px-3 py-2 text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {masks.map((m) => {
            const ot = objectTypeByRid.get(m.objectTypeRid);
            return (
              <tr
                key={m.rid}
                data-testid="column-masks-row"
                data-mask-rid={m.rid}
                data-object-type-rid={m.objectTypeRid}
                data-object-type-api-name={ot?.apiName ?? ''}
                data-property-api-name={m.propertyApiName}
                data-mask-rule={m.maskRule}
                className="border-t border-border/30"
              >
                <td className="px-3 py-2 align-top">
                  <div className="font-mono text-text-primary">
                    {ot?.apiName ?? m.objectTypeRid}
                  </div>
                  {ot?.displayName && (
                    <div className="text-xs text-text-secondary">
                      {ot.displayName}
                    </div>
                  )}
                </td>
                <td className="px-3 py-2 align-top font-mono text-text-primary">
                  {m.propertyApiName}
                </td>
                <td className="px-3 py-2 align-top">
                  <span
                    data-testid="column-masks-rule-badge"
                    data-mask-rule={m.maskRule}
                    className="inline-flex rounded-full border border-violet-500/40 bg-violet-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-violet-300"
                  >
                    {m.maskRule}
                  </span>
                </td>
                <td className="px-3 py-2 align-top">
                  <AppliesToBadges applies={m.appliesTo} testidPrefix="column-masks" />
                </td>
                <td className="px-3 py-2 align-top text-xs text-text-secondary">
                  {m.description ? m.description : <em>—</em>}
                </td>
                <td className="px-3 py-2 align-top text-right">
                  <div className="flex justify-end gap-2">
                    <button
                      type="button"
                      data-testid="column-masks-edit-btn"
                      data-mask-rid={m.rid}
                      onClick={() => onEdit(m)}
                      className="rounded-md border border-border/60 px-2 py-1 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      data-testid="column-masks-delete-btn"
                      data-mask-rid={m.rid}
                      onClick={() => onDelete(m)}
                      className="rounded-md border border-rose-500/40 bg-rose-500/10 px-2 py-1 text-xs font-medium text-rose-300 hover:bg-rose-500/20"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

interface ColumnMaskEditorProps {
  existing: ColumnMask | null;
  objectTypes: Array<{ rid: string; apiName: string; displayName: string }>;
  activeOntology: string;
  onClose: () => void;
}

function ColumnMaskEditor({
  existing,
  objectTypes,
  activeOntology,
  onClose,
}: ColumnMaskEditorProps) {
  const isEdit = existing !== null;
  const createMutation = useCreateColumnMask();
  const updateMutation = useUpdateColumnMask();
  const pushToast = useToastStore((s) => s.push);

  const [objectTypeRid, setObjectTypeRid] = useState(
    existing?.objectTypeRid ?? objectTypes[0]?.rid ?? '',
  );
  const [propertyApiName, setPropertyApiName] = useState(
    existing?.propertyApiName ?? '',
  );
  const [maskRule, setMaskRule] = useState<MaskRule>(
    existing?.maskRule ?? 'redact',
  );
  const [rolesText, setRolesText] = useState(
    (existing?.appliesTo.roles ?? []).join(', '),
  );
  const [groupsText, setGroupsText] = useState(
    (existing?.appliesTo.groups ?? []).join(', '),
  );
  const [usersText, setUsersText] = useState(
    (existing?.appliesTo.users ?? []).join(', '),
  );
  const [description, setDescription] = useState(existing?.description ?? '');
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Resolve the picked ObjectType's apiName so we can fetch its full
  // metadata (with properties Record) to populate the property dropdown.
  const pickedObjectType = useMemo(
    () => objectTypes.find((ot) => ot.rid === objectTypeRid),
    [objectTypes, objectTypeRid],
  );
  const objectTypeDetail = useObjectType(
    activeOntology,
    pickedObjectType?.apiName ?? '',
  );
  const availableProperties = useMemo(() => {
    const data = objectTypeDetail.data;
    if (!data?.properties) return [] as string[];
    return Object.keys(data.properties).sort();
  }, [objectTypeDetail.data]);

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!objectTypeRid) {
      setSubmitError('Object Type is required.');
      return;
    }
    const trimmedProp = propertyApiName.trim();
    if (!trimmedProp) {
      setSubmitError('Property is required.');
      return;
    }
    setSubmitError(null);
    const appliesTo: AppliesTo = {
      roles: parseList(rolesText),
      groups: parseList(groupsText),
      users: parseList(usersText),
    };
    const trimmedDesc = description.trim();

    if (isEdit && existing) {
      updateMutation.mutate(
        {
          rid: existing.rid,
          body: {
            maskRule,
            appliesTo,
            description: trimmedDesc,
          },
        },
        {
          onSuccess: () => {
            pushToast({
              message: 'Column mask updated.',
              severity: 'success',
            });
            onClose();
          },
          onError: (err) => {
            const msg = describeApiError(err);
            setSubmitError(msg);
            pushToast({ message: msg, severity: 'error' });
          },
        },
      );
    } else {
      createMutation.mutate(
        {
          objectTypeRid,
          propertyApiName: trimmedProp,
          maskRule,
          appliesTo,
          description: trimmedDesc,
        },
        {
          onSuccess: () => {
            pushToast({
              message: 'Column mask created.',
              severity: 'success',
            });
            onClose();
          },
          onError: (err) => {
            const msg = describeApiError(err);
            setSubmitError(msg);
            pushToast({ message: msg, severity: 'error' });
          },
        },
      );
    }
  };

  const submitting = createMutation.isPending || updateMutation.isPending;
  const canSubmit =
    !!objectTypeRid && !!propertyApiName.trim() && KNOWN_MASK_RULES.includes(maskRule);

  return (
    <div
      data-testid="column-mask-editor"
      data-mode={isEdit ? 'edit' : 'create'}
      role="dialog"
      aria-modal="true"
      aria-label={isEdit ? 'Edit column mask' : 'Create column mask'}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/60"
    >
      <form
        data-testid="column-mask-editor-form"
        onSubmit={onSubmit}
        className="w-[36rem] max-w-full space-y-4 rounded-lg border border-border/60 bg-bg-primary p-5 shadow-xl"
      >
        <header className="space-y-1">
          <h2 className="text-base font-semibold text-text-primary">
            {isEdit ? 'Edit column mask' : 'Create column mask'}
          </h2>
          <p className="text-xs text-text-secondary">
            AppliesTo lists the identities ALLOWED to see the clear value
            (allow list). All other callers receive the masked value. The
            mask rule is one of <span className="font-mono">hash</span> /{' '}
            <span className="font-mono">redact</span> /{' '}
            <span className="font-mono">partial</span> (mirrors{' '}
            <span className="font-mono">pkg/masking.MaskRule</span>).
          </p>
        </header>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Object Type
          </span>
          <select
            data-testid="column-mask-editor-objectType"
            value={objectTypeRid}
            onChange={(e) => {
              setObjectTypeRid(e.target.value);
              setPropertyApiName('');
            }}
            disabled={isEdit}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary disabled:opacity-50"
          >
            {!objectTypeRid && <option value="">Select an object type…</option>}
            {objectTypes.map((ot) => (
              <option key={ot.rid} value={ot.rid}>
                {ot.apiName} ({ot.displayName})
              </option>
            ))}
          </select>
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Property
          </span>
          {availableProperties.length > 0 ? (
            <select
              data-testid="column-mask-editor-property"
              value={propertyApiName}
              onChange={(e) => setPropertyApiName(e.target.value)}
              disabled={isEdit}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm font-mono text-text-primary disabled:opacity-50"
            >
              {!propertyApiName && (
                <option value="">Select a property…</option>
              )}
              {availableProperties.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          ) : (
            <input
              type="text"
              data-testid="column-mask-editor-property"
              value={propertyApiName}
              onChange={(e) => setPropertyApiName(e.target.value)}
              disabled={isEdit}
              placeholder="propertyApiName (e.g. email)"
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm font-mono text-text-primary disabled:opacity-50"
            />
          )}
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Mask Rule
          </span>
          <select
            data-testid="column-mask-editor-rule"
            value={maskRule}
            onChange={(e) => setMaskRule(e.target.value as MaskRule)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
          >
            {KNOWN_MASK_RULES.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>

        <fieldset className="grid grid-cols-1 gap-2 rounded-md border border-border/40 bg-bg-tertiary/30 p-3">
          <legend className="px-1 text-xs font-semibold uppercase tracking-wider text-text-secondary">
            Applies To (allow list — clear value)
          </legend>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Roles (comma-separated)
            </span>
            <input
              type="text"
              data-testid="column-mask-editor-roles"
              value={rolesText}
              onChange={(e) => setRolesText(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
              placeholder="ontology-owner, dpo"
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Groups (comma-separated)
            </span>
            <input
              type="text"
              data-testid="column-mask-editor-groups"
              value={groupsText}
              onChange={(e) => setGroupsText(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
              placeholder="security, eng"
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Users (id or email, comma-separated)
            </span>
            <input
              type="text"
              data-testid="column-mask-editor-users"
              value={usersText}
              onChange={(e) => setUsersText(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
              placeholder="alice@test"
            />
          </label>
        </fieldset>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Description (optional)
          </span>
          <input
            type="text"
            data-testid="column-mask-editor-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
          />
        </label>

        {submitError && (
          <div
            data-testid="column-mask-editor-error"
            role="alert"
            className="rounded-md border border-rose-500/40 bg-rose-500/10 p-2 text-xs text-rose-200"
          >
            {submitError}
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="column-mask-editor-cancel-btn"
            onClick={onClose}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="column-mask-editor-submit-btn"
            disabled={!canSubmit || submitting}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Create mask'}
          </button>
        </div>
      </form>
    </div>
  );
}

function ColumnMaskDeleteDialog({
  target,
  pending,
  onCancel,
  onConfirm,
}: {
  target: ColumnMask;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div
      data-testid="column-mask-delete-dialog"
      role="dialog"
      aria-modal="true"
      aria-label="Confirm delete"
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/60"
    >
      <div className="w-[26rem] max-w-full space-y-4 rounded-lg border border-border/60 bg-bg-primary p-5 shadow-xl">
        <header className="space-y-1">
          <h2 className="text-base font-semibold text-text-primary">
            Delete column mask
          </h2>
          <p className="text-xs text-text-secondary">
            Removing{' '}
            <span
              data-testid="column-mask-delete-target-rid"
              className="font-mono text-text-primary"
            >
              {target.rid}
            </span>{' '}
            takes effect on the next read after the cache reload.
          </p>
        </header>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="column-mask-delete-cancel-btn"
            onClick={onCancel}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="column-mask-delete-confirm-btn"
            disabled={pending}
            onClick={onConfirm}
            className="rounded-md bg-rose-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 disabled:opacity-50"
          >
            {pending ? 'Deleting…' : 'Delete mask'}
          </button>
        </div>
      </div>
    </div>
  );
}

interface ColumnMasksSimulatorPanelProps {
  masks: ColumnMask[];
  objectTypeByRid: Map<string, { apiName: string; displayName: string }>;
}

function ColumnMasksSimulatorPanel({
  masks,
  objectTypeByRid,
}: ColumnMasksSimulatorPanelProps) {
  // Test-as-user simulator for column masks. As with row policies there is
  // no backend endpoint that returns per-mask decisions for a hypothetical
  // caller (Engine.Compile() resolves internally at read time). Honest
  // mapping: replicate pkg/masking.AppliesTo.IsApplicable client-side and
  // surface which masks would EXEMPT the simulated user (semantic flip vs
  // row policies — see api/securityPolicies.ts comment).
  const [userId, setUserId] = useState('alice');
  const [email, setEmail] = useState('alice@test');
  const [rolesText, setRolesText] = useState('viewer');
  const [groupsText, setGroupsText] = useState('');

  const simulated: SimulatedUser = useMemo(
    () => ({
      id: userId.trim(),
      email: email.trim() || undefined,
      roles: parseList(rolesText),
      groups: parseList(groupsText),
    }),
    [userId, email, rolesText, groupsText],
  );

  const decisions = useMemo(
    () =>
      masks.map((m) => ({
        mask: m,
        exempt: isMaskExempt(m.appliesTo, simulated),
      })),
    [masks, simulated],
  );
  const exemptCount = decisions.filter((d) => d.exempt).length;
  const maskedCount = decisions.length - exemptCount;

  return (
    <div
      data-testid="column-masks-simulator"
      data-exempt-count={exemptCount}
      data-masked-count={maskedCount}
      className="space-y-3 rounded-md border border-sky-500/30 bg-sky-500/5 p-3"
    >
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-sky-200">
          Test as user — column-mask simulator
        </h3>
        <span className="text-[10px] text-text-secondary">
          Replicates pkg/masking AppliesTo.IsApplicable client-side as an
          allow list (exempt = sees clear). Backend reads remain
          authoritative.
        </span>
      </header>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            User ID
          </span>
          <input
            type="text"
            data-testid="column-masks-simulator-user-id"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            Email
          </span>
          <input
            type="text"
            data-testid="column-masks-simulator-email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            Roles (comma-separated)
          </span>
          <input
            type="text"
            data-testid="column-masks-simulator-roles"
            value={rolesText}
            onChange={(e) => setRolesText(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
        <label className="block">
          <span className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1">
            Groups (comma-separated)
          </span>
          <input
            type="text"
            data-testid="column-masks-simulator-groups"
            value={groupsText}
            onChange={(e) => setGroupsText(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-2 py-1 text-xs text-text-primary"
          />
        </label>
      </div>
      <div
        data-testid="column-masks-simulator-summary"
        className="text-xs text-text-secondary"
      >
        <span
          data-testid="column-masks-simulator-exempt-count"
          className="font-mono text-text-primary"
        >
          {exemptCount}
        </span>{' '}
        of {decisions.length} mask
        {decisions.length === 1 ? '' : 's'} would be bypassed (clear value
        visible) for this user;{' '}
        <span
          data-testid="column-masks-simulator-masked-count"
          className="font-mono text-text-primary"
        >
          {maskedCount}
        </span>{' '}
        would apply.
      </div>
      {decisions.length > 0 && (
        <ul
          data-testid="column-masks-simulator-decisions"
          className="space-y-1 text-xs"
        >
          {decisions.map((d) => {
            const ot = objectTypeByRid.get(d.mask.objectTypeRid);
            return (
              <li
                key={d.mask.rid}
                data-testid="column-masks-simulator-decision-row"
                data-mask-rid={d.mask.rid}
                data-exempt={d.exempt ? 'true' : 'false'}
                className={`flex items-center gap-2 rounded-md border px-2 py-1 ${
                  d.exempt
                    ? 'border-emerald-500/40 bg-emerald-500/5'
                    : 'border-amber-500/40 bg-amber-500/5'
                }`}
              >
                <span
                  data-testid="column-masks-simulator-decision-badge"
                  className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${
                    d.exempt
                      ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/40'
                      : 'bg-amber-500/15 text-amber-300 border border-amber-500/40'
                  }`}
                >
                  {d.exempt ? 'exempt' : 'masked'}
                </span>
                <span className="font-mono text-text-primary">
                  {ot?.apiName ?? d.mask.objectTypeRid}.{d.mask.propertyApiName}
                </span>
                <span className="text-text-secondary">
                  — rule:{d.mask.maskRule}
                </span>
                {d.mask.description && (
                  <span className="truncate text-text-secondary">
                    · {d.mask.description}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
