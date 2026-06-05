import { useMemo } from 'react';
import { Link, useParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import {
  actionLogRid,
  getActionImpact,
  type ImpactObject,
} from '../../api/actionImpact';
import { SkeletonTable } from '../common/Skeleton';
import { EmptyState } from '../common/EmptyState';

// Operation badge colours mirror the action-history status palette so the two
// views read consistently. Unknown operations fall back to a neutral chip.
const OPERATION_BADGE_STYLE: Record<string, string> = {
  CREATE: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  MODIFY: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  DELETE: 'bg-rose-500/10 text-rose-400 border border-rose-500/30',
};

function OperationBadge({ operation }: { operation?: string }) {
  if (!operation) {
    return <span className="text-text-secondary">—</span>;
  }
  return (
    <span
      data-testid={`impact-operation-${operation}`}
      className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${
        OPERATION_BADGE_STYLE[operation] ??
        'bg-bg-tertiary text-text-secondary border border-border/50'
      }`}
    >
      {operation}
    </span>
  );
}

export function ActionImpactPage() {
  const { ontology, actionLogId } = useParams<{
    ontology?: string;
    actionLogId?: string;
  }>();
  const activeOntology = ontology ?? '';
  const historyHref = `/actions/${activeOntology}/history`;

  // The impact endpoint is keyed by the action-log RID, derived from the
  // numeric id carried in the route. Guard against a non-numeric / missing id
  // so the query stays disabled rather than firing a malformed RID.
  const numericId = actionLogId !== undefined ? Number(actionLogId) : NaN;
  const hasValidId = actionLogId !== undefined && Number.isFinite(numericId);
  const rid = hasValidId ? actionLogRid(numericId) : '';

  const impactQuery = useQuery({
    queryKey: ['action-impact', rid],
    queryFn: () => getActionImpact(rid),
    enabled: hasValidId,
  });

  const objects = useMemo<ImpactObject[]>(
    () => impactQuery.data?.objects ?? [],
    [impactQuery.data],
  );
  const truncated = impactQuery.data?.truncated === true;

  return (
    <div
      className="mx-auto max-w-6xl space-y-6"
      data-testid="action-impact-page"
    >
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
          Action Impact
        </h1>
        <p className="text-sm text-text-secondary">
          Objects affected by execution{' '}
          <span className="font-mono text-text-primary">#{actionLogId}</span>
          {activeOntology && (
            <>
              {' '}
              in{' '}
              <span className="font-mono text-text-primary">
                {activeOntology}
              </span>
            </>
          )}
          .
        </p>
        <Link
          to={historyHref}
          data-testid="action-impact-back-link"
          className="inline-flex items-center gap-1 text-xs text-accent-cyan hover:underline"
        >
          ← Back to action history
        </Link>
      </header>

      {truncated && (
        <div
          role="alert"
          data-testid="action-impact-truncated"
          className="rounded-md border border-amber-500/40 bg-amber-500/10 px-4 py-2 text-xs text-amber-300"
        >
          Results truncated — showing partial impact.
        </div>
      )}

      {!hasValidId ? (
        <div data-testid="action-impact-invalid-id">
          <EmptyState
            title="Invalid action reference"
            description="The action log id in the URL is not a valid number."
          />
        </div>
      ) : impactQuery.isLoading ? (
        <div data-testid="action-impact-loading">
          <SkeletonTable
            rows={5}
            columns={5}
            aria-label="Loading action impact"
          />
        </div>
      ) : impactQuery.isError ? (
        <div data-testid="action-impact-error">
          <EmptyState
            title="Failed to load action impact"
            description={
              impactQuery.error instanceof Error
                ? impactQuery.error.message
                : 'Unexpected error.'
            }
          />
        </div>
      ) : objects.length === 0 ? (
        <div data-testid="action-impact-empty">
          <EmptyState
            title="No impact"
            description="This action did not affect any objects."
          />
        </div>
      ) : (
        <div
          className="overflow-hidden rounded-lg border border-border/50"
          data-testid="action-impact-table"
        >
          <table className="min-w-full divide-y divide-border/40 text-sm">
            <thead className="bg-bg-secondary/60 text-left text-xs text-text-secondary">
              <tr>
                <th scope="col" className="px-4 py-2 font-medium">
                  Operation
                </th>
                <th scope="col" className="px-4 py-2 font-medium">
                  Object Type
                </th>
                <th scope="col" className="px-4 py-2 font-medium">
                  Primary Key
                </th>
                <th scope="col" className="px-4 py-2 font-medium">
                  RID
                </th>
                <th scope="col" className="px-4 py-2 font-medium">
                  When
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/30">
              {objects.map((obj, idx) => (
                <tr
                  key={`${obj.rid}|${idx}`}
                  className="hover:bg-bg-tertiary/40"
                  data-testid="action-impact-row"
                  data-impact-rid={obj.rid}
                >
                  <td className="px-4 py-2">
                    <OperationBadge operation={obj.operation} />
                  </td>
                  <td className="px-4 py-2 text-xs text-text-primary">
                    {obj.objectType ?? '—'}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-text-primary">
                    {obj.primaryKey ?? '—'}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-text-secondary break-all">
                    {obj.rid}
                  </td>
                  <td className="px-4 py-2 text-xs text-text-secondary">
                    {obj.timestamp
                      ? new Date(obj.timestamp).toLocaleString()
                      : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
