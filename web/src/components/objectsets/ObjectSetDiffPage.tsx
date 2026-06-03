import { useCallback, useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type { ObjectSetDefinition, WireObject } from '../../api/types';
import type { SavedObjectSet } from '../../lib/objectSetBuilder';
import { useSavedObjectSets } from '../../hooks/useObjectSets';
import { loadObjectSet } from '../../api/objectsets';
import { getObjectType } from '../../api/ontologies';
import { useObjectType, useObjectTypes } from '../../hooks/useObjectTypes';
import { diffObjectSets, type ObjectSetDiff } from '../../lib/objectSetDiff';
import {
  downloadObjectSetDiffCsv,
  objectSetDiffCsvFilename,
} from '../../lib/objectSetDiffCsv';
import { EmptyState } from '../common/EmptyState';
import { LoadingSpinner } from '../common/LoadingSpinner';

const DIFF_PAGE_SIZE = 500;

function staticRootType(def: ObjectSetDefinition | undefined): string {
  if (!def) return '';
  switch (def.type) {
    case 'base':
    case 'static':
    case 'asType':
      return def.objectType;
    case 'filter':
    case 'withProperties':
    case 'nearestNeighbors':
    case 'sample':
    case 'asBaseObjectTypes':
      return staticRootType(def.objectSet);
    case 'union':
    case 'intersect':
    case 'subtract':
      return def.objectSets.length > 0 ? staticRootType(def.objectSets[0]) : '';
    default:
      return '';
  }
}

export function ObjectSetDiffPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { isLoading: typesLoading } = useObjectTypes(ontologyApiName);

  const { items: saved } = useSavedObjectSets(ontologyApiName);

  const [savedAId, setSavedAId] = useState<string>('');
  const [savedBId, setSavedBId] = useState<string>('');

  const savedA = useMemo(
    () => saved.find((s) => s.id === savedAId),
    [saved, savedAId],
  );
  const savedB = useMemo(
    () => saved.find((s) => s.id === savedBId),
    [saved, savedBId],
  );

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [diff, setDiff] = useState<ObjectSetDiff | null>(null);
  const [resolvedTypeName, setResolvedTypeName] = useState<string>('');

  const handleCompute = useCallback(async () => {
    if (!savedA || !savedB) return;
    setLoading(true);
    setError(null);
    setDiff(null);
    try {
      const aType = staticRootType(savedA.def);
      const bType = staticRootType(savedB.def);
      const rootType = aType || bType;
      if (!rootType) {
        throw new Error(
          'Cannot statically resolve the root object type for diff (e.g. searchAround changes type). Pick a base / filter / withProperties saved set.',
        );
      }
      const objectType = await getObjectType(ontologyApiName, rootType);
      const select = Object.keys(objectType.properties ?? {});
      if (select.length === 0) {
        throw new Error(
          `Object type "${rootType}" has no declared properties to compare.`,
        );
      }
      const [respA, respB] = await Promise.all([
        loadObjectSet(ontologyApiName, {
          objectSet: savedA.def,
          select,
          pageSize: DIFF_PAGE_SIZE,
        }),
        loadObjectSet(ontologyApiName, {
          objectSet: savedB.def,
          select,
          pageSize: DIFF_PAGE_SIZE,
        }),
      ]);
      const result = diffObjectSets(respA.data, respB.data);
      setDiff(result);
      const sample = respA.data[0] ?? respB.data[0] ?? null;
      setResolvedTypeName(
        (sample?.__apiName as string | undefined) ?? rootType,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to compute diff');
    } finally {
      setLoading(false);
    }
  }, [savedA, savedB, ontologyApiName]);

  if (!ontologyApiName) {
    return (
      <div
        data-testid="objectset-diff-no-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No ontology selected"
          description="Pick an ontology to compare ObjectSets."
        />
      </div>
    );
  }

  return (
    <div
      data-testid="objectset-diff-page"
      className="flex flex-col h-full overflow-hidden"
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-primary">
        <div>
          <h1 className="text-base font-sans font-semibold text-text-primary">
            Object Set Diff
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </p>
        </div>
      </div>

      <div className="px-4 py-3 border-b border-border flex flex-col gap-3 lg:flex-row lg:items-end">
        <div className="flex-1 flex flex-col gap-1">
          <label
            htmlFor="diff-saved-a"
            className="text-xs font-sans text-text-secondary uppercase tracking-wider"
          >
            Object Set A
          </label>
          <select
            id="diff-saved-a"
            aria-label="Object Set A"
            value={savedAId}
            onChange={(e) => setSavedAId(e.target.value)}
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
        <div className="flex-1 flex flex-col gap-1">
          <label
            htmlFor="diff-saved-b"
            className="text-xs font-sans text-text-secondary uppercase tracking-wider"
          >
            Object Set B
          </label>
          <select
            id="diff-saved-b"
            aria-label="Object Set B"
            value={savedBId}
            onChange={(e) => setSavedBId(e.target.value)}
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
          data-testid="objectset-diff-compute-btn"
          onClick={handleCompute}
          disabled={!savedA || !savedB || loading}
          className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed self-start lg:self-end"
        >
          {loading ? 'Computing...' : 'Compute Diff'}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {!saved.length && !typesLoading && (
          <div data-testid="objectset-diff-no-saved-sets">
            <EmptyState
              title="No saved object sets"
              description="Save at least two object sets in the Composer first."
            />
          </div>
        )}
        {saved.length > 0 && !diff && !loading && !error && (
          <div data-testid="objectset-diff-pending">
            <EmptyState
              title="Pick two saved object sets"
              description="Then click Compute Diff to compare them."
            />
          </div>
        )}
        {loading && (
          <div
            data-testid="objectset-diff-loading"
            className="flex items-center justify-center py-12"
          >
            <LoadingSpinner />
          </div>
        )}
        {error && (
          <div
            data-testid="objectset-diff-error"
            className="px-4 py-3 border border-accent-error/30 bg-accent-error/5 rounded text-xs font-mono text-accent-error"
          >
            {error}
          </div>
        )}
        {diff && (
          <div data-testid="objectset-diff-results">
            <DiffResults
              ontologyApiName={ontologyApiName}
              resolvedTypeName={resolvedTypeName}
              diff={diff}
              savedA={savedA}
              savedB={savedB}
            />
          </div>
        )}
      </div>
    </div>
  );
}

interface DiffResultsProps {
  ontologyApiName: string;
  resolvedTypeName: string;
  diff: ObjectSetDiff;
  savedA?: SavedObjectSet;
  savedB?: SavedObjectSet;
}

function DiffResults({
  ontologyApiName,
  resolvedTypeName,
  diff,
  savedA,
  savedB,
}: DiffResultsProps) {
  const { data: objectType } = useObjectType(ontologyApiName, resolvedTypeName);
  const propertyOrder = useMemo(() => {
    if (!objectType?.properties) return [];
    return Object.keys(objectType.properties);
  }, [objectType]);

  const handleExportCsv = useCallback(() => {
    downloadObjectSetDiffCsv(
      diff,
      propertyOrder,
      objectSetDiffCsvFilename(ontologyApiName, savedA?.name, savedB?.name),
    );
  }, [diff, ontologyApiName, propertyOrder, savedA?.name, savedB?.name]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-end">
        <button
          type="button"
          onClick={handleExportCsv}
          data-testid="objectset-diff-export-csv"
          className="rounded border border-accent-cyan/40 bg-accent-cyan/10 px-3 py-1.5 text-xs font-medium text-accent-cyan hover:bg-accent-cyan/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-cyan/50"
        >
          Export CSV
        </button>
      </div>
      <div
        data-testid="objectset-diff-three-column"
        className="grid grid-cols-1 lg:grid-cols-3 gap-3"
      >
        <DiffSection
          title="Only in A"
          nameLabel={savedA?.name}
          rows={diff.onlyInA}
          propertyOrder={propertyOrder}
          accentClass="text-accent-cyan"
          testId="diff-only-in-a"
        />
        <ChangedSection
          rows={diff.changed}
          savedAName={savedA?.name}
          savedBName={savedB?.name}
        />
        <DiffSection
          title="Only in B"
          nameLabel={savedB?.name}
          rows={diff.onlyInB}
          propertyOrder={propertyOrder}
          accentClass="text-accent-amber"
          testId="diff-only-in-b"
        />
      </div>
    </div>
  );
}

interface DiffSectionProps {
  title: string;
  nameLabel?: string;
  rows: WireObject[];
  propertyOrder: string[];
  accentClass: string;
  testId: string;
}

function DiffSection({
  title,
  nameLabel,
  rows,
  propertyOrder,
  accentClass,
  testId,
}: DiffSectionProps) {
  return (
    <section
      data-testid={testId}
      className="border border-border rounded bg-bg-tertiary"
    >
      <header className="flex items-center justify-between px-3 py-2 border-b border-border">
        <h2 className={`text-xs font-mono font-semibold ${accentClass}`}>
          {title}
          {nameLabel ? (
            <span className="text-text-secondary"> ({nameLabel})</span>
          ) : null}
        </h2>
        <span className="text-xs font-mono text-text-secondary">
          {rows.length}
        </span>
      </header>
      {rows.length === 0 ? (
        <div className="px-3 py-3 text-xs font-mono text-text-muted">
          (none)
        </div>
      ) : (
        <DiffTable rows={rows} propertyOrder={propertyOrder} />
      )}
    </section>
  );
}

function DiffTable({
  rows,
  propertyOrder,
}: {
  rows: WireObject[];
  propertyOrder: string[];
}) {
  const headerKeys = useMemo(() => {
    if (propertyOrder.length > 0) return propertyOrder;
    const seen = new Set<string>();
    for (const r of rows) {
      for (const k of Object.keys(r)) {
        if (k.startsWith('__')) continue;
        if (!seen.has(k)) seen.add(k);
      }
    }
    return Array.from(seen);
  }, [rows, propertyOrder]);

  return (
    <div className="overflow-x-auto">
      <table className="min-w-full text-xs font-mono">
        <thead>
          <tr className="border-b border-border bg-bg-secondary/50">
            <th className="px-3 py-1.5 text-left text-text-secondary">
              __primaryKey
            </th>
            {headerKeys.map((k) => (
              <th
                key={k}
                className="px-3 py-1.5 text-left text-text-secondary"
              >
                {k}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={String(row.__primaryKey)} className="border-b border-border/40">
              <td className="px-3 py-1.5 text-text-primary">
                {String(row.__primaryKey)}
              </td>
              {headerKeys.map((k) => (
                <td key={k} className="px-3 py-1.5 text-text-primary">
                  {renderValue(row[k])}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ChangedSection({
  rows,
  savedAName,
  savedBName,
}: {
  rows: ObjectSetDiff['changed'];
  savedAName?: string;
  savedBName?: string;
}) {
  return (
    <section
      data-testid="diff-changed"
      className="border border-border rounded bg-bg-tertiary"
    >
      <header className="flex items-center justify-between px-3 py-2 border-b border-border">
        <h2 className="text-xs font-mono font-semibold text-accent-magenta">
          Changed
        </h2>
        <span className="text-xs font-mono text-text-secondary">
          {rows.length}
        </span>
      </header>
      {rows.length === 0 ? (
        <div className="px-3 py-3 text-xs font-mono text-text-muted">
          (none)
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="min-w-full text-xs font-mono">
            <thead>
              <tr className="border-b border-border bg-bg-secondary/50">
                <th className="px-3 py-1.5 text-left text-text-secondary">
                  __primaryKey
                </th>
                <th className="px-3 py-1.5 text-left text-text-secondary">
                  field
                </th>
                <th className="px-3 py-1.5 text-left text-text-secondary">
                  A {savedAName ? `(${savedAName})` : ''}
                </th>
                <th className="px-3 py-1.5 text-left text-text-secondary">
                  B {savedBName ? `(${savedBName})` : ''}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.flatMap((row) =>
                row.fieldChanges.map((fc, idx) => (
                  <tr
                    key={`${row.primaryKey}::${fc.field}`}
                    className="border-b border-border/40"
                  >
                    {idx === 0 ? (
                      <td
                        className="px-3 py-1.5 text-text-primary align-top"
                        rowSpan={row.fieldChanges.length}
                      >
                        {row.primaryKey}
                      </td>
                    ) : null}
                    <td className="px-3 py-1.5 text-text-primary">
                      {fc.field}
                    </td>
                    <td className="px-3 py-1.5 text-accent-cyan">
                      {renderValue(fc.valueA)}
                    </td>
                    <td className="px-3 py-1.5 text-accent-amber">
                      {renderValue(fc.valueB)}
                    </td>
                  </tr>
                )),
              )}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function renderValue(v: unknown): string {
  if (v === undefined) return '';
  if (v === null) return 'null';
  if (typeof v === 'object') {
    try {
      return JSON.stringify(v);
    } catch {
      return String(v);
    }
  }
  return String(v);
}
