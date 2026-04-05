import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listInterfaces,
  createInterface,
  deleteInterface,
  type CreateInterfaceInput,
} from '../../api/admin';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

interface InterfaceListPageProps {
  ontologyApiName: string;
}

export function InterfaceListPage({ ontologyApiName }: InterfaceListPageProps) {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string; name: string } | null>(null);

  // Form state
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [extendsRid, setExtendsRid] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['interfaces', ontologyApiName],
    queryFn: () => listInterfaces(ontologyApiName),
    enabled: !!ontologyApiName,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateInterfaceInput) => createInterface(ontologyApiName, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['interfaces', ontologyApiName] });
      setShowCreate(false);
      setApiName('');
      setDisplayName('');
      setExtendsRid('');
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteInterface(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['interfaces', ontologyApiName] });
      setDeleteTarget(null);
    },
  });

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    createMutation.mutate({
      apiName,
      displayName,
      extendsRid: extendsRid || undefined,
    });
  }

  const interfaces = data?.data ?? [];

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-text-primary">
          Interfaces{interfaces.length > 0 && ` (${interfaces.length})`}
        </h3>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
        >
          + Create Interface
        </button>
      </div>

      {isLoading ? (
        <LoadingSpinner />
      ) : interfaces.length === 0 ? (
        <EmptyState
          title="No Interfaces"
          description="Create an interface to define shared property contracts across object types."
          action={
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Interface
            </button>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {interfaces.map((iface) => (
            <div
              key={iface.rid}
              className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
            >
              <div>
                <div className="text-sm font-mono text-text-primary">{iface.apiName}</div>
                <div className="text-xs text-text-secondary mt-0.5">
                  {iface.displayName}
                  {iface.extendsRid && (
                    <span className="ml-2">
                      extends <span className="font-mono">{iface.extendsRid}</span>
                    </span>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="info">Interface</Badge>
                <button
                  onClick={() => setDeleteTarget({ rid: iface.rid, name: iface.apiName })}
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

      {/* Create Interface Modal */}
      <Modal open={showCreate} onClose={() => setShowCreate(false)} title="Create Interface">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>API Name</label>
            <input
              type="text"
              value={apiName}
              onChange={(e) => setApiName(e.target.value)}
              className={inputClass}
              placeholder="e.g. GeoLocatable"
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
              placeholder="e.g. Geo-Locatable"
              required
            />
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Extends (Interface RID, optional)</label>
            <input
              type="text"
              value={extendsRid}
              onChange={(e) => setExtendsRid(e.target.value)}
              className={inputClass}
              placeholder="ri.ontology.main.interface.uuid"
            />
          </div>
          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={() => setShowCreate(false)}
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
        title="Delete Interface"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete <span className="font-mono text-text-primary">{deleteTarget?.name}</span>?
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
