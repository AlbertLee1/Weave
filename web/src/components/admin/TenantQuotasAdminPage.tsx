import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createQuota,
  deleteQuota,
  getUsage,
  listQuotas,
  updateQuota,
  type CreateQuotaRequest,
  type MonthlyUsage,
  type Quota,
  type UpdateQuotaRequest,
} from '../../api/tenantQuotas';
import { describeApiError } from '../../api/describeError';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

const QUOTAS_QUERY_KEY = ['admin', 'tenant-quotas'] as const;

// Percent at or above this threshold paints the usage row in the warning
// palette — surfaces tenants approaching their cap before they get throttled.
const USAGE_WARN_PERCENT = 80;

// Compact number formatter so seven-figure object/storage caps stay readable
// in the table without truncation (e.g. 5,000,000 → "5,000,000").
function formatNumber(n: number): string {
  if (!Number.isFinite(n)) return '—';
  return n.toLocaleString();
}

export function TenantQuotasAdminPage() {
  const {
    data: quotas,
    isLoading,
    error,
  } = useQuery({ queryKey: QUOTAS_QUERY_KEY, queryFn: listQuotas });

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<Quota | null>(null);
  const [deleting, setDeleting] = useState<Quota | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  const sorted = quotas
    ? [...quotas].sort((a, b) => a.tenant.localeCompare(b.tenant))
    : [];

  return (
    <div
      data-testid="tenant-quotas-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Tenant Quotas
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Multi-Tenant Resource Limits
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="quota-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Quota
        </button>
      </header>

      <div className="flex flex-1 overflow-hidden">
        {/* Left: quota list */}
        <section className="flex-1 overflow-y-auto px-6 py-4">
          {isLoading && (
            <div
              data-testid="quotas-loading"
              className="flex items-center justify-center py-20"
            >
              <LoadingSpinner size="lg" />
            </div>
          )}
          {!isLoading && error && (
            <p data-testid="quotas-error" className="text-sm text-accent-error">
              Failed to load tenant quotas: {(error as Error).message}
            </p>
          )}
          {!isLoading && !error && sorted.length === 0 && (
            <div
              data-testid="quotas-empty"
              className="rounded border px-6 py-10 text-center"
              style={{
                borderColor: 'rgba(31,41,55,0.5)',
                background: 'rgba(13,17,23,0.4)',
              }}
            >
              <p className="text-sm text-text-primary font-semibold">
                No tenant quotas yet
              </p>
              <p className="text-xs text-text-secondary mt-2">
                Create a quota to cap a tenant's objects, storage, and request
                rate.
              </p>
            </div>
          )}
          {!isLoading && !error && sorted.length > 0 && (
            <QuotasTable
              rows={sorted}
              selected={selected}
              onSelect={setSelected}
              onEdit={setEditing}
              onDelete={setDeleting}
            />
          )}
        </section>

        {/* Right: usage for selected tenant */}
        <aside
          data-testid="usage-pane"
          className="w-96 border-l overflow-y-auto"
          style={{ borderColor: 'rgba(31,41,55,0.5)' }}
        >
          {selected ? (
            <UsagePanel tenant={selected} />
          ) : (
            <div className="flex items-center justify-center h-full px-6 text-center text-sm text-text-secondary">
              Select a tenant to view its current-month usage.
            </div>
          )}
        </aside>
      </div>

      {createOpen && <CreateQuotaModal onClose={() => setCreateOpen(false)} />}
      {editing && (
        <EditQuotaModal quota={editing} onClose={() => setEditing(null)} />
      )}
      {deleting && (
        <DeleteQuotaModal quota={deleting} onClose={() => setDeleting(null)} />
      )}
    </div>
  );
}

function QuotasTable({
  rows,
  selected,
  onSelect,
  onEdit,
  onDelete,
}: {
  rows: Quota[];
  selected: string | null;
  onSelect: (tenant: string) => void;
  onEdit: (quota: Quota) => void;
  onDelete: (quota: Quota) => void;
}) {
  return (
    <div
      data-testid="quotas-table"
      className="rounded border overflow-hidden"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <table className="w-full text-sm">
        <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
          <tr className="border-b" style={{ borderColor: 'rgba(31,41,55,0.5)' }}>
            <th className="text-left px-4 py-2 font-medium">Tenant</th>
            <th className="text-right px-4 py-2 font-medium">Max Objects</th>
            <th className="text-right px-4 py-2 font-medium">Max Storage</th>
            <th className="text-right px-4 py-2 font-medium">Max QPS</th>
            <th className="text-right px-4 py-2 font-medium">Burst</th>
            <th className="text-left px-4 py-2 font-medium">Description</th>
            <th className="text-right px-4 py-2 font-medium">Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((q) => (
            <tr
              key={q.tenant}
              data-testid={`quota-row-${q.tenant}`}
              className={`border-b last:border-0 hover:bg-bg-tertiary/30 ${
                selected === q.tenant ? 'bg-bg-tertiary/40' : ''
              }`}
              style={{ borderColor: 'rgba(31,41,55,0.5)' }}
            >
              <td className="px-4 py-2 text-text-primary font-mono">
                {q.tenant}
              </td>
              <td className="px-4 py-2 text-right text-xs text-text-secondary font-mono">
                {formatNumber(q.maxObjects)}
              </td>
              <td className="px-4 py-2 text-right text-xs text-text-secondary font-mono">
                {formatNumber(q.maxStorage)}
              </td>
              <td className="px-4 py-2 text-right text-xs text-text-secondary font-mono">
                {formatNumber(q.maxQPS)}
              </td>
              <td className="px-4 py-2 text-right text-xs text-text-secondary font-mono">
                {formatNumber(q.burst)}
              </td>
              <td className="px-4 py-2 text-xs text-text-secondary max-w-[16rem] truncate">
                {q.description || '—'}
              </td>
              <td className="px-4 py-2 text-right whitespace-nowrap">
                <button
                  type="button"
                  data-testid="quota-select-btn"
                  onClick={() => onSelect(q.tenant)}
                  className="text-xs text-accent-cyan hover:underline mr-3"
                >
                  Usage
                </button>
                <button
                  type="button"
                  data-testid="quota-edit-btn"
                  onClick={() => onEdit(q)}
                  className="text-xs text-text-secondary hover:text-text-primary hover:underline mr-3"
                >
                  Edit
                </button>
                <button
                  type="button"
                  data-testid="quota-delete-btn"
                  onClick={() => onDelete(q)}
                  className="text-xs text-accent-error hover:underline"
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

function UsagePanel({ tenant }: { tenant: string }) {
  const {
    data: usage,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['admin', 'tenant-usage', tenant],
    queryFn: () => getUsage(tenant),
  });

  return (
    <div className="p-6 space-y-4">
      <div>
        <h2 className="text-sm font-semibold text-text-primary">
          Usage —{' '}
          <span className="font-mono text-text-primary">{tenant}</span>
        </h2>
        <p className="text-[10px] uppercase tracking-widest text-text-secondary mt-1">
          Current Calendar Month
        </p>
      </div>

      {isLoading && (
        <div
          data-testid="usage-loading"
          className="flex items-center justify-center py-10"
        >
          <LoadingSpinner size="md" />
        </div>
      )}

      {!isLoading && error && (
        <p data-testid="usage-error" className="text-xs text-accent-error">
          Failed to load usage: {(error as Error).message}
        </p>
      )}

      {!isLoading && !error && (usage ?? []).length === 0 && (
        <p
          data-testid="usage-empty"
          className="text-xs text-text-secondary italic"
        >
          No usage recorded for{' '}
          <span className="font-mono not-italic">{tenant}</span> this month.
        </p>
      )}

      {!isLoading && !error && (usage ?? []).length > 0 && (
        <ul className="flex flex-col gap-4">
          {(usage ?? []).map((row) => (
            <UsageRow key={row.metric} row={row} />
          ))}
        </ul>
      )}
    </div>
  );
}

function UsageRow({ row }: { row: MonthlyUsage }) {
  const warn = row.percent >= USAGE_WARN_PERCENT;
  const pct = Math.max(0, Math.min(100, row.percent));
  return (
    <li
      data-testid={`usage-row-${row.metric}`}
      data-warn={warn ? 'true' : 'false'}
      className="flex flex-col gap-1"
    >
      <div className="flex items-center justify-between text-xs">
        <span className="font-mono text-text-primary">{row.metric}</span>
        <span
          className={`font-mono ${warn ? 'text-accent-error' : 'text-text-secondary'}`}
        >
          {row.percent}%
        </span>
      </div>
      <div
        className="h-2 w-full rounded-full overflow-hidden"
        style={{ background: 'rgba(31,41,55,0.6)' }}
        role="progressbar"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={`${row.metric} usage`}
      >
        <div
          data-testid={`usage-bar-${row.metric}`}
          className="h-full rounded-full transition-all"
          style={{
            width: `${pct}%`,
            background: warn ? 'var(--accent-error, #f87171)' : 'var(--accent-cyan, #22d3ee)',
          }}
        />
      </div>
      <div className="text-[10px] text-text-secondary font-mono">
        {formatNumber(row.amount)} / {formatNumber(row.cap)}
      </div>
    </li>
  );
}

// numeric field keys shared by the create + edit forms.
type NumericField = 'maxObjects' | 'maxStorage' | 'maxQPS' | 'burst';
const NUMERIC_FIELDS: { key: NumericField; label: string }[] = [
  { key: 'maxObjects', label: 'Max Objects' },
  { key: 'maxStorage', label: 'Max Storage' },
  { key: 'maxQPS', label: 'Max QPS' },
  { key: 'burst', label: 'Burst' },
];

// parseNonNegative returns the parsed number when `raw` is a finite,
// non-negative number, or null otherwise. Empty string is treated as invalid
// so required numeric fields cannot be silently submitted as 0.
function parseNonNegative(raw: string): number | null {
  const trimmed = raw.trim();
  if (trimmed === '') return null;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || n < 0) return null;
  return n;
}

function CreateQuotaModal({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const [tenant, setTenant] = useState('');
  const [values, setValues] = useState<Record<NumericField, string>>({
    maxObjects: '',
    maxStorage: '',
    maxQPS: '',
    burst: '',
  });
  const [description, setDescription] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: (body: CreateQuotaRequest) => createQuota(body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUOTAS_QUERY_KEY });
      pushToast({
        message: `Quota for "${tenant.trim()}" created.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to create tenant quota.'),
        severity: 'error',
      });
    },
  });

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (!tenant.trim()) {
      setFormError('Tenant is required.');
      return;
    }
    const parsed: Partial<Record<NumericField, number>> = {};
    for (const { key, label } of NUMERIC_FIELDS) {
      const n = parseNonNegative(values[key]);
      if (n === null) {
        setFormError(`${label} must be a non-negative number.`);
        return;
      }
      parsed[key] = n;
    }

    create.mutate({
      tenant: tenant.trim(),
      maxObjects: parsed.maxObjects!,
      maxStorage: parsed.maxStorage!,
      maxQPS: parsed.maxQPS!,
      burst: parsed.burst!,
      description: description.trim(),
    });
  }

  return (
    <Modal open onClose={onClose} title="New Tenant Quota">
      <form
        data-testid="quota-create-form"
        onSubmit={onSubmit}
        noValidate
        className="flex flex-col gap-4"
      >
        <Field label="Tenant" required>
          <input
            type="text"
            data-testid="quota-tenant-input"
            value={tenant}
            onChange={(e) => setTenant(e.target.value)}
            required
            autoFocus
            className={inputClass + ' font-mono'}
            placeholder="acme"
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          {NUMERIC_FIELDS.map(({ key, label }) => (
            <Field key={key} label={label} required>
              <input
                type="number"
                min={0}
                data-testid={`quota-${key}-input`}
                value={values[key]}
                onChange={(e) =>
                  setValues((v) => ({ ...v, [key]: e.target.value }))
                }
                className={inputClass + ' font-mono'}
                placeholder="0"
              />
            </Field>
          ))}
        </div>
        <Field label="Description">
          <input
            type="text"
            data-testid="quota-description-input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
            placeholder="What this tenant is for"
          />
        </Field>
        {formError && (
          <p data-testid="quota-form-error" className="text-xs text-accent-error">
            {formError}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="quota-create-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="quota-create-submit"
            disabled={create.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function EditQuotaModal({
  quota,
  onClose,
}: {
  quota: Quota;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const [values, setValues] = useState<Record<NumericField, string>>({
    maxObjects: String(quota.maxObjects),
    maxStorage: String(quota.maxStorage),
    maxQPS: String(quota.maxQPS),
    burst: String(quota.burst),
  });
  const [description, setDescription] = useState(quota.description);
  const [formError, setFormError] = useState<string | null>(null);

  const update = useMutation({
    mutationFn: (body: UpdateQuotaRequest) => updateQuota(quota.tenant, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUOTAS_QUERY_KEY });
      pushToast({
        message: `Quota for "${quota.tenant}" updated.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to update tenant quota.'),
        severity: 'error',
      });
    },
  });

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);

    const body: UpdateQuotaRequest = {};
    for (const { key, label } of NUMERIC_FIELDS) {
      const n = parseNonNegative(values[key]);
      if (n === null) {
        setFormError(`${label} must be a non-negative number.`);
        return;
      }
      body[key] = n;
    }
    body.description = description.trim();

    update.mutate(body);
  }

  return (
    <Modal open onClose={onClose} title={`Edit Quota — ${quota.tenant}`}>
      <form
        data-testid="quota-edit-form"
        onSubmit={onSubmit}
        noValidate
        className="flex flex-col gap-4"
      >
        <div className="grid grid-cols-2 gap-3">
          {NUMERIC_FIELDS.map(({ key, label }) => (
            <Field key={key} label={label} required>
              <input
                type="number"
                min={0}
                data-testid={`quota-${key}-input`}
                value={values[key]}
                onChange={(e) =>
                  setValues((v) => ({ ...v, [key]: e.target.value }))
                }
                className={inputClass + ' font-mono'}
                placeholder="0"
              />
            </Field>
          ))}
        </div>
        <Field label="Description">
          <input
            type="text"
            data-testid="quota-description-input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className={inputClass}
            placeholder="What this tenant is for"
          />
        </Field>
        {formError && (
          <p data-testid="quota-form-error" className="text-xs text-accent-error">
            {formError}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="quota-edit-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="quota-edit-submit"
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

function DeleteQuotaModal({
  quota,
  onClose,
}: {
  quota: Quota;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const pushToast = useToastStore((s) => s.push);

  const del = useMutation({
    mutationFn: () => deleteQuota(quota.tenant),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUOTAS_QUERY_KEY });
      pushToast({
        message: `Quota for "${quota.tenant}" deleted.`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(err, 'Failed to delete tenant quota.'),
        severity: 'error',
      });
    },
  });

  return (
    <Modal open onClose={onClose} title="Delete Tenant Quota">
      <div data-testid="quota-delete-modal" className="flex flex-col gap-3">
        <p className="text-sm text-text-primary">
          Delete the quota for{' '}
          <span className="font-semibold font-mono">{quota.tenant}</span>?
        </p>
        <p className="text-xs text-text-secondary">
          The tenant will fall back to default (unbounded) limits until a new
          quota is created. This cannot be undone.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="quota-delete-cancel"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="quota-delete-confirm"
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
