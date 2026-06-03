import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type { ActionType, LinkType, ObjectType } from '../../api/types';
import type {
  ActionTypeParamDef,
  ActionTypeRule,
  ActionTypeRuleType,
  CreateActionTypeRequest,
  UpdateActionTypeRequest,
} from '../../api/ontologies';
import {
  useActionTypesAdmin,
  useCreateActionType,
  useDeleteActionType,
  useUpdateActionType,
} from '../../hooks/useActionTypes';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { useLinkTypes } from '../../hooks/useLinkTypes';
import { toApiName } from '../../utils/naming';
import { Modal } from '../common/Modal';
import { Badge } from '../common/Badge';
import { LoadingSpinner } from '../common/LoadingSpinner';

const PARAMETER_TYPES = [
  'string',
  'integer',
  'long',
  'double',
  'boolean',
  'date',
  'timestamp',
  'attachment',
] as const;

type ParamType = (typeof PARAMETER_TYPES)[number];

const RULE_TYPES: Array<{ value: ActionTypeRuleType; label: string }> = [
  { value: 'createObject', label: 'Create object' },
  { value: 'modifyObject', label: 'Modify object' },
  { value: 'deleteObject', label: 'Delete object' },
  { value: 'createLink', label: 'Create link' },
  { value: 'deleteLink', label: 'Delete link' },
  { value: 'createOrModifyObject', label: 'Create or modify (upsert)' },
  { value: 'createInterfaceObject', label: 'Create interface object' },
  { value: 'modifyInterfaceObject', label: 'Modify interface object' },
  { value: 'deleteInterfaceObject', label: 'Delete interface object' },
];

const STATUS_OPTIONS = ['ACTIVE', 'EXPERIMENTAL', 'DEPRECATED'] as const;

// Sentinel returned by the JSON editors when the text fails to parse, so the
// submit handler can distinguish "invalid" from "intentionally empty".
const INVALID_JSON = Symbol('invalid-json');

export function ActionTypeAdminPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { data: actionTypes, isLoading, error } = useActionTypesAdmin(
    ontologyApiName,
  );
  const { data: objectTypes } = useObjectTypes(ontologyApiName);
  const { data: linkTypes } = useLinkTypes(ontologyApiName);

  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>('ALL');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<ActionType | null>(null);
  const [deleting, setDeleting] = useState<ActionType | null>(null);

  const filtered = useMemo(() => {
    if (!actionTypes) return [];
    const q = search.trim().toLowerCase();
    const list = actionTypes.filter((at) => {
      if (statusFilter !== 'ALL' && at.status !== statusFilter) return false;
      if (!q) return true;
      return (
        at.apiName.toLowerCase().includes(q) ||
        at.displayName.toLowerCase().includes(q)
      );
    });
    list.sort((a, b) => a.displayName.localeCompare(b.displayName));
    return list;
  }, [actionTypes, search, statusFilter]);

  if (!ontologyApiName) {
    return (
      <div
        data-testid="action-type-admin-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="action-type-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Action Types
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="action-type-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Action Type
        </button>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <input
          type="search"
          aria-label="Search action types"
          placeholder="Search by name or apiName…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-[12rem] max-w-md px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
        <label className="text-xs text-text-secondary flex items-center gap-2">
          <span className="uppercase tracking-widest">Status</span>
          <select
            aria-label="Filter by status"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="px-2 py-1 rounded bg-bg-tertiary text-text-primary text-xs outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="ALL">All statuses</option>
            {STATUS_OPTIONS.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="action-type-admin-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="action-type-admin-error"
            className="text-sm text-accent-error"
          >
            Failed to load action types: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            data-testid="action-type-admin-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No action types match the filters
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Adjust the search or create a new Action Type.
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <ActionTypeTable
            rows={filtered}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>

      {createOpen && (
        <ActionTypeBuilderModal
          ontologyApiName={ontologyApiName}
          objectTypes={objectTypes ?? []}
          linkTypes={linkTypes ?? []}
          existing={actionTypes ?? []}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <ActionTypeBuilderModal
          ontologyApiName={ontologyApiName}
          objectTypes={objectTypes ?? []}
          linkTypes={linkTypes ?? []}
          existing={actionTypes ?? []}
          editing={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteActionTypeModal
          ontologyApiName={ontologyApiName}
          actionType={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function ActionTypeTable({
  rows,
  onEdit,
  onDelete,
}: {
  rows: ActionType[];
  onEdit: (at: ActionType) => void;
  onDelete: (at: ActionType) => void;
}) {
  return (
    <div
      data-testid="action-type-admin-table"
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
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-left px-4 py-2 font-medium">Parameters</th>
            <th className="text-left px-4 py-2 font-medium">Rules</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((at) => {
            const paramCount = Object.keys(at.parameters ?? {}).length;
            const ruleCount = Array.isArray(at.rules) ? at.rules.length : 0;
            return (
              <tr
                key={at.rid}
                data-testid="action-type-row"
                data-action-type-api-name={at.apiName}
                data-action-type-rid={at.rid}
                data-action-type-status={at.status}
                data-action-type-parameter-count={paramCount}
                data-action-type-rule-count={ruleCount}
                className="border-b last:border-0 hover:bg-bg-tertiary/30"
                style={{ borderColor: 'rgba(31,41,55,0.5)' }}
              >
                <td className="px-4 py-2 text-text-primary">
                  {at.displayName}
                </td>
                <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                  {at.apiName}
                </td>
                <td className="px-4 py-2 text-xs">
                  <Badge variant="info">{at.status}</Badge>
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {paramCount}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {ruleCount}
                </td>
                <td className="px-4 py-2 text-right whitespace-nowrap">
                  <button
                    type="button"
                    data-testid="action-type-edit-btn"
                    data-action-type-api-name={at.apiName}
                    onClick={() => onEdit(at)}
                    className="text-xs text-accent-cyan hover:underline mr-3"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    data-testid="action-type-delete-btn"
                    data-action-type-api-name={at.apiName}
                    onClick={() => onDelete(at)}
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

// Builder form state is a friendlier shape than the wire format, geared
// toward inline editing. It's converted to the wire format on submit.
interface BuilderRuleBinding {
  propertyName: string;
  source: 'parameter' | 'static';
  // For parameter: parameter id. For static: raw string value (caller parses).
  value: string;
}

interface BuilderRule {
  id: string;
  type: ActionTypeRuleType;
  objectType?: string;
  interfaceApiName?: string;
  linkTypeApiName?: string;
  sourceObjectPrimaryKey?: string;
  targetObjectPrimaryKey?: string;
  bindings: BuilderRuleBinding[];
}

interface BuilderState {
  apiName: string;
  apiNameDirty: boolean;
  displayName: string;
  description: string;
  status: string;
  parameters: ActionTypeParamDef[];
  rules: BuilderRule[];
  // Approval gating (US-242). approvers is the raw comma-separated input;
  // it is split into a string[] on submit.
  requiresApproval: boolean;
  approvers: string;
  // submissionCriteria / parameterSchema are raw JSON text the author edits
  // directly; they are parsed (and surfaced as inline errors) on submit.
  submissionCriteria: string;
  parameterSchema: string;
  // compensateActionRid points at another ActionType in the same ontology
  // (US-239). Empty means no compensation pairing.
  compensateActionRid: string;
}

// Parse a comma/newline-separated approver list into a trimmed, de-duped
// string[]. Empty entries are dropped.
function parseApprovers(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(/[,\n]/)) {
    const v = part.trim();
    if (v && !seen.has(v)) {
      seen.add(v);
      out.push(v);
    }
  }
  return out;
}

// Pretty-print a stored JSON value back into editable text for the modal.
function jsonToEditorText(value: unknown): string {
  if (value == null) return '';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}

function emptyRule(): BuilderRule {
  return {
    id: cryptoRandomId(),
    type: 'createObject',
    objectType: '',
    bindings: [],
  };
}

function cryptoRandomId(): string {
  // Small random id used for keying, not cryptographic
  return Math.random().toString(36).slice(2, 10);
}

function initialStateFromAction(
  at: ActionType | undefined,
): BuilderState {
  if (!at) {
    return {
      apiName: '',
      apiNameDirty: false,
      displayName: '',
      description: '',
      status: 'ACTIVE',
      parameters: [],
      rules: [],
      requiresApproval: false,
      approvers: '',
      submissionCriteria: '',
      parameterSchema: '',
      compensateActionRid: '',
    };
  }
  const parameters: ActionTypeParamDef[] = Object.entries(
    at.parameters ?? {},
  ).map(([id, v]) => {
    const pv = v as { dataType?: { type?: string }; required?: boolean; description?: string };
    return {
      id,
      type: pv.dataType?.type ?? 'string',
      required: !!pv.required,
      description: pv.description,
    };
  });
  const rawRules = Array.isArray(at.rules) ? (at.rules as unknown[]) : [];
  const rules: BuilderRule[] = rawRules.map((r) => {
    const rr = r as Partial<ActionTypeRule>;
    const bindings: BuilderRuleBinding[] = Object.entries(
      rr.propertyBindings ?? {},
    ).map(([propertyName, b]) => {
      const bb = b as { type?: string; value?: unknown };
      return {
        propertyName,
        source: bb.type === 'static' ? 'static' : 'parameter',
        value: bb.value == null ? '' : String(bb.value),
      };
    });
    return {
      id: cryptoRandomId(),
      type: (rr.type as ActionTypeRuleType) ?? 'createObject',
      objectType: rr.objectType ?? '',
      interfaceApiName: rr.interfaceApiName ?? '',
      linkTypeApiName: rr.linkTypeApiName ?? '',
      sourceObjectPrimaryKey: rr.sourceObjectPrimaryKey ?? '',
      targetObjectPrimaryKey: rr.targetObjectPrimaryKey ?? '',
      bindings,
    };
  });
  return {
    apiName: at.apiName,
    apiNameDirty: true,
    displayName: at.displayName,
    description: at.description ?? '',
    status: at.status,
    parameters,
    rules,
    requiresApproval: !!at.requiresApproval,
    approvers: (at.approvers ?? []).join(', '),
    submissionCriteria: jsonToEditorText(at.submissionCriteria),
    parameterSchema: jsonToEditorText(at.parameterSchema),
    compensateActionRid: at.compensateActionRid ?? '',
  };
}

// Convert the friendly builder state into the wire payloads.
function builderRulesToWire(rules: BuilderRule[]): ActionTypeRule[] {
  return rules.map((r) => {
    const out: ActionTypeRule = { type: r.type };
    if (
      r.type === 'createObject' ||
      r.type === 'modifyObject' ||
      r.type === 'deleteObject' ||
      r.type === 'createOrModifyObject'
    ) {
      out.objectType = r.objectType ?? '';
    }
    if (
      r.type === 'createInterfaceObject' ||
      r.type === 'modifyInterfaceObject' ||
      r.type === 'deleteInterfaceObject'
    ) {
      out.interfaceApiName = r.interfaceApiName ?? '';
    }
    if (r.type === 'createLink' || r.type === 'deleteLink') {
      out.linkTypeApiName = r.linkTypeApiName ?? '';
      out.sourceObjectPrimaryKey = r.sourceObjectPrimaryKey ?? '';
      out.targetObjectPrimaryKey = r.targetObjectPrimaryKey ?? '';
    }
    if (
      r.type === 'createObject' ||
      r.type === 'modifyObject' ||
      r.type === 'createOrModifyObject' ||
      r.type === 'createInterfaceObject' ||
      r.type === 'modifyInterfaceObject'
    ) {
      const bindings: Record<string, { type: string; value: unknown }> = {};
      for (const b of r.bindings) {
        if (!b.propertyName) continue;
        bindings[b.propertyName] = { type: b.source, value: b.value };
      }
      if (Object.keys(bindings).length > 0) {
        out.propertyBindings = bindings;
      }
    }
    return out;
  });
}

function ActionTypeBuilderModal({
  ontologyApiName,
  objectTypes,
  linkTypes,
  existing,
  editing,
  onClose,
}: {
  ontologyApiName: string;
  objectTypes: ObjectType[];
  linkTypes: LinkType[];
  existing: ActionType[];
  editing?: ActionType;
  onClose: () => void;
}) {
  const isEdit = !!editing;
  const create = useCreateActionType(ontologyApiName);
  const update = useUpdateActionType(ontologyApiName);
  const [form, setForm] = useState<BuilderState>(() =>
    initialStateFromAction(editing),
  );
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [criteriaError, setCriteriaError] = useState<string | null>(null);
  const [schemaError, setSchemaError] = useState<string | null>(null);

  const apiNameTaken = useMemo(
    () =>
      new Set(
        existing
          .filter((at) => !editing || at.rid !== editing.rid)
          .map((at) => at.apiName),
      ),
    [existing, editing],
  );

  // Candidate compensating actions: every other ActionType in this ontology.
  // An action can't compensate itself, so the action being edited is excluded.
  const compensateOptions = useMemo(
    () =>
      existing
        .filter((at) => !editing || at.rid !== editing.rid)
        .slice()
        .sort((a, b) => a.displayName.localeCompare(b.displayName)),
    [existing, editing],
  );

  function updateDisplayName(next: string) {
    setForm((f) => ({
      ...f,
      displayName: next,
      apiName: f.apiNameDirty ? f.apiName : toApiName(next),
    }));
  }

  const duplicateApiName =
    !isEdit && !!form.apiName && apiNameTaken.has(form.apiName);

  const parametersWire = useMemo(() => {
    const out: Record<string, unknown> = {};
    for (const p of form.parameters) {
      if (!p.id) continue;
      out[p.id] = {
        dataType: { type: p.type },
        required: !!p.required,
        ...(p.description ? { description: p.description } : {}),
      };
    }
    return out;
  }, [form.parameters]);

  const rulesWire = useMemo(() => builderRulesToWire(form.rules), [form.rules]);

  const previewJson = useMemo(() => {
    return JSON.stringify(
      {
        apiName: form.apiName,
        displayName: form.displayName,
        status: form.status,
        parameters: parametersWire,
        rules: rulesWire,
      },
      null,
      2,
    );
  }, [form, parametersWire, rulesWire]);

  const canSubmit =
    !!form.apiName.trim() &&
    !!form.displayName.trim() &&
    !duplicateApiName &&
    !create.isPending &&
    !update.isPending;

  // Parse a raw JSON editor string. Returns the parsed value, or sets the
  // matching inline error and returns the sentinel `INVALID` so the caller
  // can abort. An empty/whitespace string means "field omitted".
  function parseOptionalJson(
    raw: string,
    setError: (m: string | null) => void,
  ): unknown {
    const trimmed = raw.trim();
    if (trimmed === '') {
      setError(null);
      return undefined;
    }
    try {
      const parsed = JSON.parse(trimmed);
      setError(null);
      return parsed;
    } catch (err) {
      setError(`Invalid JSON: ${(err as Error).message}`);
      return INVALID_JSON;
    }
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);

    // The wire format expects parameters as an array (internal stored format).
    const parametersArray: ActionTypeParamDef[] = form.parameters
      .filter((p) => p.id.trim() !== '')
      .map((p) => ({
        id: p.id.trim(),
        type: p.type,
        required: !!p.required,
        ...(p.description ? { description: p.description } : {}),
      }));

    const submissionCriteria = parseOptionalJson(
      form.submissionCriteria,
      setCriteriaError,
    );
    const parameterSchema = parseOptionalJson(
      form.parameterSchema,
      setSchemaError,
    );
    if (submissionCriteria === INVALID_JSON || parameterSchema === INVALID_JSON) {
      return;
    }

    const approvers = parseApprovers(form.approvers);
    const compensateActionRid = form.compensateActionRid.trim();

    try {
      if (editing) {
        const body: UpdateActionTypeRequest = {
          displayName: form.displayName.trim(),
          description: form.description.trim() || undefined,
          status: form.status,
          parameters: parametersArray,
          rules: rulesWire,
          requiresApproval: form.requiresApproval,
          approvers,
          compensateActionRid,
          // parameterSchema is tri-state on the server: an explicit `null`
          // clears the stored schema, so emptying the editor removes it.
          parameterSchema: parameterSchema === undefined ? null : parameterSchema,
          ...(submissionCriteria !== undefined ? { submissionCriteria } : {}),
        };
        await update.mutateAsync({ rid: editing.rid, body });
      } else {
        const body: CreateActionTypeRequest = {
          apiName: form.apiName.trim(),
          displayName: form.displayName.trim(),
          description: form.description.trim() || undefined,
          status: form.status,
          parameters: parametersArray,
          rules: rulesWire,
          ...(form.requiresApproval ? { requiresApproval: true } : {}),
          ...(approvers.length > 0 ? { approvers } : {}),
          ...(compensateActionRid ? { compensateActionRid } : {}),
          ...(submissionCriteria !== undefined ? { submissionCriteria } : {}),
          ...(parameterSchema !== undefined ? { parameterSchema } : {}),
        };
        await create.mutateAsync(body);
      }
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={isEdit ? `Edit: ${editing.displayName}` : 'New Action Type'}
      size="xl"
    >
      <form
        onSubmit={onSubmit}
        data-testid={isEdit ? 'action-type-edit-form' : 'action-type-create-form'}
        className="flex flex-col gap-4"
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Display Name" required>
            <input
              type="text"
              data-testid="action-type-display-name"
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
                ? `An Action with apiName "${form.apiName}" already exists.`
                : undefined
            }
          >
            <input
              type="text"
              data-testid="action-type-api-name"
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
        <div className="grid grid-cols-[1fr_12rem] gap-3">
          <Field label="Description">
            <textarea
              data-testid="action-type-description"
              value={form.description}
              onChange={(e) =>
                setForm((f) => ({ ...f, description: e.target.value }))
              }
              rows={2}
              className={inputClass}
            />
          </Field>
          <Field label="Status">
            <select
              aria-label="Status"
              data-testid="action-type-status"
              value={form.status}
              onChange={(e) =>
                setForm((f) => ({ ...f, status: e.target.value }))
              }
              className={inputClass}
            >
              {STATUS_OPTIONS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <Section
          title="Parameters"
          testId="action-type-parameters-section"
          action={
            <button
              type="button"
              data-testid="action-type-add-parameter"
              onClick={() =>
                setForm((f) => ({
                  ...f,
                  parameters: [
                    ...f.parameters,
                    { id: '', type: 'string', required: false },
                  ],
                }))
              }
              className="text-xs text-accent-cyan hover:underline"
            >
              + Add parameter
            </button>
          }
        >
          <ParameterList
            parameters={form.parameters}
            onChange={(parameters) => setForm((f) => ({ ...f, parameters }))}
          />
        </Section>

        <Section
          title="Rules"
          testId="action-type-rules-section"
          action={
            <button
              type="button"
              data-testid="action-type-add-rule"
              onClick={() =>
                setForm((f) => ({ ...f, rules: [...f.rules, emptyRule()] }))
              }
              className="text-xs text-accent-cyan hover:underline"
            >
              + Add rule
            </button>
          }
        >
          <RuleList
            rules={form.rules}
            parameters={form.parameters}
            objectTypes={objectTypes}
            linkTypes={linkTypes}
            onChange={(rules) => setForm((f) => ({ ...f, rules }))}
          />
        </Section>

        <Section title="Approval" testId="action-type-approval-section">
          <label className="flex items-center gap-2 text-xs text-text-secondary">
            <input
              type="checkbox"
              data-testid="action-type-requires-approval"
              checked={form.requiresApproval}
              onChange={(e) =>
                setForm((f) => ({ ...f, requiresApproval: e.target.checked }))
              }
            />
            <span>
              Require human approval before edits are committed
            </span>
          </label>
          {form.requiresApproval && (
            <Field
              label="Approvers"
              hint="Comma-separated role names or user IDs allowed to approve. Without any approver, gated actions can never be applied."
            >
              <input
                type="text"
                data-testid="action-type-approvers"
                placeholder="role:approver, alice"
                value={form.approvers}
                onChange={(e) =>
                  setForm((f) => ({ ...f, approvers: e.target.value }))
                }
                className={inputClass + ' font-mono text-xs'}
              />
            </Field>
          )}
        </Section>

        <Section
          title="Submission Criteria"
          testId="action-type-submission-criteria-section"
        >
          <Field
            label="Criteria (JSON)"
            hint="Optional criteria tree evaluated at apply time (e.g. parameterMatch / and_ / or_ / not_). Validated by the server on save."
            error={criteriaError ?? undefined}
          >
            <textarea
              data-testid="action-type-submission-criteria"
              value={form.submissionCriteria}
              onChange={(e) =>
                setForm((f) => ({ ...f, submissionCriteria: e.target.value }))
              }
              spellCheck={false}
              rows={6}
              placeholder={'{\n  "type": "parameterMatch",\n  "value": { "parameter": "status", "operator": "eq", "value": "active" }\n}'}
              className={inputClass + ' font-mono text-xs'}
            />
          </Field>
        </Section>

        <Section
          title="Compensating Action"
          testId="action-type-compensate-section"
        >
          <Field
            label="On saga rollback, run"
            hint="Another ActionType whose rules compensate (roll back) this one when a saga batch fails downstream."
          >
            <select
              aria-label="Compensating action"
              data-testid="action-type-compensate-select"
              value={form.compensateActionRid}
              onChange={(e) =>
                setForm((f) => ({
                  ...f,
                  compensateActionRid: e.target.value,
                }))
              }
              className={inputClass + ' text-xs'}
            >
              <option value="">No compensating action</option>
              {compensateOptions.map((at) => (
                <option key={at.rid} value={at.rid}>
                  {at.displayName} ({at.apiName})
                </option>
              ))}
            </select>
          </Field>
        </Section>

        <details
          data-testid="action-type-parameter-schema-section"
          className="rounded border px-3 py-2"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          <summary className="text-[10px] uppercase tracking-widest text-text-secondary cursor-pointer select-none">
            Parameter Schema (JSON Schema Draft-07)
          </summary>
          <div className="pt-2">
            <Field
              label="Schema (JSON)"
              hint="Optional Draft-07 JSON Schema evaluated after the per-parameter validator. Leave blank for no schema."
              error={schemaError ?? undefined}
            >
              <textarea
                data-testid="action-type-parameter-schema"
                value={form.parameterSchema}
                onChange={(e) =>
                  setForm((f) => ({ ...f, parameterSchema: e.target.value }))
                }
                spellCheck={false}
                rows={6}
                placeholder={'{\n  "type": "object",\n  "properties": { "amount": { "type": "number", "minimum": 0 } },\n  "required": ["amount"]\n}'}
                className={inputClass + ' font-mono text-xs'}
              />
            </Field>
          </div>
        </details>

        <Section title="JSON Preview">
          <pre
            data-testid="action-json-preview"
            className="font-mono text-[11px] text-text-secondary rounded p-3 overflow-auto max-h-64"
            style={{
              background: 'rgba(13,17,23,0.6)',
              borderColor: 'rgba(31,41,55,0.5)',
              border: '1px solid rgba(31,41,55,0.5)',
            }}
          >
            {previewJson}
          </pre>
        </Section>

        {submitError && (
          <p
            role="alert"
            data-testid={
              isEdit
                ? 'action-type-edit-error'
                : 'action-type-create-error'
            }
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid={
              isEdit
                ? 'action-type-edit-cancel'
                : 'action-type-create-cancel'
            }
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid={
              isEdit
                ? 'action-type-edit-submit'
                : 'action-type-create-submit'
            }
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

function ParameterList({
  parameters,
  onChange,
}: {
  parameters: ActionTypeParamDef[];
  onChange: (next: ActionTypeParamDef[]) => void;
}) {
  if (parameters.length === 0) {
    return (
      <p className="text-[11px] text-text-muted italic">
        No parameters. Actions without parameters run with an empty input.
      </p>
    );
  }
  const paramTypes: readonly string[] = PARAMETER_TYPES;
  return (
    <div className="flex flex-col gap-2">
      {parameters.map((p, idx) => (
        <div
          key={idx}
          className="grid grid-cols-[1fr_10rem_6rem_auto_auto] gap-2 items-center"
        >
          <input
            aria-label={`Parameter ${idx + 1} id`}
            type="text"
            placeholder="parameterId"
            value={p.id}
            onChange={(e) => {
              const next = [...parameters];
              next[idx] = { ...p, id: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' font-mono text-xs'}
          />
          <select
            aria-label={`Parameter ${idx + 1} type`}
            value={p.type}
            onChange={(e) => {
              const next = [...parameters];
              next[idx] = { ...p, type: e.target.value as ParamType };
              onChange(next);
            }}
            className={inputClass + ' text-xs'}
          >
            {paramTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          <label className="flex items-center gap-1 text-[11px] text-text-secondary">
            <input
              type="checkbox"
              checked={!!p.required}
              onChange={(e) => {
                const next = [...parameters];
                next[idx] = { ...p, required: e.target.checked };
                onChange(next);
              }}
            />
            Required
          </label>
          <input
            aria-label={`Parameter ${idx + 1} description`}
            type="text"
            placeholder="description"
            value={p.description ?? ''}
            onChange={(e) => {
              const next = [...parameters];
              next[idx] = { ...p, description: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' text-xs'}
          />
          <button
            type="button"
            aria-label={`Remove parameter ${idx + 1}`}
            onClick={() => onChange(parameters.filter((_, i) => i !== idx))}
            className="px-2 py-1 text-[11px] rounded text-accent-error hover:bg-accent-error/10"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}

function RuleList({
  rules,
  parameters,
  objectTypes,
  linkTypes,
  onChange,
}: {
  rules: BuilderRule[];
  parameters: ActionTypeParamDef[];
  objectTypes: ObjectType[];
  linkTypes: LinkType[];
  onChange: (next: BuilderRule[]) => void;
}) {
  if (rules.length === 0) {
    return (
      <p className="text-[11px] text-text-muted italic">
        No rules yet. Add at least one rule to describe what this action does.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-3">
      {rules.map((rule, idx) => (
        <RuleEditor
          key={rule.id}
          index={idx}
          rule={rule}
          parameters={parameters}
          objectTypes={objectTypes}
          linkTypes={linkTypes}
          onChange={(next) => {
            const arr = [...rules];
            arr[idx] = next;
            onChange(arr);
          }}
          onRemove={() => onChange(rules.filter((_, i) => i !== idx))}
        />
      ))}
    </div>
  );
}

function RuleEditor({
  index,
  rule,
  parameters,
  objectTypes,
  linkTypes,
  onChange,
  onRemove,
}: {
  index: number;
  rule: BuilderRule;
  parameters: ActionTypeParamDef[];
  objectTypes: ObjectType[];
  linkTypes: LinkType[];
  onChange: (next: BuilderRule) => void;
  onRemove: () => void;
}) {
  const showObjectType =
    rule.type === 'createObject' ||
    rule.type === 'modifyObject' ||
    rule.type === 'deleteObject' ||
    rule.type === 'createOrModifyObject';
  const showInterface =
    rule.type === 'createInterfaceObject' ||
    rule.type === 'modifyInterfaceObject' ||
    rule.type === 'deleteInterfaceObject';
  const showLink = rule.type === 'createLink' || rule.type === 'deleteLink';
  const showBindings =
    rule.type === 'createObject' ||
    rule.type === 'modifyObject' ||
    rule.type === 'createOrModifyObject' ||
    rule.type === 'createInterfaceObject' ||
    rule.type === 'modifyInterfaceObject';

  return (
    <div
      className="rounded border p-3 flex flex-col gap-2"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <div className="flex items-center gap-2">
        <span className="text-[10px] uppercase tracking-widest text-text-secondary">
          Rule {index + 1}
        </span>
        <select
          aria-label={`Rule ${index + 1} type`}
          value={rule.type}
          onChange={(e) =>
            onChange({ ...rule, type: e.target.value as ActionTypeRuleType })
          }
          className={inputClass + ' text-xs max-w-xs'}
        >
          {RULE_TYPES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
        <div className="flex-1" />
        <button
          type="button"
          aria-label={`Remove rule ${index + 1}`}
          onClick={onRemove}
          className="text-[11px] text-accent-error hover:underline"
        >
          Remove
        </button>
      </div>

      {showObjectType && (
        <Field label="Object Type" required>
          <select
            aria-label={`Rule ${index + 1} object type`}
            value={rule.objectType ?? ''}
            onChange={(e) => onChange({ ...rule, objectType: e.target.value })}
            className={inputClass + ' text-xs'}
            required
          >
            <option value="">Select object type…</option>
            {objectTypes.map((ot) => (
              <option key={ot.rid} value={ot.apiName}>
                {ot.displayName} ({ot.apiName})
              </option>
            ))}
          </select>
        </Field>
      )}

      {showInterface && (
        <Field label="Interface API Name" required>
          <input
            aria-label={`Rule ${index + 1} interface api name`}
            type="text"
            placeholder="interfaceApiName"
            value={rule.interfaceApiName ?? ''}
            onChange={(e) =>
              onChange({ ...rule, interfaceApiName: e.target.value })
            }
            className={inputClass + ' font-mono text-xs'}
            required
          />
        </Field>
      )}

      {showLink && (
        <div className="grid grid-cols-3 gap-2">
          <Field label="Link Type" required>
            <select
              aria-label={`Rule ${index + 1} link type`}
              value={rule.linkTypeApiName ?? ''}
              onChange={(e) =>
                onChange({ ...rule, linkTypeApiName: e.target.value })
              }
              className={inputClass + ' text-xs'}
              required
            >
              <option value="">Select link type…</option>
              {linkTypes.map((lt) => (
                <option key={lt.rid} value={lt.apiName}>
                  {lt.displayName}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Source Parameter" required>
            <ParamSelect
              aria-label={`Rule ${index + 1} source parameter`}
              value={rule.sourceObjectPrimaryKey ?? ''}
              parameters={parameters}
              onChange={(v) =>
                onChange({ ...rule, sourceObjectPrimaryKey: v })
              }
            />
          </Field>
          <Field label="Target Parameter" required>
            <ParamSelect
              aria-label={`Rule ${index + 1} target parameter`}
              value={rule.targetObjectPrimaryKey ?? ''}
              parameters={parameters}
              onChange={(v) =>
                onChange({ ...rule, targetObjectPrimaryKey: v })
              }
            />
          </Field>
        </div>
      )}

      {showBindings && (
        <div>
          <div className="flex items-center gap-2 mb-1">
            <span className="text-[10px] uppercase tracking-widest text-text-secondary">
              Property Bindings
            </span>
            <button
              type="button"
              onClick={() =>
                onChange({
                  ...rule,
                  bindings: [
                    ...rule.bindings,
                    { propertyName: '', source: 'parameter', value: '' },
                  ],
                })
              }
              className="text-[11px] text-accent-cyan hover:underline"
            >
              + Add binding
            </button>
          </div>
          <BindingEditor
            ruleIndex={index}
            bindings={rule.bindings}
            parameters={parameters}
            onChange={(bindings) => onChange({ ...rule, bindings })}
          />
        </div>
      )}
    </div>
  );
}

function ParamSelect({
  value,
  parameters,
  onChange,
  ...rest
}: {
  value: string;
  parameters: ActionTypeParamDef[];
  onChange: (v: string) => void;
  'aria-label'?: string;
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={inputClass + ' text-xs'}
      required
      {...rest}
    >
      <option value="">Select parameter…</option>
      {parameters
        .filter((p) => p.id.trim() !== '')
        .map((p) => (
          <option key={p.id} value={p.id}>
            {p.id} ({p.type})
          </option>
        ))}
    </select>
  );
}

function BindingEditor({
  ruleIndex,
  bindings,
  parameters,
  onChange,
}: {
  ruleIndex: number;
  bindings: BuilderRuleBinding[];
  parameters: ActionTypeParamDef[];
  onChange: (next: BuilderRuleBinding[]) => void;
}) {
  if (bindings.length === 0) {
    return (
      <p className="text-[11px] text-text-muted italic">
        No bindings. Use them to map parameters or static values to properties
        on the object being written.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-1.5">
      {bindings.map((b, idx) => (
        <div
          key={idx}
          className="grid grid-cols-[1fr_8rem_1fr_auto] gap-2 items-center"
        >
          <input
            aria-label={`Rule ${ruleIndex + 1} binding ${idx + 1} property`}
            type="text"
            placeholder="propertyName"
            value={b.propertyName}
            onChange={(e) => {
              const next = [...bindings];
              next[idx] = { ...b, propertyName: e.target.value };
              onChange(next);
            }}
            className={inputClass + ' font-mono text-xs'}
          />
          <select
            aria-label={`Rule ${ruleIndex + 1} binding ${idx + 1} source`}
            value={b.source}
            onChange={(e) => {
              const next = [...bindings];
              next[idx] = {
                ...b,
                source: e.target.value as BuilderRuleBinding['source'],
              };
              onChange(next);
            }}
            className={inputClass + ' text-xs'}
          >
            <option value="parameter">Parameter</option>
            <option value="static">Static</option>
          </select>
          {b.source === 'parameter' ? (
            <ParamSelect
              aria-label={`Rule ${ruleIndex + 1} binding ${idx + 1} value`}
              value={b.value}
              parameters={parameters}
              onChange={(v) => {
                const next = [...bindings];
                next[idx] = { ...b, value: v };
                onChange(next);
              }}
            />
          ) : (
            <input
              aria-label={`Rule ${ruleIndex + 1} binding ${idx + 1} value`}
              type="text"
              placeholder="static value"
              value={b.value}
              onChange={(e) => {
                const next = [...bindings];
                next[idx] = { ...b, value: e.target.value };
                onChange(next);
              }}
              className={inputClass + ' text-xs'}
            />
          )}
          <button
            type="button"
            aria-label={`Remove binding ${idx + 1} from rule ${ruleIndex + 1}`}
            onClick={() => onChange(bindings.filter((_, i) => i !== idx))}
            className="px-2 py-1 text-[11px] rounded text-accent-error hover:bg-accent-error/10"
          >
            Remove
          </button>
        </div>
      ))}
    </div>
  );
}

function DeleteActionTypeModal({
  ontologyApiName,
  actionType,
  onClose,
}: {
  ontologyApiName: string;
  actionType: ActionType;
  onClose: () => void;
}) {
  const del = useDeleteActionType(ontologyApiName);
  const [submitError, setSubmitError] = useState<string | null>(null);

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync(actionType.rid);
      onClose();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Action Type">
      <div
        data-testid="action-type-delete-modal"
        data-action-type-api-name={actionType.apiName}
        data-action-type-rid={actionType.rid}
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{actionType.displayName}</span>{' '}
          <span className="text-xs text-text-secondary font-mono">
            ({actionType.apiName})
          </span>
          ?
        </p>
        <p className="text-xs text-text-secondary">
          Applications relying on this action type will fail until the code is
          updated. This cannot be undone.
        </p>
        {submitError && (
          <p
            role="alert"
            data-testid="action-type-delete-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="action-type-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="action-type-delete-confirm"
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
  testId,
  action,
  children,
}: {
  title: string;
  testId?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div data-testid={testId} className="flex flex-col gap-2">
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
