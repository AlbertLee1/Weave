import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { listSnapshots, createSnapshot, getSnapshot } from '../../api/admin';
import type { OntologySnapshot } from '../../api/types';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

interface SnapshotListPageProps {
  ontologyApiName: string;
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function dataSize(data: unknown): string {
  try {
    const bytes = new TextEncoder().encode(JSON.stringify(data)).length;
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  } catch {
    return '—';
  }
}

export function SnapshotListPage({ ontologyApiName }: SnapshotListPageProps) {
  const queryClient = useQueryClient();
  const [viewSnapshot, setViewSnapshot] = useState<OntologySnapshot | null>(null);
  const [loadingVersion, setLoadingVersion] = useState<number | null>(null);

  const { data: snapshots, isLoading } = useQuery({
    queryKey: ['snapshots', ontologyApiName],
    queryFn: () => listSnapshots(ontologyApiName),
    enabled: !!ontologyApiName,
  });

  const createMutation = useMutation({
    mutationFn: () => createSnapshot(ontologyApiName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['snapshots', ontologyApiName] });
    },
  });

  async function handleViewSnapshot(version: number) {
    setLoadingVersion(version);
    try {
      const snap = await getSnapshot(ontologyApiName, version);
      setViewSnapshot(snap);
    } finally {
      setLoadingVersion(null);
    }
  }

  const list = snapshots ?? [];

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-text-primary">
          Snapshots{list.length > 0 && ` (${list.length})`}
        </h3>
        <button
          onClick={() => createMutation.mutate()}
          disabled={createMutation.isPending}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
        >
          {createMutation.isPending ? 'Creating...' : '+ Create Snapshot'}
        </button>
      </div>

      {createMutation.isError && (
        <div className="mb-3 px-3 py-2 bg-red-900/20 border border-red-600/30 rounded text-xs text-red-400">
          Failed to create snapshot. Please try again.
        </div>
      )}

      {isLoading ? (
        <LoadingSpinner />
      ) : list.length === 0 ? (
        <EmptyState
          title="No Snapshots"
          description="Create a snapshot to capture the current state of this ontology."
          action={
            <button
              onClick={() => createMutation.mutate()}
              disabled={createMutation.isPending}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
            >
              + Create Snapshot
            </button>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {list.map((snap) => (
            <div
              key={snap.id}
              className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded cursor-pointer hover:border-accent-cyan/50 transition-colors"
              onClick={() => handleViewSnapshot(snap.version)}
            >
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-mono text-text-primary">
                    v{snap.version}
                  </span>
                  <Badge variant="info">snapshot</Badge>
                </div>
                <div className="text-xs text-text-secondary mt-0.5">
                  {formatDate(snap.createdAt)}
                  {snap.createdBy && (
                    <span className="ml-2 font-mono">{snap.createdBy}</span>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs text-text-muted font-mono">
                  {dataSize(snap.data)}
                </span>
                {loadingVersion === snap.version ? (
                  <LoadingSpinner size="sm" />
                ) : (
                  <svg
                    className="w-4 h-4 text-text-muted"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M9 18l6-6-6-6" />
                  </svg>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Snapshot Detail Modal */}
      <Modal
        open={viewSnapshot !== null}
        onClose={() => setViewSnapshot(null)}
        title={viewSnapshot ? `Snapshot v${viewSnapshot.version}` : 'Snapshot'}
      >
        {viewSnapshot && (
          <div className="flex flex-col gap-4">
            <div className="grid grid-cols-2 gap-3 text-xs">
              <div>
                <div className="text-text-secondary mb-1">Version</div>
                <div className="font-mono text-text-primary">v{viewSnapshot.version}</div>
              </div>
              <div>
                <div className="text-text-secondary mb-1">Created</div>
                <div className="font-mono text-text-primary">{formatDate(viewSnapshot.createdAt)}</div>
              </div>
              {viewSnapshot.createdBy && (
                <div>
                  <div className="text-text-secondary mb-1">Created By</div>
                  <div className="font-mono text-text-primary">{viewSnapshot.createdBy}</div>
                </div>
              )}
              <div>
                <div className="text-text-secondary mb-1">Size</div>
                <div className="font-mono text-text-primary">{dataSize(viewSnapshot.data)}</div>
              </div>
            </div>

            <div>
              <div className="text-xs text-text-secondary mb-1">Data</div>
              <pre className="bg-bg-tertiary border border-border rounded p-3 text-xs font-mono text-text-primary overflow-auto max-h-96 whitespace-pre-wrap break-all">
                {JSON.stringify(viewSnapshot.data, null, 2)}
              </pre>
            </div>

            <div className="flex justify-end">
              <button
                onClick={() => setViewSnapshot(null)}
                className="px-4 py-2 rounded text-sm text-text-secondary hover:text-text-primary border border-border"
              >
                Close
              </button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
