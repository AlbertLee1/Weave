import { useCallback, useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  loadObjectSet,
  aggregateObjectSet,
  createTemporaryObjectSet,
} from '../api/objectsets';
import type {
  ObjectSetDefinition,
  AggregationMetric,
  GroupByClause,
  OrderBy,
} from '../api/types';
import {
  localStorageKey,
  type SavedObjectSet,
} from '../lib/objectSetBuilder';

export interface UseLoadObjectSetParams {
  ontologyApiName: string;
  objectSet: ObjectSetDefinition | null;
  select?: string[];
  pageSize?: number;
  pageToken?: string;
  orderBy?: OrderBy;
  enabled?: boolean;
}

export function useLoadObjectSet(params: UseLoadObjectSetParams) {
  const { ontologyApiName, objectSet, enabled = true } = params;
  return useQuery({
    queryKey: [
      'objectSets',
      'load',
      ontologyApiName,
      objectSet,
      params.select,
      params.pageSize,
      params.pageToken,
      params.orderBy,
    ],
    queryFn: () => {
      const body: Parameters<typeof loadObjectSet>[1] = { objectSet: objectSet! };
      if (params.select !== undefined) body.select = params.select;
      if (params.pageSize !== undefined) body.pageSize = params.pageSize;
      if (params.pageToken !== undefined) body.pageToken = params.pageToken;
      if (params.orderBy !== undefined) body.orderBy = params.orderBy;
      return loadObjectSet(ontologyApiName, body);
    },
    enabled: enabled && !!ontologyApiName && objectSet !== null,
  });
}

export interface UseAggregateObjectSetParams {
  ontologyApiName: string;
  objectSet: ObjectSetDefinition | null;
  aggregation: AggregationMetric[];
  groupBy?: GroupByClause[];
  enabled?: boolean;
}

export function useAggregateObjectSet(params: UseAggregateObjectSetParams) {
  const { ontologyApiName, objectSet, aggregation, groupBy, enabled = true } =
    params;
  return useQuery({
    queryKey: [
      'objectSets',
      'aggregate',
      ontologyApiName,
      objectSet,
      aggregation,
      groupBy,
    ],
    queryFn: () =>
      aggregateObjectSet(ontologyApiName, objectSet!, aggregation, groupBy),
    enabled:
      enabled &&
      !!ontologyApiName &&
      objectSet !== null &&
      aggregation.length > 0,
  });
}

export function useCreateTemporaryObjectSet(ontologyApiName: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (objectSet: ObjectSetDefinition) =>
      createTemporaryObjectSet(ontologyApiName, objectSet),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['objectSets', 'temporary', ontologyApiName],
      });
    },
  });
}

// useSavedObjectSets is a localStorage-backed CRUD store keyed by ontology.
export function useSavedObjectSets(ontologyApiName: string) {
  const [items, setItems] = useState<SavedObjectSet[]>(() =>
    readSaved(ontologyApiName),
  );

  // Reset items when the ontology changes.
  useEffect(() => {
    setItems(readSaved(ontologyApiName));
  }, [ontologyApiName]);

  const persist = useCallback(
    (next: SavedObjectSet[]) => {
      setItems(next);
      try {
        window.localStorage.setItem(
          localStorageKey(ontologyApiName),
          JSON.stringify(next),
        );
      } catch {
        // ignore quota / disabled storage errors
      }
    },
    [ontologyApiName],
  );

  const save = useCallback(
    (name: string, def: ObjectSetDefinition): SavedObjectSet => {
      const entry: SavedObjectSet = {
        id: `s-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`,
        name,
        def,
        createdAt: new Date().toISOString(),
      };
      persist([entry, ...items]);
      return entry;
    },
    [items, persist],
  );

  const remove = useCallback(
    (id: string) => {
      persist(items.filter((it) => it.id !== id));
    },
    [items, persist],
  );

  const clear = useCallback(() => {
    persist([]);
  }, [persist]);

  return { items, save, remove, clear };
}

function readSaved(ontologyApiName: string): SavedObjectSet[] {
  if (!ontologyApiName || typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(localStorageKey(ontologyApiName));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (x): x is SavedObjectSet =>
        typeof x === 'object' &&
        x !== null &&
        typeof (x as SavedObjectSet).id === 'string' &&
        typeof (x as SavedObjectSet).name === 'string' &&
        typeof (x as SavedObjectSet).def === 'object',
    );
  } catch {
    return [];
  }
}
