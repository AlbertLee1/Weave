import { useCallback, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import { useSavedObjectSets } from '../../hooks/useObjectSets';
import {
  OBJECT_SET_URL_PARAM,
  encodeDefinitionToParam,
} from '../../lib/objectSetUrl';
import type { SavedObjectSet } from '../../lib/objectSetBuilder';
import { Modal } from '../common/Modal';
import { EmptyState } from '../common/EmptyState';

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

function previewType(saved: SavedObjectSet): string {
  return saved.def.type;
}

export function SavedObjectSetsPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const {
    items,
    remove,
    setActiveVersion,
    removeVersion,
  } = useSavedObjectSets(ontologyApiName);

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{
    savedId: string;
    versionId?: string;
  } | null>(null);

  const sorted = useMemo(
    () =>
      items
        .slice()
        .sort((a, b) =>
          (b.createdAt ?? '').localeCompare(a.createdAt ?? ''),
        ),
    [items],
  );

  const openInComposer = useCallback(
    (saved: SavedObjectSet): string => {
      const param = encodeDefinitionToParam(saved.def);
      return `/objectsets/${ontologyApiName}?${OBJECT_SET_URL_PARAM}=${param}`;
    },
    [ontologyApiName],
  );

  const handleConfirmDelete = useCallback(() => {
    if (!confirmDelete) return;
    if (confirmDelete.versionId) {
      removeVersion(confirmDelete.savedId, confirmDelete.versionId);
    } else {
      remove(confirmDelete.savedId);
    }
    setConfirmDelete(null);
  }, [confirmDelete, remove, removeVersion]);

  if (!ontologyApiName) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="No ontology selected"
          description="Pick an ontology from the dashboard to manage saved Object Sets."
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-primary">
        <div>
          <h1 className="text-base font-sans font-semibold text-text-primary">
            Saved Object Sets
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </p>
        </div>
        <Link
          to={`/objectsets/${ontologyApiName}`}
          className="bg-accent-cyan text-bg-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-accent-cyan/80"
        >
          New Object Set
        </Link>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {sorted.length === 0 ? (
          <EmptyState
            title="No saved Object Sets"
            description="Open the composer and click Save As to keep a query here."
          />
        ) : (
          <ul className="flex flex-col gap-2" data-testid="saved-list">
            {sorted.map((s) => {
              const isExpanded = expandedId === s.id;
              const activeVersion =
                s.versions.find((v) => v.versionId === s.activeVersionId) ??
                s.versions[0];
              return (
                <li
                  key={s.id}
                  className="border border-border rounded bg-bg-tertiary"
                  data-testid={`saved-row-${s.id}`}
                >
                  <div className="flex items-center gap-3 px-3 py-2">
                    <button
                      type="button"
                      onClick={() => setExpandedId(isExpanded ? null : s.id)}
                      aria-label={isExpanded ? 'Collapse' : 'Expand'}
                      className="text-xs font-mono text-text-secondary hover:text-text-primary w-4"
                    >
                      {isExpanded ? '▼' : '▶'}
                    </button>
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-sans text-text-primary truncate">
                        {s.name}
                      </div>
                      <div className="text-xs font-mono text-text-secondary truncate">
                        {previewType(s)} ·{' '}
                        {s.versions.length} version
                        {s.versions.length === 1 ? '' : 's'} · active{' '}
                        {activeVersion
                          ? formatTimestamp(activeVersion.createdAt)
                          : '—'}
                      </div>
                    </div>
                    <Link
                      to={openInComposer(s)}
                      className="text-xs font-mono text-accent-cyan hover:underline"
                      aria-label={`open ${s.name}`}
                    >
                      Open
                    </Link>
                    <button
                      type="button"
                      onClick={() =>
                        setConfirmDelete({ savedId: s.id })
                      }
                      className="text-xs font-mono text-accent-error hover:text-accent-error/70"
                      aria-label={`delete ${s.name}`}
                    >
                      Delete
                    </button>
                  </div>

                  {isExpanded && (
                    <div className="px-3 pb-3 border-t border-border">
                      <div className="text-xs font-sans text-text-secondary uppercase tracking-wider mt-2 mb-1">
                        Versions
                      </div>
                      <ul
                        className="flex flex-col gap-1"
                        data-testid={`versions-${s.id}`}
                      >
                        {s.versions.map((v) => {
                          const isActive = v.versionId === s.activeVersionId;
                          return (
                            <li
                              key={v.versionId}
                              className={`flex items-center gap-2 px-2 py-1 rounded border ${
                                isActive
                                  ? 'border-accent-cyan bg-accent-cyan/10'
                                  : 'border-border bg-bg-secondary'
                              }`}
                              data-testid={`version-${v.versionId}`}
                            >
                              <span
                                className="text-xs font-mono text-text-secondary w-32 truncate"
                                title={v.createdAt}
                              >
                                {formatTimestamp(v.createdAt)}
                              </span>
                              {v.note ? (
                                <span className="text-xs font-sans text-text-primary truncate flex-1">
                                  {v.note}
                                </span>
                              ) : (
                                <span className="text-xs font-mono text-text-muted truncate flex-1">
                                  (no note)
                                </span>
                              )}
                              {isActive ? (
                                <span className="text-xs font-mono text-accent-cyan">
                                  active
                                </span>
                              ) : (
                                <button
                                  type="button"
                                  onClick={() =>
                                    setActiveVersion(s.id, v.versionId)
                                  }
                                  className="text-xs font-mono text-text-primary hover:text-accent-cyan"
                                  aria-label={`switch to version ${v.versionId}`}
                                >
                                  Switch
                                </button>
                              )}
                              <button
                                type="button"
                                disabled={s.versions.length <= 1}
                                onClick={() =>
                                  setConfirmDelete({
                                    savedId: s.id,
                                    versionId: v.versionId,
                                  })
                                }
                                className="text-xs font-mono text-accent-error hover:text-accent-error/70 disabled:opacity-30 disabled:cursor-not-allowed"
                                aria-label={`delete version ${v.versionId}`}
                                title={
                                  s.versions.length <= 1
                                    ? 'Cannot delete the last version; delete the whole saved set instead'
                                    : 'Delete this version'
                                }
                              >
                                ×
                              </button>
                            </li>
                          );
                        })}
                      </ul>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <Modal
        open={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        title={
          confirmDelete?.versionId
            ? 'Delete version'
            : 'Delete saved Object Set'
        }
      >
        <div className="flex flex-col gap-3">
          <p className="text-sm font-sans text-text-primary">
            {confirmDelete?.versionId
              ? 'This version will be permanently removed. The saved Object Set keeps its other versions.'
              : 'This saved Object Set and all its versions will be permanently removed.'}
          </p>
          <div className="flex justify-end gap-2 mt-2">
            <button
              type="button"
              onClick={() => setConfirmDelete(null)}
              className="px-3 py-1.5 bg-bg-tertiary border border-border rounded text-xs font-mono text-text-secondary hover:text-text-primary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleConfirmDelete}
              className="px-3 py-1.5 bg-accent-error text-bg-primary rounded text-xs font-mono font-medium"
            >
              Delete
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
