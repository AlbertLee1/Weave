import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import type { ObjectType } from '../../api/types';
import { rebuildIndex, type IndexRebuildResponse } from '../../api/adminIndex';
import { describeApiError } from '../../api/describeError';
import { useOntologies } from '../../hooks/useOntologies';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { useToastStore } from '../../stores/toastStore';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';

// IndexManagementAdminPage surfaces the heavyweight Bleve index rebuild
// operations exposed by cmd/server/admin_index.go (POST
// /api/admin/indexes/rebuild). Operators pick an ontology, then an ObjectType
// whose full-text index they want to re-index from the latest document
// source. Because a rebuild fully re-indexes the scope, the action is gated
// behind an explicit confirmation modal before any request is sent.
export function IndexManagementAdminPage() {
  const {
    data: ontologies,
    isLoading: ontologiesLoading,
    error: ontologiesError,
  } = useOntologies();

  const [ontology, setOntology] = useState('');

  const {
    data: objectTypes,
    isLoading: objectTypesLoading,
    error: objectTypesError,
  } = useObjectTypes(ontology);

  const [confirming, setConfirming] = useState<ObjectType | null>(null);

  return (
    <div
      data-testid="index-management-page"
      className="flex flex-col h-[calc(100vh-3rem)] bg-bg-primary overflow-y-auto"
    >
      <header
        className="px-6 py-4 border-b flex flex-wrap items-center gap-4"
        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      >
        <h1 className="text-base font-semibold tracking-wide text-text-primary">
          Index Management
        </h1>
        <span className="text-xs text-text-secondary uppercase tracking-widest">
          Bleve Rebuild
        </span>
      </header>

      <div className="flex-1 px-6 py-5 flex flex-col gap-6 max-w-3xl">
        <p className="text-xs text-text-secondary leading-relaxed">
          Rebuilding re-indexes an ObjectType's full-text Bleve index from the
          latest document source. This is a heavyweight operation — it scans and
          re-writes every document in the scope — so it must be confirmed before
          it runs. Use it after a disaster-recovery wipe or when search results
          drift out of sync with the underlying objects.
        </p>

        {/* Ontology selector */}
        <label className="flex flex-col gap-1 text-xs text-text-secondary max-w-sm">
          <span className="uppercase tracking-widest">Ontology</span>
          {ontologiesLoading ? (
            <div
              data-testid="index-ontology-loading"
              className="flex items-center gap-2 py-2"
            >
              <LoadingSpinner size="sm" />
              <span className="text-xs text-text-muted">Loading ontologies…</span>
            </div>
          ) : ontologiesError ? (
            <p
              data-testid="index-ontology-error"
              role="alert"
              className="text-xs text-accent-error"
            >
              Failed to load ontologies:{' '}
              {(ontologiesError as Error).message}
            </p>
          ) : !ontologies || ontologies.length === 0 ? (
            <p
              data-testid="index-ontology-empty"
              className="text-xs text-text-muted italic"
            >
              No ontologies are available.
            </p>
          ) : (
            <select
              data-testid="index-ontology-select"
              aria-label="Select ontology"
              value={ontology}
              onChange={(e) => setOntology(e.target.value)}
              className={inputClass}
            >
              <option value="">— Select an ontology —</option>
              {ontologies.map((o) => (
                <option key={o.rid} value={o.apiName}>
                  {o.displayName}
                </option>
              ))}
            </select>
          )}
        </label>

        {/* ObjectType list */}
        {ontology && (
          <section className="flex flex-col gap-3">
            <h2 className="text-[10px] uppercase tracking-widest text-text-secondary font-medium">
              Object Types — {ontology}
            </h2>

            {objectTypesLoading && (
              <div
                data-testid="index-objecttype-loading"
                className="flex items-center justify-center py-10"
              >
                <LoadingSpinner size="md" />
              </div>
            )}
            {!objectTypesLoading && objectTypesError && (
              <p
                data-testid="index-objecttype-error"
                role="alert"
                className="text-xs text-accent-error"
              >
                Failed to load object types:{' '}
                {(objectTypesError as Error).message}
              </p>
            )}
            {!objectTypesLoading &&
              !objectTypesError &&
              (!objectTypes || objectTypes.length === 0) && (
                <div
                  data-testid="index-objecttype-empty"
                  className="rounded border px-6 py-10 text-center"
                  style={{
                    borderColor: 'rgba(31,41,55,0.5)',
                    background: 'rgba(13,17,23,0.4)',
                  }}
                >
                  <p className="text-sm text-text-primary font-semibold">
                    No object types
                  </p>
                  <p className="text-xs text-text-secondary mt-2">
                    This ontology has no ObjectTypes to re-index.
                  </p>
                </div>
              )}
            {!objectTypesLoading &&
              !objectTypesError &&
              objectTypes &&
              objectTypes.length > 0 && (
                <div
                  data-testid="index-objecttype-table"
                  className="rounded border overflow-hidden"
                  style={{
                    borderColor: 'rgba(31,41,55,0.5)',
                    background: 'rgba(13,17,23,0.4)',
                  }}
                >
                  <table className="w-full text-sm">
                    <thead className="text-[10px] uppercase tracking-widest text-text-secondary">
                      <tr
                        className="border-b"
                        style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                      >
                        <th className="text-left px-4 py-2 font-medium">
                          Display Name
                        </th>
                        <th className="text-left px-4 py-2 font-medium">
                          API Name
                        </th>
                        <th className="text-right px-4 py-2 font-medium">
                          Actions
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {objectTypes.map((ot) => (
                        <tr
                          key={ot.rid}
                          data-testid={`index-objecttype-row-${ot.apiName}`}
                          data-object-type-api-name={ot.apiName}
                          className="border-b last:border-0 hover:bg-bg-tertiary/30"
                          style={{ borderColor: 'rgba(31,41,55,0.5)' }}
                        >
                          <td className="px-4 py-2 text-text-primary">
                            {ot.displayName}
                          </td>
                          <td className="px-4 py-2 font-mono text-xs text-text-secondary">
                            {ot.apiName}
                          </td>
                          <td className="px-4 py-2 text-right whitespace-nowrap">
                            <button
                              type="button"
                              data-testid="index-rebuild-btn"
                              data-object-type-api-name={ot.apiName}
                              onClick={() => setConfirming(ot)}
                              className="px-3 py-1 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 transition-colors"
                            >
                              Rebuild index
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
          </section>
        )}
      </div>

      {confirming && (
        <RebuildConfirmModal
          ontology={ontology}
          objectType={confirming}
          onClose={() => setConfirming(null)}
        />
      )}
    </div>
  );
}

function RebuildConfirmModal({
  ontology,
  objectType,
  onClose,
}: {
  ontology: string;
  objectType: ObjectType;
  onClose: () => void;
}) {
  const pushToast = useToastStore((s) => s.push);

  const rebuild = useMutation({
    mutationFn: (): Promise<IndexRebuildResponse> =>
      rebuildIndex({ ontology, objectType: objectType.apiName }),
    onSuccess: (res) => {
      pushToast({
        message: `Rebuilt "${objectType.apiName}" — ${res.indexedCount} document(s) indexed (${res.scopedKey}).`,
        severity: 'success',
      });
      onClose();
    },
    onError: (err) => {
      pushToast({
        message: describeApiError(
          err,
          `Failed to rebuild index for "${objectType.apiName}".`,
        ),
        severity: 'error',
      });
    },
  });

  return (
    <Modal open onClose={onClose} title="Rebuild index">
      <div
        data-testid="index-rebuild-confirm-modal"
        data-object-type-api-name={objectType.apiName}
        className="flex flex-col gap-3"
      >
        <p className="text-sm text-text-primary">
          Rebuild the Bleve index for{' '}
          <span className="font-semibold">{objectType.displayName}</span>{' '}
          <span className="text-xs text-text-secondary font-mono">
            ({ontology}.{objectType.apiName})
          </span>
          ?
        </p>
        <p
          className="text-xs rounded px-3 py-2 border"
          style={{
            borderColor: 'rgba(245,158,11,0.4)',
            background: 'rgba(245,158,11,0.08)',
            color: 'var(--color-text-secondary, #9ca3af)',
          }}
        >
          This is a heavyweight operation. It re-indexes <strong>every</strong>{' '}
          document in this scope from the latest source and can take a while on
          large ObjectTypes. Search may return partial results until it
          completes.
        </p>
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            data-testid="index-rebuild-cancel"
            onClick={onClose}
            disabled={rebuild.isPending}
            className="px-3 py-1.5 text-xs rounded text-text-secondary hover:text-text-primary disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="index-rebuild-confirm"
            onClick={() => rebuild.mutate()}
            disabled={rebuild.isPending}
            className="px-3 py-1.5 text-xs font-semibold rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/30 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {rebuild.isPending ? 'Rebuilding…' : 'Rebuild index'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

const inputClass =
  'w-full px-3 py-1.5 text-sm rounded bg-bg-tertiary text-text-primary outline-none border border-transparent focus:border-accent-cyan/40 disabled:opacity-60';
