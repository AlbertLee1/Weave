import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import { useSavedObjectSets } from '../../hooks/useObjectSets';
import {
  createObjectSetSnapshot,
  createTemporaryObjectSet,
} from '../../api/objectsets';
import {
  OBJECT_SET_URL_PARAM,
  encodeDefinitionToParam,
} from '../../lib/objectSetUrl';
import {
  readLocalSnapshots,
  writeLocalSnapshots,
  type LocalSnapshotEntry,
} from '../../lib/objectSetSnapshots';
import { EmptyState } from '../common/EmptyState';

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

export function ObjectSetSnapshotsPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { items: saved } = useSavedObjectSets(ontologyApiName);

  const [snapshots, setSnapshots] = useState<LocalSnapshotEntry[]>(() =>
    readLocalSnapshots(ontologyApiName),
  );

  useEffect(() => {
    setSnapshots(readLocalSnapshots(ontologyApiName));
  }, [ontologyApiName]);

  const persistSnapshots = useCallback(
    (next: LocalSnapshotEntry[]) => {
      setSnapshots(next);
      writeLocalSnapshots(ontologyApiName, next);
    },
    [ontologyApiName],
  );

  const [selectedSavedId, setSelectedSavedId] = useState<string>('');
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sorted = useMemo(
    () =>
      snapshots
        .slice()
        .sort((a, b) => (b.createdAt ?? '').localeCompare(a.createdAt ?? '')),
    [snapshots],
  );

  const handleCreateSnapshot = useCallback(async () => {
    if (!selectedSavedId) return;
    const target = saved.find((s) => s.id === selectedSavedId);
    if (!target) return;
    setCreating(true);
    setError(null);
    try {
      const temp = await createTemporaryObjectSet(ontologyApiName, target.def);
      const snap = await createObjectSetSnapshot(
        ontologyApiName,
        temp.objectSetRid,
      );
      const entry: LocalSnapshotEntry = {
        snapshotRid: snap.snapshotRid,
        ontologyApiName,
        objectType: snap.objectType,
        savedSetId: target.id,
        savedSetName: target.name,
        def: target.def,
        createdAt: snap.createdAt,
        totalCount: parseInt(snap.totalCount, 10) || 0,
        definitionHash: snap.definitionHash,
        snapshotAt: snap.snapshotAt,
        truncated: snap.truncated,
      };
      persistSnapshots([entry, ...snapshots]);
      setSelectedSavedId('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create snapshot');
    } finally {
      setCreating(false);
    }
  }, [
    selectedSavedId,
    saved,
    ontologyApiName,
    snapshots,
    persistSnapshots,
  ]);

  const handleDeleteSnapshot = useCallback(
    (snapshotRid: string) => {
      persistSnapshots(
        snapshots.filter((s) => s.snapshotRid !== snapshotRid),
      );
    },
    [snapshots, persistSnapshots],
  );

  const restoreHref = useCallback(
    (entry: LocalSnapshotEntry): string => {
      const param = encodeDefinitionToParam(entry.def);
      return `/objectsets/${ontologyApiName}?${OBJECT_SET_URL_PARAM}=${param}`;
    },
    [ontologyApiName],
  );

  if (!ontologyApiName) {
    return (
      <div
        data-testid="objectset-snapshots-no-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No ontology selected"
          description="Pick an ontology to manage ObjectSet snapshots."
        />
      </div>
    );
  }

  return (
    <div
      data-testid="objectset-snapshots-page"
      className="flex flex-col h-full overflow-hidden"
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-primary">
        <div>
          <h1 className="text-base font-sans font-semibold text-text-primary">
            Object Set Snapshots
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            to={`/objectsets/${ontologyApiName}`}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Composer
          </Link>
          <Link
            to={`/objectsets/${ontologyApiName}/diff`}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Diff
          </Link>
        </div>
      </div>

      <div className="px-4 py-3 border-b border-border flex flex-col gap-3 lg:flex-row lg:items-end">
        <div className="flex-1 flex flex-col gap-1">
          <label
            htmlFor="snapshot-saved-pick"
            className="text-xs font-sans text-text-secondary uppercase tracking-wider"
          >
            Saved Object Set
          </label>
          <select
            id="snapshot-saved-pick"
            aria-label="Saved Object Set"
            data-testid="objectset-snapshots-saved-pick"
            value={selectedSavedId}
            onChange={(e) => setSelectedSavedId(e.target.value)}
            className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none"
          >
            <option value="">-- pick a saved set --</option>
            {saved.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          data-testid="objectset-snapshots-create-btn"
          onClick={handleCreateSnapshot}
          disabled={!selectedSavedId || creating}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed self-start lg:self-end"
        >
          {creating ? 'Creating...' : 'Create snapshot'}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {error && (
          <div
            data-testid="objectset-snapshots-error"
            className="mb-3 px-4 py-3 border border-accent-error/30 bg-accent-error/5 rounded text-xs font-mono text-accent-error"
          >
            {error}
          </div>
        )}

        {sorted.length === 0 ? (
          <EmptyState
            title="No snapshots"
            description="Pick a saved Object Set above and click Create snapshot to freeze its membership."
          />
        ) : (
          <ul
            className="flex flex-col gap-2"
            data-testid="objectset-snapshots-list"
          >
            {sorted.map((entry) => (
              <li
                key={entry.snapshotRid}
                className="border border-border rounded bg-bg-tertiary"
                data-testid={`objectset-snapshot-row-${entry.snapshotRid}`}
              >
                <div className="flex items-center gap-3 px-3 py-2">
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-sans text-text-primary truncate">
                      {entry.savedSetName ?? '(snapshot)'}
                    </div>
                    <div
                      data-testid="objectset-snapshot-rid"
                      className="text-xs font-mono text-text-secondary truncate"
                      title={entry.snapshotRid}
                    >
                      {entry.snapshotRid}
                    </div>
                    <div className="text-xs font-mono text-text-secondary mt-0.5">
                      {entry.objectType} · {entry.totalCount} rows ·{' '}
                      {formatTimestamp(entry.createdAt)}
                      {entry.truncated ? ' · truncated' : ''}
                    </div>
                  </div>
                  <Link
                    to={restoreHref(entry)}
                    data-testid={`objectset-snapshot-restore-${entry.snapshotRid}`}
                    aria-label={`restore ${entry.snapshotRid}`}
                    className="text-xs font-mono text-accent-cyan hover:underline"
                  >
                    Restore
                  </Link>
                  <button
                    type="button"
                    data-testid={`objectset-snapshot-forget-${entry.snapshotRid}`}
                    aria-label={`forget ${entry.snapshotRid}`}
                    onClick={() => handleDeleteSnapshot(entry.snapshotRid)}
                    className="text-xs font-mono text-accent-error hover:text-accent-error/70"
                  >
                    Forget
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
