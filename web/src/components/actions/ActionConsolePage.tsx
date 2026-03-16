import { useState } from 'react';
import { useParams } from 'react-router';
import { useActionTypes, useApplyAction } from '../../hooks/useActions';
import type { ActionType, ActionApplyResponse } from '../../api/types';
import { ParameterForm } from './ParameterForm';
import { ActionResult } from './ActionResult';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

export function ActionConsolePage() {
  const { ontology } = useParams<{ ontology: string }>();
  const { data: actionTypes, isLoading } = useActionTypes(ontology ?? '');
  const applyMutation = useApplyAction(ontology ?? '');

  const [selectedAction, setSelectedAction] = useState<ActionType | null>(null);
  const [paramValues, setParamValues] = useState<Record<string, unknown>>({});
  const [result, setResult] = useState<ActionApplyResponse | null>(null);

  function handleSelectAction(action: ActionType) {
    setSelectedAction(action);
    setParamValues({});
    setResult(null);
  }

  function handleExecute() {
    if (!selectedAction) return;
    applyMutation.mutate(
      { actionType: selectedAction.apiName, parameters: paramValues },
      {
        onSuccess: (data) => setResult(data),
      },
    );
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

            {/* Parameters form */}
            <div>
              <h3 className="text-xs font-medium text-text-primary mb-3">Parameters</h3>
              <ParameterForm
                parameters={selectedAction.parameters}
                values={paramValues}
                onChange={setParamValues}
              />
            </div>

            {/* Execute button */}
            <div>
              <button
                onClick={handleExecute}
                disabled={applyMutation.isPending}
                className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
              >
                {applyMutation.isPending ? 'Executing...' : 'Execute Action'}
              </button>
              {applyMutation.isError && (
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
