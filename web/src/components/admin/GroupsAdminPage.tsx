import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  addGroupMember,
  createGroup,
  deleteGroup,
  listGroupMembers,
  listGroups,
  removeGroupMember,
  type Group,
} from '../../api/groups';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const GROUPS_KEY = ['admin', 'groups'] as const;
function membersKey(id: string) {
  return ['admin', 'groups', id, 'members'] as const;
}

function formatTimestamp(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

export function GroupsAdminPage() {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const {
    data: groups,
    isLoading,
    error,
  } = useQuery({ queryKey: GROUPS_KEY, queryFn: listGroups });

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [deleting, setDeleting] = useState<Group | null>(null);

  // Keep the selected group valid as the list changes: clear the selection
  // when the selected group disappears (e.g. after a delete) so the member
  // pane doesn't query a stale id.
  useEffect(() => {
    if (!groups) return;
    if (selectedId && !groups.some((g) => g.id === selectedId)) {
      setSelectedId(null);
    }
  }, [groups, selectedId]);

  const selectedGroup: Group | null = useMemo(() => {
    if (!groups || !selectedId) return null;
    return groups.find((g) => g.id === selectedId) ?? null;
  }, [groups, selectedId]);

  return (
    <div
      data-testid="groups-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Groups
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Role-Based Access Control
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="groups-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Group
        </button>
      </header>

      <div className="flex flex-1 overflow-hidden">
        {/* Left pane: group list */}
        <aside
          data-testid="groups-list"
          className="w-72 border-r overflow-y-auto"
          style={{ borderColor: 'rgba(31,41,55,0.5)' }}
        >
          {isLoading && (
            <div
              data-testid="groups-loading"
              className="flex flex-col gap-2 p-4"
            >
              {[0, 1, 2].map((i) => (
                <div
                  key={i}
                  className="h-12 rounded animate-pulse"
                  style={{ background: 'rgba(31,41,55,0.4)' }}
                />
              ))}
            </div>
          )}

          {!isLoading && error && (
            <p
              data-testid="groups-error"
              role="alert"
              className="px-4 py-3 text-sm text-accent-error"
            >
              Failed to load groups: {(error as Error).message}
            </p>
          )}

          {!isLoading && !error && (groups ?? []).length === 0 && (
            <div
              data-testid="groups-empty"
              className="m-4 rounded border px-4 py-6 text-center"
              style={{
                borderColor: 'rgba(31,41,55,0.5)',
                background: 'rgba(13,17,23,0.4)',
              }}
            >
              <p className="text-sm text-text-primary font-semibold">
                No groups yet
              </p>
              <p className="text-xs text-text-secondary mt-2">
                Create a group to bundle users for role-based access. Use{' '}
                <span className="text-accent-cyan">+ New Group</span> to get
                started.
              </p>
            </div>
          )}

          {!isLoading && !error && (groups ?? []).length > 0 && (
            <ul className="py-2">
              {(groups ?? []).map((g) => (
                <li key={g.id} className="flex items-stretch">
                  <button
                    type="button"
                    data-testid={`group-row-${g.id}`}
                    onClick={() => setSelectedId(g.id)}
                    className={`flex-1 min-w-0 text-left px-4 py-2 flex items-start gap-3 text-sm transition-colors ${
                      selectedId === g.id
                        ? 'bg-white/5 text-text-primary'
                        : 'text-text-secondary hover:bg-white/[0.03]'
                    }`}
                  >
                    <span className="flex-1 min-w-0">
                      <span className="block truncate text-text-primary">
                        {g.name}
                      </span>
                      {g.description && (
                        <span className="block truncate text-[11px] text-text-secondary">
                          {g.description}
                        </span>
                      )}
                    </span>
                  </button>
                  <button
                    type="button"
                    data-testid={`group-delete-btn-${g.id}`}
                    onClick={() => setDeleting(g)}
                    aria-label={`Delete group ${g.name}`}
                    className="shrink-0 self-start mt-2 mr-2 text-[11px] px-2 py-0.5 rounded border border-accent-error/30 text-accent-error hover:bg-accent-error/10"
                  >
                    Delete
                  </button>
                </li>
              ))}
            </ul>
          )}
        </aside>

        {/* Right pane: members of selected group */}
        <section className="flex-1 overflow-y-auto">
          {selectedGroup ? (
            <GroupMembersPane group={selectedGroup} />
          ) : (
            <div
              data-testid="groups-no-selection"
              className="flex items-center justify-center h-full text-text-secondary text-sm"
            >
              Select a group to manage its members.
            </div>
          )}
        </section>
      </div>

      {createOpen && (
        <CreateGroupModal
          onClose={() => setCreateOpen(false)}
          onCreated={() => {
            queryClient.invalidateQueries({ queryKey: GROUPS_KEY });
            setCreateOpen(false);
          }}
          pushToast={pushToast}
        />
      )}

      {deleting && (
        <DeleteGroupModal
          group={deleting}
          onClose={() => setDeleting(null)}
          onDeleted={() => {
            queryClient.invalidateQueries({ queryKey: GROUPS_KEY });
            setDeleting(null);
          }}
          pushToast={pushToast}
        />
      )}
    </div>
  );
}

type PushToast = ReturnType<typeof useToastStore.getState>['push'];

function CreateGroupModal({
  onClose,
  onCreated,
  pushToast,
}: {
  onClose: () => void;
  onCreated: () => void;
  pushToast: PushToast;
}) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');

  const mutation = useMutation({
    mutationFn: () =>
      createGroup({
        name: name.trim(),
        description: description.trim() || undefined,
      }),
    onSuccess: () => onCreated(),
    onError: (err) =>
      pushToast({
        message: describeApiError(err, 'Failed to create group.'),
        severity: 'error',
      }),
  });

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    mutation.mutate();
  }

  return (
    <Modal open onClose={onClose} title="New Group">
      <form
        data-testid="group-create-form"
        onSubmit={onSubmit}
        className="flex flex-col gap-4"
      >
        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          <span className="uppercase tracking-widest">
            Name<span className="text-accent-error"> *</span>
          </span>
          <input
            type="text"
            data-testid="group-create-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            autoFocus
            className={inputClass}
            placeholder="Engineers"
          />
        </label>
        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          <span className="uppercase tracking-widest">Description</span>
          <input
            type="text"
            data-testid="group-create-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
            placeholder="What this group is for"
          />
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="group-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="group-create-submit"
            disabled={mutation.isPending || !name.trim()}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {mutation.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteGroupModal({
  group,
  onClose,
  onDeleted,
  pushToast,
}: {
  group: Group;
  onClose: () => void;
  onDeleted: () => void;
  pushToast: PushToast;
}) {
  const mutation = useMutation({
    mutationFn: () => deleteGroup(group.id),
    onSuccess: () => onDeleted(),
    onError: (err) =>
      pushToast({
        message: describeApiError(err, 'Failed to delete group.'),
        severity: 'error',
      }),
  });

  return (
    <Modal open onClose={onClose} title="Delete Group">
      <div data-testid="group-delete-modal" className="flex flex-col gap-3">
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{group.name}</span>?
        </p>
        <p className="text-xs text-text-secondary">
          Members will lose any access granted through this group. This cannot
          be undone.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="group-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="group-delete-confirm"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {mutation.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function GroupMembersPane({ group }: { group: Group }) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const [newMember, setNewMember] = useState('');

  const {
    data: members,
    isLoading,
    error,
  } = useQuery({
    queryKey: membersKey(group.id),
    queryFn: () => listGroupMembers(group.id),
  });

  function invalidateMembers() {
    queryClient.invalidateQueries({ queryKey: membersKey(group.id) });
  }

  const addMutation = useMutation({
    mutationFn: (userId: string) => addGroupMember(group.id, userId),
    onSuccess: () => {
      setNewMember('');
      invalidateMembers();
    },
    onError: (err) =>
      pushToast({
        message: describeApiError(err, 'Failed to add member.'),
        severity: 'error',
      }),
  });

  const removeMutation = useMutation({
    mutationFn: (userId: string) => removeGroupMember(group.id, userId),
    onSuccess: () => invalidateMembers(),
    onError: (err) =>
      pushToast({
        message: describeApiError(err, 'Failed to remove member.'),
        severity: 'error',
      }),
  });

  function onAdd(e: React.FormEvent) {
    e.preventDefault();
    const userId = newMember.trim();
    if (!userId) return;
    addMutation.mutate(userId);
  }

  return (
    <div data-testid="group-members-pane" className="p-6 space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-text-primary">
          {group.name}
        </h2>
        {group.description && (
          <p className="text-sm text-text-secondary mt-1">
            {group.description}
          </p>
        )}
        <p className="text-[11px] text-text-secondary mt-1">
          <span className="uppercase tracking-widest">Created</span>{' '}
          {formatTimestamp(group.createdAt)}
        </p>
      </div>

      <form
        data-testid="group-member-form"
        onSubmit={onAdd}
        className="flex items-end gap-2"
      >
        <label className="flex-1 flex flex-col gap-1 text-xs text-text-secondary">
          <span className="uppercase tracking-widest">Add member (userId)</span>
          <input
            type="text"
            data-testid="group-member-input"
            value={newMember}
            onChange={(e) => setNewMember(e.target.value)}
            placeholder="user:alice@example.com"
            className={inputClass + ' font-mono'}
          />
        </label>
        <button
          type="submit"
          data-testid="group-member-add"
          disabled={addMutation.isPending || !newMember.trim()}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {addMutation.isPending ? 'Adding…' : 'Add'}
        </button>
      </form>

      {isLoading && (
        <div
          data-testid="group-members-loading"
          className="flex justify-center py-8"
        >
          <LoadingSpinner />
        </div>
      )}

      {!isLoading && error && (
        <p
          data-testid="group-members-error"
          role="alert"
          className="text-sm text-accent-error"
        >
          Failed to load members: {(error as Error).message}
        </p>
      )}

      {!isLoading && !error && (members ?? []).length === 0 && (
        <div
          data-testid="group-members-empty"
          className="rounded border px-4 py-6 text-center text-sm text-text-secondary"
          style={{
            borderColor: 'rgba(31,41,55,0.5)',
            background: 'rgba(13,17,23,0.4)',
          }}
        >
          No members yet. Add a userId above to grant this group's access.
        </div>
      )}

      {!isLoading && !error && (members ?? []).length > 0 && (
        <table data-testid="group-members-table" className="w-full text-sm">
          <thead className="text-[10px] uppercase tracking-widest text-text-secondary border-b border-border/30">
            <tr>
              <th className="text-left py-2 px-2 font-semibold">User ID</th>
              <th className="py-2 px-2 w-20"></th>
            </tr>
          </thead>
          <tbody>
            {(members ?? []).map((userId) => (
              <tr
                key={userId}
                data-testid={`group-member-row-${userId}`}
                className="border-b border-border/10 hover:bg-white/[0.02]"
              >
                <td className="py-2 px-2 font-mono text-text-primary">
                  {userId}
                </td>
                <td className="py-2 px-2 text-right">
                  <button
                    type="button"
                    data-testid={`group-member-remove-${userId}`}
                    onClick={() => removeMutation.mutate(userId)}
                    disabled={removeMutation.isPending}
                    className="text-xs px-2 py-1 rounded border border-accent-error/30 text-accent-error hover:bg-accent-error/10 disabled:opacity-40"
                  >
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-60';
