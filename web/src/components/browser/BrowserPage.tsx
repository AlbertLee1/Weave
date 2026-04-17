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
import { ObjectTable } from './ObjectTable';
import { ObjectDetail } from './ObjectDetail';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import type { WireObject } from '../../api/types';

const PAGE_SIZE = 25;

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

  // Pagination state
  const [pageTokens, setPageTokens] = useState<string[]>([]);
  const [currentPage, setCurrentPage] = useState(1);

  const currentPageToken = pageTokens[currentPage - 2]; // page 1 has no token

  // Detail panel state
  const [selectedObject, setSelectedObject] = useState<WireObject | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

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

  // Determine whether we need to use search or list
  const hasActiveSearch = searchText.trim().length > 0 || filters.length > 0;

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

    return buildWhereClause(allFilters);
  }, [filters, searchText, objectType]);

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

      {/* Table */}
      {!isLoading && page && page.data.length > 0 && (
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
        />
      )}

      {/* Empty state */}
      {!isLoading && page && page.data.length === 0 && (
        <EmptyState
          title="No objects found"
          description={
            hasActiveSearch
              ? 'Try adjusting your search or filters.'
              : 'This object type has no data yet.'
          }
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
