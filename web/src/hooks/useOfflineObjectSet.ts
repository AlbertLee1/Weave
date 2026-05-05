// useOfflineObjectSet (US-451). Wraps useLoadObjectSet with an IndexedDB-
// backed cache so the SPA can render last-known rows when offline, and
// surfaces a server-vs-cache conflict on reconnect that the user resolves
// with keep-mine / use-server. The cache layer (`objectSetSnapshotCache`)
// rides on the same IDB wrapper as US-354's offlineCache; no `dexie` dep.
//
// Design decisions:
//   - Conflict is raised when the *server* fingerprint differs from the
//     cached one — equality skips the dialog entirely so a redundant fetch
//     is invisible.
//   - Default UI shows the SERVER rows (consistent with classic React Query
//     behaviour) but exposes `keepMine()` which swaps the displayed rows
//     back to the cached snapshot AND leaves the cache untouched, so the
//     local view stays "mine" until the user decides otherwise.
//   - `useServer()` overwrites the cache with the server payload and clears
//     the conflict, locking in the server view as the new baseline.

import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { loadObjectSet } from '../api/objectsets';
import type {
  ObjectSetDefinition,
  LoadObjectSetResponse,
  OrderBy,
} from '../api/types';
import {
  buildObjectSetSnapshotKey,
  detectObjectSetConflict,
  fingerprintObjectSetRows,
  loadObjectSetSnapshot,
  saveObjectSetSnapshot,
  type ObjectSetConflict,
  type ObjectSetSnapshot,
} from '../lib/objectSetSnapshotCache';

export interface UseOfflineObjectSetParams {
  ontologyApiName: string;
  objectSet: ObjectSetDefinition | null;
  select: string[];
  pageSize?: number;
  pageToken?: string;
  orderBy?: OrderBy;
  enabled?: boolean;
}

export interface UseOfflineObjectSetResult {
  data: LoadObjectSetResponse | null;
  isLoading: boolean;
  isStale: boolean;
  error: unknown;
  conflict: ObjectSetConflict | null;
  // Resolution actions. Both are no-ops when there is no active conflict.
  keepMine: () => Promise<void>;
  useServer: () => Promise<void>;
}

export function useOfflineObjectSet(
  params: UseOfflineObjectSetParams,
): UseOfflineObjectSetResult {
  const {
    ontologyApiName,
    objectSet,
    select,
    pageSize,
    pageToken,
    orderBy,
    enabled = true,
  } = params;
  const queryClient = useQueryClient();

  // Cache key is recomputed on every render but is cheap (one stringify +
  // hash). Memoising would force a useMemo with a deep-equality dep array
  // that's actually slower for typical objectSet shapes.
  const cacheKey =
    objectSet && ontologyApiName
      ? buildObjectSetSnapshotKey(ontologyApiName, objectSet, select)
      : '';

  const [cached, setCached] = useState<ObjectSetSnapshot | null>(null);
  const [conflict, setConflict] = useState<ObjectSetConflict | null>(null);
  // displayOverride holds the rows the user picked via keepMine. When set,
  // it shadows the live React Query data in the returned `data` field
  // until the next reload or a useServer() call clears it.
  const [displayOverride, setDisplayOverride] = useState<
    LoadObjectSetResponse | null
  >(null);
  const [trackedKey, setTrackedKey] = useState('');

  // Render-phase setState comparison (progress.txt:24, US-450 / US-315 prior
  // art): detect a cache-key change and reset transient state without an
  // effect-driven setState, which the React 19 lint rule
  // (`react-hooks/set-state-in-effect`) rejects.
  if (trackedKey !== cacheKey) {
    setTrackedKey(cacheKey);
    setCached(null);
    setConflict(null);
    setDisplayOverride(null);
  }

  // Hydrate the cached snapshot from IndexedDB once per key. This effect
  // only updates state via the resolved snapshot, NOT via a synchronous
  // reset — the reset already happened in render-phase above.
  useEffect(() => {
    if (!cacheKey) return;
    let cancelled = false;
    void loadObjectSetSnapshot(cacheKey).then((snap) => {
      if (cancelled) return;
      setCached(snap);
    });
    return () => {
      cancelled = true;
    };
  }, [cacheKey]);

  const query = useQuery({
    queryKey: [
      'objectSets',
      'offline-load',
      ontologyApiName,
      objectSet,
      select,
      pageSize,
      pageToken,
      orderBy,
    ],
    queryFn: () => {
      const body: Parameters<typeof loadObjectSet>[1] = {
        objectSet: objectSet!,
        select,
      };
      if (pageSize !== undefined) body.pageSize = pageSize;
      if (pageToken !== undefined) body.pageToken = pageToken;
      if (orderBy !== undefined) body.orderBy = orderBy;
      return loadObjectSet(ontologyApiName, body);
    },
    enabled: enabled && !!ontologyApiName && objectSet !== null,
  });

  // Side-effect: on every successful fetch, compare with the cached
  // fingerprint. Mismatch → conflict. Match (or no prior cache) → write the
  // new snapshot through to IDB so the next reload starts from this row set.
  const lastObservedDataRef = useRef<LoadObjectSetResponse | null>(null);
  useEffect(() => {
    const data = query.data;
    if (!data || !cacheKey) return;
    if (lastObservedDataRef.current === data) return;
    lastObservedDataRef.current = data;
    // Deferred conflict detection — `cached` may still be null on the very
    // first render after key change while the IDB read is in flight. We
    // re-read it inline so the comparison sees the current cached value.
    void (async () => {
      const snap = await loadObjectSetSnapshot(cacheKey);
      const detected = detectObjectSetConflict(snap, data.data);
      if (detected) {
        setConflict(detected);
      } else {
        setConflict(null);
        // Server matches cache (or no prior cache): refresh the snapshot
        // with the latest `savedAt` so retention sweeps treat it as fresh.
        await saveObjectSetSnapshot(cacheKey, {
          rows: data.data,
          fingerprint: fingerprintObjectSetRows(data.data),
          savedAt: Date.now(),
        });
        setCached({
          rows: data.data,
          fingerprint: fingerprintObjectSetRows(data.data),
          savedAt: Date.now(),
        });
      }
      // Clear any keepMine display override now that fresh server data is
      // available — the caller can re-pick via the conflict banner.
      setDisplayOverride(null);
    })();
  }, [query.data, cacheKey]);

  const keepMine = useCallback(async () => {
    if (!cacheKey || !cached) return;
    setDisplayOverride({
      data: cached.rows,
    });
    setConflict(null);
  }, [cacheKey, cached]);

  const useServer = useCallback(async () => {
    if (!cacheKey || !query.data) return;
    const next: ObjectSetSnapshot = {
      rows: query.data.data,
      fingerprint: fingerprintObjectSetRows(query.data.data),
      savedAt: Date.now(),
    };
    await saveObjectSetSnapshot(cacheKey, next);
    setCached(next);
    setConflict(null);
    setDisplayOverride(null);
    // Invalidate so dependent views (detail panels) refresh against the
    // new authoritative cache.
    void queryClient.invalidateQueries({
      queryKey: ['objectSets', 'offline-load', ontologyApiName, objectSet],
    });
  }, [cacheKey, query.data, queryClient, ontologyApiName, objectSet]);

  // Compose the visible result. When the network call fails AND we have a
  // cached snapshot, fall back to it under `isStale=true` so the UI can
  // render the cached rows while showing the offline banner.
  let visible: LoadObjectSetResponse | null = null;
  let stale = false;
  if (displayOverride) {
    visible = displayOverride;
  } else if (query.data) {
    visible = query.data;
  } else if (query.error && cached) {
    visible = { data: cached.rows };
    stale = true;
  }

  return {
    data: visible,
    isLoading: query.isLoading,
    isStale: stale,
    error: query.error ?? null,
    conflict,
    keepMine,
    useServer,
  };
}
