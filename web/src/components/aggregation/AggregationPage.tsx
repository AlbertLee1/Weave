import { useCallback, useRef, useState, useMemo, type KeyboardEvent } from 'react';
import { useParams } from 'react-router';
import { useObjectType } from '../../hooks/useObjectTypes';
import { useAggregation } from '../../hooks/useAggregation';
import {
  useAggregationSubscription,
  type AggregationSubscriptionMetric,
  type AggregationSubscriptionStatus,
} from '../../hooks/useAggregationSubscription';
import type { AggregationResponse } from '../../api/aggregation';
import type { AggregationMetric, GroupByClause, AggregationRequest } from '../../api/types';
import { MetricSelector } from './MetricSelector';
import {
  GroupByBuilder,
  HavingBuilder,
  toHavingClauses,
  type HavingDraft,
  type SubtotalMode,
} from './GroupByBuilder';
import { ResultTable } from './ResultTable';
import { SimpleChart, type SimpleChartType } from './SimpleChart';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { FilterBuilder } from '../browser/FilterBuilder';
import { buildWhereClause, type FilterCondition } from '../../lib/whereBuilder';
import {
  aggregationCsvFilename,
  downloadAggregationCsv,
} from '../../lib/aggregationCsv';

const CHART_TYPES: SimpleChartType[] = ['bar', 'line', 'pie'];

export function AggregationPage() {
  const { ontology, objectType } = useParams<{ ontology: string; objectType: string }>();

  const { data: objectTypeDef, isLoading: typeLoading } = useObjectType(
    ontology ?? '',
    objectType ?? '',
  );

  const [metrics, setMetrics] = useState<AggregationMetric[]>([{ type: 'count' }]);
  const [groupBy, setGroupBy] = useState<GroupByClause[]>([]);
  const [subtotalMode, setSubtotalMode] = useState<SubtotalMode>('none');
  const [having, setHaving] = useState<HavingDraft[]>([]);
  const [filters, setFilters] = useState<FilterCondition[]>([]);
  const [aggRequest, setAggRequest] = useState<AggregationRequest | null>(null);
  const [chartType, setChartType] = useState<SimpleChartType>('bar');
  const [accuracyMode, setAccuracyMode] = useState<
    'ALLOW_APPROXIMATE' | 'REQUIRE_ACCURATE'
  >('ALLOW_APPROXIMATE');

  // Live mode: subscribe to the backend aggregation WebSocket so the result
  // refreshes automatically as objects change. The subscription only supports a
  // single metric (count/sum/avg/min/max) over an optional single exact-match
  // groupBy, so Live is gated to that subset — richer shapes stay on Execute.
  const [live, setLive] = useState(false);
  const [liveResult, setLiveResult] = useState<AggregationResponse | null>(null);
  const [liveStatus, setLiveStatus] =
    useState<AggregationSubscriptionStatus>('idle');

  const availableProperties = useMemo<
    Record<string, { dataType: { type: string; itemType?: unknown }; rid: string }>
  >(() => objectTypeDef?.properties ?? {}, [objectTypeDef]);

  // Derive available fields from object type properties
  const availableFields = useMemo(() => {
    return Object.keys(availableProperties);
  }, [availableProperties]);

  // Metric output names a having clause can reference: the user-supplied
  // alias on each metric. Offered as datalist hints in the having editor.
  const metricNames = useMemo(
    () =>
      metrics
        .map((m) => m.name?.trim())
        .filter((n): n is string => !!n),
    [metrics],
  );

  const { data: aggResult, isLoading: aggLoading, isError, error } = useAggregation(
    ontology ?? '',
    objectType ?? '',
    aggRequest,
  );

  // The metric the live subscription would track: the first metric whose type
  // is supported by the backend aggregation subscription. count needs no field;
  // the others require one.
  const liveMetric = useMemo<AggregationSubscriptionMetric | null>(() => {
    const m = metrics[0];
    if (!m) return null;
    if (m.type === 'count') {
      return { type: 'count', name: m.name?.trim() || 'count' };
    }
    if (
      (m.type === 'sum' ||
        m.type === 'avg' ||
        m.type === 'min' ||
        m.type === 'max') &&
      m.field
    ) {
      return { type: m.type, field: m.field, name: m.name?.trim() || m.type };
    }
    return null;
  }, [metrics]);

  // Live groupBy: the subscription supports a single exact-match field only.
  // When the first (and only) groupBy clause is an exact field we forward it;
  // any other grouping shape disqualifies Live.
  const liveGroupBy = useMemo<string | undefined>(() => {
    if (groupBy.length === 0) return undefined;
    if (groupBy.length === 1 && groupBy[0].type === 'exact' && groupBy[0].field) {
      return groupBy[0].field;
    }
    return undefined;
  }, [groupBy]);

  // Live is only offered when the metric is subscribable AND the grouping (if
  // any) is a single exact field. Multi-metric / ranges / fixedWidth / having /
  // cube / rollup configurations fall back to the on-demand Execute path.
  const liveEligible = useMemo(() => {
    if (!liveMetric) return false;
    if (metrics.length !== 1) return false;
    if (subtotalMode !== 'none') return false;
    if (toHavingClauses(having)) return false;
    if (groupBy.length > 1) return false;
    if (groupBy.length === 1 && groupBy[0].type !== 'exact') return false;
    return true;
  }, [liveMetric, metrics.length, subtotalMode, having, groupBy]);

  // Drop out of Live the moment the config stops being subscribable so we never
  // hold an open socket whose result no longer matches the on-screen builder.
  if (live && !liveEligible) {
    setLive(false);
  }

  const liveWhere = useMemo(() => buildWhereClause(filters), [filters]);

  const handleLiveSnapshot = useCallback((snapshot: AggregationResponse) => {
    setLiveResult(snapshot);
  }, []);

  // Roving-tabindex refs for the chart-type tablist so keyboard navigation can
  // move DOM focus to the activated tab (WAI-ARIA tabs pattern).
  const chartTabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  // ArrowLeft/Right move between tabs (wrapping), Home/End jump to the ends.
  // Activation is automatic: moving focus also selects (setChartType), which is
  // the recommended pattern for tablists whose panels render cheaply.
  const handleChartTabKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const last = CHART_TYPES.length - 1;
      let nextIndex: number | null = null;
      switch (event.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          nextIndex = index === last ? 0 : index + 1;
          break;
        case 'ArrowLeft':
        case 'ArrowUp':
          nextIndex = index === 0 ? last : index - 1;
          break;
        case 'Home':
          nextIndex = 0;
          break;
        case 'End':
          nextIndex = last;
          break;
        default:
          return;
      }
      event.preventDefault();
      setChartType(CHART_TYPES[nextIndex]);
      chartTabRefs.current[nextIndex]?.focus();
    },
    [],
  );

  useAggregationSubscription(ontology ?? '', {
    objectType: objectType ?? '',
    metric: liveMetric ?? { type: 'count', name: 'count' },
    where: liveWhere,
    groupBy: liveGroupBy,
    enabled: live && liveEligible && !!ontology && !!objectType,
    onSnapshot: handleLiveSnapshot,
    onStatusChange: setLiveStatus,
  });

  // While Live is on, the live snapshot drives the result panel; otherwise the
  // on-demand Execute result does.
  const displayResult = live ? liveResult : aggResult;

  // Determine which metric key to chart (first metric with a name, or first metric key from results)
  const chartMetricKey = useMemo(() => {
    if (!displayResult?.data || displayResult.data.length === 0) return '';
    const first = displayResult.data[0];
    const keys = Object.keys(first.metrics);
    // Try to find a named metric
    const named = metrics.find((m) => m.name);
    if (named?.name && keys.includes(named.name)) return named.name;
    return keys[0] ?? '';
  }, [displayResult, metrics]);

  // Block Execute on groupBy clauses the backend would reject outright:
  //  - a `ranges` clause with zero rows sends an empty ranges array → zero
  //    buckets (silent wrong result);
  //  - a `fixedWidth` clause without a positive width always errors server-side.
  const hasInvalidGroupBy = useMemo(
    () =>
      groupBy.some(
        (g) =>
          (g.type === 'ranges' && (g.ranges?.length ?? 0) === 0) ||
          (g.type === 'fixedWidth' &&
            !(typeof g.fixedWidth === 'number' && g.fixedWidth > 0)),
      ),
    [groupBy],
  );

  function handleExecute() {
    const where = buildWhereClause(filters);
    const hasGroupBy = groupBy.length > 0;
    // cube/rollup are no-ops without grouping dimensions, and mutually
    // exclusive on the wire — send exactly one boolean, only when a groupBy
    // exists. Omit both for the 'none' default so the body stays minimal.
    const cube = hasGroupBy && subtotalMode === 'cube' ? true : undefined;
    const rollup = hasGroupBy && subtotalMode === 'rollup' ? true : undefined;
    setAggRequest({
      aggregation: metrics,
      groupBy: hasGroupBy ? groupBy : undefined,
      where,
      // Only send accuracy when the user opts out of the approximate default;
      // omitting it lets the backend apply ALLOW_APPROXIMATE.
      accuracy: accuracyMode === 'REQUIRE_ACCURATE' ? 'REQUIRE_ACCURATE' : undefined,
      // Drop incomplete having rows; undefined omits `having` from the body.
      having: toHavingClauses(having),
      cube,
      rollup,
    });
  }

  function toggleLive() {
    // Reset the snapshot on every toggle so a stale prior result never flashes
    // before the first aggregationChanged arrives (on) or after leaving Live
    // (off). Both setters are independent statements so each updater stays pure.
    setLiveResult(null);
    setLive((on) => !on);
  }

  function handleExportCsv() {
    if (!displayResult?.data.length || !ontology || !objectType) return;
    downloadAggregationCsv(
      displayResult.data,
      aggregationCsvFilename(ontology, objectType),
    );
  }

  if (!ontology || !objectType) {
    return (
      <div
        data-testid="aggregation-no-params"
        className="flex items-center justify-center h-full"
      >
        <EmptyState title="Missing Parameters" description="Ontology and object type are required." />
      </div>
    );
  }

  if (typeLoading) {
    return (
      <div
        data-testid="aggregation-typeloading"
        className="flex items-center justify-center h-full"
      >
        <LoadingSpinner />
      </div>
    );
  }

  return (
    <div
      data-testid="aggregation-page"
      data-ontology={ontology}
      data-object-type={objectType}
      className="flex flex-col h-full overflow-hidden"
    >
      {/* Config panel */}
      <div
        data-testid="aggregation-config-panel"
        className="border-b border-border bg-bg-primary p-4 flex flex-col gap-4"
      >
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-sm font-medium text-text-primary">Aggregation</h2>
            <div className="text-xs font-mono text-text-secondary mt-0.5">
              {ontology} / {objectType}
            </div>
          </div>
          <div className="flex items-center gap-3">
            <label className="flex items-center gap-2 text-xs text-text-secondary font-sans">
              <span>Accuracy</span>
              <select
                value={accuracyMode}
                onChange={(e) =>
                  setAccuracyMode(
                    e.target.value === 'REQUIRE_ACCURATE'
                      ? 'REQUIRE_ACCURATE'
                      : 'ALLOW_APPROXIMATE',
                  )
                }
                data-testid="aggregation-accuracy-select"
                aria-label="Aggregation accuracy mode"
                title="REQUIRE_ACCURATE promotes approximateDistinct/percentile to exact computation"
                className="bg-bg-tertiary border border-border rounded px-2 py-1.5 text-xs text-text-primary font-mono focus:border-accent-cyan focus:outline-none"
              >
                <option value="ALLOW_APPROXIMATE">Allow approximate</option>
                <option value="REQUIRE_ACCURATE">Require accurate</option>
              </select>
            </label>
            <button
              type="button"
              role="switch"
              aria-checked={live}
              aria-label="Live updates"
              onClick={toggleLive}
              disabled={!liveEligible}
              data-testid="aggregation-live-toggle"
              data-live={live ? 'on' : 'off'}
              data-live-status={liveStatus}
              title={
                liveEligible
                  ? 'Stream aggregation results as objects change'
                  : 'Live supports a single count/sum/avg/min/max metric over an optional single exact-match group by'
              }
              className={
                live
                  ? 'flex items-center gap-1.5 rounded border border-accent-cyan bg-accent-cyan/15 px-3 py-2 text-xs font-medium text-accent-cyan disabled:opacity-50'
                  : 'flex items-center gap-1.5 rounded border border-border bg-bg-tertiary px-3 py-2 text-xs font-medium text-text-secondary hover:text-text-primary disabled:opacity-50'
              }
            >
              <span
                aria-hidden="true"
                className={
                  live
                    ? liveStatus === 'connected'
                      ? 'h-2 w-2 rounded-full bg-accent-cyan'
                      : 'h-2 w-2 rounded-full bg-accent-cyan/50 animate-pulse'
                    : 'h-2 w-2 rounded-full bg-text-secondary/40'
                }
              />
              {live ? 'Live on' : 'Live'}
            </button>
            <button
              onClick={handleExecute}
              disabled={live || metrics.length === 0 || hasInvalidGroupBy}
              data-testid="aggregation-execute"
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
            >
              Execute
            </button>
          </div>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <MetricSelector
            metrics={metrics}
            onChange={setMetrics}
            availableFields={availableFields}
          />
          <GroupByBuilder
            groupBy={groupBy}
            onChange={setGroupBy}
            availableFields={availableFields}
            subtotalMode={subtotalMode}
            onSubtotalModeChange={setSubtotalMode}
          />
        </div>

        <div
          data-testid="aggregation-having-section"
          className="flex flex-col gap-3 border-t border-border pt-4"
        >
          <HavingBuilder
            having={having}
            onChange={setHaving}
            metricNames={metricNames}
          />
        </div>

        <div
          data-testid="aggregation-filter-section"
          className="flex flex-col gap-3 border-t border-border pt-4"
        >
          <label className="text-xs text-text-secondary font-sans mb-1">
            Where Filter
          </label>
          {availableFields.length > 0 ? (
            <FilterBuilder
              properties={availableProperties}
              filters={filters}
              onFiltersChange={setFilters}
            />
          ) : (
            <div className="text-xs text-text-secondary">
              No filterable fields.
            </div>
          )}
        </div>
      </div>

      {/* Results panel */}
      <div className="flex-1 overflow-y-auto p-4">
        {live ? (
          !displayResult ? (
            <div
              data-testid="aggregation-live-waiting"
              className="flex flex-col items-center justify-center gap-2 py-12 text-xs text-text-secondary"
            >
              <LoadingSpinner />
              <span>
                {liveStatus === 'reconnecting'
                  ? 'Reconnecting to live updates…'
                  : 'Waiting for live aggregation…'}
              </span>
            </div>
          ) : null
        ) : aggLoading ? (
          <div
            data-testid="aggregation-loading"
            className="flex items-center justify-center py-12"
          >
            <LoadingSpinner />
          </div>
        ) : isError ? (
          <div data-testid="aggregation-error" className="text-sm text-accent-error">
            Error: {error instanceof Error ? error.message : 'Aggregation failed'}
          </div>
        ) : !displayResult ? (
          <div data-testid="aggregation-empty-state">
            <EmptyState
              title="No Results Yet"
              description="Configure metrics and group by, then click Execute."
            />
          </div>
        ) : null}
        {displayResult && (
          <div
            data-testid="aggregation-results"
            data-bucket-count={displayResult.data.length}
            data-live={live ? 'on' : 'off'}
            className="flex flex-col gap-6"
          >
            {live && (
              <div
                data-testid="aggregation-live-indicator"
                data-live-status={liveStatus}
                className="inline-flex self-start items-center gap-2 rounded border border-accent-cyan/40 bg-accent-cyan/10 px-2 py-1 text-xs font-mono text-accent-cyan"
              >
                <span
                  aria-hidden="true"
                  className={
                    liveStatus === 'connected'
                      ? 'h-2 w-2 rounded-full bg-accent-cyan'
                      : 'h-2 w-2 rounded-full bg-accent-cyan/50 animate-pulse'
                  }
                />
                Live · {liveStatus}
              </div>
            )}
            {displayResult.data.length > 0 && (
              <div className="flex items-center justify-end">
                <button
                  type="button"
                  onClick={handleExportCsv}
                  data-testid="aggregation-export-csv"
                  className="rounded border border-accent-cyan/40 bg-accent-cyan/10 px-3 py-1.5 text-xs font-medium text-accent-cyan hover:bg-accent-cyan/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-cyan/50"
                >
                  Export CSV
                </button>
              </div>
            )}
            {displayResult.accuracy && (
              <div
                data-testid="aggregation-accuracy-badge"
                data-accuracy={displayResult.accuracy}
                className="inline-flex self-start items-center gap-2 rounded border border-border bg-bg-tertiary px-2 py-1 text-xs font-mono text-text-secondary"
              >
                <span className="text-text-secondary">accuracy</span>
                <span className="text-accent-cyan">{displayResult.accuracy}</span>
                {typeof displayResult.excludedItems === 'number' && displayResult.excludedItems > 0 && (
                  <span className="text-text-secondary">
                    · excluded {displayResult.excludedItems}
                  </span>
                )}
              </div>
            )}
            <ResultTable data={displayResult.data} />
            {displayResult.data.length > 0 && chartMetricKey && groupBy.length > 0 && (
              <div
                data-testid="aggregation-chart"
                data-chart-type={chartType}
                className="border border-border rounded p-4 bg-bg-tertiary"
              >
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                  <h3 className="text-xs font-medium text-text-primary">Chart</h3>
                  <div
                    role="tablist"
                    aria-label="Chart type"
                    data-testid="aggregation-chart-type-tabs"
                    className="inline-flex overflow-hidden rounded border border-border bg-bg-primary"
                  >
                    {CHART_TYPES.map((type, index) => {
                      const selected = chartType === type;
                      return (
                        <button
                          key={type}
                          ref={(el) => {
                            chartTabRefs.current[index] = el;
                          }}
                          type="button"
                          role="tab"
                          aria-selected={selected}
                          tabIndex={selected ? 0 : -1}
                          data-testid={`aggregation-chart-type-${type}`}
                          onClick={() => setChartType(type)}
                          onKeyDown={(event) => handleChartTabKeyDown(event, index)}
                          className={
                            selected
                              ? 'px-3 py-1.5 text-xs font-medium capitalize text-bg-primary bg-accent-cyan'
                              : 'px-3 py-1.5 text-xs font-medium capitalize text-text-secondary hover:bg-bg-elevated hover:text-text-primary'
                          }
                        >
                          {type}
                        </button>
                      );
                    })}
                  </div>
                </div>
                <SimpleChart
                  data={displayResult.data}
                  metricKey={chartMetricKey}
                  chartType={chartType}
                />
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
