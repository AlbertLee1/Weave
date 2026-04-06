import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listTypeGroups,
  createTypeGroup,
  deleteTypeGroup,
  assignTypeGroup,
  removeTypeGroup,
  listTypeGroupsForObjectType,
  type CreateTypeGroupInput,
} from '../../api/admin';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { ColorPicker } from '../common/ColorPicker';

interface TypeGroupListPageProps {
  ontologyApiName: string;
}

export function TypeGroupListPage({ ontologyApiName }: TypeGroupListPageProps) {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string; name: string } | null>(null);
  const [selectedObjectTypeRid, setSelectedObjectTypeRid] = useState('');
  const [groupToAssign, setGroupToAssign] = useState('');

  // Form state
  const [apiName, setApiName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [color, setColor] = useState('#6366f1');

  const { data: objectTypes } = useObjectTypes(ontologyApiName);

  const { data, isLoading } = useQuery({
    queryKey: ['typeGroups', ontologyApiName],
    queryFn: () => listTypeGroups(ontologyApiName),
    enabled: !!ontologyApiName,
  });

  const { data: assignedData, isLoading: assignedLoading } = useQuery({
    queryKey: ['typeGroupsForObjectType', selectedObjectTypeRid],
    queryFn: () => listTypeGroupsForObjectType(selectedObjectTypeRid),
    enabled: !!selectedObjectTypeRid,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateTypeGroupInput) => createTypeGroup(ontologyApiName, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['typeGroups', ontologyApiName] });
      setShowCreate(false);
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteTypeGroup(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['typeGroups', ontologyApiName] });
      setDeleteTarget(null);
    },
  });

  const assignMutation = useMutation({
    mutationFn: (typeGroupRid: string) => assignTypeGroup(selectedObjectTypeRid, typeGroupRid),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['typeGroupsForObjectType', selectedObjectTypeRid],
      });
      setGroupToAssign('');
    },
  });

  const removeMutation = useMutation({
    mutationFn: (typeGroupRid: string) => removeTypeGroup(selectedObjectTypeRid, typeGroupRid),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['typeGroupsForObjectType', selectedObjectTypeRid],
      });
    },
  });

  function resetForm() {
    setApiName('');
    setDisplayName('');
    setDescription('');
    setColor('#6366f1');
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    createMutation.mutate({
      apiName,
      displayName,
      description: description || undefined,
      color: color || undefined,
    });
  }

  function handleCloseCreate() {
    setShowCreate(false);
    resetForm();
  }

  const allGroups = data?.data ?? [];
  const assignedGroups = assignedData?.data ?? [];
  const unassignedGroups = allGroups.filter(
    (g) => !assignedGroups.find((ag) => ag.rid === g.rid),
  );
  const selectedObjectType = objectTypes?.find((ot) => ot.rid === selectedObjectTypeRid);

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-text-primary">
          Type Groups{allGroups.length > 0 && ` (${allGroups.length})`}
        </h3>
        <button
          onClick={() => setShowCreate(true)}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
        >
          + Create Type Group
        </button>
      </div>

      {isLoading ? (
        <LoadingSpinner />
      ) : allGroups.length === 0 ? (
        <EmptyState
          title="No Type Groups"
          description="Create a type group to organize object types into categories."
          action={
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Type Group
            </button>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {allGroups.map((tg) => (
            <div
              key={tg.rid}
              className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
            >
              <div className="flex items-center gap-3">
                {tg.color && (
                  <div
                    data-testid={`color-swatch-${tg.apiName}`}
                    className="w-4 h-4 rounded-full border border-border"
                    style={{ backgroundColor: tg.color }}
                  />
                )}
                <div>
                  <div className="text-sm font-mono text-text-primary">{tg.apiName}</div>
                  <div className="text-xs text-text-secondary mt-0.5">
                    {tg.displayName}
                    {tg.description && ` — ${tg.description}`}
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setDeleteTarget({ rid: tg.rid, name: tg.apiName })}
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

      {/* Assignment Section */}
      <div className="mt-8 pt-6 border-t border-border">
        <h3 className="text-sm font-medium text-text-primary mb-3">
          Assign Groups to Object Type
        </h3>
        <div className="mb-4">
          <label className={labelClass}>Select Object Type</label>
          {!objectTypes || objectTypes.length === 0 ? (
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

        {selectedObjectTypeRid && (
          <div className="flex flex-col gap-3">
            <div>
              <div className="text-xs text-text-secondary mb-2">
                Current groups for{' '}
                <span className="font-mono text-text-primary">
                  {selectedObjectType?.apiName ?? selectedObjectTypeRid}
                </span>
                :
              </div>
              {assignedLoading ? (
                <LoadingSpinner size="sm" />
              ) : assignedGroups.length === 0 ? (
                <div className="text-xs text-text-muted">No groups assigned.</div>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {assignedGroups.map((ag) => (
                    <div
                      key={ag.rid}
                      className="flex items-center gap-2 px-3 py-1.5 bg-bg-tertiary border border-border rounded"
                    >
                      {ag.color && (
                        <div
                          className="w-3 h-3 rounded-full"
                          style={{ backgroundColor: ag.color }}
                        />
                      )}
                      <span className="text-xs font-mono text-text-primary">{ag.apiName}</span>
                      <button
                        onClick={() => removeMutation.mutate(ag.rid)}
                        disabled={removeMutation.isPending}
                        className="text-text-muted hover:text-red-400 transition-colors"
                        title="Remove"
                        aria-label={`Remove ${ag.apiName}`}
                      >
                        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                          <path d="M18 6L6 18M6 6l12 12" />
                        </svg>
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {unassignedGroups.length > 0 && (
              <div className="flex items-end gap-2">
                <div className="flex-1 flex flex-col">
                  <label className={labelClass}>Assign group</label>
                  <select
                    value={groupToAssign}
                    onChange={(e) => setGroupToAssign(e.target.value)}
                    className={inputClass}
                  >
                    <option value="">— choose a group —</option>
                    {unassignedGroups.map((ug) => (
                      <option key={ug.rid} value={ug.rid}>
                        {ug.apiName}
                      </option>
                    ))}
                  </select>
                </div>
                <button
                  onClick={() => groupToAssign && assignMutation.mutate(groupToAssign)}
                  disabled={!groupToAssign || assignMutation.isPending}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
                >
                  Assign
                </button>
              </div>
            )}
          </div>
        )}
      </div>

      {/* Create Type Group Modal */}
      <Modal open={showCreate} onClose={handleCloseCreate} title="Create Type Group">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>API Name</label>
            <input
              type="text"
              value={apiName}
              onChange={(e) => setApiName(e.target.value)}
              className={inputClass}
              placeholder="e.g. people"
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
              placeholder="e.g. People"
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
            <label className={labelClass}>Color</label>
            <ColorPicker value={color} onChange={setColor} />
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
        title="Delete Type Group"
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
