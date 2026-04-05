import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { useActionTypes } from '../../hooks/useActions';
import { useUpdateActionType } from '../../hooks/useAdminMutations';
import { listActionLogs } from '../../api/admin';
import { Badge, statusVariant } from '../common/Badge';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import type { ActionType, ActionLog } from '../../api/types';

type TabKey = 'overview' | 'logic' | 'observability';

const tabs: { key: TabKey; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'logic', label: 'Logic' },
  { key: 'observability', label: 'Observability' },
];

export function ActionTypeDetailPage() {
  const {
    ontology: ontologyApiName = '',
    actionType: actionTypeApiName = '',
  } = useParams<{ ontology: string; actionType: string }>();
  const navigate = useNavigate();

  const { data: actionTypes, isLoading } = useActionTypes(ontologyApiName);
  const actionType: ActionType | undefined = actionTypes?.find(
    (at) => at.apiName === actionTypeApiName,
  );

  const [activeTab, setActiveTab] = useState<TabKey>('overview');

  // --- Overview state ---
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [status, setStatus] = useState('ACTIVE');

  // --- Logic state ---
  const [parametersJson, setParametersJson] = useState('[]');
  const [rulesJson, setRulesJson] = useState('[]');
  const [submissionCriteriaJson, setSubmissionCriteriaJson] = useState('{}');
  const [sideEffectsJson, setSideEffectsJson] = useState('[]');
  const [jsonError, setJsonError] = useState<string | null>(null);

  useEffect(() => {
    if (actionType) {
      setDisplayName(actionType.displayName);
      setDescription(actionType.description ?? '');
      setStatus(actionType.status);
      setParametersJson(
        actionType.parameters
          ? JSON.stringify(actionType.parameters, null, 2)
          : '[]',
      );
      setRulesJson(
        actionType.rules
          ? JSON.stringify(actionType.rules, null, 2)
          : '[]',
      );
      setSubmissionCriteriaJson(
        actionType.submissionCriteria
          ? JSON.stringify(actionType.submissionCriteria, null, 2)
          : '{}',
      );
      setSideEffectsJson(
        actionType.sideEffects
          ? JSON.stringify(actionType.sideEffects, null, 2)
          : '[]',
      );
    }
  }, [actionType]);

  // --- Observability ---
  const {
    data: logsData,
    isLoading: logsLoading,
  } = useQuery({
    queryKey: ['actionLogs', actionType?.rid],
    queryFn: () => listActionLogs(actionType!.rid),
    enabled: !!actionType?.rid && activeTab === 'observability',
  });

  // --- Mutations ---
  const updateMutation = useUpdateActionType(ontologyApiName);

  // --- Handlers ---
  function handleSaveOverview() {
    if (!actionType) return;
    updateMutation.mutate({
      rid: actionType.rid,
      input: {
        displayName,
        description: description || undefined,
        status,
      },
    });
  }

  function handleSaveLogic() {
    if (!actionType) return;
    setJsonError(null);
    try {
      const params = JSON.parse(parametersJson);
      const rules = JSON.parse(rulesJson);
      const criteria = JSON.parse(submissionCriteriaJson);
      const effects = JSON.parse(sideEffectsJson);
      updateMutation.mutate({
        rid: actionType.rid,
        input: {
          displayName: actionType.displayName,
          status: actionType.status,
          parameters: params,
          rules,
          submissionCriteria: criteria,
          sideEffects: effects,
        },
      });
    } catch {
      setJsonError('Invalid JSON. Please fix the syntax and try again.');
      return;
    }
  }

  // --- Parse parameters for card view ---
  let parameterCards: { name: string; type: string; required: boolean }[] = [];
  try {
    const parsed = JSON.parse(parametersJson);
    if (Array.isArray(parsed)) {
      parameterCards = parsed.map((p: Record<string, unknown>) => ({
        name: (p.name as string) ?? (p.apiName as string) ?? 'unknown',
        type: (p.type as string) ?? (p.baseType as string) ?? 'unknown',
        required: !!p.required,
      }));
    }
  } catch {
    // not valid JSON, skip card view
  }

  // --- Parse rules for card view ---
  let ruleCards: { type: string; objectType?: string }[] = [];
  try {
    const parsed = JSON.parse(rulesJson);
    if (Array.isArray(parsed)) {
      ruleCards = parsed.map((r: Record<string, unknown>) => ({
        type: (r.type as string) ?? 'unknown',
        objectType: r.objectType as string | undefined,
      }));
    }
  } catch {
    // not valid JSON
  }

  // --- Styling constants ---
  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';
  const textareaClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none resize-y';

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (!actionType) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Action Type Not Found"
          description={`Could not load action type "${actionTypeApiName}".`}
          action={
            <button
              onClick={() => navigate(`/admin/${ontologyApiName}`)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              Back to Admin
            </button>
          }
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-6 py-4 border-b border-border bg-bg-primary">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate(`/admin/${ontologyApiName}`)}
            className="text-text-secondary hover:text-text-primary transition-colors"
            aria-label="Back"
          >
            <svg
              className="w-5 h-5"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
          <div className="flex-1">
            <h1 className="text-lg font-semibold text-text-primary">
              {actionType.displayName}
            </h1>
            <p className="text-xs font-mono text-text-secondary mt-0.5">
              {actionType.apiName}
            </p>
          </div>
          <Badge variant={statusVariant(actionType.status)}>
            {actionType.status}
          </Badge>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border bg-bg-primary px-6">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            className={`px-4 py-2.5 text-sm font-medium transition-colors border-b-2 ${
              activeTab === tab.key
                ? 'border-accent-cyan text-accent-cyan'
                : 'border-transparent text-text-secondary hover:text-text-primary'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      <div className="flex-1 overflow-y-auto p-6">
        {/* ==================== OVERVIEW TAB ==================== */}
        {activeTab === 'overview' && (
          <div className="max-w-2xl">
            <div className="grid grid-cols-2 gap-4 mb-6">
              <div className="p-4 bg-bg-tertiary border border-border rounded">
                <div className="text-2xl font-semibold text-text-primary">
                  {parameterCards.length}
                </div>
                <div className="text-xs text-text-secondary mt-1">
                  Parameters
                </div>
              </div>
              <div className="p-4 bg-bg-tertiary border border-border rounded">
                <div className="text-2xl font-semibold text-text-primary">
                  {ruleCards.length}
                </div>
                <div className="text-xs text-text-secondary mt-1">Rules</div>
              </div>
            </div>

            <div className="flex flex-col gap-4">
              <div className="flex flex-col">
                <label className={labelClass}>Display Name</label>
                <input
                  type="text"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  className={inputClass}
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Description</label>
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={3}
                  className={inputClass}
                />
              </div>

              <div className="flex flex-col">
                <label className={labelClass}>Status</label>
                <select
                  value={status}
                  onChange={(e) => setStatus(e.target.value)}
                  className={inputClass}
                >
                  <option value="ACTIVE">ACTIVE</option>
                  <option value="EXPERIMENTAL">EXPERIMENTAL</option>
                  <option value="DEPRECATED">DEPRECATED</option>
                </select>
              </div>

              <div>
                <button
                  onClick={handleSaveOverview}
                  disabled={updateMutation.isPending}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
                >
                  {updateMutation.isPending ? 'Saving...' : 'Save'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* ==================== LOGIC TAB ==================== */}
        {activeTab === 'logic' && (
          <div className="max-w-3xl">
            {/* Parameters section */}
            <div className="mb-8">
              <h3 className="text-sm font-medium text-text-primary mb-3">
                Parameters ({parameterCards.length})
              </h3>
              {parameterCards.length > 0 && (
                <div className="flex flex-wrap gap-2 mb-3">
                  {parameterCards.map((p, i) => (
                    <div
                      key={i}
                      className="flex items-center gap-2 px-3 py-2 bg-bg-tertiary border border-border rounded"
                    >
                      <span className="text-sm font-mono text-text-primary">
                        {p.name}
                      </span>
                      <Badge>{p.type}</Badge>
                      {p.required && (
                        <Badge variant="warning">required</Badge>
                      )}
                    </div>
                  ))}
                </div>
              )}
              <div className="flex flex-col">
                <label className={labelClass}>Parameters JSON</label>
                <textarea
                  value={parametersJson}
                  onChange={(e) => setParametersJson(e.target.value)}
                  rows={8}
                  className={textareaClass}
                  spellCheck={false}
                />
              </div>
            </div>

            {/* Rules section */}
            <div className="mb-8">
              <h3 className="text-sm font-medium text-text-primary mb-3">
                Rules ({ruleCards.length})
              </h3>
              {ruleCards.length > 0 && (
                <div className="flex flex-wrap gap-2 mb-3">
                  {ruleCards.map((r, i) => (
                    <div
                      key={i}
                      className="flex items-center gap-2 px-3 py-2 bg-bg-tertiary border border-border rounded"
                    >
                      <Badge variant="info">{r.type}</Badge>
                      {r.objectType && (
                        <span className="text-xs font-mono text-text-secondary">
                          {r.objectType}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              )}
              <div className="flex flex-col">
                <label className={labelClass}>Rules JSON</label>
                <textarea
                  value={rulesJson}
                  onChange={(e) => setRulesJson(e.target.value)}
                  rows={8}
                  className={textareaClass}
                  spellCheck={false}
                />
              </div>
            </div>

            {/* Submission Criteria */}
            <div className="mb-8">
              <h3 className="text-sm font-medium text-text-primary mb-3">
                Submission Criteria
              </h3>
              <div className="flex flex-col">
                <label className={labelClass}>Submission Criteria JSON</label>
                <textarea
                  value={submissionCriteriaJson}
                  onChange={(e) => setSubmissionCriteriaJson(e.target.value)}
                  rows={5}
                  className={textareaClass}
                  spellCheck={false}
                />
              </div>
            </div>

            {/* Side Effects */}
            <div className="mb-8">
              <h3 className="text-sm font-medium text-text-primary mb-3">
                Side Effects
              </h3>
              <div className="flex flex-col">
                <label className={labelClass}>Side Effects JSON</label>
                <textarea
                  value={sideEffectsJson}
                  onChange={(e) => setSideEffectsJson(e.target.value)}
                  rows={5}
                  className={textareaClass}
                  spellCheck={false}
                />
              </div>
            </div>

            {jsonError && (
              <p className="text-red-400 text-sm">{jsonError}</p>
            )}
            <div>
              <button
                onClick={handleSaveLogic}
                disabled={updateMutation.isPending}
                className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
              >
                {updateMutation.isPending ? 'Saving...' : 'Save Logic'}
              </button>
            </div>
          </div>
        )}

        {/* ==================== OBSERVABILITY TAB ==================== */}
        {activeTab === 'observability' && (
          <div>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-sm font-medium text-text-primary">
                Action Logs
                {logsData && (
                  <span className="ml-1.5 text-xs text-text-muted">
                    ({logsData.total} total)
                  </span>
                )}
              </h3>
            </div>

            {logsLoading ? (
              <div className="flex items-center justify-center py-12">
                <LoadingSpinner />
              </div>
            ) : !logsData || logsData.data.length === 0 ? (
              <EmptyState
                title="No Logs"
                description="Action execution logs will appear here once actions are invoked."
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border text-left">
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">
                        ID
                      </th>
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">
                        User
                      </th>
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">
                        Status
                      </th>
                      <th className="px-3 py-2 text-xs font-medium text-text-secondary">
                        Created
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {logsData.data.map((log: ActionLog) => (
                      <tr
                        key={log.id}
                        className="border-b border-border hover:bg-bg-tertiary/50 transition-colors"
                      >
                        <td className="px-3 py-2 font-mono text-text-primary">
                          {log.id}
                        </td>
                        <td className="px-3 py-2 text-text-primary">
                          {log.userId}
                        </td>
                        <td className="px-3 py-2">
                          <Badge
                            variant={
                              log.status === 'SUCCESS'
                                ? 'success'
                                : log.status === 'FAILURE'
                                  ? 'error'
                                  : 'warning'
                            }
                          >
                            {log.status}
                          </Badge>
                        </td>
                        <td className="px-3 py-2 text-text-secondary text-xs">
                          {new Date(log.createdAt).toLocaleString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
