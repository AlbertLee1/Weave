import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import {
  useAutomationExecutions,
  useAutomationRules,
  useCreateAutomationRule,
  useDeleteAutomationRule,
  usePauseAutomationRule,
  useResumeAutomationRule,
  useUpdateAutomationRule,
} from '../../hooks/useAutomationRules';
import type {
  AutomationExecution,
  AutomationRule,
  CreateAutomationRuleRequest,
  UpdateAutomationRuleRequest,
} from '../../api/automationRules';
import { ApiRequestError } from '../../api/client';
import { useToastStore } from '../../stores/toastStore';
import { EmptyState } from '../common/EmptyState';
import { SkeletonTable } from '../common/Skeleton';

// US-039 (PC-A01): Automation Rules management UI.
//
// The page lists every automation rule for the active ontology and lets
// ontology admins (a) create new rules, (b) edit trigger / condition /
// effects / debounce / throttle, (c) pause/resume the rule, (d) inspect
// recent executions for a rule. The page is reachable via
// `/automation/:ontology` (App.tsx) and surfaces in the Sidebar under
// the active ontology section once one is selected.
//
// Three opaque JSON blobs persist on every rule (`triggerConfig`,
// `effects`, `retryPolicy`). The UI keeps them as text-areas — admins
// hand-edit CEL-style condition expressions, effects sequences, and
// debounce / throttle config. The form validates JSON locally before
// submitting so syntax errors surface inline instead of through a 500
// from the backend.

interface RuleFormState {
  name: string;
  description: string;
  triggerType: 'schedule' | 'dataChange' | 'manual';
  // The "condition" key inside triggerConfig drives the BDD lifecycle
  // gate (see US-015 codebase note about `extractCondition`). We split
  // it out as a first-class form field for ergonomic editing — the
  // condition string is merged back into triggerConfig on submit.
  condition: string;
  triggerConfigJson: string;
  effectsJson: string;
  debounceMs: string;
  throttleMs: string;
}

const EMPTY_FORM: RuleFormState = {
  name: '',
  description: '',
  triggerType: 'schedule',
  condition: 'true',
  triggerConfigJson: '{}',
  effectsJson: '[]',
  debounceMs: '',
  throttleMs: '',
};

const TRIGGER_BADGE_STYLE: Record<AutomationRule['triggerType'], string> = {
  schedule: 'bg-indigo-500/10 text-indigo-300 border border-indigo-500/30',
  dataChange: 'bg-teal-500/10 text-teal-300 border border-teal-500/30',
  manual: 'bg-amber-500/10 text-amber-300 border border-amber-500/30',
};

const STATUS_BADGE_STYLE: Record<AutomationRule['status'], string> = {
  active: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  paused: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  disabled: 'bg-slate-500/10 text-slate-400 border border-slate-500/30',
};

function describeApiError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason ?? err.parameters?.error;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return 'Operation failed.';
}

function parseJson(value: string, fallback: unknown): unknown {
  if (value.trim() === '') return fallback;
  return JSON.parse(value);
}

function stringifyJson(value: unknown): string {
  if (value === undefined || value === null) return '';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function ruleToFormState(rule: AutomationRule): RuleFormState {
  // Pull `condition` out of triggerConfig and surface it as a top-level
  // form field. Keep the rest of the triggerConfig JSON (without the
  // condition key) in the raw textarea so admins can edit it.
  let condition = 'true';
  let trimmedConfigJson = '{}';
  if (rule.triggerConfig && typeof rule.triggerConfig === 'object') {
    const cfg = rule.triggerConfig as Record<string, unknown>;
    if (typeof cfg.condition === 'string') condition = cfg.condition;
    const { condition: _ignored, ...rest } = cfg;
    void _ignored;
    trimmedConfigJson = Object.keys(rest).length === 0 ? '{}' : stringifyJson(rest);
  }
  // retryPolicy carries debounce / throttle hints — we surface them as
  // separate numeric fields. Anything else on retryPolicy survives via
  // round-trip merge during submit (the page preserves untouched keys).
  let debounceMs = '';
  let throttleMs = '';
  if (rule.retryPolicy && typeof rule.retryPolicy === 'object') {
    const rp = rule.retryPolicy as Record<string, unknown>;
    if (typeof rp.debounceMs === 'number') debounceMs = String(rp.debounceMs);
    if (typeof rp.throttleMs === 'number') throttleMs = String(rp.throttleMs);
  }
  return {
    name: rule.name,
    description: rule.description ?? '',
    triggerType: rule.triggerType,
    condition,
    triggerConfigJson: trimmedConfigJson,
    effectsJson: stringifyJson(rule.effects ?? []),
    debounceMs,
    throttleMs,
  };
}

interface BuiltPayload {
  triggerConfig: Record<string, unknown>;
  effects: unknown;
  retryPolicy: Record<string, unknown> | undefined;
}

function buildPayload(form: RuleFormState, base?: AutomationRule): BuiltPayload {
  const cfgRest = parseJson(form.triggerConfigJson, {}) as Record<string, unknown>;
  const triggerConfig: Record<string, unknown> = {
    ...cfgRest,
    condition: form.condition,
  };

  const effects = parseJson(form.effectsJson, []);

  // Preserve other retryPolicy keys on the existing rule (e.g. retries
  // / backoff) — only overwrite debounceMs / throttleMs when the user
  // supplied them.
  const baseRP: Record<string, unknown> =
    base?.retryPolicy && typeof base.retryPolicy === 'object'
      ? { ...(base.retryPolicy as Record<string, unknown>) }
      : {};
  if (form.debounceMs.trim() === '') {
    delete baseRP.debounceMs;
  } else {
    baseRP.debounceMs = Number(form.debounceMs);
  }
  if (form.throttleMs.trim() === '') {
    delete baseRP.throttleMs;
  } else {
    baseRP.throttleMs = Number(form.throttleMs);
  }
  const retryPolicy = Object.keys(baseRP).length > 0 ? baseRP : undefined;

  return { triggerConfig, effects, retryPolicy };
}

export function AutomationRulesPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const activeOntology = ontology ?? '';

  const listQuery = useAutomationRules(activeOntology);
  const createMutation = useCreateAutomationRule(activeOntology);
  const updateMutation = useUpdateAutomationRule(activeOntology);
  const deleteMutation = useDeleteAutomationRule(activeOntology);
  const pauseMutation = usePauseAutomationRule(activeOntology);
  const resumeMutation = useResumeAutomationRule(activeOntology);
  const pushToast = useToastStore((s) => s.push);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editingRuleId, setEditingRuleId] = useState<string | null>(null);
  const [form, setForm] = useState<RuleFormState>(EMPTY_FORM);
  const [formError, setFormError] = useState<string | null>(null);
  const [pendingRuleId, setPendingRuleId] = useState<string | null>(null);
  const [executionsForRuleId, setExecutionsForRuleId] = useState<string | null>(null);

  const rules = useMemo(() => listQuery.data?.data ?? [], [listQuery.data]);

  if (!activeOntology) {
    return (
      <div
        data-testid="automation-rules-empty-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to manage its automation rules."
        />
      </div>
    );
  }

  const beginCreate = () => {
    setEditingRuleId(null);
    setForm(EMPTY_FORM);
    setFormError(null);
    setEditorOpen(true);
  };

  const beginEdit = (rule: AutomationRule) => {
    setEditingRuleId(rule.id);
    setForm(ruleToFormState(rule));
    setFormError(null);
    setEditorOpen(true);
  };

  const closeEditor = () => {
    setEditorOpen(false);
    setEditingRuleId(null);
    setFormError(null);
  };

  const submitForm = () => {
    setFormError(null);
    if (form.name.trim() === '') {
      setFormError('Name is required.');
      return;
    }
    let payload: BuiltPayload;
    try {
      const base = editingRuleId
        ? rules.find((r) => r.id === editingRuleId)
        : undefined;
      payload = buildPayload(form, base);
    } catch (err) {
      setFormError(
        err instanceof Error ? `Invalid JSON: ${err.message}` : 'Invalid JSON.',
      );
      return;
    }

    if (editingRuleId) {
      const body: UpdateAutomationRuleRequest = {
        name: form.name.trim(),
        description: form.description,
        triggerType: form.triggerType,
        triggerConfig: payload.triggerConfig,
        effects: payload.effects,
        retryPolicy: payload.retryPolicy,
      };
      updateMutation.mutate(
        { ruleId: editingRuleId, body },
        {
          onSuccess: () => {
            pushToast({
              message: `Saved rule "${form.name.trim()}".`,
              severity: 'success',
            });
            closeEditor();
          },
          onError: (err) => {
            const msg = describeApiError(err);
            setFormError(msg);
            pushToast({ message: msg, severity: 'error' });
          },
        },
      );
      return;
    }

    const createBody: CreateAutomationRuleRequest = {
      name: form.name.trim(),
      description: form.description,
      triggerType: form.triggerType,
      triggerConfig: payload.triggerConfig,
      effects: payload.effects,
      retryPolicy: payload.retryPolicy,
    };
    createMutation.mutate(createBody, {
      onSuccess: () => {
        pushToast({
          message: `Created rule "${createBody.name}".`,
          severity: 'success',
        });
        closeEditor();
      },
      onError: (err) => {
        const msg = describeApiError(err);
        setFormError(msg);
        pushToast({ message: msg, severity: 'error' });
      },
    });
  };

  const togglePause = (rule: AutomationRule) => {
    // A `disabled` rule has no honest active/paused toggle: the backend
    // resume handler does not distinguish disabled from paused and would
    // silently re-activate it. The toggle button is rendered disabled for
    // this state, but guard here too so no programmatic path can resume it.
    if (rule.status === 'disabled') return;
    setPendingRuleId(rule.id);
    const onSettled = () => setPendingRuleId(null);
    if (rule.status === 'active') {
      pauseMutation.mutate(rule.id, {
        onSettled,
        onSuccess: () => {
          pushToast({
            message: `Paused rule "${rule.name}".`,
            severity: 'info',
          });
        },
        onError: (err) =>
          pushToast({ message: describeApiError(err), severity: 'error' }),
      });
    } else {
      resumeMutation.mutate(rule.id, {
        onSettled,
        onSuccess: () => {
          pushToast({
            message: `Resumed rule "${rule.name}".`,
            severity: 'info',
          });
        },
        onError: (err) =>
          pushToast({ message: describeApiError(err), severity: 'error' }),
      });
    }
  };

  const deleteRule = (rule: AutomationRule) => {
    if (!window.confirm(`Delete automation rule "${rule.name}"?`)) return;
    setPendingRuleId(rule.id);
    deleteMutation.mutate(rule.id, {
      onSettled: () => setPendingRuleId(null),
      onSuccess: () => {
        pushToast({
          message: `Deleted rule "${rule.name}".`,
          severity: 'info',
        });
      },
      onError: (err) =>
        pushToast({ message: describeApiError(err), severity: 'error' }),
    });
  };

  return (
    <div
      data-testid="automation-rules-page"
      className="mx-auto max-w-6xl space-y-6"
    >
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            Automation Rules
          </h1>
          <p className="text-sm text-text-secondary">
            Schedule / data-change / manual triggers on{' '}
            <span className="font-mono text-text-primary">{activeOntology}</span>
            . Rules execute against the active ontology's actions and
            objects.
          </p>
        </div>
        <button
          type="button"
          onClick={beginCreate}
          data-testid="automation-rules-create-btn"
          className="rounded-md bg-amber-600 px-3 py-1.5 text-sm font-semibold text-white shadow hover:bg-amber-500"
        >
          New rule
        </button>
      </header>

      {listQuery.isLoading ? (
        <div data-testid="automation-rules-loading">
          <SkeletonTable rows={5} columns={4} aria-label="Loading automation rules" />
        </div>
      ) : listQuery.isError ? (
        <div data-testid="automation-rules-error">
          <EmptyState
            title="Failed to load automation rules"
            description={
              listQuery.error instanceof Error
                ? listQuery.error.message
                : 'Unexpected error.'
            }
          />
        </div>
      ) : rules.length === 0 ? (
        <div data-testid="automation-rules-empty">
          <EmptyState
            title="No automation rules yet"
            description="Create a rule to automate scheduled jobs or react to data changes."
            action={
              <button
                type="button"
                data-testid="automation-rules-empty-cta"
                onClick={beginCreate}
                className="rounded-md bg-amber-600 px-3 py-1.5 text-sm font-semibold text-white shadow hover:bg-amber-500"
              >
                + New rule
              </button>
            }
          />
        </div>
      ) : (
        <ul
          data-testid="automation-rules-list"
          aria-label="Automation rules"
          className="space-y-3"
        >
          {rules.map((rule) => (
            <li
              key={rule.id}
              data-testid="automation-rule-row"
              data-rule-id={rule.id}
              className="rounded-lg border border-border/50 bg-bg-secondary/60 p-4"
            >
              <div className="flex flex-wrap items-start gap-3">
                <div className="flex-1 space-y-1">
                  <div className="flex items-center gap-2">
                    <span
                      data-testid="automation-rule-trigger-badge"
                      className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${TRIGGER_BADGE_STYLE[rule.triggerType]}`}
                    >
                      {rule.triggerType}
                    </span>
                    <span
                      data-testid="automation-rule-status-badge"
                      className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[rule.status]}`}
                    >
                      {rule.status}
                    </span>
                    <h2
                      data-testid="automation-rule-name"
                      className="text-sm font-medium text-text-primary"
                    >
                      {rule.name}
                    </h2>
                  </div>
                  {rule.description && (
                    <p className="text-xs text-text-secondary">
                      {rule.description}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 gap-2">
                  <button
                    type="button"
                    onClick={() => togglePause(rule)}
                    disabled={
                      rule.status === 'disabled' || pendingRuleId === rule.id
                    }
                    title={
                      rule.status === 'disabled'
                        ? 'Disabled — this rule cannot be paused or resumed here.'
                        : rule.status === 'active'
                          ? 'Pause this rule'
                          : 'Resume this rule'
                    }
                    data-testid="automation-rule-toggle-btn"
                    data-rule-id={rule.id}
                    data-current-status={rule.status}
                    className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {rule.status === 'disabled'
                      ? 'Disabled'
                      : rule.status === 'active'
                        ? 'Pause'
                        : 'Resume'}
                  </button>
                  <button
                    type="button"
                    onClick={() => beginEdit(rule)}
                    data-testid="automation-rule-edit-btn"
                    data-rule-id={rule.id}
                    className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    onClick={() => setExecutionsForRuleId(rule.id)}
                    data-testid="automation-rule-executions-btn"
                    data-rule-id={rule.id}
                    className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
                  >
                    Executions
                  </button>
                  <button
                    type="button"
                    onClick={() => deleteRule(rule)}
                    disabled={pendingRuleId === rule.id}
                    data-testid="automation-rule-delete-btn"
                    data-rule-id={rule.id}
                    className="rounded-md border border-rose-500/50 px-3 py-1.5 text-xs font-medium text-rose-300 hover:bg-rose-500/10 disabled:opacity-60"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}

      {editorOpen && (
        <AutomationRuleEditorDrawer
          mode={editingRuleId ? 'edit' : 'create'}
          form={form}
          setForm={setForm}
          formError={formError}
          onCancel={closeEditor}
          onSubmit={submitForm}
          saving={createMutation.isPending || updateMutation.isPending}
        />
      )}

      {executionsForRuleId && (
        <AutomationRuleExecutionsDrawer
          ontology={activeOntology}
          ruleId={executionsForRuleId}
          ruleName={
            rules.find((r) => r.id === executionsForRuleId)?.name ??
            executionsForRuleId
          }
          onClose={() => setExecutionsForRuleId(null)}
        />
      )}
    </div>
  );
}

interface DrawerShellProps {
  testId: string;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}

function DrawerShell({ testId, title, onClose, children }: DrawerShellProps) {
  return (
    <div
      data-testid={testId}
      role="dialog"
      aria-modal="true"
      aria-label={title}
      className="fixed inset-0 z-40 flex"
    >
      <div
        data-testid={`${testId}-overlay`}
        className="flex-1 bg-black/50"
        onClick={onClose}
      />
      <div
        className="w-[36rem] max-w-full bg-bg-primary border-l border-border/60 overflow-y-auto"
        style={{ boxShadow: '-8px 0 32px rgba(0,0,0,0.4)' }}
      >
        <header className="flex items-center justify-between border-b border-border/40 px-4 py-3">
          <h2 className="text-base font-semibold text-text-primary">{title}</h2>
          <button
            type="button"
            data-testid={`${testId}-close-btn`}
            onClick={onClose}
            aria-label="Close drawer"
            className="rounded p-1 text-text-secondary hover:bg-bg-tertiary hover:text-text-primary"
          >
            <svg viewBox="0 0 16 16" className="h-4 w-4" aria-hidden="true">
              <path
                d="M3 3 L13 13 M13 3 L3 13"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
                fill="none"
              />
            </svg>
          </button>
        </header>
        <div className="p-4">{children}</div>
      </div>
    </div>
  );
}

interface EditorDrawerProps {
  mode: 'create' | 'edit';
  form: RuleFormState;
  setForm: React.Dispatch<React.SetStateAction<RuleFormState>>;
  formError: string | null;
  onCancel: () => void;
  onSubmit: () => void;
  saving: boolean;
}

function AutomationRuleEditorDrawer({
  mode,
  form,
  setForm,
  formError,
  onCancel,
  onSubmit,
  saving,
}: EditorDrawerProps) {
  const setField = <K extends keyof RuleFormState>(
    key: K,
    value: RuleFormState[K],
  ) => setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <DrawerShell
      testId="automation-rule-editor-drawer"
      title={mode === 'create' ? 'New automation rule' : 'Edit automation rule'}
      onClose={onCancel}
    >
      <form
        className="space-y-4 text-sm"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
      >
        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Name
          </span>
          <input
            type="text"
            data-testid="automation-rule-form-name"
            value={form.name}
            onChange={(e) => setField('name', e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-text-primary"
          />
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Description
          </span>
          <input
            type="text"
            data-testid="automation-rule-form-description"
            value={form.description}
            onChange={(e) => setField('description', e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-text-primary"
          />
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Trigger
          </span>
          <select
            data-testid="automation-rule-form-trigger"
            value={form.triggerType}
            onChange={(e) =>
              setField(
                'triggerType',
                e.target.value as RuleFormState['triggerType'],
              )
            }
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-text-primary"
          >
            <option value="schedule">schedule</option>
            <option value="dataChange">dataChange</option>
            <option value="manual">manual</option>
          </select>
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Condition (CEL)
          </span>
          <input
            type="text"
            data-testid="automation-rule-form-condition"
            value={form.condition}
            onChange={(e) => setField('condition', e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 font-mono text-text-primary"
          />
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Trigger config (JSON)
          </span>
          <textarea
            data-testid="automation-rule-form-trigger-config"
            value={form.triggerConfigJson}
            onChange={(e) => setField('triggerConfigJson', e.target.value)}
            rows={4}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 font-mono text-xs text-text-primary"
          />
        </label>

        <label className="block">
          <span className="block text-xs font-medium text-text-secondary mb-1">
            Effects (JSON array)
          </span>
          <textarea
            data-testid="automation-rule-form-effects"
            value={form.effectsJson}
            onChange={(e) => setField('effectsJson', e.target.value)}
            rows={4}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 font-mono text-xs text-text-primary"
          />
        </label>

        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Debounce (ms)
            </span>
            <input
              type="number"
              data-testid="automation-rule-form-debounce"
              value={form.debounceMs}
              onChange={(e) => setField('debounceMs', e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-text-primary"
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Throttle (ms)
            </span>
            <input
              type="number"
              data-testid="automation-rule-form-throttle"
              value={form.throttleMs}
              onChange={(e) => setField('throttleMs', e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-text-primary"
            />
          </label>
        </div>

        {formError && (
          <div
            role="alert"
            data-testid="automation-rule-form-error"
            className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          >
            {formError}
          </div>
        )}

        <footer className="flex items-center justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onCancel}
            data-testid="automation-rule-form-cancel-btn"
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="automation-rule-form-save-btn"
            disabled={saving}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </footer>
      </form>
    </DrawerShell>
  );
}

interface ExecutionsDrawerProps {
  ontology: string;
  ruleId: string;
  ruleName: string;
  onClose: () => void;
}

function AutomationRuleExecutionsDrawer({
  ontology,
  ruleId,
  ruleName,
  onClose,
}: ExecutionsDrawerProps) {
  const query = useAutomationExecutions(ontology, ruleId);
  const executions = query.data?.data ?? [];

  return (
    <DrawerShell
      testId="automation-rule-executions-drawer"
      title={`Executions — ${ruleName}`}
      onClose={onClose}
    >
      {query.isLoading ? (
        <div data-testid="automation-rule-executions-loading">
          <SkeletonTable rows={4} columns={3} aria-label="Loading executions" />
        </div>
      ) : query.isError ? (
        <div data-testid="automation-rule-executions-error">
          <EmptyState
            title="Failed to load executions"
            description={
              query.error instanceof Error
                ? query.error.message
                : 'Unexpected error.'
            }
          />
        </div>
      ) : executions.length === 0 ? (
        <div data-testid="automation-rule-executions-empty">
          <EmptyState
            title="No executions yet"
            description="The rule has not fired."
          />
        </div>
      ) : (
        <ul
          data-testid="automation-rule-executions-list"
          className="space-y-2"
        >
          {executions.map((exec) => (
            <ExecutionRow key={exec.id} exec={exec} />
          ))}
        </ul>
      )}
    </DrawerShell>
  );
}

const EXEC_STATUS_STYLE: Record<AutomationExecution['status'], string> = {
  running: 'bg-indigo-500/10 text-indigo-300 border border-indigo-500/30',
  success: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  error: 'bg-rose-500/10 text-rose-300 border border-rose-500/30',
  retrying: 'bg-amber-500/10 text-amber-300 border border-amber-500/30',
};

function ExecutionRow({ exec }: { exec: AutomationExecution }) {
  return (
    <li
      data-testid="automation-rule-execution-row"
      data-execution-id={exec.id}
      className="rounded-md border border-border/40 bg-bg-secondary/40 px-3 py-2"
    >
      <div className="flex items-center gap-2">
        <span
          data-testid="automation-rule-execution-status"
          className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${EXEC_STATUS_STYLE[exec.status]}`}
        >
          {exec.status}
        </span>
        <span className="font-mono text-xs text-text-primary">{exec.id}</span>
      </div>
      <dl className="mt-1 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-[11px] text-text-secondary">
        <dt>Started</dt>
        <dd className="text-text-primary">
          {new Date(exec.startedAt).toLocaleString()}
        </dd>
        {exec.completedAt && (
          <>
            <dt>Completed</dt>
            <dd className="text-text-primary">
              {new Date(exec.completedAt).toLocaleString()}
            </dd>
          </>
        )}
        <dt>Retries</dt>
        <dd className="text-text-primary">{exec.retryCount}</dd>
        {exec.error && (
          <>
            <dt>Error</dt>
            <dd className="text-rose-300">{exec.error}</dd>
          </>
        )}
      </dl>
    </li>
  );
}
