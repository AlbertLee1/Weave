import { useState } from 'react';
import { useParams, Link } from 'react-router';
import { useBranchDiff } from '../../hooks/useBranches';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import type { BranchDiffEntry } from '../../api/types';

type EntityFilter = 'all' | string;

const changeColors: Record<string, { bg: string; text: string; label: string }> = {
  ADDED: { bg: 'bg-green-900/30', text: 'text-green-400', label: 'Added' },
  MODIFIED: { bg: 'bg-yellow-900/30', text: 'text-yellow-400', label: 'Modified' },
  DELETED: { bg: 'bg-red-900/30', text: 'text-red-400', label: 'Deleted' },
};

function ChangeBadge({ changeType }: { changeType: string }) {
  const style = changeColors[changeType] ?? { bg: 'bg-bg-tertiary', text: 'text-text-secondary', label: changeType };
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${style.bg} ${style.text}`}>
      {style.label}
    </span>
  );
}

function EntityTypeBadge({ entityType }: { entityType: string }) {
  return (
    <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-mono bg-bg-tertiary text-text-secondary border border-border">
      {entityType}
    </span>
  );
}

function JsonDiffView({ before, after }: { before: Record<string, unknown> | null; after: Record<string, unknown> | null }) {
  const allKeys = new Set<string>();
  if (before) Object.keys(before).forEach((k) => allKeys.add(k));
  if (after) Object.keys(after).forEach((k) => allKeys.add(k));
  const sortedKeys = Array.from(allKeys).sort();

  return (
    <div className="grid grid-cols-2 gap-px bg-border rounded overflow-hidden text-xs font-mono">
      <div className="bg-bg-secondary px-3 py-1.5 text-text-muted font-sans font-medium text-xs">Before</div>
      <div className="bg-bg-secondary px-3 py-1.5 text-text-muted font-sans font-medium text-xs">After</div>
      {sortedKeys.map((key) => {
        const bVal = before?.[key];
        const aVal = after?.[key];
        const bStr = bVal !== undefined ? JSON.stringify(bVal) : '';
        const aStr = aVal !== undefined ? JSON.stringify(aVal) : '';
        const changed = bStr !== aStr;

        return (
          <div key={key} className="contents">
            <div className={`px-3 py-1 ${changed ? 'bg-red-950/30' : 'bg-bg-primary'}`}>
              <span className="text-text-muted">{key}: </span>
              <span className={changed ? 'text-red-400' : 'text-text-secondary'}>{bStr || <span className="text-text-muted italic">—</span>}</span>
            </div>
            <div className={`px-3 py-1 ${changed ? 'bg-green-950/30' : 'bg-bg-primary'}`}>
              <span className="text-text-muted">{key}: </span>
              <span className={changed ? 'text-green-400' : 'text-text-secondary'}>{aStr || <span className="text-text-muted italic">—</span>}</span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function DiffEntryCard({ entry }: { entry: BranchDiffEntry }) {
  const [expanded, setExpanded] = useState(false);
  const hasDetail = entry.changeType === 'MODIFIED' || entry.before || entry.after;

  return (
    <div className="border border-border rounded bg-bg-secondary">
      <button
        type="button"
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-bg-tertiary transition-colors"
        onClick={() => hasDetail && setExpanded(!expanded)}
      >
        <ChangeBadge changeType={entry.changeType} />
        <EntityTypeBadge entityType={entry.entityType} />
        <span className="text-sm text-text-primary font-mono truncate flex-1">{entry.entityRid}</span>
        {hasDetail && (
          <svg
            className={`w-4 h-4 text-text-muted transition-transform ${expanded ? 'rotate-180' : ''}`}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={2}
          >
            <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
          </svg>
        )}
      </button>
      {expanded && hasDetail && (
        <div className="px-4 pb-4">
          {entry.changeType === 'MODIFIED' && entry.before && entry.after ? (
            <JsonDiffView before={entry.before} after={entry.after} />
          ) : entry.changeType === 'ADDED' && entry.after ? (
            <pre className="text-xs font-mono text-green-400 bg-green-950/20 rounded p-3 overflow-x-auto whitespace-pre-wrap">
              {JSON.stringify(entry.after, null, 2)}
            </pre>
          ) : entry.changeType === 'DELETED' && entry.before ? (
            <pre className="text-xs font-mono text-red-400 bg-red-950/20 rounded p-3 overflow-x-auto whitespace-pre-wrap">
              {JSON.stringify(entry.before, null, 2)}
            </pre>
          ) : null}
        </div>
      )}
    </div>
  );
}

function groupByEntityType(entries: BranchDiffEntry[]): Map<string, BranchDiffEntry[]> {
  const groups = new Map<string, BranchDiffEntry[]>();
  for (const entry of entries) {
    const group = groups.get(entry.entityType) ?? [];
    group.push(entry);
    groups.set(entry.entityType, group);
  }
  return groups;
}

export function BranchDiffPage() {
  const { ontology, branch } = useParams<{ ontology: string; branch: string }>();
  const [filter, setFilter] = useState<EntityFilter>('all');

  const ontologyApiName = ontology ?? '';
  const branchId = branch ?? '';

  const { data: entries, isLoading, error } = useBranchDiff(ontologyApiName, branchId);

  if (!ontologyApiName || !branchId) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Missing parameters"
          description="Both ontology and branch are required."
        />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Failed to load branch diff"
          description={String(error)}
        />
      </div>
    );
  }

  const allEntries = entries ?? [];
  const entityTypes = Array.from(new Set(allEntries.map((e) => e.entityType))).sort();
  const filtered = filter === 'all' ? allEntries : allEntries.filter((e) => e.entityType === filter);
  const grouped = groupByEntityType(filtered);

  const addedCount = allEntries.filter((e) => e.changeType === 'ADDED').length;
  const modifiedCount = allEntries.filter((e) => e.changeType === 'MODIFIED').length;
  const deletedCount = allEntries.filter((e) => e.changeType === 'DELETED').length;

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="px-6 py-4 border-b border-border shrink-0">
        <div className="flex items-center gap-2 text-xs text-text-muted mb-2">
          <Link to={`/explorer/${ontologyApiName}`} className="hover:text-text-secondary transition-colors">
            {ontologyApiName}
          </Link>
          <span>/</span>
          <span className="text-text-secondary">branches</span>
          <span>/</span>
          <span className="text-text-primary font-medium">{branchId}</span>
        </div>
        <h2 className="text-lg font-semibold text-text-primary">Branch Diff</h2>
        <p className="text-xs text-text-secondary mt-0.5">
          Changes on this branch compared to main.
        </p>

        {/* Summary badges */}
        <div className="flex items-center gap-3 mt-3">
          {addedCount > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-green-900/30 text-green-400">
              +{addedCount} added
            </span>
          )}
          {modifiedCount > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-yellow-900/30 text-yellow-400">
              ~{modifiedCount} modified
            </span>
          )}
          {deletedCount > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-red-900/30 text-red-400">
              -{deletedCount} deleted
            </span>
          )}
          {allEntries.length === 0 && (
            <span className="text-xs text-text-muted">No changes</span>
          )}
        </div>

        {/* Entity type filter */}
        {entityTypes.length > 1 && (
          <div className="flex items-center gap-2 mt-3">
            <button
              type="button"
              onClick={() => setFilter('all')}
              className={`px-2.5 py-1 rounded text-xs transition-colors ${
                filter === 'all'
                  ? 'bg-accent-cyan/20 text-accent-cyan'
                  : 'bg-bg-tertiary text-text-secondary hover:text-text-primary'
              }`}
            >
              All ({allEntries.length})
            </button>
            {entityTypes.map((type) => (
              <button
                key={type}
                type="button"
                onClick={() => setFilter(type)}
                className={`px-2.5 py-1 rounded text-xs font-mono transition-colors ${
                  filter === type
                    ? 'bg-accent-cyan/20 text-accent-cyan'
                    : 'bg-bg-tertiary text-text-secondary hover:text-text-primary'
                }`}
              >
                {type} ({allEntries.filter((e) => e.entityType === type).length})
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Diff entries */}
      <div className="flex-1 overflow-auto p-6">
        {filtered.length === 0 ? (
          <EmptyState
            title="No changes"
            description="This branch has no schema changes compared to main."
          />
        ) : (
          <div className="space-y-6 max-w-4xl">
            {Array.from(grouped.entries()).map(([entityType, groupEntries]) => (
              <div key={entityType}>
                <h3 className="text-sm font-semibold text-text-primary mb-3 flex items-center gap-2">
                  <span className="capitalize">{entityType}</span>
                  <span className="text-xs text-text-muted font-normal">({groupEntries.length})</span>
                </h3>
                <div className="space-y-2">
                  {groupEntries.map((entry) => (
                    <DiffEntryCard key={entry.entityRid} entry={entry} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
