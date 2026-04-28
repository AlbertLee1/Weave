import { useMemo, useState } from 'react';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import { useObjectActivity } from '../../hooks/useObjects';
import type { ObjectActivityEntry } from '../../api/types';
import { LoadingSpinner } from '../common/LoadingSpinner';

interface ObjectDiffPanelProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
}

function canonicalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === 'object') {
    const sorted: Record<string, unknown> = {};
    for (const k of Object.keys(value as Record<string, unknown>).sort()) {
      sorted[k] = canonicalize((value as Record<string, unknown>)[k]);
    }
    return sorted;
  }
  return value;
}

function formatState(state: ObjectActivityEntry['newState']): string {
  if (state === null || state === undefined) return '';
  return JSON.stringify(canonicalize(state), null, 2);
}

function entryLabel(entry: ObjectActivityEntry): string {
  return `v${entry.version} · ${entry.editType}`;
}

export function ObjectDiffPanel({
  ontologyApiName,
  objectType,
  primaryKey,
}: ObjectDiffPanelProps) {
  const {
    data,
    error,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useObjectActivity({
    ontologyApiName,
    objectType,
    primaryKey,
    pageSize: 25,
  });

  const entries = useMemo<ObjectActivityEntry[]>(
    () => (data ? data.pages.flatMap((p) => p.data ?? []) : []),
    [data],
  );

  const [leftVersion, setLeftVersion] = useState<number | null>(null);
  const [rightVersion, setRightVersion] = useState<number | null>(null);

  const effectiveRight = rightVersion ?? entries[0]?.version ?? null;
  const effectiveLeft = leftVersion ?? entries[1]?.version ?? null;

  if (isLoading) {
    return (
      <div
        className="flex items-center justify-center py-12"
        data-testid="diff-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (error) {
    return (
      <p className="text-xs text-accent-error" data-testid="diff-error">
        Failed to load history: {(error as Error).message}
      </p>
    );
  }

  if (entries.length < 2) {
    return (
      <p
        className="text-xs text-text-secondary py-6 text-center"
        data-testid="diff-insufficient"
      >
        At least two recorded versions are needed to render a diff.
      </p>
    );
  }

  const leftEntry = entries.find((e) => e.version === effectiveLeft) ?? null;
  const rightEntry = entries.find((e) => e.version === effectiveRight) ?? null;
  const leftValue = formatState(leftEntry?.newState);
  const rightValue = formatState(rightEntry?.newState);
  const sameVersion =
    effectiveLeft !== null && effectiveLeft === effectiveRight;

  return (
    <div className="space-y-3" data-testid="diff-panel">
      <div className="grid grid-cols-2 gap-3">
        <label className="text-[11px] font-mono text-text-secondary">
          From version
          <select
            value={String(effectiveLeft ?? '')}
            onChange={(e) => setLeftVersion(Number(e.target.value))}
            className="mt-1 w-full bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1"
            data-testid="diff-left-select"
          >
            {entries.map((entry) => (
              <option key={entry.version} value={entry.version}>
                {entryLabel(entry)}
              </option>
            ))}
          </select>
        </label>
        <label className="text-[11px] font-mono text-text-secondary">
          To version
          <select
            value={String(effectiveRight ?? '')}
            onChange={(e) => setRightVersion(Number(e.target.value))}
            className="mt-1 w-full bg-bg-elevated border border-border rounded text-xs font-mono px-2 py-1"
            data-testid="diff-right-select"
          >
            {entries.map((entry) => (
              <option key={entry.version} value={entry.version}>
                {entryLabel(entry)}
              </option>
            ))}
          </select>
        </label>
      </div>

      {hasNextPage && (
        <div className="flex justify-center">
          <button
            type="button"
            onClick={() => fetchNextPage()}
            disabled={isFetchingNextPage}
            className="px-3 py-1.5 text-xs font-mono rounded bg-accent-cyan/15 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/25 disabled:opacity-50"
            data-testid="diff-load-more"
          >
            {isFetchingNextPage ? 'Loading…' : 'Load older versions'}
          </button>
        </div>
      )}

      {sameVersion ? (
        <p
          className="text-xs text-text-secondary py-4 text-center"
          data-testid="diff-same-version"
        >
          Pick two distinct versions to compare.
        </p>
      ) : (
        <div
          className="border border-border rounded overflow-x-auto"
          data-testid="diff-viewer"
        >
          <ReactDiffViewer
            oldValue={leftValue}
            newValue={rightValue}
            splitView
            useDarkTheme
            disableWorker
            compareMethod={DiffMethod.LINES}
            leftTitle={leftEntry ? entryLabel(leftEntry) : 'older'}
            rightTitle={rightEntry ? entryLabel(rightEntry) : 'newer'}
          />
        </div>
      )}
    </div>
  );
}
