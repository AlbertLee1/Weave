import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createFlag,
  deleteFlag,
  listFlags,
  updateFlag,
  type CreateFlagRequest,
  type FeatureFlag,
  type UpdateFlagRequest,
} from '../../api/featureFlags';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const FLAGS_QUERY_KEY = ['admin', 'feature-flags'] as const;

// parseScopeList splits a comma / newline-separated free-text field into a
// trimmed, de-duplicated, non-empty string array — the wire shape the backend
// expects for `realms` / `users`. Empty input maps to [].
function parseScopeList(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const piece of raw.split(/[\n,]/)) {
    const v = piece.trim();
    if (v.length === 0 || seen.has(v)) continue;
    seen.add(v);
    out.push(v);
  }
  return out;
}

export function FeatureFlagsAdminPage() {
  const {
    data: flags,
    isLoading,
    error,
  } = useQuery({ queryKey: FLAGS_QUERY_KEY, queryFn: listFlags });

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<FeatureFlag | null>(null);
  const [deleting, setDeleting] = useState<FeatureFlag | null>(null);

  const sorted = flags
    ? [...flags].sort((a, b) => a.name.localeCompare(b.name))
    : [];

  return (
    <div
      data-testid="feature-flags-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Feature Flags
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Rollout & Targeting
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="flag-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Flag
        </button>
      </header>

      <div className="flex-1 overflow-y-auto px-6 py-4">
        {isLoading && (
          <div
            data-testid="flags-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p data-testid="flags-error" className="text-sm text-accent-error">
            Failed to load feature flags: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && sorted.length === 0 && (
          <div
            data-testid="flags-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No feature flags yet
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Create a flag to gate functionality behind a global switch or a
              targeted realm / user rollout.
            </p>
          </div>
        )}
        {!isLoading && !error && sorted.length > 0 && (
          <FlagsTable rows={sorted} onEdit={setEditing} onDelete={setDeleting} />
        )}
      </div>

      {createOpen && <CreateFlagModal onClose={() => setCreateOpen(false)} />}
      {editing && (
        <EditFlagModal flag={editing} onClose={() => setEditing(null)} />
      )}
      {deleting && (
        <DeleteFlagModal flag={deleting} onClose={() => setDeleting(null)} />
      )}
    </div>
  );
}

function FlagsTable({
  rows,
  onEdit,
  onDelete,
}: {
  rows: FeatureFlag[];
  onEdit: (flag: FeatureFlag) => void;
  onDelete: (flag: FeatureFlag) => void;
}) {
  return (
    <div
      data-testid="flags-table"
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
            <th className="text-center px-4 py-2 font-medium">Enabled</th>
            <th className="text-right px-4 py-2 font-medium">Realms</th>
            <th className="text-right px-4 py-2 font-medium">Users</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((f) => (
            <FlagRow
              key={f.name}
              flag={f}
              onEdit={onEdit}
              onDelete={onDelete}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FlagRow({
  flag,
  onEdit,
  onDelete,
}: {
  flag: FeatureFlag;
  onEdit: (flag: FeatureFlag) => void;
  onDelete: (flag: FeatureFlag) => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  // Inline enabled toggle: a single-field PUT flipping `enabled`. We
  // invalidate on success so the row repaints from the server's truth, and
  // surface a toast (the row does not optimistically move) on failure.
  const toggle = useMutation({
    mutationFn: (next: boolean) =>
      updateFlag(flag.name, { enabled: next } satisfies UpdateFlagRequest),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: FLAGS_QUERY_KEY });
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to update feature flag.'),
        severity: 'error',
      });
    },
  });

  const realms = flag.realms ?? [];
  const users = flag.users ?? [];

  return (
    <tr
      data-testid={`flag-row-${flag.name}`}
      data-enabled={flag.enabled ? 'true' : 'false'}
      className="border-b last:border-0 hover:bg-bg-tertiary/30"
      style={{ borderColor: 'rgba(31,41,55,0.5)' }}
    >
      <td className="px-4 py-2 text-text-primary font-mono">{flag.name}</td>
      <td className="px-4 py-2 text-xs text-text-secondary max-w-[20rem] truncate">
        {flag.description || '—'}
      </td>
      <td className="px-4 py-2 text-center">
        <button
          type="button"
          role="switch"
          aria-checked={flag.enabled}
          aria-label={`Toggle ${flag.name}`}
          data-testid={`flag-toggle-${flag.name}`}
          disabled={toggle.isPending}
          onClick={() => toggle.mutate(!flag.enabled)}
          className={`inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-50 ${
            flag.enabled ? 'bg-accent-cyan/70' : 'bg-bg-tertiary'
          }`}
        >
          <span
            className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${
              flag.enabled ? 'translate-x-4' : 'translate-x-1'
            }`}
          />
        </button>
      </td>
      <td
        className="px-4 py-2 text-right text-xs text-text-secondary font-mono"
        data-testid={`flag-realms-count-${flag.name}`}
      >
        {realms.length}
      </td>
      <td
        className="px-4 py-2 text-right text-xs text-text-secondary font-mono"
        data-testid={`flag-users-count-${flag.name}`}
      >
        {users.length}
      </td>
      <td className="px-4 py-2 text-right whitespace-nowrap">
        <button
          type="button"
          data-testid="flag-edit-btn"
          onClick={() => onEdit(flag)}
          className="text-xs text-text-secondary hover:text-text-primary hover:underline mr-3"
        >
          Edit
        </button>
        <button
          type="button"
          data-testid="flag-delete-btn"
          onClick={() => onDelete(flag)}
          className="text-xs text-accent-error hover:underline"
        >
          Delete
        </button>
      </td>
    </tr>
  );
}

function CreateFlagModal({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [enabled, setEnabled] = useState(false);
  const [realms, setRealms] = useState('');
  const [users, setUsers] = useState('');

  const create = useMutation({
    mutationFn: (body: CreateFlagRequest) => createFlag(body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: FLAGS_QUERY_KEY });
      pushToast({
        message: `Flag "${name.trim()}" created.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to create feature flag.'),
        severity: 'error',
      });
    },
  });

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    create.mutate({
      name: name.trim(),
      description: description.trim(),
      enabled,
      realms: parseScopeList(realms),
      users: parseScopeList(users),
    });
  }

  return (
    <Modal open onClose={onClose} title="New Feature Flag">
      <form
        data-testid="flag-create-form"
        onSubmit={onSubmit}
        noValidate
        className="flex flex-col gap-4"
      >
        <Field label="Name" required>
          <input
            type="text"
            data-testid="flag-name-input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            autoFocus
            className={inputClass + ' font-mono'}
            placeholder="dark-mode"
          />
        </Field>
        <Field label="Description">
          <input
            type="text"
            data-testid="flag-description-input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
            placeholder="What this flag gates"
          />
        </Field>
        <EnabledCheckbox checked={enabled} onChange={setEnabled} />
        <ScopeField
          label="Realms"
          hint="Comma or newline separated"
          value={realms}
          onChange={setRealms}
          testId="flag-realms-input"
        />
        <ScopeField
          label="Users"
          hint="Comma or newline separated"
          value={users}
          onChange={setUsers}
          testId="flag-users-input"
        />
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="flag-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="flag-create-submit"
            disabled={!name.trim() || create.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function EditFlagModal({
  flag,
  onClose,
}: {
  flag: FeatureFlag;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const [description, setDescription] = useState(flag.description);
  const [enabled, setEnabled] = useState(flag.enabled);
  const [realms, setRealms] = useState((flag.realms ?? []).join(', '));
  const [users, setUsers] = useState((flag.users ?? []).join(', '));

  const update = useMutation({
    mutationFn: (body: UpdateFlagRequest) => updateFlag(flag.name, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: FLAGS_QUERY_KEY });
      pushToast({
        message: `Flag "${flag.name}" updated.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to update feature flag.'),
        severity: 'error',
      });
    },
  });

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    update.mutate({
      description: description.trim(),
      enabled,
      realms: parseScopeList(realms),
      users: parseScopeList(users),
    });
  }

  return (
    <Modal open onClose={onClose} title={`Edit Flag — ${flag.name}`}>
      <form
        data-testid="flag-edit-form"
        onSubmit={onSubmit}
        noValidate
        className="flex flex-col gap-4"
      >
        <Field label="Description">
          <input
            type="text"
            data-testid="flag-description-input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
            placeholder="What this flag gates"
          />
        </Field>
        <EnabledCheckbox checked={enabled} onChange={setEnabled} />
        <ScopeField
          label="Realms"
          hint="Comma or newline separated"
          value={realms}
          onChange={setRealms}
          testId="flag-realms-input"
        />
        <ScopeField
          label="Users"
          hint="Comma or newline separated"
          value={users}
          onChange={setUsers}
          testId="flag-users-input"
        />
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="flag-edit-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="flag-edit-submit"
            disabled={update.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {update.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteFlagModal({
  flag,
  onClose,
}: {
  flag: FeatureFlag;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const del = useMutation({
    mutationFn: () => deleteFlag(flag.name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: FLAGS_QUERY_KEY });
      pushToast({
        message: `Flag "${flag.name}" deleted.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to delete feature flag.'),
        severity: 'error',
      });
    },
  });

  return (
    <Modal open onClose={onClose} title="Delete Feature Flag">
      <div data-testid="flag-delete-modal" className="flex flex-col gap-3">
        <p className="text-sm text-text-primary">
          Delete the flag{' '}
          <span className="font-semibold font-mono">{flag.name}</span>?
        </p>
        <p className="text-xs text-text-secondary">
          Any code gated on this flag will fall back to its default behavior.
          This cannot be undone.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="flag-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="flag-delete-confirm"
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

function EnabledCheckbox({
  checked,
  onChange,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
      <input
        type="checkbox"
        data-testid="flag-enabled-input"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="h-4 w-4 rounded border-border/50 bg-bg-tertiary accent-accent-cyan"
      />
      <span className="uppercase tracking-widest">Enabled (global switch)</span>
    </label>
  );
}

function ScopeField({
  label,
  hint,
  value,
  onChange,
  testId,
}: {
  label: string;
  hint: string;
  value: string;
  onChange: (v: string) => void;
  testId: string;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary w-full">
      <span className="uppercase tracking-widest">
        {label}{' '}
        <span className="normal-case tracking-normal text-text-muted">
          ({hint})
        </span>
      </span>
      <textarea
        data-testid={testId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={2}
        className={inputClass + ' font-mono resize-none'}
        placeholder="prod, staging"
      />
    </label>
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
