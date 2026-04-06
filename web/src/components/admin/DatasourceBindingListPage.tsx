import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listDatasourceBindings,
  createDatasourceBinding,
  deleteDatasourceBinding,
  type CreateDatasourceBindingInput,
} from '../../api/admin';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

interface DatasourceBindingListPageProps {
  ontologyApiName: string;
}

export function DatasourceBindingListPage({ ontologyApiName }: DatasourceBindingListPageProps) {
  const queryClient = useQueryClient();
  const [selectedObjectTypeRid, setSelectedObjectTypeRid] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string; name: string } | null>(null);

  // Form state
  const [datasetRid, setDatasetRid] = useState('');
  const [branch, setBranch] = useState('master');
  const [columnMappingRaw, setColumnMappingRaw] = useState('');
  const [columnMappingError, setColumnMappingError] = useState('');
  const [isPrimary, setIsPrimary] = useState(false);

  const { data: objectTypes, isLoading: objectTypesLoading } = useObjectTypes(ontologyApiName);

  const { data, isLoading: bindingsLoading } = useQuery({
    queryKey: ['datasourceBindings', selectedObjectTypeRid],
    queryFn: () => listDatasourceBindings(selectedObjectTypeRid),
    enabled: !!selectedObjectTypeRid,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateDatasourceBindingInput) =>
      createDatasourceBinding(selectedObjectTypeRid, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['datasourceBindings', selectedObjectTypeRid] });
      setShowCreate(false);
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteDatasourceBinding(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['datasourceBindings', selectedObjectTypeRid] });
      setDeleteTarget(null);
    },
  });

  function resetForm() {
    setDatasetRid('');
    setBranch('master');
    setColumnMappingRaw('');
    setColumnMappingError('');
    setIsPrimary(false);
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setColumnMappingError('');

    let columnMapping: unknown = undefined;
    if (columnMappingRaw.trim()) {
      try {
        columnMapping = JSON.parse(columnMappingRaw);
      } catch {
        setColumnMappingError('Invalid JSON');
        return;
      }
    }

    createMutation.mutate({ datasetRid, branch, columnMapping, isPrimary });
  }

  function handleCloseCreate() {
    setShowCreate(false);
    resetForm();
  }

  const bindings = data?.data ?? [];
  const selectedObjectType = objectTypes?.find((ot) => ot.rid === selectedObjectTypeRid);
  const primaryCount = bindings.filter((b) => b.isPrimary).length;

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      {/* Object Type selector */}
      <div className="mb-4">
        <label className={labelClass}>Select Object Type</label>
        {objectTypesLoading ? (
          <LoadingSpinner size="sm" />
        ) : !objectTypes || objectTypes.length === 0 ? (
          <div className="text-xs text-text-secondary">No object types in this ontology.</div>
        ) : (
          <select
            value={selectedObjectTypeRid}
            onChange={(e) => setSelectedObjectTypeRid(e.target.value)}
            className={inputClass}
          >
            <option value="">— choose an object type —</option>
            {objectTypes.map((ot) => (
              <option key={ot.rid} value={ot.rid}>
                {ot.apiName} ({ot.displayName})
              </option>
            ))}
          </select>
        )}
      </div>

      {!selectedObjectTypeRid ? (
        <EmptyState
          title="Select an Object Type"
          description="Choose an object type above to view and manage its datasource bindings."
        />
      ) : (
        <>
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-medium text-text-primary">
              Datasource Bindings for{' '}
              <span className="font-mono">{selectedObjectType?.apiName ?? selectedObjectTypeRid}</span>
              {bindings.length > 0 && ` (${bindings.length})`}
            </h3>
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Binding
            </button>
          </div>

          {primaryCount > 1 && (
            <div className="mb-3 px-3 py-2 bg-yellow-900/20 border border-yellow-600/30 rounded text-xs text-yellow-400">
              Warning: more than one binding is marked primary.
            </div>
          )}

          {bindingsLoading ? (
            <LoadingSpinner />
          ) : bindings.length === 0 ? (
            <EmptyState
              title="No Datasource Bindings"
              description="Create a binding to link this object type to a dataset."
              action={
                <button
                  onClick={() => setShowCreate(true)}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
                >
                  + Create Binding
                </button>
              }
            />
          ) : (
            <div className="flex flex-col gap-2">
              {bindings.map((b) => (
                <div
                  key={b.rid}
                  className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
                >
                  <div>
                    <div className="text-sm font-mono text-text-primary">{b.datasetRid}</div>
                    <div className="text-xs text-text-secondary mt-0.5 font-mono">
                      branch: {b.branch}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant="default">{b.branch}</Badge>
                    {b.isPrimary && <Badge variant="info">primary</Badge>}
                    <button
                      onClick={() => setDeleteTarget({ rid: b.rid, name: b.datasetRid })}
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
        </>
      )}

      {/* Create Binding Modal */}
      <Modal open={showCreate} onClose={handleCloseCreate} title="Create Datasource Binding">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>Dataset RID</label>
            <input
              type="text"
              value={datasetRid}
              onChange={(e) => setDatasetRid(e.target.value)}
              className={inputClass}
              placeholder="ri.foundry.main.dataset.uuid"
              required
            />
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Branch</label>
            <input
              type="text"
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              className={inputClass}
              placeholder="master"
              required
            />
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Column Mapping (JSON, optional)</label>
            <textarea
              value={columnMappingRaw}
              onChange={(e) => {
                setColumnMappingRaw(e.target.value);
                setColumnMappingError('');
              }}
              className={`${inputClass} h-24 resize-y font-mono`}
              placeholder={'{ "id": "user_id", "name": "full_name" }'}
            />
            {columnMappingError && (
              <span className="text-xs text-red-400 mt-1">{columnMappingError}</span>
            )}
          </div>
          <label className="flex items-center gap-2 text-sm text-text-primary">
            <input
              type="checkbox"
              checked={isPrimary}
              onChange={(e) => setIsPrimary(e.target.checked)}
              className="accent-accent-cyan"
            />
            Is Primary
          </label>
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
              disabled={createMutation.isPending || !datasetRid || !branch}
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
        title="Delete Datasource Binding"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete binding to{' '}
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
