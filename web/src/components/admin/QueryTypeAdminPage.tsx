import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type { QueryType } from '../../api/types';
import {
  useQueryTypes,
  useCreateQueryType,
  useUpdateQueryType,
  useDeleteQueryType,
} from '../../hooks/useQueryTypes';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

// QueryTypeAdminPage is the admin CRUD surface for QueryTypes — the last
// first-class ontology entity to get a create/edit/delete form. It mirrors
// the ValueType / ActionType admin pages but keeps the editor focused on the
// core fields (apiName / displayName / description / status + JSON-encoded
// parameters / output / query). The richer schema builders the other entities
// have are intentionally deferred; raw-JSON textareas keep the CRUD loop
// usable without a bespoke editor.

// stringifyJSON renders a JSONB column value as pretty text for the textareas.
function stringifyJSON(v: unknown, fallback: string): string {
  if (v === undefined || v === null) return fallback;
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return fallback;
  }
}

export function QueryTypeAdminPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { data: queryTypes, isLoading, error } = useQueryTypes(ontologyApiName);

  const [search, setSearch] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<QueryType | null>(null);
  const [deleting, setDeleting] = useState<QueryType | null>(null);

  const filtered = useMemo(() => {
    if (!queryTypes) return [];
    const q = search.trim().toLowerCase();
    const list = queryTypes.filter((qt) => {
      if (!q) return true;
      return (
        qt.apiName.toLowerCase().includes(q) ||
        qt.displayName.toLowerCase().includes(q)
      );
    });
    list.sort((a, b) => a.displayName.localeCompare(b.displayName));
    return list;
  }, [queryTypes, search]);

  if (!ontologyApiName) {
    return (
      <div
        data-testid="query-type-admin-no-ontology"
        className="flex items-center justify-center h-[calc(100vh-3rem)] text-text-secondary text-sm"
      >
        Select an ontology from the dashboard first.
      </div>
    );
  }

  return (
    <div
      data-testid="query-type-admin-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Ontology Manager — Query Types
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          {ontologyApiName}
        </span>
        <div className="flex-1" />
        <button
          type="button"
          data-testid="query-type-new-btn"
          onClick={() => setCreateOpen(true)}
          className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
        >
          + New Query Type
        </button>
      </header>

      <div
        className="px-6 py-3 border-b flex flex-wrap items-center gap-3"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <input
          type="search"
          data-testid="query-type-search-input"
          aria-label="Search query types"
          placeholder="Search by name or apiName…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-[12rem] max-w-md px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
        />
      </div>

      <div className="flex-1 px-6 py-4">
        {isLoading && (
          <div
            data-testid="query-type-admin-loading"
            className="flex items-center justify-center py-20"
          >
            <LoadingSpinner size="lg" />
          </div>
        )}
        {!isLoading && error && (
          <p
            data-testid="query-type-admin-error"
            className="text-sm text-accent-error"
          >
            Failed to load query types: {(error as Error).message}
          </p>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div
            data-testid="query-type-admin-empty"
            className="rounded border px-6 py-10 text-center"
            style={{
              borderColor: 'rgba(31,41,55,0.5)',
              background: 'rgba(13,17,23,0.4)',
            }}
          >
            <p className="text-sm text-text-primary font-semibold">
              No query types yet
            </p>
            <p className="text-xs text-text-secondary mt-2">
              Create a Query Type to define a reusable, parameterised query
              (predefined filter + aggregation) that callers can execute by
              apiName.
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <QueryTypeTable
            rows={filtered}
            onEdit={setEditing}
            onDelete={setDeleting}
          />
        )}
      </div>

      {createOpen && (
        <QueryTypeFormModal
          ontologyApiName={ontologyApiName}
          onClose={() => setCreateOpen(false)}
        />
      )}
      {editing && (
        <QueryTypeFormModal
          ontologyApiName={ontologyApiName}
          editing={editing}
          onClose={() => setEditing(null)}
        />
      )}
      {deleting && (
        <DeleteQueryTypeModal
          ontologyApiName={ontologyApiName}
          queryType={deleting}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function QueryTypeTable({
  rows,
  onEdit,
  onDelete,
}: {
  rows: QueryType[];
  onEdit: (qt: QueryType) => void;
  onDelete: (qt: QueryType) => void;
}) {
  return (
    <table
      data-testid="query-type-table"
      className="w-full text-sm border-collapse"
    >
      <thead>
        <tr className="text-left text-xs uppercase tracking-wider text-text-secondary">
          <th className="px-3 py-2">Display Name</th>
          <th className="px-3 py-2">API Name</th>
          <th className="px-3 py-2">Status</th>
          <th className="px-3 py-2 w-px">Actions</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((qt) => (
          <tr
            key={qt.rid}
            data-testid={`query-type-row-${qt.apiName}`}
            className="border-t"
            style={{ borderColor: 'rgba(31,41,55,0.4)' }}
          >
            <td className="px-3 py-2 text-text-primary font-medium">
              {qt.displayName}
            </td>
            <td className="px-3 py-2 text-text-secondary font-mono text-xs">
              {qt.apiName}
            </td>
            <td className="px-3 py-2 text-text-secondary">{qt.status}</td>
            <td className="px-3 py-2 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onEdit(qt)}
                className="px-2 py-1 text-xs rounded text-accent-cyan hover:bg-accent-cyan/10"
              >
                Edit
              </button>
              <button
                type="button"
                onClick={() => onDelete(qt)}
                className="px-2 py-1 text-xs rounded text-accent-error hover:bg-accent-error/10"
              >
                Delete
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function QueryTypeFormModal({
  ontologyApiName,
  editing,
  onClose,
}: {
  ontologyApiName: string;
  editing?: QueryType;
  onClose: () => void;
}) {
  const isEdit = !!editing;
  const createMut = useCreateQueryType(ontologyApiName);
  const updateMut = useUpdateQueryType(ontologyApiName);

  const [apiName, setApiName] = useState(editing?.apiName ?? '');
  const [displayName, setDisplayName] = useState(editing?.displayName ?? '');
  const [description, setDescription] = useState(editing?.description ?? '');
  const [status, setStatus] = useState(editing?.status ?? 'ACTIVE');
  const [parametersText, setParametersText] = useState(
    stringifyJSON(editing?.parameters, '[]'),
  );
  const [outputText, setOutputText] = useState(
    stringifyJSON(editing?.output, '{}'),
  );
  const [queryText, setQueryText] = useState(
    stringifyJSON(editing?.query, '{}'),
  );
  const [formError, setFormError] = useState<string | null>(null);

  const pending = createMut.isPending || updateMut.isPending;

  function parseOrThrow(label: string, text: string): unknown {
    const trimmed = text.trim();
    if (!trimmed) return undefined;
    try {
      return JSON.parse(trimmed);
    } catch {
      throw new Error(`${label} is not valid JSON`);
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);

    if (!isEdit && !apiName.trim()) {
      setFormError('apiName is required');
      return;
    }
    if (!displayName.trim()) {
      setFormError('displayName is required');
      return;
    }

    let parameters: unknown;
    let output: unknown;
    let query: unknown;
    try {
      parameters = parseOrThrow('Parameters', parametersText);
      output = parseOrThrow('Output', outputText);
      query = parseOrThrow('Query', queryText);
    } catch (err) {
      setFormError((err as Error).message);
      return;
    }

    try {
      if (isEdit && editing) {
        await updateMut.mutateAsync({
          rid: editing.rid,
          body: {
            displayName: displayName.trim(),
            description: description.trim() || undefined,
            status: status.trim() || undefined,
            ...(parameters !== undefined ? { parameters } : {}),
            ...(output !== undefined ? { output } : {}),
            ...(query !== undefined ? { query } : {}),
          },
        });
      } else {
        await createMut.mutateAsync({
          apiName: apiName.trim(),
          displayName: displayName.trim(),
          description: description.trim() || undefined,
          status: status.trim() || undefined,
          ...(parameters !== undefined ? { parameters } : {}),
          ...(output !== undefined ? { output } : {}),
          ...(query !== undefined ? { query } : {}),
        });
      }
      onClose();
    } catch (err) {
      setFormError((err as Error).message || 'Request failed');
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={isEdit ? 'Edit Query Type' : 'New Query Type'}
      size="lg"
    >
      <form
        data-testid={isEdit ? 'query-type-edit-form' : 'query-type-create-form'}
        onSubmit={handleSubmit}
        className="flex flex-col gap-4"
      >
        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          API Name
          <input
            data-testid="query-type-apiName"
            value={apiName}
            disabled={isEdit}
            onChange={(e) => setApiName(e.target.value)}
            placeholder="topCustomers"
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-50"
          />
        </label>

        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Display Name
          <input
            data-testid="query-type-displayName"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Top Customers"
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>

        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Description
          <input
            data-testid="query-type-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>

        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Status
          <select
            data-testid="query-type-status"
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            className="px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          >
            <option value="ACTIVE">ACTIVE</option>
            <option value="EXPERIMENTAL">EXPERIMENTAL</option>
            <option value="DEPRECATED">DEPRECATED</option>
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Parameters (JSON)
          <textarea
            data-testid="query-type-parameters"
            value={parametersText}
            onChange={(e) => setParametersText(e.target.value)}
            rows={3}
            className="px-3 py-1.5 text-xs font-mono rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>

        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Output (JSON)
          <textarea
            data-testid="query-type-output"
            value={outputText}
            onChange={(e) => setOutputText(e.target.value)}
            rows={2}
            className="px-3 py-1.5 text-xs font-mono rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>

        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Query (JSON)
          <textarea
            data-testid="query-type-query"
            value={queryText}
            onChange={(e) => setQueryText(e.target.value)}
            rows={3}
            className="px-3 py-1.5 text-xs font-mono rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40"
          />
        </label>

        {formError && (
          <p data-testid="query-type-form-error" className="text-xs text-accent-error">
            {formError}
          </p>
        )}

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="query-type-submit"
            disabled={pending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-50"
          >
            {isEdit ? 'Save changes' : 'Create Query Type'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteQueryTypeModal({
  ontologyApiName,
  queryType,
  onClose,
}: {
  ontologyApiName: string;
  queryType: QueryType;
  onClose: () => void;
}) {
  const deleteMut = useDeleteQueryType(ontologyApiName);
  const [err, setErr] = useState<string | null>(null);

  async function handleDelete() {
    setErr(null);
    try {
      await deleteMut.mutateAsync(queryType.rid);
      onClose();
    } catch (e) {
      setErr((e as Error).message || 'Delete failed');
    }
  }

  return (
    <Modal open onClose={onClose} title="Delete Query Type">
      <div data-testid="query-type-delete-confirm" className="flex flex-col gap-4">
        <p className="text-sm text-text-primary">
          Delete <span className="font-semibold">{queryType.displayName}</span>{' '}
          (<span className="font-mono text-xs">{queryType.apiName}</span>)? This
          cannot be undone.
        </p>
        {err && (
          <p data-testid="query-type-delete-error" className="text-xs text-accent-error">
            {err}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="query-type-delete-confirm-btn"
            onClick={handleDelete}
            disabled={deleteMut.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-error/20 text-accent-error border border-accent-error/40 hover:bg-accent-error/30 disabled:opacity-50"
          >
            Delete
          </button>
        </div>
      </div>
    </Modal>
  );
}
