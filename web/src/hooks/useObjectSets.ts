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
  findActiveVersion,
  localStorageKey,
  newVersionId,
  type ObjectSetVersion,
  type SavedObjectSet,
} from '../lib/objectSetBuilder';

export interface UseLoadObjectSetParams {
  ontologyApiName: string;
  objectSet: ObjectSetDefinition | null;
  select: string[];
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
      const body: Parameters<typeof loadObjectSet>[1] = {
        objectSet: objectSet!,
        select: params.select,
      };
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
      const now = new Date().toISOString();
      const version: ObjectSetVersion = {
        versionId: newVersionId(),
        def,
        createdAt: now,
      };
      const entry: SavedObjectSet = {
        id: `s-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`,
        name,
        def,
        createdAt: now,
        versions: [version],
        activeVersionId: version.versionId,
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

  // US-332: append a new named version to an existing saved ObjectSet and
  // make it the active version. Returns the updated saved set.
  const addVersion = useCallback(
    (
      id: string,
      def: ObjectSetDefinition,
      note?: string,
    ): SavedObjectSet | undefined => {
      const idx = items.findIndex((it) => it.id === id);
      if (idx === -1) return undefined;
      const existing = items[idx];
      const version: ObjectSetVersion = {
        versionId: newVersionId(),
        def,
        createdAt: new Date().toISOString(),
        ...(note ? { note } : {}),
      };
      const updated: SavedObjectSet = {
        ...existing,
        def,
        versions: [version, ...existing.versions],
        activeVersionId: version.versionId,
      };
      const next = items.slice();
      next[idx] = updated;
      persist(next);
      return updated;
    },
    [items, persist],
  );

  // US-332: switch which version of a saved set is the active one. Other
  // surfaces (composer, diff page) read `def` and load the active version.
  const setActiveVersion = useCallback(
    (id: string, versionId: string): SavedObjectSet | undefined => {
      const idx = items.findIndex((it) => it.id === id);
      if (idx === -1) return undefined;
      const existing = items[idx];
      const target = existing.versions.find((v) => v.versionId === versionId);
      if (!target) return undefined;
      const updated: SavedObjectSet = {
        ...existing,
        def: target.def,
        activeVersionId: versionId,
      };
      const next = items.slice();
      next[idx] = updated;
      persist(next);
      return updated;
    },
    [items, persist],
  );

  // US-332: drop a single version from a saved set. Refuses to drop the
  // last remaining version (callers should `remove` the whole saved set
  // instead). If the active version is dropped the most recent remaining
  // version becomes active.
  const removeVersion = useCallback(
    (id: string, versionId: string): SavedObjectSet | undefined => {
      const idx = items.findIndex((it) => it.id === id);
      if (idx === -1) return undefined;
      const existing = items[idx];
      if (existing.versions.length <= 1) return undefined;
      const remaining = existing.versions.filter(
        (v) => v.versionId !== versionId,
      );
      const nextActiveId =
        existing.activeVersionId === versionId
          ? remaining[0].versionId
          : existing.activeVersionId;
      const nextActive = remaining.find((v) => v.versionId === nextActiveId)!;
      const updated: SavedObjectSet = {
        ...existing,
        def: nextActive.def,
        versions: remaining,
        activeVersionId: nextActiveId,
      };
      const next = items.slice();
      next[idx] = updated;
      persist(next);
      return updated;
    },
    [items, persist],
  );

  const clear = useCallback(() => {
    persist([]);
  }, [persist]);

  return {
    items,
    save,
    remove,
    addVersion,
    setActiveVersion,
    removeVersion,
    clear,
  };
}

function readSaved(ontologyApiName: string): SavedObjectSet[] {
  if (!ontologyApiName || typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(localStorageKey(ontologyApiName));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter(
        (x) =>
          typeof x === 'object' &&
          x !== null &&
          typeof (x as { id?: unknown }).id === 'string' &&
          typeof (x as { name?: unknown }).name === 'string' &&
          typeof (x as { def?: unknown }).def === 'object',
      )
      .map((x) => migrateLegacy(x as Partial<SavedObjectSet>));
  } catch {
    return [];
  }
}

// US-332: legacy entries (before versions support) carry only def + createdAt;
// promote them into a single-entry versions array so version-aware code can
// iterate uniformly without per-call-site shape checks.
function migrateLegacy(raw: Partial<SavedObjectSet>): SavedObjectSet {
  if (
    Array.isArray(raw.versions) &&
    raw.versions.length > 0 &&
    typeof raw.activeVersionId === 'string'
  ) {
    return raw as SavedObjectSet;
  }
  const createdAt = raw.createdAt ?? new Date().toISOString();
  const version: ObjectSetVersion = {
    versionId: newVersionId(),
    def: raw.def!,
    createdAt,
  };
  return {
    id: raw.id!,
    name: raw.name!,
    def: raw.def!,
    createdAt,
    versions: [version],
    activeVersionId: version.versionId,
  };
}

// Re-export for callers that need to resolve the active version.
export { findActiveVersion };
