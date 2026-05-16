import { useMemo } from 'react';
import { useParams } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { EmptyState } from '../common/EmptyState';
import {
  QuiverWorkbenchView,
  type SeriesSpec,
} from './QuiverWorkbenchView';
import {
  viewQuiverDashboard,
  type QuiverDashboardConfig,
} from '../../api/quiver';
import { ApiRequestError } from '../../api/client';
import { useQuiverSparklines } from '../../hooks/useQuiverSparklines';
import type { TimeSeriesPoint } from '../../api/timeseries';

// US-403: read-only view of a saved Quiver dashboard. Any authenticated
// caller who knows the RID can render the workbench — the rid IS the
// share secret. The page mounts the same QuiverWorkbenchView used by
// the editor but does not expose the picker form, save controls, or
// per-row remove buttons.
export function QuiverViewPage() {
  const { rid } = useParams<{ rid: string }>();
  const { data, isLoading, error } = useQuery({
    queryKey: ['quiver', 'view', rid],
    queryFn: () => viewQuiverDashboard(rid!),
    enabled: !!rid,
    retry: false,
  });

  const seriesList = useMemo<SeriesSpec[]>(() => {
    if (!data) return [];
    const cfg: QuiverDashboardConfig = data.config ?? {
      ontologyApiName: '',
      series: [],
    };
    if (!Array.isArray(cfg.series)) return [];
    return cfg.series.map((s) => ({
      id: s.id,
      ontologyApiName: cfg.ontologyApiName,
      objectType: s.objectType,
      primaryKey: s.primaryKey,
      property: s.property,
      label: s.label,
      color: s.color,
      ...(s.branch ? { branch: s.branch } : {}),
    }));
  }, [data]);

  // US-483: one POST replaces the per-series fan-out. Enabled only once
  // the dashboard envelope has loaded so we never hit /sparklines with
  // a stale RID, and only when the dashboard actually declares series.
  const sparklinesQuery = useQuiverSparklines({
    rid,
    enabled: !!data && seriesList.length > 0,
  });

  const preloadedPoints = useMemo<Record<string, TimeSeriesPoint[]>>(() => {
    const out: Record<string, TimeSeriesPoint[]> = {};
    const resp = sparklinesQuery.data;
    if (!resp || !Array.isArray(resp.series)) return out;
    for (const s of resp.series) {
      out[s.id] = (s.points ?? []).map((p) => ({
        time: p.time,
        value: p.value,
      }));
    }
    return out;
  }, [sparklinesQuery.data]);

  if (!rid) {
    return (
      <div className="flex items-center justify-center h-full">
        <EmptyState
          title="Missing dashboard"
          description="No dashboard RID supplied in the URL."
        />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div
        className="flex items-center justify-center h-full text-xs text-text-muted"
        data-testid="quiver-view-loading"
      >
        Loading dashboard…
      </div>
    );
  }

  if (error) {
    const isApiErr = error instanceof ApiRequestError;
    const title =
      isApiErr && error.statusCode === 404
        ? 'Dashboard not found'
        : 'Failed to load dashboard';
    const description = isApiErr
      ? `${error.errorName}${error.parameters ? ` — ${JSON.stringify(error.parameters)}` : ''}`
      : String(error);
    return (
      <div
        className="flex items-center justify-center h-full"
        data-testid="quiver-view-error"
      >
        <EmptyState title={title} description={description} />
      </div>
    );
  }

  if (!data) return null;

  return (
    <div
      className="flex flex-col h-full overflow-hidden"
      data-testid="quiver-view-page"
    >
      <div className="border-b border-border bg-bg-primary p-4">
        <h2
          className="text-sm font-medium text-text-primary"
          data-testid="quiver-view-title"
        >
          {data.name}
        </h2>
        <div className="text-xs font-mono text-text-secondary mt-0.5">
          {data.config?.ontologyApiName ?? ''} ·{' '}
          <span className="text-text-muted">read-only · {data.rid}</span>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {seriesList.length === 0 ? (
          <EmptyState
            title="Empty dashboard"
            description="This dashboard has no series."
          />
        ) : (
          <QuiverWorkbenchView
            seriesList={seriesList}
            preloadedPoints={preloadedPoints}
          />
        )}
      </div>
    </div>
  );
}
