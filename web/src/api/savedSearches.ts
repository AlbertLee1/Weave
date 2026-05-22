import { request } from './client';
import type { WhereClause } from './types';
import type { FilterCondition } from '../lib/whereBuilder';
import type { FacetSelection } from '../components/browser/FacetsPanel';

export type BrowserViewMode = 'table' | 'map' | 'gantt' | 'sankey' | 'pivot';

// SavedSearchDefinition is the front-end-owned envelope persisted on
// the backend as opaque JSONB. The shape is intentionally a wire-only
// concern — the server round-trips it untouched. Adding new keys to
// capture additional Browser-page state (e.g. column visibility,
// pinned rows) does not require a backend change.
export interface SavedSearchDefinition {
  searchText?: string;
  filters?: FilterCondition[];
  facets?: FacetSelection;
  where?: WhereClause | null;
  sort?: { field: string; direction: 'asc' | 'desc' } | null;
  viewMode?: BrowserViewMode;
}

export interface SavedSearch {
  id: string;
  name: string;
  ontology: string;
  objectType: string;
  createdBy: string;
  definition: SavedSearchDefinition;
  createdAt: string;
  updatedAt: string;
}

export interface ListSavedSearchesResponse {
  savedSearches: SavedSearch[];
}

export interface ListSavedSearchesParams {
  ontology: string;
  objectType: string;
}

export function listSavedSearches(
  params: ListSavedSearchesParams,
): Promise<ListSavedSearchesResponse> {
  const qs = new URLSearchParams({
    ontology: params.ontology,
    objectType: params.objectType,
  });
  return request<ListSavedSearchesResponse>(
    'GET',
    `/api/v2/saved-searches?${qs.toString()}`,
  );
}

export interface CreateSavedSearchInput {
  name: string;
  ontology: string;
  objectType: string;
  definition: SavedSearchDefinition;
}

export function createSavedSearch(
  input: CreateSavedSearchInput,
): Promise<SavedSearch> {
  return request<SavedSearch>('POST', '/api/v2/saved-searches', input);
}

export interface UpdateSavedSearchInput {
  id: string;
  name?: string;
  definition?: SavedSearchDefinition;
}

export function updateSavedSearch(
  input: UpdateSavedSearchInput,
): Promise<SavedSearch> {
  const { id, ...body } = input;
  return request<SavedSearch>(
    'PUT',
    `/api/v2/saved-searches/${encodeURIComponent(id)}`,
    body,
  );
}

export function deleteSavedSearch(id: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/saved-searches/${encodeURIComponent(id)}`,
  );
}
