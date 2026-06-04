import { useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import {
  useActionTypes,
  useApplyAction,
  useRevertActionLog,
} from '../../hooks/useActions';
import { useObjectVersion } from '../../hooks/useObjectVersion';
import type {
  ActionType,
  ActionApplyResponse,
  ActionApplyOptions,
} from '../../api/types';
import { ApiRequestError } from '../../api/client';
import { applyActionAsync } from '../../api/actions';
import { ParameterForm } from './ParameterForm';
import { ActionResult } from './ActionResult';
import { AsyncJobProgressPanel } from './AsyncJobProgressPanel';
import { ActionTemplatesPanel } from './ActionTemplatesPanel';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { useToastStore } from '../../stores/toastStore';
import { AuthContext } from '../../auth/AuthContext';
import {
  buildParameterDefaults,
  buildParameterZodSchema,
  parseSchemaViolations,
} from './parameterSchema';

export function ActionConsolePage() {
  const { ontology } = useParams<{ ontology: string }>();
  const { data: actionTypes, isLoading } = useActionTypes(ontology ?? '');
  const applyMutation = useApplyAction(ontology ?? '');
  const revertMutation = useRevertActionLog(ontology ?? '');
  const pushToast = useToastStore((s) => s.push);
  const dismissToast = useToastStore((s) => s.dismiss);
  // Use the AuthContext directly (rather than useAuth) so unit tests
  // mounting this page without an AuthProvider don't have to grow a
  // wrapper. Templates owned by the authenticated caller gain a
  // delete affordance; without a context the panel still works for
  // load-and-save against the caller's id (decided server-side).
  const authCtx = useContext(AuthContext);
  const currentUserId = authCtx?.user?.id;

  const [selectedAction, setSelectedAction] = useState<ActionType | null>(null);
  // Mirror of selectedAction read inside async callbacks to detect when the
  // user has moved on before an in-flight apply resolves (staleness guard).
  const selectedActionRef = useRef<ActionType | null>(selectedAction);
  selectedActionRef.current = selectedAction;
  const [result, setResult] = useState<ActionApplyResponse | null>(null);
  const [targetObjectType, setTargetObjectType] = useState('');
  const [targetPrimaryKey, setTargetPrimaryKey] = useState('');
  // Foundry OSv2 single-apply options. Defaults match the server: the apply
  // runs (VALIDATE_AND_EXECUTE) and returns the full edit summary (ALL).
  // ALL_V2_WITH_DELETIONS is single-apply only — the batch path rejects it.
  const [applyMode, setApplyMode] =
    useState<NonNullable<ActionApplyOptions['mode']>>('VALIDATE_AND_EXECUTE');
  const [returnEdits, setReturnEdits] =
    useState<NonNullable<ActionApplyOptions['returnEdits']>>('ALL');
  // US-240: opt-in async single-apply. When enabled, Execute hits
  // /apply?async=true, captures the 202 {jobId}, and the AsyncJobProgressPanel
  // polls GET /actions/jobs/{jobId} until the job settles.
  const [asyncMode, setAsyncMode] = useState(false);
  const [asyncJobId, setAsyncJobId] = useState<string | null>(null);
  const [asyncSubmitting, setAsyncSubmitting] = useState(false);
  const [asyncError, setAsyncError] = useState<string | null>(null);
  const [staleConflict, setStaleConflict] = useState<null | {
    currentVersion?: string;
  }>(null);

  // Build the Zod schema + defaults from the selected action's parameters.
  // The schema is rebuilt whenever the user picks a different action; an
  // empty schema is used as a placeholder when no action is selected.
  const formSchema = useMemo(
    () => buildParameterZodSchema(selectedAction?.parameters ?? {}),
    [selectedAction],
  );
  const formDefaults = useMemo(
    () => buildParameterDefaults(selectedAction?.parameters ?? {}),
    [selectedAction],
  );

  const form = useForm<Record<string, unknown>>({
    resolver: zodResolver(formSchema),
    defaultValues: formDefaults,
    mode: 'onBlur',
  });

  // Reset form whenever the selected action changes so the new schema
  // governs the inputs and stale values from a previous action don't leak.
  useEffect(() => {
    form.reset(formDefaults);
    // form is stable; depending on it would loop. Reset must run when the
    // selected action's defaults change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [formDefaults]);

  const versionQuery = useObjectVersion({
    ontologyApiName: ontology ?? '',
    objectType: targetObjectType,
    primaryKey: targetPrimaryKey,
  });

  function handleSelectAction(action: ActionType) {
    setSelectedAction(action);
    setResult(null);
    setStaleConflict(null);
    setAsyncJobId(null);
    setAsyncError(null);
  }

  function executeWithValues(paramValues: Record<string, unknown>) {
    if (!selectedAction) return;
    setStaleConflict(null);
    const expectedVersion = versionQuery.version;
    // Strip undefined entries so optional fields don't surface as
    // explicit-null on the wire.
    const cleanedParams: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(paramValues)) {
      if (v !== undefined) cleanedParams[k] = v;
    }
    // Assemble apply options. Only send fields that diverge from the server
    // defaults so the wire stays minimal (and so omitted == server default).
    const options: ActionApplyOptions = {};
    if (applyMode !== 'VALIDATE_AND_EXECUTE') options.mode = applyMode;
    if (returnEdits !== 'ALL') options.returnEdits = returnEdits;
    if (expectedVersion !== undefined) options.expectedVersion = expectedVersion;

    // Async path: POST /apply?async=true, capture the 202 {jobId}, and let the
    // AsyncJobProgressPanel poll the job-status endpoint to completion. The
    // synchronous result panel + Undo toast are skipped — the polled job's
    // result payload carries the same edit summary.
    if (asyncMode) {
      // Capture the action so a late-resolving promise that lands after the
      // user picked a different action (or toggled async off) is ignored.
      const submittedAction = selectedAction;
      setResult(null);
      setAsyncJobId(null);
      setAsyncError(null);
      setAsyncSubmitting(true);
      applyActionAsync(ontology ?? '', submittedAction.apiName, {
        parameters: cleanedParams,
        ...(Object.keys(options).length > 0 ? { options } : {}),
      })
        .then((resp) => {
          // Staleness guard: drop the response if the user moved on.
          if (selectedActionRef.current !== submittedAction) return;
          if (resp.jobId) {
            setAsyncJobId(resp.jobId);
            return;
          }
          // Degraded-mode fallthrough: when no ActionJobStore is wired the
          // server ignores ?async=true and returns the synchronous
          // SyncApplyActionResponseV2 envelope (no jobId) at HTTP 200. Surface
          // it like a sync result rather than mounting a poll panel that would
          // hang on "Pending" with no job to poll.
          const syncResult = resp as unknown as ActionApplyResponse;
          setResult(syncResult);
          if (syncResult.actionLogId) {
            pushUndoToast(
              syncResult.actionLogId,
              submittedAction.displayName ?? submittedAction.apiName,
            );
          }
        })
        .catch((err) => {
          if (selectedActionRef.current !== submittedAction) return;
          setAsyncError(
            err instanceof Error ? err.message : 'Async apply failed',
          );
        })
        .finally(() => {
          if (selectedActionRef.current !== submittedAction) return;
          setAsyncSubmitting(false);
        });
      return;
    }

    applyMutation.mutate(
      {
        action: selectedAction.apiName,
        parameters: cleanedParams,
        ...(Object.keys(options).length > 0 ? { options } : {}),
      },
      {
        onSuccess: (data) => {
          setResult(data);
          // US-319: 5-second Undo toast. Only fires when the apply path
          // produced a persisted action_logs row (validate-only and noop
          // edits skip the log entirely so actionLogId is absent).
          if (data.actionLogId) {
            pushUndoToast(data.actionLogId, selectedAction.displayName ?? selectedAction.apiName);
          }
        },
        onError: (err) => {
          if (err instanceof ApiRequestError) {
            if (err.statusCode === 409 && err.errorName === 'StaleObject') {
              setStaleConflict({
                currentVersion: err.parameters?.currentVersion,
              });
              return;
            }
            // US-322: surface 422 schema violations as field-level errors
            // on the corresponding form inputs.
            if (err.statusCode === 422 && err.errorCode === 'WEAVE_VALIDATION_SCHEMA') {
              const violations = parseSchemaViolations(err.parameters);
              if (violations.length > 0) {
                for (const v of violations) {
                  if (v.field) {
                    form.setError(v.field, { type: 'server', message: v.reason });
                  }
                }
                return;
              }
            }
          }
        },
      },
    );
  }

  function pushUndoToast(actionLogId: number, label: string) {
    const toastId = pushToast({
      message: `Action "${label}" applied`,
      severity: 'success',
      ttlMs: 5000,
      actionLabel: 'Undo',
      onAction: () => {
        revertMutation.mutate(actionLogId, {
          onSuccess: () => {
            dismissToast(toastId);
            pushToast({
              message: `Action "${label}" reverted`,
              severity: 'info',
              ttlMs: 4000,
            });
          },
          onError: (err) => {
            dismissToast(toastId);
            const message =
              err instanceof ApiRequestError && err.errorName === 'AlreadyReverted'
                ? `Action already reverted`
                : err instanceof Error
                  ? `Undo failed: ${err.message}`
                  : 'Undo failed';
            pushToast({
              message,
              severity: 'error',
              ttlMs: 4000,
            });
          },
        });
      },
    });
  }

  function handleReload() {
    setStaleConflict(null);
    void versionQuery.refetch();
  }

  if (!ontology) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState title="No Ontology" description="Select an ontology to use the action console." />
      </div>
    );
  }

  // Watching the form values keeps the templates panel and the Save button
  // gating in sync as the user edits inputs.
  const watchedValues = form.watch();

  return (
    <div className="flex h-full">
      {/* Stable page-level heading. Visually hidden so the layout is
          unchanged, but it gives the standalone Action Console route exactly
          one <h1> across every main state, matching the rest of the app's
          heading structure (the visible panel headings stay <h2>). */}
      <h1 className="sr-only">Action Console</h1>

      {/* Left panel: action types list */}
      <div className="w-72 border-r border-border flex flex-col bg-bg-primary">
        <div className="px-4 py-3 border-b border-border">
          <h2 className="text-sm font-medium text-text-primary">Action Types</h2>
          <div className="text-xs text-text-secondary mt-0.5 font-mono">{ontology}</div>
        </div>
        <div className="flex-1 overflow-y-auto">
          {isLoading ? (
            <div className="py-8">
              <LoadingSpinner size="sm" />
            </div>
          ) : !actionTypes || actionTypes.length === 0 ? (
            <div className="p-4 text-xs text-text-secondary">No action types found.</div>
          ) : (
            actionTypes.map((at) => (
              <button
                key={at.rid}
                onClick={() => handleSelectAction(at)}
                className={`w-full text-left px-4 py-3 text-sm border-b border-border transition-colors ${
                  selectedAction?.rid === at.rid
                    ? 'bg-bg-tertiary text-accent-cyan'
                    : 'text-text-primary hover:bg-bg-tertiary'
                }`}
              >
                <div className="font-mono text-xs">{at.apiName}</div>
                <div className="text-text-secondary text-xs mt-0.5">{at.displayName}</div>
              </button>
            ))
          )}
        </div>
      </div>

      {/* Right panel: action details + form + result */}
      <div className="flex-1 overflow-y-auto p-6">
        {!selectedAction ? (
          <div className="flex items-center justify-center h-full">
            <EmptyState
              title="Select an Action"
              description="Choose an action type from the left panel to configure and execute."
            />
          </div>
        ) : (
          <FormProvider {...form}>
            <form
              className="max-w-2xl flex flex-col gap-6"
              onSubmit={form.handleSubmit(executeWithValues)}
              noValidate
            >
              {/* Action info */}
              <div>
                <h2 className="text-sm font-medium text-text-primary">{selectedAction.displayName}</h2>
                <div className="text-xs font-mono text-text-secondary mt-1">{selectedAction.apiName}</div>
                {selectedAction.description && (
                  <p className="text-xs text-text-secondary mt-2">{selectedAction.description}</p>
                )}
              </div>

              {/* Target object (optional) — used for optimistic concurrency */}
              <div>
                <h3 className="text-xs font-medium text-text-primary mb-3">Target Object (optional)</h3>
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col">
                    <label
                      htmlFor="target-object-type"
                      className="text-xs text-text-secondary font-sans mb-1"
                    >
                      Target object type
                    </label>
                    <input
                      id="target-object-type"
                      type="text"
                      value={targetObjectType}
                      onChange={(e) => setTargetObjectType(e.target.value)}
                      className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full"
                      placeholder="Employee"
                    />
                  </div>
                  <div className="flex flex-col">
                    <label
                      htmlFor="target-primary-key"
                      className="text-xs text-text-secondary font-sans mb-1"
                    >
                      Target primary key
                    </label>
                    <input
                      id="target-primary-key"
                      type="text"
                      value={targetPrimaryKey}
                      onChange={(e) => setTargetPrimaryKey(e.target.value)}
                      className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full"
                      placeholder="E1"
                    />
                  </div>
                  {versionQuery.version !== undefined && (
                    <div className="text-xs text-text-secondary">
                      Current version:{' '}
                      <span
                        data-testid="object-version"
                        className="font-mono text-accent-cyan"
                      >
                        {versionQuery.version}
                      </span>
                    </div>
                  )}
                </div>
              </div>

              {/* Parameter templates panel — save / load named parameter sets. */}
              <ActionTemplatesPanel
                ontology={ontology}
                actionType={selectedAction.apiName}
                currentParameters={watchedValues}
                hasCurrentState={Object.keys(watchedValues).some(
                  (k) => watchedValues[k] !== undefined && watchedValues[k] !== '',
                )}
                onLoad={(parameters) => form.reset({ ...formDefaults, ...parameters })}
                currentUserId={currentUserId}
              />

              {/* Parameters form. Passing ontology + action wires the
                  debounced, side-effect-free real-time /validate surface so
                  fields red-line as the user types and submission-criteria
                  failures surface as a form-level banner. */}
              <div>
                <h3 className="text-xs font-medium text-text-primary mb-3">Parameters</h3>
                <ParameterForm
                  parameters={selectedAction.parameters}
                  ontologyApiName={ontology}
                  actionApiName={selectedAction.apiName}
                />
              </div>

              {/* Stale-object conflict banner */}
              {staleConflict && (
                <div
                  role="alert"
                  className="border border-accent-error rounded p-3 bg-accent-error/10"
                >
                  <div className="text-xs text-text-primary font-medium">
                    This object was updated elsewhere
                  </div>
                  <div className="text-xs text-text-secondary mt-1">
                    Reload to continue
                    {staleConflict.currentVersion &&
                      ` (current version: ${staleConflict.currentVersion})`}
                  </div>
                  <button
                    type="button"
                    onClick={handleReload}
                    className="mt-2 bg-accent-error text-bg-primary px-3 py-1 rounded text-xs font-medium hover:bg-accent-error/80"
                  >
                    Reload
                  </button>
                </div>
              )}

              {/* Apply options — validation mode + edit summary verbosity */}
              <div>
                <h3 className="text-xs font-medium text-text-primary mb-3">Apply Options</h3>
                <div className="flex flex-col gap-3 sm:flex-row sm:gap-4">
                  <div className="flex flex-col flex-1">
                    <label
                      htmlFor="apply-mode-select"
                      className="text-xs text-text-secondary font-sans mb-1"
                    >
                      Validation mode
                    </label>
                    <select
                      id="apply-mode-select"
                      data-testid="apply-mode-select"
                      value={applyMode}
                      onChange={(e) =>
                        setApplyMode(
                          e.target.value as NonNullable<ActionApplyOptions['mode']>,
                        )
                      }
                      className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full"
                    >
                      <option value="VALIDATE_AND_EXECUTE">Validate and execute</option>
                      <option value="VALIDATE_ONLY">Validate only</option>
                    </select>
                  </div>
                  <div className="flex flex-col flex-1">
                    <label
                      htmlFor="apply-return-edits-select"
                      className="text-xs text-text-secondary font-sans mb-1"
                    >
                      Return edits
                    </label>
                    <select
                      id="apply-return-edits-select"
                      data-testid="apply-return-edits-select"
                      value={returnEdits}
                      onChange={(e) =>
                        setReturnEdits(
                          e.target.value as NonNullable<
                            ActionApplyOptions['returnEdits']
                          >,
                        )
                      }
                      className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full"
                    >
                      <option value="ALL">All</option>
                      <option value="ALL_V2_WITH_DELETIONS">All (with deletions)</option>
                      <option value="NONE">None</option>
                    </select>
                  </div>
                </div>
                {/* Async toggle — when on, Execute runs the action in the
                    background and the job-status panel polls to completion. */}
                <label className="flex items-center gap-2 mt-3 text-xs text-text-secondary font-sans cursor-pointer">
                  <input
                    type="checkbox"
                    data-testid="async-apply-toggle"
                    checked={asyncMode}
                    onChange={(e) => {
                      setAsyncMode(e.target.checked);
                      // Clear any prior job state when the mode flips so the
                      // panel doesn't show a stale run.
                      setAsyncJobId(null);
                      setAsyncError(null);
                    }}
                    className="accent-accent-cyan"
                  />
                  Run asynchronously (background job)
                </label>
              </div>

              {/* Execute button */}
              <div>
                <button
                  type="submit"
                  disabled={applyMutation.isPending || asyncSubmitting}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
                >
                  {applyMutation.isPending || asyncSubmitting
                    ? 'Executing...'
                    : 'Execute Action'}
                </button>
                {applyMutation.isError && !staleConflict && (
                  <div className="mt-2 text-xs text-accent-error">
                    Error: {applyMutation.error instanceof Error ? applyMutation.error.message : 'Action failed'}
                  </div>
                )}
                {asyncError && (
                  <div className="mt-2 text-xs text-accent-error" role="alert">
                    Error: {asyncError}
                  </div>
                )}
              </div>

              {/* Async job progress — only mounts once an async apply has been
                  scheduled. Polls GET /actions/jobs/{jobId} to completion. */}
              {asyncMode && (asyncJobId !== null || asyncSubmitting) && (
                <AsyncJobProgressPanel
                  ontologyApiName={ontology}
                  jobId={asyncJobId}
                />
              )}

              {/* Sync result — also covers the async degraded-mode fallthrough
                  where the server returns a synchronous envelope (no jobId). */}
              <ActionResult result={result} />
            </form>
          </FormProvider>
        )}
      </div>
    </div>
  );
}
