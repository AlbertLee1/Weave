import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type {
  CreateServiceAccountRequest,
  ServiceAccount,
  UpdateServiceAccountRequest,
} from '../../api/serviceAccounts';
import {
  createServiceAccount,
  deleteServiceAccount,
  listServiceAccounts,
  updateServiceAccount,
} from '../../api/serviceAccounts';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const QUERY_KEY = ['admin', 'service-accounts'];

// parseScopes splits the free-text scopes field into a trimmed, de-empty'd
// list. Both commas and newlines are accepted as separators so operators can
// paste a multi-line list or type a CSV interchangeably.
function parseScopes(raw: string): string[] {
  return raw
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter((s) => s !== '');
}

// formatTimestamp renders an ISO timestamp for display, falling back to an
// em-dash for empty / unparseable values so degraded rows stay clean.
function formatTimestamp(iso?: string | null): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

export function ServiceAccountsAdminPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: listServiceAccounts,
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<ServiceAccount | null>(null);
  const [deleting, setDeleting] = useState<ServiceAccount | null>(null);

  const accounts = data ?? [];

  return (
    <div
      data-testid="service-accounts-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Service Accounts
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Non-interactive identities
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="service-account-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Service Account
        </button>
      </header>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="service-accounts-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="service-accounts-error"
            role="alert"
            className="text-sm text-accent-error"
          >
            Failed to load service accounts: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && accounts.length === 0 && (
          <div
            data-testid="service-accounts-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No service accounts yet
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Create a service account to issue a non-interactive identity for
              CI pipelines, automations, or integrations.
            </p>
          </div>
        )}
        {!isLoading && !error && accounts.length > 0 && (
          <ServiceAccountsTable
            rows={accounts}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>

      {createOpen && (
        <ServiceAccountFormModal onClose={() => setCreateOpen(false)} />
      )}
      {editing && (
        <ServiceAccountFormModal
          editing={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteServiceAccountModal
          account={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function ServiceAccountsTable({
  rows,
  onEdit,
  onDelete,
}: {
  rows: ServiceAccount[];
  onEdit: (sa: ServiceAccount) => void;
  onDelete: (sa: ServiceAccount) => void;
}) {
  return (
    <div
      data-testid="service-accounts-table"
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
            <th className="text-left px-4 py-2 font-medium">Scopes</th>
            <th className="text-left px-4 py-2 font-medium">Owner</th>
            <th className="text-left px-4 py-2 font-medium">Expires</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((sa) => {
            const isDisabled = !!sa.disabledAt;
            return (
              <tr
                key={sa.id}
                data-testid={`service-account-row-${sa.id}`}
                data-service-account-id={sa.id}
                className="border-b last:border-0 hover:bg-bg-tertiary/30"
                style={{ borderColor: 'rgba(31,41,55,0.5)' }}
              >
                <td className="px-4 py-2 text-text-primary">
                  <span className="flex items-center gap-2">
                    <span>{sa.name}</span>
                    {isDisabled && (
                      <span
                        data-testid="service-account-disabled-badge"
                        className="px-1.5 py-0.5 rounded text-[10px] uppercase tracking-widest bg-accent-error/20 text-accent-error border border-accent-error/40"
                      >
                        Disabled
                      </span>
                    )}
                  </span>
                </td>
                <td className="px-4 py-2 text-text-secondary">
                  {sa.description || '—'}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary font-mono">
                  {sa.scopes.length > 0 ? sa.scopes.join(', ') : '—'}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary font-mono">
                  {sa.ownerUserId}
                </td>
                <td className="px-4 py-2 text-xs text-text-secondary">
                  {formatTimestamp(sa.expiresAt)}
                </td>
                <td className="px-4 py-2 text-right whitespace-nowrap">
                  <button
                    type="button"
                    data-testid="service-account-edit-btn"
                    data-service-account-id={sa.id}
                    onClick={() => onEdit(sa)}
                    className="text-xs text-accent-cyan hover:underline mr-3"
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    data-testid="service-account-delete-btn"
                    data-service-account-id={sa.id}
                    onClick={() => onDelete(sa)}
                    className="text-xs text-accent-error hover:underline"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// toDateInput converts an ISO timestamp into the yyyy-mm-dd value an
// <input type="date"> expects; an empty / unparseable value yields ''.
function toDateInput(iso?: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toISOString().slice(0, 10);
}

// dateInputToRFC3339 turns a yyyy-mm-dd date-input value into the RFC3339
// timestamp the backend's expiresAt parser requires (start of that UTC day).
function dateInputToRFC3339(value: string): string {
  return new Date(`${value}T00:00:00Z`).toISOString();
}

function ServiceAccountFormModal({
  editing,
  onClose,
}: {
  editing?: ServiceAccount;
  onClose: () => void;
}) {
  const isEdit = !!editing;
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const [name, setName] = useState(editing?.name ?? '');
  const [description, setDescription] = useState(editing?.description ?? '');
  const [scopesText, setScopesText] = useState(
    (editing?.scopes ?? []).join(', '),
  );
  // The date input only carries day precision, so we snapshot the original
  // day on mount and only forward a new expiresAt when the operator actually
  // changes it. Editing an untouched account therefore preserves the stored
  // timestamp's time-of-day instead of truncating it to midnight UTC.
  const originalExpiresDay = toDateInput(editing?.expiresAt);
  const [expiresAt, setExpiresAt] = useState(originalExpiresDay);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: (body: CreateServiceAccountRequest) => createServiceAccount(body),
  });
  const update = useMutation({
    mutationFn: (body: UpdateServiceAccountRequest) =>
      updateServiceAccount(editing!.id, body),
  });

  const isPending = create.isPending || update.isPending;
  const canSubmit = (isEdit || name.trim() !== '') && !isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    const scopes = parseScopes(scopesText);
    try {
      if (editing) {
        const body: UpdateServiceAccountRequest = {
          description: description.trim(),
          scopes,
        };
        // Only touch expiresAt when the operator changed the day. Sending the
        // field on an unchanged edit would truncate the stored timestamp to
        // midnight UTC (the date input has no time component). An empty value
        // explicitly clears the expiry server-side.
        if (expiresAt !== originalExpiresDay) {
          body.expiresAt = expiresAt.trim()
            ? dateInputToRFC3339(expiresAt)
            : '';
        }
        await update.mutateAsync(body);
      } else {
        const body: CreateServiceAccountRequest = {
          name: name.trim(),
          description: description.trim(),
          scopes,
        };
        if (expiresAt.trim()) {
          body.expiresAt = dateInputToRFC3339(expiresAt);
        }
        await create.mutateAsync(body);
      }
      await queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      onClose();
    } catch (err) {
      const msg = describeApiError(
        err,
        isEdit
          ? 'Failed to update service account.'
          : 'Failed to create service account.',
      );
      setSubmitError(msg);
      pushToast({ message: msg, severity: 'error' });
    }
  }

  const modePrefix = isEdit ? 'service-account-edit' : 'service-account-create';
  return (
    <Modal
      open
      onClose={onClose}
      title={isEdit ? `Edit: ${editing.name}` : 'New Service Account'}
      size="lg"
    >
      <form
        onSubmit={onSubmit}
        data-testid={`${modePrefix}-form`}
        className="flex flex-col gap-4"
      >
        <Field label="Name" required>
          <input
            type="text"
            data-testid="service-account-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={isEdit}
            required={!isEdit}
            autoFocus={!isEdit}
            className={inputClass + ' font-mono'}
            placeholder="ci-runner"
          />
        </Field>
        <Field label="Description">
          <input
            type="text"
            data-testid="service-account-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            autoFocus={isEdit}
            className={inputClass}
          />
        </Field>
        <Field
          label="Scopes"
          hint="Comma- or newline-separated list. Values are trimmed; empties are dropped."
        >
          <textarea
            data-testid="service-account-scopes"
            value={scopesText}
            onChange={(e) => setScopesText(e.target.value)}
            rows={3}
            className={inputClass + ' font-mono'}
            placeholder="ontology.read, action.apply"
          />
        </Field>
        <Field
          label="Expires At"
          hint="Optional. Leave blank for a non-expiring account."
        >
          <input
            type="date"
            data-testid="service-account-expires-at"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
            className={inputClass}
          />
        </Field>

        {submitError && (
          <p
            role="alert"
            data-testid={`${modePrefix}-error`}
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid={`${modePrefix}-cancel`}
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid={`${modePrefix}-submit`}
            disabled={!canSubmit}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {isPending
              ? isEdit
                ? 'Saving…'
                : 'Creating…'
              : isEdit
                ? 'Save changes'
                : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteServiceAccountModal({
  account,
  onClose,
}: {
  account: ServiceAccount;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const del = useMutation({
    mutationFn: () => deleteServiceAccount(account.id),
  });

  async function onConfirm() {
    setSubmitError(null);
    try {
      await del.mutateAsync();
      await queryClient.invalidateQueries({ queryKey: QUERY_KEY });
      onClose();
    } catch (err) {
      const msg = describeApiError(err, 'Failed to delete service account.');
      setSubmitError(msg);
      pushToast({ message: msg, severity: 'error' });
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Service Account">
      <div
        data-testid="service-account-delete-confirm"
        data-service-account-id={account.id}
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{account.name}</span>?
        </p>
        <p className="text-xs text-text-secondary">
          The service account is disabled and can no longer authenticate. Any
          credentials issued to it stop working immediately.
        </p>
        {submitError && (
          <p
            role="alert"
            data-testid="service-account-delete-error"
            className="text-xs text-accent-error"
          >
            {submitError}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="service-account-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="service-account-delete-confirm-btn"
            onClick={onConfirm}
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

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-60';

function Field({
  label,
  required,
  hint,
  children,
}: {
  label: string;
  required?: boolean;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary">
      <span className="uppercase tracking-widest">
        {label}
        {required && <span className="text-accent-error"> *</span>}
      </span>
      {children}
      {hint && <span className="text-[11px] text-text-muted">{hint}</span>}
    </label>
  );
}
