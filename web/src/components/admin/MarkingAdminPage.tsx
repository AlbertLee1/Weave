import { useEffect, useMemo, useState } from 'react';
import { ApiRequestError } from '../../api/client';
import type { Marking, MarkingGrant } from '../../api/markings';
import {
  useGrantMarking,
  useGrantsByMarking,
  useMarkings,
  useRevokeMarking,
} from '../../hooks/useMarkings';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { Modal } from '../common/Modal';

function formatGrantTimestamp(iso: string): string {
  if (!iso) return '';
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleString();
  } catch {
    return iso;
  }
}

export function MarkingAdminPage() {
  const {
    data: markings,
    isLoading: markingsLoading,
    error: markingsError,
  } = useMarkings();

  const [selectedName, setSelectedName] = useState<string | null>(null);

  useEffect(() => {
    if (!selectedName && markings && markings.length > 0) {
      setSelectedName(markings[0].name);
    }
  }, [markings, selectedName]);

  const {
    data: grants,
    isLoading: grantsLoading,
    error: grantsError,
  } = useGrantsByMarking(selectedName);

  const grantMutation = useGrantMarking();
  const revokeMutation = useRevokeMarking();

  const [grantFormOpen, setGrantFormOpen] = useState(false);
  const [grantUserId, setGrantUserId] = useState('');
  const [grantExpiresInDays, setGrantExpiresInDays] = useState('');
  const [grantError, setGrantError] = useState<string | null>(null);

  const [pendingRevoke, setPendingRevoke] = useState<MarkingGrant | null>(null);
  const [revokeError, setRevokeError] = useState<string | null>(null);

  const selectedMarking: Marking | null = useMemo(() => {
    if (!markings || !selectedName) return null;
    return markings.find((m) => m.name === selectedName) ?? null;
  }, [markings, selectedName]);

  const handleGrantSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedMarking || !grantUserId.trim()) return;
    setGrantError(null);
    const days = grantExpiresInDays.trim();
    const expiresInDays = days ? Number(days) : undefined;
    if (days && (!Number.isFinite(expiresInDays) || (expiresInDays ?? 0) < 0)) {
      setGrantError('Expires in days must be a non-negative number');
      return;
    }
    try {
      await grantMutation.mutateAsync({
        userId: grantUserId.trim(),
        marking: selectedMarking.name,
        options:
          expiresInDays && expiresInDays > 0
            ? { expiresInDays }
            : undefined,
      });
      setGrantFormOpen(false);
      setGrantUserId('');
      setGrantExpiresInDays('');
    } catch (err) {
      if (err instanceof ApiRequestError) {
        setGrantError(`${err.errorName}: ${err.parameters?.reason ?? ''}`);
      } else {
        setGrantError((err as Error).message ?? 'Grant failed');
      }
    }
  };

  const handleConfirmRevoke = async () => {
    if (!pendingRevoke) return;
    setRevokeError(null);
    try {
      await revokeMutation.mutateAsync({
        userId: pendingRevoke.userId,
        marking: pendingRevoke.markingName,
      });
      setPendingRevoke(null);
    } catch (err) {
      if (err instanceof ApiRequestError) {
        setRevokeError(`${err.errorName}: ${err.parameters?.reason ?? ''}`);
      } else {
        setRevokeError((err as Error).message ?? 'Revoke failed');
      }
    }
  };

  return (
    <div className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-hidden">
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Markings — Access Grants
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Mandatory Access Control
        </span>
      </header>

      {markingsError && (
        <div className="px-6 py-3 text-sm text-red-400">
          Failed to load markings: {(markingsError as Error).message}
        </div>
      )}

      <div className="flex flex-1 overflow-hidden">
        {/* Left pane: marking list */}
        <aside
          data-testid="marking-list"
          className="w-64 border-r overflow-y-auto"
          style={{ borderColor: 'rgba(31,41,55,0.5)' }}
        >
          {markingsLoading ? (
            <div className="flex justify-center p-6">
              <LoadingSpinner />
            </div>
          ) : (
            <ul className="py-2">
              {(markings ?? []).map((m) => (
                <li key={m.name}>
                  <button
                    type="button"
                    data-testid={`marking-row-${m.name}`}
                    onClick={() => setSelectedName(m.name)}
                    className={`w-full text-left px-4 py-2 flex items-center gap-3 text-sm transition-colors ${
                      selectedName === m.name
                        ? 'bg-white/5 text-text-primary'
                        : 'text-text-secondary hover:bg-white/[0.03]'
                    }`}
                  >
                    <span
                      aria-hidden="true"
                      className="inline-block w-3 h-3 rounded-sm"
                      style={{ background: m.color || '#64748b' }}
                    />
                    <span className="flex-1 truncate">
                      {m.displayName || m.name}
                    </span>
                    <span className="text-[10px] uppercase tracking-widest text-text-secondary">
                      {m.name}
                    </span>
                  </button>
                </li>
              ))}
              {(markings ?? []).length === 0 && !markingsLoading && (
                <li className="px-4 py-2 text-sm text-text-secondary">
                  No markings defined.
                </li>
              )}
            </ul>
          )}
        </aside>

        {/* Right pane: grants for selected marking */}
        <section className="flex-1 overflow-y-auto">
          {selectedMarking ? (
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between flex-wrap gap-3">
                <div>
                  <h2 className="text-lg font-semibold text-text-primary">
                    {selectedMarking.displayName}{' '}
                    <span className="text-text-secondary font-mono text-sm">
                      ({selectedMarking.name})
                    </span>
                  </h2>
                  {selectedMarking.description && (
                    <p className="text-sm text-text-secondary mt-1">
                      {selectedMarking.description}
                    </p>
                  )}
                </div>
                <button
                  type="button"
                  data-testid="grant-button"
                  onClick={() => {
                    setGrantError(null);
                    setGrantUserId('');
                    setGrantFormOpen(true);
                  }}
                  className="px-3 py-1.5 text-sm rounded-md bg-accent-amber/10 text-accent-amber border border-accent-amber/30 hover:bg-accent-amber/20"
                >
                  Grant to user
                </button>
              </div>

              {grantsError && (
                <div className="text-sm text-red-400">
                  Failed to load grants: {(grantsError as Error).message}
                </div>
              )}

              {grantsLoading ? (
                <div className="flex justify-center py-8">
                  <LoadingSpinner />
                </div>
              ) : (grants ?? []).length === 0 ? (
                <div
                  data-testid="grants-empty"
                  className="p-6 border border-border/30 rounded-md text-sm text-text-secondary text-center"
                >
                  No grants yet for{' '}
                  <span className="font-mono">{selectedMarking.name}</span>.
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table
                    data-testid="grants-table"
                    className="w-full text-sm"
                  >
                    <thead className="text-xs uppercase tracking-widest text-text-secondary border-b border-border/30">
                      <tr>
                        <th className="text-left py-2 px-2 font-semibold">
                          User
                        </th>
                        <th className="text-left py-2 px-2 font-semibold">
                          Granted By
                        </th>
                        <th className="text-left py-2 px-2 font-semibold">
                          Granted At
                        </th>
                        <th className="text-left py-2 px-2 font-semibold">
                          Expires At
                        </th>
                        <th className="py-2 px-2 w-16"></th>
                      </tr>
                    </thead>
                    <tbody>
                      {(grants ?? []).map((g) => (
                        <tr
                          key={`${g.userId}:${g.markingName}`}
                          data-testid={`grant-row-${g.userId}`}
                          className="border-b border-border/10 hover:bg-white/[0.02]"
                        >
                          <td className="py-2 px-2 font-mono text-text-primary">
                            {g.userId}
                          </td>
                          <td className="py-2 px-2 font-mono text-text-secondary">
                            {g.grantedBy || '—'}
                          </td>
                          <td className="py-2 px-2 text-text-secondary">
                            {formatGrantTimestamp(g.grantedAt)}
                          </td>
                          <td
                            className="py-2 px-2 text-text-secondary"
                            data-testid={`grant-expires-${g.userId}`}
                          >
                            {g.expiresAt ? formatGrantTimestamp(g.expiresAt) : '—'}
                          </td>
                          <td className="py-2 px-2 text-right">
                            <button
                              type="button"
                              data-testid={`revoke-button-${g.userId}`}
                              onClick={() => {
                                setRevokeError(null);
                                setPendingRevoke(g);
                              }}
                              className="text-xs px-2 py-1 rounded-md border border-red-500/30 text-red-400 hover:bg-red-500/10"
                            >
                              Revoke
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          ) : (
            <div className="flex items-center justify-center h-full text-text-secondary text-sm">
              Select a marking to manage its grants.
            </div>
          )}
        </section>
      </div>

      {/* Grant modal */}
      <Modal
        open={grantFormOpen}
        onClose={() => setGrantFormOpen(false)}
        title={`Grant ${selectedMarking?.name ?? ''} to user`}
      >
        <form
          data-testid="grant-form"
          onSubmit={handleGrantSubmit}
          className="p-6 space-y-4"
        >
          <label className="block text-sm">
            <span className="block mb-1 text-text-secondary">User ID</span>
            <input
              data-testid="grant-user-input"
              type="text"
              value={grantUserId}
              onChange={(e) => setGrantUserId(e.target.value)}
              placeholder="user:alice@example.com"
              className="w-full rounded-md border border-border/40 bg-bg-secondary px-3 py-2 text-sm font-mono text-text-primary focus:outline-none focus:border-accent-amber/60"
              autoFocus
              required
            />
          </label>
          <label className="block text-sm">
            <span className="block mb-1 text-text-secondary">
              Expires in days (optional — leave blank for permanent)
            </span>
            <input
              data-testid="grant-expires-input"
              type="number"
              min={0}
              value={grantExpiresInDays}
              onChange={(e) => setGrantExpiresInDays(e.target.value)}
              placeholder="30"
              className="w-full rounded-md border border-border/40 bg-bg-secondary px-3 py-2 text-sm font-mono text-text-primary focus:outline-none focus:border-accent-amber/60"
            />
          </label>
          {grantError && (
            <div
              data-testid="grant-error"
              className="text-sm text-red-400"
            >
              {grantError}
            </div>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setGrantFormOpen(false)}
              className="px-3 py-1.5 text-sm rounded-md border border-border/40 text-text-secondary hover:bg-white/5"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={grantMutation.isPending || !grantUserId.trim()}
              className="px-3 py-1.5 text-sm rounded-md bg-accent-amber/20 text-accent-amber border border-accent-amber/40 hover:bg-accent-amber/30 disabled:opacity-50"
            >
              {grantMutation.isPending ? 'Granting…' : 'Grant'}
            </button>
          </div>
        </form>
      </Modal>

      {/* Revoke confirmation dialog */}
      <Modal
        open={!!pendingRevoke}
        onClose={() => setPendingRevoke(null)}
        title="Confirm revoke"
      >
        <div data-testid="revoke-dialog" className="p-6 space-y-4">
          <p className="text-sm text-text-primary">
            Revoke marking{' '}
            <span className="font-mono">
              {pendingRevoke?.markingName}
            </span>{' '}
            from <span className="font-mono">{pendingRevoke?.userId}</span>?
          </p>
          <p className="text-xs text-text-secondary">
            The user will lose access to every object carrying this marking.
          </p>
          {revokeError && (
            <div
              data-testid="revoke-error"
              className="text-sm text-red-400"
            >
              {revokeError}
            </div>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setPendingRevoke(null)}
              className="px-3 py-1.5 text-sm rounded-md border border-border/40 text-text-secondary hover:bg-white/5"
            >
              Cancel
            </button>
            <button
              type="button"
              data-testid="confirm-revoke"
              onClick={handleConfirmRevoke}
              disabled={revokeMutation.isPending}
              className="px-3 py-1.5 text-sm rounded-md bg-red-500/20 text-red-400 border border-red-500/40 hover:bg-red-500/30 disabled:opacity-50"
            >
              {revokeMutation.isPending ? 'Revoking…' : 'Revoke'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
