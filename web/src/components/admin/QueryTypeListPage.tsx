import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listQueryTypes,
  createQueryType,
  deleteQueryType,
  executeQueryType,
  type CreateQueryTypeInput,
} from '../../api/admin';
import type { QueryType } from '../../api/types';
import { Modal } from '../common/Modal';
import { SlidePanel } from '../common/SlidePanel';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge, statusVariant } from '../common/Badge';

interface QueryTypeListPageProps {
  ontologyApiName: string;
}

export function QueryTypeListPage({ ontologyApiName }: QueryTypeListPageProps) {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string; name: string } | null>(null);
  const [selectedQuery, setSelectedQuery] = useState<QueryType | null>(null);
  const [executeParamsRaw, setExecuteParamsRaw] = useState('{}');
  const [executeError, setExecuteError] = useState('');
  const [executeResult, setExecuteResult] = useState<unknown>(null);

  // Form state
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [status, setStatus] = useState('ACTIVE');
  const [parametersRaw, setParametersRaw] = useState('');
  const [outputRaw, setOutputRaw] = useState('');
  const [queryRaw, setQueryRaw] = useState('');
  const [parametersError, setParametersError] = useState('');
  const [outputError, setOutputError] = useState('');
  const [queryError, setQueryError] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['queryTypes', ontologyApiName],
    queryFn: () => listQueryTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateQueryTypeInput) => createQueryType(ontologyApiName, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queryTypes', ontologyApiName] });
      setShowCreate(false);
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteQueryType(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['queryTypes', ontologyApiName] });
      setDeleteTarget(null);
    },
  });

  const executeMutation = useMutation({
    mutationFn: (input: { queryApiName: string; parameters: Record<string, unknown> }) =>
      executeQueryType(ontologyApiName, input.queryApiName, input.parameters),
    onSuccess: (result) => {
      setExecuteResult(result);
    },
    onError: (err: Error) => {
      setExecuteResult(null);
      setExecuteError(err.message ?? 'Execute failed');
    },
  });

  function resetForm() {
    setApiName('');
    setDisplayName('');
    setDescription('');
    setStatus('ACTIVE');
    setParametersRaw('');
    setOutputRaw('');
    setQueryRaw('');
    setParametersError('');
    setOutputError('');
    setQueryError('');
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setParametersError('');
    setOutputError('');
    setQueryError('');

    let parameters: unknown;
    let output: unknown;
    let query: unknown;
    let hasError = false;

    try {
      parameters = JSON.parse(parametersRaw);
    } catch {
      setParametersError('Invalid JSON');
      hasError = true;
    }
    try {
      output = JSON.parse(outputRaw);
    } catch {
      setOutputError('Invalid JSON');
      hasError = true;
    }
    try {
      query = JSON.parse(queryRaw);
    } catch {
      setQueryError('Invalid JSON');
      hasError = true;
    }

    if (hasError) return;

    createMutation.mutate({
      apiName,
      displayName,
      description: description || undefined,
      parameters,
      output,
      query,
      status,
    });
  }

  function handleCloseCreate() {
    setShowCreate(false);
    resetForm();
  }

  function handleOpenQuery(qt: QueryType) {
    setSelectedQuery(qt);
    setExecuteParamsRaw('{}');
    setExecuteError('');
    setExecuteResult(null);
  }

  function handleCloseQuery() {
    setSelectedQuery(null);
    setExecuteError('');
    setExecuteResult(null);
  }

  function handleExecute() {
    if (!selectedQuery) return;
    setExecuteError('');
    setExecuteResult(null);

    let parameters: Record<string, unknown>;
    try {
      parameters = JSON.parse(executeParamsRaw);
    } catch {
      setExecuteError('Invalid JSON for parameters');
      return;
    }

    executeMutation.mutate({ queryApiName: selectedQuery.apiName, parameters });
  }

  const queryTypes = data?.data ?? [];

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-text-primary">
          Query Types{queryTypes.length > 0 && ` (${queryTypes.length})`}
        </h3>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
        >
          + Create Query Type
        </button>
      </div>

      {isLoading ? (
        <LoadingSpinner />
      ) : queryTypes.length === 0 ? (
        <EmptyState
          title="No Query Types"
          description="Create a query type to define a predefined filter and aggregation combo."
          action={
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Query Type
            </button>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {queryTypes.map((qt) => (
            <div
              key={qt.rid}
              className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded cursor-pointer hover:border-accent-cyan/50 transition-colors"
              onClick={() => handleOpenQuery(qt)}
            >
              <div>
                <div className="text-sm font-mono text-text-primary">{qt.apiName}</div>
                <div className="text-xs text-text-secondary mt-0.5">
                  {qt.displayName}
                  {qt.description && ` — ${qt.description}`}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant={statusVariant(qt.status)}>{qt.status}</Badge>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    setDeleteTarget({ rid: qt.rid, name: qt.apiName });
                  }}
                  className="p-1 text-text-muted hover:text-red-400 transition-colors"
                  title="Delete"
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                  </svg>
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Execute Slide Panel */}
      <SlidePanel
        open={selectedQuery !== null}
        onClose={handleCloseQuery}
        title={selectedQuery ? `Execute: ${selectedQuery.apiName}` : 'Execute Query'}
      >
        {selectedQuery && (
          <div className="flex flex-col gap-4">
            <div>
              <div className={labelClass}>Display Name</div>
              <div className="text-sm text-text-primary">{selectedQuery.displayName}</div>
            </div>
            {selectedQuery.description && (
              <div>
                <div className={labelClass}>Description</div>
                <div className="text-sm text-text-secondary">{selectedQuery.description}</div>
              </div>
            )}
            <div>
              <div className={labelClass}>Parameters Schema</div>
              <pre className="bg-bg-tertiary border border-border rounded p-2 text-xs font-mono text-text-secondary overflow-auto max-h-32 whitespace-pre-wrap break-all">
                {JSON.stringify(selectedQuery.parameters, null, 2)}
              </pre>
            </div>
            <div className="flex flex-col">
              <label className={labelClass}>Parameters (JSON)</label>
              <textarea
                value={executeParamsRaw}
                onChange={(e) => {
                  setExecuteParamsRaw(e.target.value);
                  setExecuteError('');
                }}
                className={`${inputClass} h-28 resize-y font-mono`}
                placeholder={'{}'}
              />
              {executeError && (
                <span className="text-xs text-red-400 mt-1">{executeError}</span>
              )}
            </div>
            <button
              onClick={handleExecute}
              disabled={executeMutation.isPending}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
            >
              {executeMutation.isPending ? 'Executing...' : 'Execute'}
            </button>
            {executeResult !== null && (
              <div>
                <div className={labelClass}>Result</div>
                <pre
                  data-testid="execute-result"
                  className="bg-bg-tertiary border border-border rounded p-3 text-xs font-mono text-text-primary overflow-auto max-h-96 whitespace-pre-wrap break-all"
                >
                  {JSON.stringify(executeResult, null, 2)}
                </pre>
              </div>
            )}
          </div>
        )}
      </SlidePanel>

      {/* Create Query Type Modal */}
      <Modal open={showCreate} onClose={handleCloseCreate} title="Create Query Type">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>API Name</label>
            <input
              type="text"
              value={apiName}
              onChange={(e) => setApiName(e.target.value)}
              className={inputClass}
              placeholder="e.g. topCustomers"
              required
            />
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Display Name</label>
            <input
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              className={inputClass}
              placeholder="e.g. Top Customers"
              required
            />
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Description</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className={inputClass}
              placeholder="Optional description"
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
          <div className="flex flex-col">
            <label className={labelClass}>Parameters (JSON)</label>
            <textarea
              value={parametersRaw}
              onChange={(e) => {
                setParametersRaw(e.target.value);
                setParametersError('');
              }}
              className={`${inputClass} h-24 resize-y font-mono`}
              placeholder={'{"limit": {"type": "integer"}}'}
              required
            />
            {parametersError && (
              <span className="text-xs text-red-400 mt-1">{parametersError}</span>
            )}
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Output (JSON)</label>
            <textarea
              value={outputRaw}
              onChange={(e) => {
                setOutputRaw(e.target.value);
                setOutputError('');
              }}
              className={`${inputClass} h-24 resize-y font-mono`}
              placeholder={'{"type": "array"}'}
              required
            />
            {outputError && (
              <span className="text-xs text-red-400 mt-1">{outputError}</span>
            )}
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Query (JSON)</label>
            <textarea
              value={queryRaw}
              onChange={(e) => {
                setQueryRaw(e.target.value);
                setQueryError('');
              }}
              className={`${inputClass} h-24 resize-y font-mono`}
              placeholder={'{"type": "aggregation"}'}
              required
            />
            {queryError && (
              <span className="text-xs text-red-400 mt-1">{queryError}</span>
            )}
          </div>
          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={handleCloseCreate}
              className="px-4 py-2 rounded text-sm text-text-secondary hover:text-text-primary border border-border"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending || !apiName || !displayName}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
            >
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Query Type"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete{' '}
            <span className="font-mono text-text-primary">{deleteTarget?.name}</span>?
            This action cannot be undone.
          </p>
          <div className="flex justify-end gap-3">
            <button
              onClick={() => setDeleteTarget(null)}
              className="px-4 py-2 rounded text-sm text-text-secondary hover:text-text-primary border border-border"
            >
              Cancel
            </button>
            <button
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.rid)}
              disabled={deleteMutation.isPending}
              className="bg-red-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-red-700 disabled:opacity-50"
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
