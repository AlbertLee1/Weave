import { useState, useCallback, useEffect, useMemo } from 'react';
import { useParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useObjectType } from '../../hooks/useObjectTypes';
import { useProperties } from '../../hooks/useProperties';
import { useListObjects, useSearchObjects } from '../../hooks/useObjects';
import { useCreateTemporaryObjectSet } from '../../hooks/useObjectSets';
import {
  useObjectSetSubscription,
  type ObjectSetSubscriptionStatus,
} from '../../hooks/useObjectSetSubscription';
import {
  useWebSocketSubscription,
  type WebSocketSubscriptionStatus,
} from '../../hooks/useWebSocketSubscription';
import { buildWhereClause, type FilterCondition } from '../../lib/whereBuilder';
import { SearchBar } from './SearchBar';
import { FilterBuilder } from './FilterBuilder';
import { FacetsPanel, type FacetSelection } from './FacetsPanel';
import { SavedSearchesPanel } from './SavedSearchesPanel';
import { ObjectTable, type ObjectTableSelection } from './ObjectTable';
import { MapView } from './MapView';
import { GanttChart } from './GanttChart';
import { SankeyDiagram } from './SankeyDiagram';
import { PivotTable } from './PivotTable';
import { ObjectDetail } from './ObjectDetail';
import { ExportButton } from './ExportButton';
import { BulkActionToolbar } from './BulkActionToolbar';
import { SkeletonTable, SkeletonText } from '../common/Skeleton';
import { EmptyState } from '../common/EmptyState';
import { TimeTravelToolbar } from './TimeTravelToolbar';
import { useTimeTravelActive } from './useTimeTravel';
import type { WhereClause, WireObject } from '../../api/types';
import type { SavedSearchDefinition } from '../../api/savedSearches';
import { ApiRequestError } from '../../api/client';

// DOG-004: surface the backend `parameters.reason` alongside the
// `errorCode: errorName` summary so operators see the actual cause
// (e.g. "containsAnyTerm value must be a string") instead of a generic
// `INVALID_ARGUMENT: SearchObjectsFailed` that hides the contract issue.
function formatBrowserError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason;
    return reason ? `${err.message} — ${reason}` : err.message;
  }
  if (err instanceof Error) return err.message;
  return 'Failed to load objects';
}

const PAGE_SIZE = 25;
const MAX_FACET_FIELDS = 5;
const TIME_TRAVEL_EXPORT_DISABLED_REASON =
  'Exports are unavailable while Time Travel is active.';
const FACETABLE_BASE_TYPES = new Set([
  'string',
  'boolean',
  'date',
  'datetime',
  'timestamp',
]);

type BrowserLiveStatus = {
  label: string;
  ariaLabel: string;
  tone: 'connected' | 'connecting' | 'reconnecting' | 'disabled';
  connected: boolean;
};

export function BrowserPage() {
  const { ontology = '', objectType: objectTypeParam = '' } = useParams<{
    ontology: string;
    objectType: string;
  }>();

  return (
    <BrowserPageContent
      key={`${ontology}/${objectTypeParam}`}
      ontology={ontology}
      objectTypeParam={objectTypeParam}
    />
  );
}

interface BrowserPageContentProps {
  ontology: string;
  objectTypeParam: string;
}

function BrowserPageContent({
  ontology,
  objectTypeParam,
}: BrowserPageContentProps) {
  // Object type metadata
  const { data: objectType, isLoading: isLoadingType } = useObjectType(
    ontology,
    objectTypeParam,
  );
  const { data: detailedProperties } = useProperties(
    ontology,
    objectType?.rid ?? '',
  );

  // Search & filter state
  const [searchText, setSearchText] = useState('');
  const [filters, setFilters] = useState<FilterCondition[]>([]);
  const [showFilters, setShowFilters] = useState(false);
  const [sortField, setSortField] = useState<string | undefined>();
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc');
  const [selectedFacets, setSelectedFacets] = useState<FacetSelection>({});

  // Pagination state
  const [pageTokens, setPageTokens] = useState<string[]>([]);
  const [currentPage, setCurrentPage] = useState(1);

  const currentPageToken = pageTokens[currentPage - 2]; // page 1 has no token

  // Detail panel state
  const [selectedObject, setSelectedObject] = useState<WireObject | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

  // Bulk selection state (primary-key → full row so we can export without re-fetch)
  const [selectedRowMap, setSelectedRowMap] = useState<Map<string, WireObject>>(
    () => new Map(),
  );

  // View mode: table | map | gantt | sankey | pivot
  const [viewMode, setViewMode] = useState<
    'table' | 'map' | 'gantt' | 'sankey' | 'pivot'
  >('table');

  // Realtime mode state: 'off' | 'ws' (WebSocket) | 'sse' (SSE fallback)
  const [realtimeMode, setRealtimeMode] = useState<'off' | 'ws' | 'sse'>('off');
  const [objectSetRid, setObjectSetRid] = useState<string | null>(null);
  const [webSocketFallbackRequested, setWebSocketFallbackRequested] =
    useState(false);
  const [webSocketStatus, setWebSocketStatus] =
    useState<WebSocketSubscriptionStatus>('idle');
  const [objectSetStatus, setObjectSetStatus] =
    useState<ObjectSetSubscriptionStatus>('idle');
  const queryClient = useQueryClient();
  const createObjectSet = useCreateTemporaryObjectSet(ontology);

  // US-048: when historical mode is active every request carries
  // ?asOf=<tx-...> via the API-client interceptor. The page disables
  // mutation affordances (Live toggle / Export / Bulk selection /
  // inline edits) so the operator cannot accidentally write to the
  // current branch while reviewing a historical snapshot.
  const timeTravelActive = useTimeTravelActive(ontology);

  const realtimeEnabled = realtimeMode !== 'off';

  const invalidateObjects = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['objects'] });
  }, [queryClient]);

  const handleRealtimeToggle = useCallback(() => {
    if (!realtimeEnabled) {
      // Use WebSocket as primary; SSE fallback available via setRealtimeMode('sse')
      setRealtimeMode('ws');
      setWebSocketFallbackRequested(false);
      setWebSocketStatus('connecting');
      setObjectSetStatus('idle');
      // Also create ObjectSet for SSE fallback readiness
      createObjectSet.mutate(
        { type: 'base', objectType: objectTypeParam },
        {
          onSuccess: (resp) => {
            setObjectSetRid(resp.objectSetRid);
          },
        },
      );
    } else {
      // Turning off
      setRealtimeMode('off');
      setObjectSetRid(null);
      setWebSocketFallbackRequested(false);
      setWebSocketStatus('idle');
      setObjectSetStatus('idle');
    }
  }, [realtimeEnabled, createObjectSet, objectTypeParam]);

  const handleWebSocketStatusChange = useCallback(
    (status: WebSocketSubscriptionStatus) => {
      setWebSocketStatus(status);
      if (status === 'connected') {
        setWebSocketFallbackRequested(false);
        return;
      }
      if (status === 'reconnecting') {
        setWebSocketFallbackRequested(true);
      }
    },
    [],
  );

  const handleObjectSetStatusChange = useCallback(
    (status: ObjectSetSubscriptionStatus) => {
      setObjectSetStatus(status);
    },
    [],
  );

  useEffect(() => {
    if (
      realtimeMode === 'ws' &&
      webSocketFallbackRequested &&
      objectSetRid
    ) {
      setObjectSetStatus('connecting');
      setRealtimeMode('sse');
    }
  }, [objectSetRid, realtimeMode, webSocketFallbackRequested]);

  const liveStatus = useMemo<BrowserLiveStatus | null>(() => {
    if (timeTravelActive) {
      return {
        label: 'Unavailable',
        ariaLabel: 'Live updates unavailable while Time Travel is active',
        tone: 'disabled',
        connected: false,
      };
    }

    if (realtimeMode === 'off') return null;

    const showingSseConnectingFallback =
      realtimeMode === 'sse' &&
      objectSetStatus === 'connecting' &&
      webSocketStatus === 'reconnecting';
    const transport =
      realtimeMode === 'sse' && !showingSseConnectingFallback
        ? 'SSE fallback'
        : 'WebSocket';
    const status =
      realtimeMode === 'sse' && !showingSseConnectingFallback
        ? objectSetStatus
        : webSocketStatus;
    const effectiveStatus = status === 'idle' ? 'connecting' : status;

    if (effectiveStatus === 'connected') {
      return {
        label: 'Connected',
        ariaLabel: `Live updates connected over ${transport}`,
        tone: 'connected',
        connected: true,
      };
    }

    if (effectiveStatus === 'reconnecting') {
      return {
        label: 'Reconnecting',
        ariaLabel: `Live updates reconnecting over ${transport}`,
        tone: 'reconnecting',
        connected: false,
      };
    }

    return {
      label: 'Connecting',
      ariaLabel: `Live updates connecting over ${transport}`,
      tone: 'connecting',
      connected: false,
    };
  }, [objectSetStatus, realtimeMode, timeTravelActive, webSocketStatus]);

  const liveStatusClassName = useMemo(() => {
    const base = 'text-[10px] font-mono uppercase tracking-wider';
    switch (liveStatus?.tone) {
      case 'connected':
        return `${base} text-green-500`;
      case 'reconnecting':
        return `${base} text-accent-amber`;
      case 'connecting':
        return `${base} text-text-secondary`;
      case 'disabled':
        return `${base} text-accent-amber`;
      default:
        return base;
    }
  }, [liveStatus]);

  // WebSocket subscription (primary)
  useWebSocketSubscription(ontology, {
    objectType: objectTypeParam,
    enabled: realtimeMode === 'ws',
    onEvent: useCallback(() => {
      invalidateObjects();
    }, [invalidateObjects]),
    onStatusChange: handleWebSocketStatusChange,
  });

  // SSE subscription (fallback when WebSocket is unavailable)
  useObjectSetSubscription(ontology, objectSetRid ?? '', {
    enabled: realtimeMode === 'sse' && !!objectSetRid,
    onEvent: useCallback(() => {
      invalidateObjects();
    }, [invalidateObjects]),
    onStatusChange: handleObjectSetStatusChange,
  });

  // Compute the set of facet-able fields from the object type's properties.
  // Picks string/boolean/date-typed fields, excludes the primary key, and caps
  // at MAX_FACET_FIELDS so the response stays bounded.
  const facetFields = useMemo<string[]>(() => {
    if (!objectType?.properties) return [];
    const out: string[] = [];
    for (const [name, prop] of Object.entries(objectType.properties)) {
      if (name === objectType.primaryKey) continue;
      const t = prop.dataType?.type?.toLowerCase?.() ?? '';
      if (FACETABLE_BASE_TYPES.has(t)) {
        out.push(name);
        if (out.length >= MAX_FACET_FIELDS) break;
      }
    }
    return out;
  }, [objectType]);

  const hasFacetSelection = useMemo(
    () => Object.values(selectedFacets).some((vs) => vs.length > 0),
    [selectedFacets],
  );

  const sortablePropertyKeys = useMemo(() => {
    const out = new Set<string>();
    for (const prop of detailedProperties ?? []) {
      if (prop.isSortable) out.add(prop.apiName);
    }
    return out;
  }, [detailedProperties]);

  // Determine whether we need to use search or list
  const hasActiveSearch =
    searchText.trim().length > 0 || filters.length > 0 || hasFacetSelection;

  const effectiveSortField = useMemo(() => {
    if (!sortField) return undefined;
    if (sortField === objectType?.primaryKey) return sortField;
    return sortablePropertyKeys.has(sortField) ? sortField : undefined;
  }, [objectType?.primaryKey, sortField, sortablePropertyKeys]);

  // Build where clause for search
  const whereClause = useMemo(() => {
    const allFilters: FilterCondition[] = [...filters];

    if (searchText.trim()) {
      // Add a full-text search filter using containsAnyTerm on the title property.
      // DOG-004: backend expects a single string (Bleve MatchQuery tokenises and
      // ORs on whitespace internally); sending an array yielded
      // `containsAnyTerm value must be a string` / SearchObjectsFailed.
      const titleProp = objectType?.titleProperty ?? objectType?.primaryKey;
      if (titleProp) {
        const normalized = searchText
          .trim()
          .split(/\s+/)
          .filter((t) => t.length > 0)
          .join(' ');
        if (normalized.length > 0) {
          allFilters.push({
            field: titleProp,
            op: 'containsAnyTerm',
            value: normalized,
          });
        }
      }
    }

    const baseWhere = buildWhereClause(allFilters);

    // Apply selected facets as an AND of (OR-of-eq) per field. Backend `where`
    // doesn't expose an `in` operator, so multi-select within one field uses
    // a disjunction of `eq` clauses.
    const facetClauses: WhereClause[] = [];
    for (const [field, values] of Object.entries(selectedFacets)) {
      if (!values || values.length === 0) continue;
      const eqs: WhereClause[] = values.map((v) => ({
        type: 'eq',
        field,
        value: v,
      }));
      facetClauses.push(
        eqs.length === 1 ? eqs[0] : { type: 'or', value: eqs },
      );
    }

    if (facetClauses.length === 0) return baseWhere;
    const combined: WhereClause[] = baseWhere
      ? [baseWhere, ...facetClauses]
      : facetClauses;
    return combined.length === 1
      ? combined[0]
      : { type: 'and', value: combined };
  }, [filters, searchText, objectType, selectedFacets]);

  // List objects (no filters/search)
  const listResult = useListObjects({
    ontologyApiName: ontology,
    objectType: objectTypeParam,
    pageSize: PAGE_SIZE,
    pageToken: currentPageToken,
    orderBy: effectiveSortField
      ? `${effectiveSortField}:${sortDirection}`
      : undefined,
  });

  // Build the select array from known properties (Foundry V2 requires it).
  const selectFields = useMemo(() => {
    if (!objectType?.properties) return [];
    return Object.keys(objectType.properties);
  }, [objectType]);

  // Search objects (with filters/search)
  const searchResult = useSearchObjects({
    ontologyApiName: ontology,
    objectType: objectTypeParam,
    where: whereClause,
    pageSize: PAGE_SIZE,
    pageToken: currentPageToken,
    orderBy: effectiveSortField
      ? { field: effectiveSortField, direction: sortDirection }
      : undefined,
    select: selectFields,
    facets: facetFields,
    enabled: hasActiveSearch,
  });

  const result = hasActiveSearch ? searchResult : listResult;
  const { data: page, isLoading, error } = result;

  // Handlers
  const handleSearch = useCallback((text: string) => {
    setSearchText(text);
    setPageTokens([]);
    setCurrentPage(1);
  }, []);

  const handleFiltersChange = useCallback((newFilters: FilterCondition[]) => {
    setFilters(newFilters);
    setPageTokens([]);
    setCurrentPage(1);
  }, []);

  const handleToggleFacet = useCallback((field: string, value: string) => {
    setSelectedFacets((prev) => {
      const cur = prev[field] ?? [];
      const next = cur.includes(value)
        ? cur.filter((v) => v !== value)
        : [...cur, value];
      const out = { ...prev, [field]: next };
      if (next.length === 0) delete out[field];
      return out;
    });
    setPageTokens([]);
    setCurrentPage(1);
  }, []);

  const handleClearFacets = useCallback(() => {
    setSelectedFacets({});
    setPageTokens([]);
    setCurrentPage(1);
  }, []);

  const handleSort = useCallback(
    (field: string, direction: 'asc' | 'desc') => {
      setSortField(field === '__primaryKey' ? objectType?.primaryKey : field);
      setSortDirection(direction);
      setPageTokens([]);
      setCurrentPage(1);
    },
    [objectType],
  );

  // Saved-searches integration: serialise the current view into a
  // round-trippable definition, and accept a loaded definition by
  // resetting every controlled field. The definition deliberately
  // mirrors the BrowserPage's local state — no derived/server-only
  // values (whereClause, pageTokens) so a load is a clean re-mount of
  // the user-visible filters.
  const currentDefinition = useMemo<SavedSearchDefinition>(() => {
    return {
      searchText: searchText.trim() || undefined,
      filters: filters.length > 0 ? filters : undefined,
      facets: hasFacetSelection ? selectedFacets : undefined,
      sort: sortField ? { field: sortField, direction: sortDirection } : null,
    };
  }, [searchText, filters, hasFacetSelection, selectedFacets, sortField, sortDirection]);

  const handleApplySavedSearch = useCallback(
    (def: SavedSearchDefinition) => {
      setSearchText(def.searchText ?? '');
      setFilters(def.filters ?? []);
      setSelectedFacets(def.facets ?? {});
      if (def.sort) {
        setSortField(def.sort.field);
        setSortDirection(def.sort.direction);
      } else {
        setSortField(undefined);
        setSortDirection('asc');
      }
      setShowFilters((def.filters ?? []).length > 0);
      setPageTokens([]);
      setCurrentPage(1);
    },
    [],
  );

  const handleNextPage = useCallback(() => {
    if (page?.nextPageToken) {
      setPageTokens((prev) => {
        const next = [...prev];
        next[currentPage - 1] = page.nextPageToken!;
        return next;
      });
      setCurrentPage((p) => p + 1);
    }
  }, [page, currentPage]);

  const handlePrevPage = useCallback(() => {
    if (currentPage > 1) {
      setCurrentPage((p) => p - 1);
    }
  }, [currentPage]);

  const handleRowClick = useCallback((row: WireObject) => {
    setSelectedObject(row);
    setDetailOpen(true);
  }, []);

  const handleCloseDetail = useCallback(() => {
    setDetailOpen(false);
  }, []);

  // Selection handlers
  const selectedKeys = useMemo(
    () => new Set(selectedRowMap.keys()),
    [selectedRowMap],
  );

  const handleToggleSelection = useCallback((primaryKey: string) => {
    setSelectedRowMap((prev) => {
      const next = new Map(prev);
      if (next.has(primaryKey)) {
        next.delete(primaryKey);
      } else {
        const row = page?.data.find(
          (r) => String(r.__primaryKey ?? '') === primaryKey,
        );
        if (row) next.set(primaryKey, row);
      }
      return next;
    });
  }, [page]);

  const handleToggleAllOnPage = useCallback(
    (select: boolean) => {
      setSelectedRowMap((prev) => {
        const next = new Map(prev);
        for (const row of page?.data ?? []) {
          const pk = String(row.__primaryKey ?? '');
          if (!pk) continue;
          if (select) {
            next.set(pk, row);
          } else {
            next.delete(pk);
          }
        }
        return next;
      });
    },
    [page],
  );

  const handleClearSelection = useCallback(() => {
    setSelectedRowMap(new Map());
  }, []);

  const tableSelection = useMemo<ObjectTableSelection>(
    () => ({
      selectedKeys,
      onToggle: handleToggleSelection,
      onToggleAll: handleToggleAllOnPage,
    }),
    [selectedKeys, handleToggleSelection, handleToggleAllOnPage],
  );

  const selectedRows = useMemo(
    () => Array.from(selectedRowMap.values()),
    [selectedRowMap],
  );

  // Loading state for object type metadata: shows the page chrome that the
  // table will eventually fill, so the layout doesn't reflow on data arrival.
  if (isLoadingType) {
    return (
      <div
        className="flex flex-col gap-4 p-6 bg-bg-primary min-h-full"
        data-testid="browser-loading"
      >
        <SkeletonText lines={2} lineHeight={14} aria-label="Loading object type" />
        <SkeletonTable rows={6} columns={4} aria-label="Loading objects" />
      </div>
    );
  }

  if (!objectType) {
    return (
      <EmptyState
        title="Object type not found"
        description={`Could not load object type "${objectTypeParam}" from ontology "${ontology}".`}
      />
    );
  }

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-full">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-sans font-semibold text-text-primary">
            {objectType.pluralDisplayName ?? objectType.displayName}
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {objectType.apiName}
          </p>
        </div>
        <div className="flex items-center gap-4">
          {page?.totalCount && (
            <span className="text-xs font-mono text-text-secondary">
              {page.totalCount} total
            </span>
          )}
          <div
            role="group"
            aria-label="View mode"
            className="inline-flex rounded border border-border overflow-hidden"
          >
            <button
              type="button"
              onClick={() => setViewMode('table')}
              aria-pressed={viewMode === 'table'}
              data-testid="view-mode-table"
              className={[
                'px-2 py-1 text-xs font-mono transition-colors',
                viewMode === 'table'
                  ? 'bg-accent-cyan/10 text-accent-cyan'
                  : 'text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              Table
            </button>
            <button
              type="button"
              onClick={() => setViewMode('map')}
              aria-pressed={viewMode === 'map'}
              data-testid="view-mode-map"
              className={[
                'px-2 py-1 text-xs font-mono transition-colors border-l border-border',
                viewMode === 'map'
                  ? 'bg-accent-cyan/10 text-accent-cyan'
                  : 'text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              Map
            </button>
            <button
              type="button"
              onClick={() => setViewMode('gantt')}
              aria-pressed={viewMode === 'gantt'}
              data-testid="view-mode-gantt"
              className={[
                'px-2 py-1 text-xs font-mono transition-colors border-l border-border',
                viewMode === 'gantt'
                  ? 'bg-accent-cyan/10 text-accent-cyan'
                  : 'text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              Gantt
            </button>
            <button
              type="button"
              onClick={() => setViewMode('sankey')}
              aria-pressed={viewMode === 'sankey'}
              data-testid="view-mode-sankey"
              className={[
                'px-2 py-1 text-xs font-mono transition-colors border-l border-border',
                viewMode === 'sankey'
                  ? 'bg-accent-cyan/10 text-accent-cyan'
                  : 'text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              Sankey
            </button>
            <button
              type="button"
              onClick={() => setViewMode('pivot')}
              aria-pressed={viewMode === 'pivot'}
              data-testid="view-mode-pivot"
              className={[
                'px-2 py-1 text-xs font-mono transition-colors border-l border-border',
                viewMode === 'pivot'
                  ? 'bg-accent-cyan/10 text-accent-cyan'
                  : 'text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              Pivot
            </button>
          </div>
          <ExportButton
            objectType={objectType}
            disabled={timeTravelActive}
            disabledReason={
              timeTravelActive ? TIME_TRAVEL_EXPORT_DISABLED_REASON : undefined
            }
            query={{
              ontologyApiName: ontology,
              objectType: objectTypeParam,
              where: whereClause,
              orderBy: effectiveSortField
                ? { field: effectiveSortField, direction: sortDirection }
                : undefined,
              select: selectFields,
              hasActiveSearch,
            }}
          />
          <label
            className={[
              'flex items-center gap-2 select-none',
              timeTravelActive
                ? 'cursor-not-allowed opacity-40'
                : 'cursor-pointer',
            ].join(' ')}
            title={
              timeTravelActive
                ? 'Live updates are disabled while Time Travel is on.'
                : undefined
            }
          >
            {liveStatus?.connected && (
              <span
                data-testid="realtime-indicator"
                aria-label={liveStatus.ariaLabel}
                className="inline-block w-2 h-2 rounded-full bg-green-500 animate-pulse"
              />
            )}
            <span className="text-xs font-mono text-text-secondary">Live</span>
            {liveStatus && (
              <span
                id="browser-live-status"
                data-testid="live-status"
                aria-label={liveStatus.ariaLabel}
                className={liveStatusClassName}
              >
                {liveStatus.label}
              </span>
            )}
            <input
              type="checkbox"
              aria-label="Live"
              aria-describedby={liveStatus ? 'browser-live-status' : undefined}
              data-testid="live-toggle"
              checked={realtimeEnabled}
              onChange={handleRealtimeToggle}
              disabled={timeTravelActive}
              className="sr-only peer"
            />
            <span className="relative w-8 h-4 rounded-full bg-border-secondary peer-checked:bg-green-500 transition-colors after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:w-3 after:h-3 after:rounded-full after:bg-white after:transition-transform peer-checked:after:translate-x-4" />
          </label>
        </div>
      </div>

      {/* US-048 Time Travel toolbar: dataset-transaction picker + toggle. */}
      <TimeTravelToolbar ontologyApiName={ontology} />

      {/* US-048: historical-mode hint banner. Renders only while a tx is
          pinned so it does not push other chrome around in steady state. */}
      {timeTravelActive && (
        <div
          data-testid="time-travel-hint-banner"
          className="px-3 py-2 text-xs font-mono rounded border border-accent-amber/40 bg-accent-amber/5 text-accent-amber"
        >
          Viewing a historical snapshot — edit / live / bulk actions are
          disabled. Toggle Time Travel off above to return to the live view.
        </div>
      )}

      {/* Search bar */}
      <SearchBar
        onSearch={handleSearch}
        onToggleFilters={() => setShowFilters((v) => !v)}
      />

      {/* Filter builder */}
      {showFilters && objectType.properties && (
        <FilterBuilder
          properties={objectType.properties}
          filters={filters}
          onFiltersChange={handleFiltersChange}
        />
      )}

      {/* Error */}
      {error && (
        <div
          className="px-4 py-3 border border-accent-error/30 bg-accent-error/5 rounded text-xs font-mono text-accent-error"
          data-testid="browser-error"
        >
          {formatBrowserError(error)}
        </div>
      )}

      {/* Loading — show a table skeleton matching the future layout */}
      {isLoading && (
        <SkeletonTable rows={6} columns={4} aria-label="Loading objects" />
      )}

      {/* Results area: Saved searches + Facets sidebar + Table/Map/Empty */}
      {!isLoading && page && (
        <div className="flex gap-4">
          <div className="flex flex-col gap-4">
            <SavedSearchesPanel
              ontology={ontology}
              objectType={objectTypeParam}
              currentDefinition={currentDefinition}
              hasCurrentState={hasActiveSearch}
              onLoad={handleApplySavedSearch}
            />
            {hasActiveSearch && facetFields.length > 0 && (
              <FacetsPanel
                fields={facetFields}
                facets={page.facets}
                selected={selectedFacets}
                onToggle={handleToggleFacet}
                onClear={handleClearFacets}
              />
            )}
          </div>
          <div className="flex-1 min-w-0">
            {page.data.length > 0 && viewMode === 'table' && (
              <ObjectTable
                ontologyApiName={ontology}
                objectType={objectType}
                data={page.data}
                onRowClick={handleRowClick}
                onSort={handleSort}
                sortablePropertyKeys={sortablePropertyKeys}
                pageSize={PAGE_SIZE}
                totalCount={page.totalCount}
                hasNextPage={!!page.nextPageToken}
                hasPrevPage={currentPage > 1}
                onNextPage={handleNextPage}
                onPrevPage={handlePrevPage}
                currentPage={currentPage}
                selection={tableSelection}
              />
            )}

            {viewMode === 'map' && (
              <MapView
                objectType={objectType}
                data={page.data}
                onRowClick={handleRowClick}
              />
            )}

            {viewMode === 'gantt' && (
              <GanttChart
                objectType={objectType}
                data={page.data}
                onRowClick={handleRowClick}
              />
            )}

            {viewMode === 'sankey' && (
              <SankeyDiagram
                objectType={objectType}
                data={page.data}
                onRowClick={handleRowClick}
              />
            )}

            {viewMode === 'pivot' && (
              <PivotTable
                objectType={objectType}
                data={page.data}
                onRowClick={handleRowClick}
              />
            )}

            {page.data.length === 0 && (
              <EmptyState
                title="No objects found"
                description={
                  hasActiveSearch
                    ? 'Try adjusting your search or filters.'
                    : 'This object type has no data yet.'
                }
              />
            )}
          </div>
        </div>
      )}

      {/* Bulk-action floating toolbar (rendered only when selection is non-empty).
          US-048: bulk delete is a mutation, so hide it entirely while the page
          is rendering a historical snapshot. The selection state stays alive
          so the operator can leave Time Travel and continue the bulk operation
          where they left off. */}
      {!timeTravelActive && (
        <BulkActionToolbar
          ontologyApiName={ontology}
          objectType={objectType}
          selectedRows={selectedRows}
          onClear={handleClearSelection}
        />
      )}

      {/* Detail slide panel */}
      <ObjectDetail
        object={selectedObject}
        objectType={objectType}
        open={detailOpen}
        onClose={handleCloseDetail}
        ontologyApiName={ontology}
      />
    </div>
  );
}
