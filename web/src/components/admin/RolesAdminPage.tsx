import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createRole,
  deleteRole,
  listRolePermissions,
  listRoles,
  setRolePermissions,
  type Role,
} from '../../api/roles';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const ROLES_QUERY_KEY = ['admin', 'roles'] as const;

function formatCreatedAt(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

export function RolesAdminPage() {
  const {
    data: roles,
    isLoading,
    error,
  } = useQuery({ queryKey: ROLES_QUERY_KEY, queryFn: listRoles });

  const [createOpen, setCreateOpen] = useState(false);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [permissionsFor, setPermissionsFor] = useState<Role | null>(null);

  const sorted = useMemo(() => {
    if (!roles) return [];
    return [...roles].sort((a, b) => a.name.localeCompare(b.name));
  }, [roles]);

  return (
    <div
      data-testid="roles-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Roles
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Role-Based Access Control
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="role-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Role
        </button>
      </header>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="roles-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p data-testid="roles-error" className="text-sm text-accent-error">
            Failed to load roles: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && sorted.length === 0 && (
          <div
            data-testid="roles-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No roles yet
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Create a role to group permissions and assign it to users.
            </p>
          </div>
        )}
        {!isLoading && !error && sorted.length > 0 && (
          <RolesTable
            rows={sorted}
            onDelete={setDeleting}
            onPermissions={setPermissionsFor}
          />
        )}
      </div>

      {createOpen && (
        <CreateRoleModal onClose={() => setCreateOpen(false)} />
      )}
      {deleting && (
        <DeleteRoleModal role={deleting} onClose={() => setDeleting(null)} />
      )}
      {permissionsFor && (
        <PermissionsModal
          role={permissionsFor}
          onClose={() => setPermissionsFor(null)}
        />
      )}
    </div>
  );
}

function RolesTable({
  rows,
  onDelete,
  onPermissions,
}: {
  rows: Role[];
  onDelete: (role: Role) => void;
  onPermissions: (role: Role) => void;
}) {
  return (
    <div
      data-testid="roles-table"
      className="rounded border overflow-hidden"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <table className="w-full text-sm">
        <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
          <tr className="border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
            <th className="text-left px-4 py-2 font-medium">Name</th>
            <th className="text-left px-4 py-2 font-medium">Description</th>
            <th className="text-left px-4 py-2 font-medium">Permissions</th>
            <th className="text-left px-4 py-2 font-medium">Created</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((role) => (
            <tr
              key={role.name}
              data-testid={`role-row-${role.name}`}
              className="border-b last:border-0 hover:bg-bg-tertiary/30"
              style={{ borderColor: 'rgba(31,41,55,0.5)' }}
            >
              <td className="px-4 py-2 text-text-primary">
                <span className="flex items-center gap-2">
                  <span className="font-mono">{role.name}</span>
                  {role.builtin && (
                    <span
                      data-testid="role-builtin-badge"
                      className="text-[9px] uppercase tracking-widest px-1.5 py-0.5 rounded bg-accent-amber/15 text-accent-amber border border-accent-amber/30"
                    >
                      Built-in
                    </span>
                  )}
                </span>
              </td>
              <td className="px-4 py-2 text-xs text-text-secondary">
                {role.description || '—'}
              </td>
              <td className="px-4 py-2 text-xs text-text-secondary font-mono">
                {role.permissions.length}
              </td>
              <td className="px-4 py-2 text-xs text-text-secondary">
                {formatCreatedAt(role.createdAt)}
              </td>
              <td className="px-4 py-2 text-right whitespace-nowrap">
                <button
                  type="button"
                  data-testid="role-permissions-btn"
                  onClick={() => onPermissions(role)}
                  className="text-xs text-accent-cyan hover:underline mr-3"
                >
                  Permissions
                </button>
                <button
                  type="button"
                  data-testid="role-delete-btn"
                  onClick={() => onDelete(role)}
                  disabled={role.builtin}
                  title={
                    role.builtin
                      ? 'Built-in roles cannot be deleted'
                      : undefined
                  }
                  className="text-xs text-accent-error hover:underline disabled:opacity-40 disabled:cursor-not-allowed disabled:no-underline"
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CreateRoleModal({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');

  const create = useMutation({
    mutationFn: () =>
      createRole({
        name: name.trim(),
        description: description.trim() || undefined,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ROLES_QUERY_KEY });
      pushToast({ message: `Role "${name.trim()}" created.`, severity: 'success' });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to create role.'),
        severity: 'error',
      });
    },
  });

  const canSubmit = name.trim().length > 0 && !create.isPending;

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    create.mutate();
  }

  return (
    <Modal open onClose={onClose} title="New Role">
      <form
        data-testid="role-create-form"
        onSubmit={onSubmit}
        className="flex flex-col gap-4"
      >
        <Field label="Name" required>
          <input
            type="text"
            data-testid="role-name-input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            autoFocus
            className={inputClass + ' font-mono'}
            placeholder="auditor"
          />
        </Field>
        <Field label="Description">
          <input
            type="text"
            data-testid="role-description-input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
            placeholder="What this role is for"
          />
        </Field>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="role-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="role-create-submit"
            disabled={!canSubmit}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteRoleModal({
  role,
  onClose,
}: {
  role: Role;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const del = useMutation({
    mutationFn: () => deleteRole(role.name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ROLES_QUERY_KEY });
      pushToast({ message: `Role "${role.name}" deleted.`, severity: 'success' });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to delete role.'),
        severity: 'error',
      });
    },
  });

  return (
    <Modal open onClose={onClose} title="Delete Role">
      <div
        data-testid="role-delete-modal"
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Delete role{' '}
          <span className="font-semibold font-mono">{role.name}</span>?
        </p>
        <p className="text-xs text-text-secondary">
          Users assigned this role will lose every permission it grants. This
          cannot be undone.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="role-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="role-delete-confirm"
            onClick={() => del.mutate()}
            disabled={del.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {del.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function PermissionsModal({
  role,
  onClose,
}: {
  role: Role;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const readOnly = role.builtin;

  const {
    data: permissions,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['admin', 'roles', role.name, 'permissions'],
    queryFn: () => listRolePermissions(role.name),
  });

  const [draft, setDraft] = useState<string[] | null>(null);
  const [newPermission, setNewPermission] = useState('');

  // Seed the editable draft from the fetched list once it arrives.
  useEffect(() => {
    if (permissions && draft === null) {
      setDraft(permissions);
    }
  }, [permissions, draft]);

  const effective = draft ?? permissions ?? [];

  const save = useMutation({
    mutationFn: () => setRolePermissions(role.name, effective),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ROLES_QUERY_KEY });
      queryClient.invalidateQueries({
        queryKey: ['admin', 'roles', role.name, 'permissions'],
      });
      pushToast({
        message: `Permissions for "${role.name}" saved.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to save permissions.'),
        severity: 'error',
      });
    },
  });

  function addPermission() {
    const value = newPermission.trim();
    if (!value || effective.includes(value)) {
      setNewPermission('');
      return;
    }
    setDraft([...effective, value]);
    setNewPermission('');
  }

  function removePermission(perm: string) {
    setDraft(effective.filter((p) => p !== perm));
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={`Permissions — ${role.name}`}
      size="lg"
    >
      <div
        data-testid="role-permissions-editor"
        className="flex flex-col gap-4"
      >
        {role.builtin && (
          <p className="text-xs text-accent-amber">
            Built-in role permissions are defined by the static matrix and are
            read-only.
          </p>
        )}
        {isLoading && (
          <div className="flex items-center justify-center py-8">
            <LoadingSpinner size="md" />
          </div>
        )}
        {!isLoading && error && (
          <p className="text-xs text-accent-error" role="alert">
            Failed to load permissions: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && (
          <>
            {effective.length === 0 ? (
              <p
                data-testid="role-permissions-empty"
                className="text-xs text-text-muted italic"
              >
                No permissions assigned.
              </p>
            ) : (
              <ul
                data-testid="role-permissions-list"
                className="flex flex-col gap-1"
              >
                {effective.map((perm) => (
                  <li
                    key={perm}
                    data-testid={`role-permission-${perm}`}
                    className="flex items-center gap-3 px-3 py-2 rounded border"
                    style={{
                      borderColor: 'rgba(31,41,55,0.5)',
                      background: 'rgba(13,17,23,0.4)',
                    }}
                  >
                    <span className="flex-1 text-sm font-mono text-text-primary">
                      {perm}
                    </span>
                    {!readOnly && (
                      <button
                        type="button"
                        data-testid={`role-permission-remove-${perm}`}
                        onClick={() => removePermission(perm)}
                        className="text-xs text-accent-error hover:underline"
                      >
                        Remove
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}

            {!readOnly && (
              <div className="flex items-end gap-2">
                <Field label="Add permission">
                  <input
                    type="text"
                    data-testid="role-permission-add-input"
                    value={newPermission}
                    onChange={(e) => setNewPermission(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault();
                        addPermission();
                      }
                    }}
                    className={inputClass + ' font-mono'}
                    placeholder="ontology.edit"
                  />
                </Field>
                <button
                  type="button"
                  data-testid="role-permission-add-btn"
                  onClick={addPermission}
                  className="px-3 py-1.5 text-xs font-semibold rounded bg-bg-tertiary text-text-primary border border-border/40 hover:bg-bg-tertiary/70"
                >
                  Add
                </button>
              </div>
            )}
          </>
        )}

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="role-permissions-close"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            {readOnly ? 'Done' : 'Cancel'}
          </button>
          <button
            type="button"
            data-testid="role-permissions-save"
            onClick={() => save.mutate()}
            disabled={readOnly || isLoading || save.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {save.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-60';

function Field({
  label,
  required,
  children,
}: {
  label: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary w-full">
      <span className="uppercase tracking-widest">
        {label}
        {required && <span className="text-accent-error"> *</span>}
      </span>
      {children}
    </label>
  );
}
