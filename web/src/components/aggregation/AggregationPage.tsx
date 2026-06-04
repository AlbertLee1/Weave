import { useState, useMemo } from 'react';
import { useParams } from 'react-router';
import { useObjectType } from '../../hooks/useObjectTypes';
import { useAggregation } from '../../hooks/useAggregation';
import type { AggregationMetric, GroupByClause, AggregationRequest } from '../../api/types';
import { MetricSelector } from './MetricSelector';
import { GroupByBuilder, HavingBuilder, toHavingClauses, type HavingDraft } from './GroupByBuilder';
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
  const [having, setHaving] = useState<HavingDraft[]>([]);
  const [filters, setFilters] = useState<FilterCondition[]>([]);
  const [aggRequest, setAggRequest] = useState<AggregationRequest | null>(null);
  const [chartType, setChartType] = useState<SimpleChartType>('bar');
  const [accuracyMode, setAccuracyMode] = useState<
    'ALLOW_APPROXIMATE' | 'REQUIRE_ACCURATE'
  >('ALLOW_APPROXIMATE');

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

  // Determine which metric key to chart (first metric with a name, or first metric key from results)
  const chartMetricKey = useMemo(() => {
    if (!aggResult?.data || aggResult.data.length === 0) return '';
    const first = aggResult.data[0];
    const keys = Object.keys(first.metrics);
    // Try to find a named metric
    const named = metrics.find((m) => m.name);
    if (named?.name && keys.includes(named.name)) return named.name;
    return keys[0] ?? '';
  }, [aggResult, metrics]);

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
    setAggRequest({
      aggregation: metrics,
      groupBy: groupBy.length > 0 ? groupBy : undefined,
      where,
      // Only send accuracy when the user opts out of the approximate default;
      // omitting it lets the backend apply ALLOW_APPROXIMATE.
      accuracy: accuracyMode === 'REQUIRE_ACCURATE' ? 'REQUIRE_ACCURATE' : undefined,
      // Drop incomplete having rows; undefined omits `having` from the body.
      having: toHavingClauses(having),
    });
  }

  function handleExportCsv() {
    if (!aggResult?.data.length || !ontology || !objectType) return;
    downloadAggregationCsv(
      aggResult.data,
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
              onClick={handleExecute}
              disabled={metrics.length === 0 || hasInvalidGroupBy}
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
        {aggLoading ? (
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
        ) : !aggResult ? (
          <div data-testid="aggregation-empty-state">
            <EmptyState
              title="No Results Yet"
              description="Configure metrics and group by, then click Execute."
            />
          </div>
        ) : (
          <div
            data-testid="aggregation-results"
            data-bucket-count={aggResult.data.length}
            className="flex flex-col gap-6"
          >
            {aggResult.data.length > 0 && (
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
            {aggResult.accuracy && (
              <div
                data-testid="aggregation-accuracy-badge"
                data-accuracy={aggResult.accuracy}
                className="inline-flex self-start items-center gap-2 rounded border border-border bg-bg-tertiary px-2 py-1 text-xs font-mono text-text-secondary"
              >
                <span className="text-text-secondary">accuracy</span>
                <span className="text-accent-cyan">{aggResult.accuracy}</span>
                {typeof aggResult.excludedItems === 'number' && aggResult.excludedItems > 0 && (
                  <span className="text-text-secondary">
                    · excluded {aggResult.excludedItems}
                  </span>
                )}
              </div>
            )}
            <ResultTable data={aggResult.data} />
            {aggResult.data.length > 0 && chartMetricKey && groupBy.length > 0 && (
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
                    {CHART_TYPES.map((type) => {
                      const selected = chartType === type;
                      return (
                        <button
                          key={type}
                          type="button"
                          role="tab"
                          aria-selected={selected}
                          data-testid={`aggregation-chart-type-${type}`}
                          onClick={() => setChartType(type)}
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
                  data={aggResult.data}
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
