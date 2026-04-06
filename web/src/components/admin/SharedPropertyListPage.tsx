import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listSharedProperties,
  createSharedProperty,
  deleteSharedProperty,
  type CreateSharedPropertyInput,
} from '../../api/admin';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

interface SharedPropertyListPageProps {
  ontologyApiName: string;
}

export function SharedPropertyListPage({ ontologyApiName }: SharedPropertyListPageProps) {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string; name: string } | null>(null);

  // Form state
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [baseType, setBaseType] = useState('string');
  const [isArray, setIsArray] = useState(false);
  const [typeConfigRaw, setTypeConfigRaw] = useState('');
  const [typeConfigError, setTypeConfigError] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['sharedProperties', ontologyApiName],
    queryFn: () => listSharedProperties(ontologyApiName),
    enabled: !!ontologyApiName,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateSharedPropertyInput) =>
      createSharedProperty(ontologyApiName, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sharedProperties', ontologyApiName] });
      setShowCreate(false);
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteSharedProperty(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sharedProperties', ontologyApiName] });
      setDeleteTarget(null);
    },
  });

  function resetForm() {
    setApiName('');
    setDisplayName('');
    setDescription('');
    setBaseType('string');
    setIsArray(false);
    setTypeConfigRaw('');
    setTypeConfigError('');
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setTypeConfigError('');

    let typeConfig: unknown = undefined;
    if (typeConfigRaw.trim()) {
      try {
        typeConfig = JSON.parse(typeConfigRaw);
      } catch {
        setTypeConfigError('Invalid JSON');
        return;
      }
    }

    createMutation.mutate({
      apiName,
      displayName: displayName || undefined,
      description: description || undefined,
      baseType,
      typeConfig,
      isArray,
    });
  }

  function handleCloseCreate() {
    setShowCreate(false);
    resetForm();
  }

  const sharedProperties = data?.data ?? [];

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-text-primary">
          Shared Properties{sharedProperties.length > 0 && ` (${sharedProperties.length})`}
        </h3>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
        >
          + Create Shared Property
        </button>
      </div>

      {isLoading ? (
        <LoadingSpinner />
      ) : sharedProperties.length === 0 ? (
        <EmptyState
          title="No Shared Properties"
          description="Create a shared property to define reusable property definitions across object types."
          action={
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Shared Property
            </button>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {sharedProperties.map((sp) => (
            <div
              key={sp.rid}
              className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
            >
              <div>
                <div className="text-sm font-mono text-text-primary">{sp.apiName}</div>
                {sp.displayName && (
                  <div className="text-xs text-text-secondary mt-0.5">{sp.displayName}</div>
                )}
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="info">{sp.baseType}</Badge>
                <Badge variant="default">{sp.isArray ? 'array' : 'scalar'}</Badge>
                <button
                  onClick={() => setDeleteTarget({ rid: sp.rid, name: sp.apiName })}
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

      {/* Create Shared Property Modal */}
      <Modal open={showCreate} onClose={handleCloseCreate} title="Create Shared Property">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>API Name</label>
            <input
              type="text"
              value={apiName}
              onChange={(e) => setApiName(e.target.value)}
              className={inputClass}
              placeholder="e.g. latitude"
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
              placeholder="e.g. Latitude"
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
          <label className="flex items-center gap-2 text-sm text-text-primary">
            <input
              type="checkbox"
              checked={isArray}
              onChange={(e) => setIsArray(e.target.checked)}
              className="accent-accent-cyan"
            />
            Is Array
          </label>
          <div className="flex flex-col">
            <label className={labelClass}>Type Config (JSON, optional)</label>
            <textarea
              value={typeConfigRaw}
              onChange={(e) => {
                setTypeConfigRaw(e.target.value);
                setTypeConfigError('');
              }}
              className={`${inputClass} h-24 resize-y font-mono`}
              placeholder={'{"min": 0, "max": 100}'}
            />
            {typeConfigError && (
              <span className="text-xs text-red-400 mt-1">{typeConfigError}</span>
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
              disabled={createMutation.isPending || !apiName}
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
        title="Delete Shared Property"
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
