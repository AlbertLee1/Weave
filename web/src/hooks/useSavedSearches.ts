import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createSavedSearch,
  deleteSavedSearch,
  listSavedSearches,
  updateSavedSearch,
  type CreateSavedSearchInput,
  type ListSavedSearchesParams,
  type SavedSearch,
  type UpdateSavedSearchInput,
} from '../api/savedSearches';
import { ApiRequestError } from '../api/client';

const STABLE_DISABLED_KEY = ['saved-searches', '__none__'] as const;

export function useSavedSearches(
  params: Partial<ListSavedSearchesParams> & { enabled?: boolean } = {},
) {
  const { ontology, objectType, enabled = true } = params;
  const ready = !!ontology && !!objectType;
  return useQuery<SavedSearch[]>({
    queryKey: ready
      ? ['saved-searches', ontology, objectType]
      : (STABLE_DISABLED_KEY as unknown as string[]),
    queryFn: async () => {
      try {
        const resp = await listSavedSearches({
          ontology: ontology as string,
          objectType: objectType as string,
        });
        return resp.savedSearches ?? [];
      } catch (e) {
        // Degraded-mode deployments leave the route unmounted; surface
        // an empty list so the panel hides itself instead of erroring.
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'SavedSearchesUnavailable')
        ) {
          return [];
        }
        throw e;
      }
    },
    enabled: ready && enabled,
  });
}

export function useCreateSavedSearch() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateSavedSearchInput) => createSavedSearch(input),
    onSuccess: (saved) => {
      qc.invalidateQueries({
        queryKey: ['saved-searches', saved.ontology, saved.objectType],
      });
    },
  });
}

export function useUpdateSavedSearch() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateSavedSearchInput) => updateSavedSearch(input),
    onSuccess: (saved) => {
      qc.invalidateQueries({
        queryKey: ['saved-searches', saved.ontology, saved.objectType],
      });
    },
  });
}

export function useDeleteSavedSearch() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteSavedSearch(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['saved-searches'] });
    },
  });
}
