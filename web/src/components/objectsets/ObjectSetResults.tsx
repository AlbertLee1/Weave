import { useEffect, useMemo, useState } from 'react';
import type {
  ObjectSetDefinition,
  ObjectType,
  AggregationMetric,
  GroupByClause,
  WireObject,
} from '../../api/types';
import { useAggregateObjectSet } from '../../hooks/useObjectSets';
import { useOfflineObjectSet } from '../../hooks/useOfflineObjectSet';
import { useObjectType } from '../../hooks/useObjectTypes';
import { ObjectTable } from '../browser/ObjectTable';
import { ObjectDetail } from '../browser/ObjectDetail';
import { MetricSelector } from '../aggregation/MetricSelector';
import { GroupByBuilder } from '../aggregation/GroupByBuilder';
import { ResultTable } from '../aggregation/ResultTable';
import { SimpleChart } from '../aggregation/SimpleChart';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { OfflineConflictBanner } from '../common/OfflineConflictBanner';

interface ObjectSetResultsProps {
  ontologyApiName: string;
  def: ObjectSetDefinition | null;
  // executeKey changes whenever the user clicks Execute, forcing a refetch.
  executeKey: number;
  shareInfo?: ShareInfo | null;
}

export interface ShareInfo {
  objectSetRid: string;
  expiresAt: number; // epoch ms
}

const PAGE_SIZE = 25;

type Tab = 'browse' | 'aggregate';

// collectDerivedColumns walks the definition and returns the ordered list of
// derived property names declared by any withProperties node. These surface on
// each wire row as top-level keys merged in by the backend handler, so the
// browse table needs an extra column per name even though the ObjectType
// schema does not declare them.
function collectDerivedColumns(def: ObjectSetDefinition): string[] {
  const out: string[] = [];
  const seen = new Set<string>();

  function visit(d: ObjectSetDefinition): void {
    switch (d.type) {
      case 'withProperties': {
        for (const dp of d.derivedProperties ?? []) {
          if (dp.name && !seen.has(dp.name)) {
            seen.add(dp.name);
            out.push(dp.name);
          }
        }
        visit(d.objectSet);
        return;
      }
      case 'filter':
      case 'searchAround':
      case 'nearestNeighbors':
      case 'asType':
      case 'asBaseObjectTypes':
      case 'interfaceLinkSearchAround':
        visit(d.objectSet);
        return;
      case 'union':
      case 'intersect':
      case 'subtract':
        for (const child of d.objectSets) visit(child);
        return;
      default:
        return;
    }
  }

  visit(def);
  return out;
}

// resolveRootType walks the definition to determine the static result type for
// schema lookups. searchAround changes type so we fall back to the runtime
// __apiName from loaded objects in that case.
function resolveRootType(def: ObjectSetDefinition): string {
  switch (def.type) {
    case 'base':
    case 'static':
      return def.objectType;
    case 'asType':
      return def.objectType;
    case 'filter':
    case 'withProperties':
    case 'nearestNeighbors':
    case 'asBaseObjectTypes':
      return resolveRootType(def.objectSet);
    case 'union':
    case 'intersect':
    case 'subtract':
      return def.objectSets.length > 0
        ? resolveRootType(def.objectSets[0])
        : '';
    case 'searchAround':
    case 'interfaceLinkSearchAround':
      return ''; // backend resolves
    case 'reference':
    case 'interfaceBase':
    case 'methodInput':
      return '';
  }
}

export function ObjectSetResults({
  ontologyApiName,
  def,
  executeKey,
  shareInfo,
}: ObjectSetResultsProps) {
  const [tab, setTab] = useState<Tab>('browse');

  // Pagination state
  const [pageTokens, setPageTokens] = useState<string[]>([]);
  const [currentPage, setCurrentPage] = useState(1);

  // Detail panel
  const [selectedObject, setSelectedObject] = useState<WireObject | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

  // Aggregation config
  const [metrics, setMetrics] = useState<AggregationMetric[]>([{ type: 'count' }]);
  const [groupBy, setGroupBy] = useState<GroupByClause[]>([]);
  const [aggExecuted, setAggExecuted] = useState(false);

  // Reset pagination + agg execution when the definition or executeKey changes.
  useEffect(() => {
    setPageTokens([]);
    setCurrentPage(1);
    setAggExecuted(false);
  }, [def, executeKey]);

  const currentPageToken = pageTokens[currentPage - 2];

  // Resolve the static root type name from the definition for schema lookup.
  const staticTypeName = useMemo(
    () => (def ? resolveRootType(def) : ''),
    [def],
  );

  // Derived columns surfaced on each row by the backend, beyond the static
  // schema. Extracted from the definition so column order matches the spec
  // order the user typed, even before the first row arrives.
  const derivedColumns = useMemo(
    () => (def ? collectDerivedColumns(def) : []),
    [def],
  );

  // Fetch the object type metadata so we can derive the select fields.
  const { data: staticObjectType } = useObjectType(ontologyApiName, staticTypeName);

  // Build the select array from known properties (Foundry V2 requires it).
  const selectFields = useMemo(() => {
    if (!staticObjectType?.properties) return [];
    return Object.keys(staticObjectType.properties);
  }, [staticObjectType]);

  // Browse data — wrapped in the offline-aware hook (US-451) so the cached
  // snapshot fronts when the network drops AND a conflict surfaces when the
  // server diverges from what's in IndexedDB on reconnect.
  const browseQuery = useOfflineObjectSet({
    ontologyApiName,
    objectSet: def,
    select: selectFields,
    pageSize: PAGE_SIZE,
    pageToken: currentPageToken,
    enabled: !!def && tab === 'browse' && selectFields.length > 0,
  });

  // Determine the runtime object type from the first row, falling back to the
  // statically-resolved root type. This handles searchAround correctly because
  // the backend reports the resolved type via __apiName on each row.
  const resolvedTypeName = useMemo(() => {
    const first = browseQuery.data?.data?.[0];
    if (first?.__apiName) return first.__apiName;
    return staticTypeName;
  }, [browseQuery.data, staticTypeName]);

  const { data: resolvedObjectType } = useObjectType(
    ontologyApiName,
    resolvedTypeName,
  );

  // Available fields for aggregation
  const availableFields = useMemo(() => {
    if (!resolvedObjectType?.properties) return [];
    return Object.keys(resolvedObjectType.properties);
  }, [resolvedObjectType]);

  // Aggregate data
  const aggQuery = useAggregateObjectSet({
    ontologyApiName,
    objectSet: def,
    aggregation: metrics,
    groupBy: groupBy.length > 0 ? groupBy : undefined,
    enabled: !!def && tab === 'aggregate' && aggExecuted,
  });

  const chartMetricKey = useMemo(() => {
    if (!aggQuery.data?.data || aggQuery.data.data.length === 0) return '';
    const first = aggQuery.data.data[0];
    const keys = Object.keys(first.metrics);
    const named = metrics.find((m) => m.name);
    if (named?.name && keys.includes(named.name)) return named.name;
    return keys[0] ?? '';
  }, [aggQuery.data, metrics]);

  const browsePage = browseQuery.data;

  function handleNextPage() {
    if (browsePage?.nextPageToken) {
      setPageTokens((prev) => {
        const next = [...prev];
        next[currentPage - 1] = browsePage.nextPageToken!;
        return next;
      });
      setCurrentPage((p) => p + 1);
    }
  }

  function handlePrevPage() {
    if (currentPage > 1) setCurrentPage((p) => p - 1);
  }

  function handleRowClick(row: WireObject) {
    setSelectedObject(row);
    setDetailOpen(true);
  }

  // Empty state when no def has been provided.
  if (!def) {
    return (
      <div className="flex flex-col h-full" data-testid="objectset-results-initial">
        <ResultsHeader tab={tab} setTab={setTab} shareInfo={shareInfo} />
        <div className="flex-1 flex items-center justify-center">
          <EmptyState
            title="No Results Yet"
            description="Build an object set on the left and click Execute."
          />
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex flex-col h-full overflow-hidden"
      data-testid="objectset-results"
    >
      <ResultsHeader
        tab={tab}
        setTab={setTab}
        shareInfo={shareInfo}
        statusLine={
          tab === 'browse'
            ? browsePage?.totalCount
              ? `${browsePage.totalCount} objects`
              : undefined
            : aggQuery.data
              ? `${aggQuery.data.data.length} buckets`
              : undefined
        }
      />

      {tab === 'browse' && (
        <OfflineConflictBanner
          conflict={browseQuery.conflict}
          onKeepMine={() => void browseQuery.keepMine()}
          onUseServer={() => void browseQuery.useServer()}
        />
      )}

      <div className="flex-1 overflow-y-auto p-4">
        {tab === 'browse' && (
          <BrowsePane
            isLoading={browseQuery.isLoading}
            error={browseQuery.error}
            page={browsePage ?? null}
            objectType={resolvedObjectType}
            ontologyApiName={ontologyApiName}
            currentPage={currentPage}
            onNextPage={handleNextPage}
            onPrevPage={handlePrevPage}
            onRowClick={handleRowClick}
            derivedColumns={derivedColumns}
          />
        )}

        {tab === 'aggregate' && (
          <AggregatePane
            metrics={metrics}
            setMetrics={setMetrics}
            groupBy={groupBy}
            setGroupBy={setGroupBy}
            availableFields={availableFields}
            onExecute={() => setAggExecuted(true)}
            isLoading={aggQuery.isLoading}
            error={aggQuery.error}
            data={aggQuery.data?.data ?? null}
            chartMetricKey={chartMetricKey}
            hasGroupBy={groupBy.length > 0}
          />
        )}
      </div>

      {resolvedObjectType && (
        <ObjectDetail
          object={selectedObject}
          objectType={resolvedObjectType}
          open={detailOpen}
          onClose={() => setDetailOpen(false)}
          ontologyApiName={ontologyApiName}
        />
      )}
    </div>
  );
}

function ResultsHeader({
  tab,
  setTab,
  shareInfo,
  statusLine,
}: {
  tab: Tab;
  setTab: (t: Tab) => void;
  shareInfo?: ShareInfo | null;
  statusLine?: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-4 py-3 border-b border-border bg-bg-secondary/40">
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={() => setTab('browse')}
          className={`px-3 py-1.5 text-xs font-mono rounded transition-colors ${
            tab === 'browse'
              ? 'bg-bg-tertiary text-accent-cyan border border-accent-cyan/40'
              : 'text-text-secondary hover:text-text-primary border border-transparent'
          }`}
        >
          Browse
        </button>
        <button
          type="button"
          onClick={() => setTab('aggregate')}
          className={`px-3 py-1.5 text-xs font-mono rounded transition-colors ${
            tab === 'aggregate'
              ? 'bg-bg-tertiary text-accent-cyan border border-accent-cyan/40'
              : 'text-text-secondary hover:text-text-primary border border-transparent'
          }`}
        >
          Aggregate
        </button>
      </div>
      <div className="flex items-center gap-3 text-xs font-mono text-text-secondary">
        {statusLine && <span data-testid="objectset-status-line">{statusLine}</span>}
        {shareInfo && <ShareBadge info={shareInfo} />}
      </div>
    </div>
  );
}

function ShareBadge({ info }: { info: ShareInfo }) {
  const remainingMs = Math.max(0, info.expiresAt - Date.now());
  const minutes = Math.floor(remainingMs / 60000);
  return (
    <span
      className="px-2 py-0.5 border border-border rounded bg-bg-tertiary text-accent-cyan"
      title={info.objectSetRid}
    >
      ref {info.objectSetRid.slice(0, 8)} - expires {minutes}m
    </span>
  );
}

interface BrowsePaneProps {
  isLoading: boolean;
  error: unknown;
  page: { data: WireObject[]; totalCount?: string; nextPageToken?: string } | null;
  objectType?: ObjectType;
  ontologyApiName: string;
  currentPage: number;
  onNextPage: () => void;
  onPrevPage: () => void;
  onRowClick: (row: WireObject) => void;
  derivedColumns?: string[];
}

function BrowsePane({
  isLoading,
  error,
  page,
  objectType,
  ontologyApiName,
  currentPage,
  onNextPage,
  onPrevPage,
  onRowClick,
  derivedColumns,
}: BrowsePaneProps) {
  if (isLoading) {
    return (
      <div
        className="flex items-center justify-center py-12"
        data-testid="objectset-browse-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }
  if (error) {
    return (
      <div
        className="px-4 py-3 border border-accent-error/30 bg-accent-error/5 rounded text-xs font-mono text-accent-error"
        data-testid="objectset-browse-error"
      >
        {error instanceof Error ? error.message : 'Failed to load object set'}
      </div>
    );
  }
  if (!page) {
    return (
      <div data-testid="objectset-browse-pending">
        <EmptyState
          title="No Results Yet"
          description="Click Execute to run the object set."
        />
      </div>
    );
  }
  if (page.data.length === 0) {
    return (
      <div data-testid="objectset-browse-empty">
        <EmptyState
          title="No objects matched"
          description="The object set is valid but returned zero rows."
        />
      </div>
    );
  }
  if (!objectType) {
    return (
      <div className="text-xs font-mono text-text-secondary">
        Loaded {page.data.length} rows; resolving schema...
      </div>
    );
  }
  return (
    <ObjectTable
      ontologyApiName={ontologyApiName}
      objectType={objectType}
      data={page.data}
      onRowClick={onRowClick}
      pageSize={PAGE_SIZE}
      totalCount={page.totalCount}
      hasNextPage={!!page.nextPageToken}
      hasPrevPage={currentPage > 1}
      onNextPage={onNextPage}
      onPrevPage={onPrevPage}
      currentPage={currentPage}
      derivedColumns={derivedColumns}
    />
  );
}

interface AggregatePaneProps {
  metrics: AggregationMetric[];
  setMetrics: (m: AggregationMetric[]) => void;
  groupBy: GroupByClause[];
  setGroupBy: (g: GroupByClause[]) => void;
  availableFields: string[];
  onExecute: () => void;
  isLoading: boolean;
  error: unknown;
  data: { group?: Record<string, unknown>; metrics: Record<string, number> }[] | null;
  chartMetricKey: string;
  hasGroupBy: boolean;
}

function AggregatePane({
  metrics,
  setMetrics,
  groupBy,
  setGroupBy,
  availableFields,
  onExecute,
  isLoading,
  error,
  data,
  chartMetricKey,
  hasGroupBy,
}: AggregatePaneProps) {
  return (
    <div className="flex flex-col gap-4">
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
      <button
        type="button"
        onClick={onExecute}
        disabled={metrics.length === 0}
        className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-40 self-start"
      >
        Run Aggregation
      </button>
      {isLoading && (
        <div className="flex items-center justify-center py-8">
          <LoadingSpinner />
        </div>
      )}
      {error ? (
        <div className="text-xs text-accent-error">
          {error instanceof Error ? error.message : 'Aggregation failed'}
        </div>
      ) : null}
      {data && (
        <div className="flex flex-col gap-4">
          <ResultTable data={data} />
          {data.length > 0 && chartMetricKey && hasGroupBy && (
            <div className="border border-border rounded p-4 bg-bg-tertiary">
              <h3 className="text-xs font-medium text-text-primary mb-3">Chart</h3>
              <SimpleChart data={data} metricKey={chartMetricKey} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
