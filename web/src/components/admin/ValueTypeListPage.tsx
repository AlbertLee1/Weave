import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listValueTypes,
  createValueType,
  deleteValueType,
  type CreateValueTypeInput,
} from '../../api/admin';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

export function ValueTypeListPage() {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string; name: string } | null>(null);

  // Form state
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [baseType, setBaseType] = useState('string');
  const [constraintsRaw, setConstraintsRaw] = useState('');
  const [constraintsError, setConstraintsError] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['valueTypes'],
    queryFn: () => listValueTypes(),
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateValueTypeInput) => createValueType(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['valueTypes'] });
      setShowCreate(false);
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteValueType(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['valueTypes'] });
      setDeleteTarget(null);
    },
  });

  function resetForm() {
    setApiName('');
    setDisplayName('');
    setBaseType('string');
    setConstraintsRaw('');
    setConstraintsError('');
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setConstraintsError('');

    let constraints: unknown = undefined;
    if (constraintsRaw.trim()) {
      try {
        constraints = JSON.parse(constraintsRaw);
      } catch {
        setConstraintsError('Invalid JSON');
        return;
      }
    }

    createMutation.mutate({ apiName, displayName, baseType, constraints });
  }

  function handleCloseCreate() {
    setShowCreate(false);
    resetForm();
  }

  const valueTypes = data?.data ?? [];

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-text-primary">
          Value Types{valueTypes.length > 0 && ` (${valueTypes.length})`}
        </h3>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
        >
          + Create Value Type
        </button>
      </div>

      {isLoading ? (
        <LoadingSpinner />
      ) : valueTypes.length === 0 ? (
        <EmptyState
          title="No Value Types"
          description="Create a value type to define reusable custom types like currency or email."
          action={
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Value Type
            </button>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {valueTypes.map((vt) => (
            <div
              key={vt.rid}
              className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
            >
              <div>
                <div className="text-sm font-mono text-text-primary">{vt.apiName}</div>
                <div className="text-xs text-text-secondary mt-0.5">
                  {vt.displayName}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="info">{vt.baseType}</Badge>
                <Badge variant="default">v{vt.version}</Badge>
                <button
                  onClick={() => setDeleteTarget({ rid: vt.rid, name: vt.apiName })}
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

      {/* Create Value Type Modal */}
      <Modal open={showCreate} onClose={handleCloseCreate} title="Create Value Type">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>API Name</label>
            <input
              type="text"
              value={apiName}
              onChange={(e) => setApiName(e.target.value)}
              className={inputClass}
              placeholder="e.g. CurrencyAmount"
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
              placeholder="e.g. Currency Amount"
              required
            />
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Base Type</label>
            <select
              value={baseType}
              onChange={(e) => setBaseType(e.target.value)}
              className={inputClass}
            >
              <option value="string">string</option>
              <option value="integer">integer</option>
              <option value="double">double</option>
              <option value="boolean">boolean</option>
              <option value="timestamp">timestamp</option>
              <option value="date">date</option>
            </select>
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Constraints (JSON, optional)</label>
            <textarea
              value={constraintsRaw}
              onChange={(e) => {
                setConstraintsRaw(e.target.value);
                setConstraintsError('');
              }}
              className={`${inputClass} h-24 resize-y font-mono`}
              placeholder={'{"minLength": 1, "maxLength": 100}'}
            />
            {constraintsError && (
              <span className="text-xs text-red-400 mt-1">{constraintsError}</span>
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
        title="Delete Value Type"
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
