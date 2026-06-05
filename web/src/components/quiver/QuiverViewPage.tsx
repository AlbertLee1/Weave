import { useCallback, useMemo } from 'react';
import { useParams, useSearchParams } from 'react-router';
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
import { useQuiverData } from '../../hooks/useQuiverData';
import type { TimeSeriesPoint } from '../../api/timeseries';

// US-482: explicit step choices for the in-page override <select>. The
// TopBar TimeRangePicker bundles one step per preset, but a reader may
// want a finer/coarser resolution for the same window — this drives the
// `step` query param the windowed /data fetch keys off.
const STEP_CHOICES = ['1m', '5m', '30m', '1h', '2h', '6h'] as const;

// US-403: read-only view of a saved Quiver dashboard. Any authenticated
// caller who knows the RID can render the workbench — the rid IS the
// share secret. The page mounts the same QuiverWorkbenchView used by
// the editor but does not expose the picker form, save controls, or
// per-row remove buttons.
export function QuiverViewPage() {
  const { rid } = useParams<{ rid: string }>();
  const [searchParams, setSearchParams] = useSearchParams();

  // US-482: the TopBar TimeRangePicker writes ?from=&to=&step= into the
  // URL. When `step` is present we switch the read surface from the raw
  // /sparklines fan-out to the windowed + bucketed /data endpoint so the
  // chart shows a fixed-resolution slice of [from, to].
  const fromParam = searchParams.get('from') ?? undefined;
  const toParam = searchParams.get('to') ?? undefined;
  const stepParam = searchParams.get('step') ?? undefined;
  const windowed = !!stepParam && stepParam.trim().length > 0;

  const setStep = useCallback(
    (step: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set('step', step);
          return next;
        },
        { replace: false },
      );
    },
    [setSearchParams],
  );

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
  // a stale RID, only when the dashboard actually declares series, AND
  // only when we are NOT in windowed mode (the /data fetch supersedes it).
  const sparklinesQuery = useQuiverSparklines({
    rid,
    enabled: !!data && seriesList.length > 0 && !windowed,
  });

  // US-482: windowed + bucketed fetch. Active only when the URL carries a
  // `step`; re-runs whenever the picker rewrites from/to/step.
  const dataQuery = useQuiverData({
    rid,
    from: fromParam,
    to: toParam,
    step: stepParam,
    enabled: !!data && seriesList.length > 0 && windowed,
  });

  const preloadedPoints = useMemo<Record<string, TimeSeriesPoint[]>>(() => {
    const out: Record<string, TimeSeriesPoint[]> = {};
    // Windowed mode: the /data series replace the raw sparklines.
    const resp = windowed ? dataQuery.data : sparklinesQuery.data;
    if (!resp || !Array.isArray(resp.series)) return out;
    for (const s of resp.series) {
      out[s.id] = (s.points ?? []).map((p) => ({
        time: p.time,
        value: p.value,
      }));
    }
    return out;
  }, [windowed, dataQuery.data, sparklinesQuery.data]);

  if (!rid) {
    return (
      <div className="flex items-center justify-center h-full">
        {/* Stable page-level heading, visually hidden so the layout is
            unchanged. Repeated in every state branch so the standalone
            Quiver Dashboard route always exposes exactly one <h1>. */}
        <h1 className="sr-only">Quiver Dashboard</h1>
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
        <h1 className="sr-only">Quiver Dashboard</h1>
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
        <h1 className="sr-only">Quiver Dashboard</h1>
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
      <h1 className="sr-only">Quiver Dashboard</h1>
      <div className="border-b border-border bg-bg-primary p-4 flex items-start justify-between gap-4">
        <div>
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

        {/* US-482: step override + window summary. The window itself is
            driven by the TopBar TimeRangePicker (?from=&to=); this select
            lets the reader rebucket the same window. */}
        <div
          className="flex items-center gap-2 text-xs"
          data-testid="quiver-view-step-controls"
        >
          <label className="text-text-secondary" htmlFor="quiver-step-select">
            Step
          </label>
          <select
            id="quiver-step-select"
            value={stepParam ?? ''}
            onChange={(e) => setStep(e.target.value)}
            data-testid="quiver-step-select"
            className="px-2 py-1 bg-bg-tertiary border border-border rounded text-text-primary font-mono"
          >
            {!windowed && <option value="">— all —</option>}
            {/* A deep-linked step outside the preset list (e.g. ?step=15m)
                still drives the fetch; surface it so the select reflects
                the real URL state instead of silently showing option 0. */}
            {windowed &&
              stepParam &&
              !STEP_CHOICES.includes(stepParam as (typeof STEP_CHOICES)[number]) && (
                <option value={stepParam}>{stepParam}</option>
              )}
            {STEP_CHOICES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          {windowed && (
            <span
              className="font-mono text-text-muted"
              data-testid="quiver-view-window"
            >
              {fromParam ?? 'all'} → {toParam ?? 'all'}
            </span>
          )}
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
