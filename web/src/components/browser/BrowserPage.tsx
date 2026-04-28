import { useState, useCallback, useMemo } from 'react';
import { useParams } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useObjectType } from '../../hooks/useObjectTypes';
import { useListObjects, useSearchObjects } from '../../hooks/useObjects';
import { useCreateTemporaryObjectSet } from '../../hooks/useObjectSets';
import { useObjectSetSubscription } from '../../hooks/useObjectSetSubscription';
import { useWebSocketSubscription } from '../../hooks/useWebSocketSubscription';
import { buildWhereClause, type FilterCondition } from '../../lib/whereBuilder';
import { SearchBar } from './SearchBar';
import { FilterBuilder } from './FilterBuilder';
import { FacetsPanel, type FacetSelection } from './FacetsPanel';
import { ObjectTable, type ObjectTableSelection } from './ObjectTable';
import { MapView } from './MapView';
import { ObjectDetail } from './ObjectDetail';
import { ExportButton } from './ExportButton';
import { BulkActionToolbar } from './BulkActionToolbar';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import type { WhereClause, WireObject } from '../../api/types';

const PAGE_SIZE = 25;
const MAX_FACET_FIELDS = 5;
const FACETABLE_BASE_TYPES = new Set([
  'string',
  'boolean',
  'date',
  'datetime',
  'timestamp',
]);

export function BrowserPage() {
  const { ontology = '', objectType: objectTypeParam = '' } = useParams<{
    ontology: string;
    objectType: string;
  }>();

  // Object type metadata
  const { data: objectType, isLoading: isLoadingType } = useObjectType(
    ontology,
    objectTypeParam,
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

  // View mode: table | map
  const [viewMode, setViewMode] = useState<'table' | 'map'>('table');

  // Realtime mode state: 'off' | 'ws' (WebSocket) | 'sse' (SSE fallback)
  const [realtimeMode, setRealtimeMode] = useState<'off' | 'ws' | 'sse'>('off');
  const [objectSetRid, setObjectSetRid] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const createObjectSet = useCreateTemporaryObjectSet(ontology);

  const realtimeEnabled = realtimeMode !== 'off';

  const invalidateObjects = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['objects'] });
  }, [queryClient]);

  const handleRealtimeToggle = useCallback(() => {
    if (!realtimeEnabled) {
      // Use WebSocket as primary; SSE fallback available via setRealtimeMode('sse')
      setRealtimeMode('ws');
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
    }
  }, [realtimeEnabled, createObjectSet, objectTypeParam]);

  // WebSocket subscription (primary)
  useWebSocketSubscription(ontology, {
    objectType: objectTypeParam,
    enabled: realtimeMode === 'ws',
    onEvent: useCallback(() => {
      invalidateObjects();
    }, [invalidateObjects]),
  });

  // SSE subscription (fallback when WebSocket is unavailable)
  useObjectSetSubscription(ontology, objectSetRid ?? '', {
    enabled: realtimeMode === 'sse' && !!objectSetRid,
    onEvent: useCallback(() => {
      invalidateObjects();
    }, [invalidateObjects]),
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

  // Determine whether we need to use search or list
  const hasActiveSearch =
    searchText.trim().length > 0 || filters.length > 0 || hasFacetSelection;

  // Build where clause for search
  const whereClause = useMemo(() => {
    const allFilters: FilterCondition[] = [...filters];

    if (searchText.trim()) {
      // Add a full-text search filter using containsAnyTerm on the title property
      const titleProp = objectType?.titleProperty ?? objectType?.primaryKey;
      if (titleProp) {
        allFilters.push({
          field: titleProp,
          op: 'containsAnyTerm',
          value: searchText
            .trim()
            .split(/\s+/)
            .filter((t) => t.length > 0),
        });
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
    orderBy: sortField
      ? `${sortField}:${sortDirection}`
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
    orderBy: sortField
      ? { field: sortField, direction: sortDirection }
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

  // Loading state for object type
  if (isLoadingType) {
    return (
      <div className="flex items-center justify-center h-64">
        <LoadingSpinner size="lg" />
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
          </div>
          <ExportButton
            objectType={objectType}
            query={{
              ontologyApiName: ontology,
              objectType: objectTypeParam,
              where: whereClause,
              orderBy: sortField
                ? { field: sortField, direction: sortDirection }
                : undefined,
              select: selectFields,
              hasActiveSearch,
            }}
          />
          <label className="flex items-center gap-2 cursor-pointer select-none">
            {realtimeEnabled && (
              <span
                data-testid="realtime-indicator"
                className="inline-block w-2 h-2 rounded-full bg-green-500 animate-pulse"
              />
            )}
            <span className="text-xs font-mono text-text-secondary">Live</span>
            <input
              type="checkbox"
              aria-label="Live"
              checked={realtimeEnabled}
              onChange={handleRealtimeToggle}
              className="sr-only peer"
            />
            <span className="relative w-8 h-4 rounded-full bg-border-secondary peer-checked:bg-green-500 transition-colors after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:w-3 after:h-3 after:rounded-full after:bg-white after:transition-transform peer-checked:after:translate-x-4" />
          </label>
        </div>
      </div>

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
        <div className="px-4 py-3 border border-accent-error/30 bg-accent-error/5 rounded text-xs font-mono text-accent-error">
          {error instanceof Error ? error.message : 'Failed to load objects'}
        </div>
      )}

      {/* Loading */}
      {isLoading && (
        <div className="flex items-center justify-center h-32">
          <LoadingSpinner />
        </div>
      )}

      {/* Results area: Facets sidebar + Table/Map/Empty */}
      {!isLoading && page && (
        <div className="flex gap-4">
          {hasActiveSearch && facetFields.length > 0 && (
            <FacetsPanel
              fields={facetFields}
              facets={page.facets}
              selected={selectedFacets}
              onToggle={handleToggleFacet}
              onClear={handleClearFacets}
            />
          )}
          <div className="flex-1 min-w-0">
            {page.data.length > 0 && viewMode === 'table' && (
              <ObjectTable
                ontologyApiName={ontology}
                objectType={objectType}
                data={page.data}
                onRowClick={handleRowClick}
                onSort={handleSort}
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

      {/* Bulk-action floating toolbar (rendered only when selection is non-empty) */}
      <BulkActionToolbar
        ontologyApiName={ontology}
        objectType={objectType}
        selectedRows={selectedRows}
        onClear={handleClearSelection}
      />

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
