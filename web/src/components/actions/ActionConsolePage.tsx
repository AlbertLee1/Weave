import { useContext, useState } from 'react';
import { useParams } from 'react-router';
import {
  useActionTypes,
  useApplyAction,
  useRevertActionLog,
} from '../../hooks/useActions';
import { useObjectVersion } from '../../hooks/useObjectVersion';
import type { ActionType, ActionApplyResponse } from '../../api/types';
import { ApiRequestError } from '../../api/client';
import { ParameterForm } from './ParameterForm';
import { ActionResult } from './ActionResult';
import { ActionTemplatesPanel } from './ActionTemplatesPanel';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { useToastStore } from '../../stores/toastStore';
import { AuthContext } from '../../auth/AuthContext';

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
  const [paramValues, setParamValues] = useState<Record<string, unknown>>({});
  const [result, setResult] = useState<ActionApplyResponse | null>(null);
  const [targetObjectType, setTargetObjectType] = useState('');
  const [targetPrimaryKey, setTargetPrimaryKey] = useState('');
  const [staleConflict, setStaleConflict] = useState<null | {
    currentVersion?: string;
  }>(null);

  const versionQuery = useObjectVersion({
    ontologyApiName: ontology ?? '',
    objectType: targetObjectType,
    primaryKey: targetPrimaryKey,
  });

  function handleSelectAction(action: ActionType) {
    setSelectedAction(action);
    setParamValues({});
    setResult(null);
    setStaleConflict(null);
  }

  function handleExecute() {
    if (!selectedAction) return;
    setStaleConflict(null);
    const expectedVersion = versionQuery.version;
    applyMutation.mutate(
      {
        action: selectedAction.apiName,
        parameters: paramValues,
        ...(expectedVersion !== undefined
          ? { options: { expectedVersion } }
          : {}),
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
          if (
            err instanceof ApiRequestError &&
            err.statusCode === 409 &&
            err.errorName === 'StaleObject'
          ) {
            setStaleConflict({
              currentVersion: err.parameters?.currentVersion,
            });
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

  return (
    <div className="flex h-full">
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
          <div className="max-w-2xl flex flex-col gap-6">
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
              currentParameters={paramValues}
              hasCurrentState={Object.keys(paramValues).length > 0}
              onLoad={(parameters) => setParamValues({ ...parameters })}
              currentUserId={currentUserId}
            />

            {/* Parameters form */}
            <div>
              <h3 className="text-xs font-medium text-text-primary mb-3">Parameters</h3>
              <ParameterForm
                parameters={selectedAction.parameters}
                values={paramValues}
                onChange={setParamValues}
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
                  onClick={handleReload}
                  className="mt-2 bg-accent-error text-bg-primary px-3 py-1 rounded text-xs font-medium hover:bg-accent-error/80"
                >
                  Reload
                </button>
              </div>
            )}

            {/* Execute button */}
            <div>
              <button
                onClick={handleExecute}
                disabled={applyMutation.isPending}
                className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
              >
                {applyMutation.isPending ? 'Executing...' : 'Execute Action'}
              </button>
              {applyMutation.isError && !staleConflict && (
                <div className="mt-2 text-xs text-accent-error">
                  Error: {applyMutation.error instanceof Error ? applyMutation.error.message : 'Action failed'}
                </div>
              )}
            </div>

            {/* Result */}
            <ActionResult result={result} />
          </div>
        )}
      </div>
    </div>
  );
}
