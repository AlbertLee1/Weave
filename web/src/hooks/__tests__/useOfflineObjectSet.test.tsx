import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement, type ReactNode } from 'react';
import { useOfflineObjectSet } from '../useOfflineObjectSet';
import * as objectsetsApi from '../../api/objectsets';
import type { ObjectSetDefinition, WireObject } from '../../api/types';
import { __resetForTests as resetOfflineCache } from '../../lib/offlineCache';
import {
  buildObjectSetSnapshotKey,
  fingerprintObjectSetRows,
  saveObjectSetSnapshot,
  loadObjectSetSnapshot,
} from '../../lib/objectSetSnapshotCache';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

const def: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };

function row(pk: string, name: string): WireObject {
  return {
    __rid: `ri.test.main.employee.${pk}`,
    __primaryKey: pk,
    __apiName: 'Employee',
    name,
  };
}

function setOnLine(value: boolean) {
  Object.defineProperty(navigator, 'onLine', {
    configurable: true,
    get: () => value,
  });
}

describe('useOfflineObjectSet (US-451)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    resetOfflineCache();
    setOnLine(true);
  });

  afterEach(() => {
    setOnLine(true);
  });

  it('caches the response on a successful online fetch', async () => {
    const data = [row('1', 'Alice')];
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data,
      totalCount: '1',
    });

    const { result } = renderHook(
      () =>
        useOfflineObjectSet({
          ontologyApiName: 'northwind',
          objectSet: def,
          select: ['name'],
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.data?.data).toEqual(data));

    const key = buildObjectSetSnapshotKey('northwind', def, ['name']);
    const cached = await loadObjectSetSnapshot(key);
    expect(cached).not.toBeNull();
    expect(cached?.rows).toEqual(data);
    expect(cached?.fingerprint).toBe(fingerprintObjectSetRows(data));
  });

  it('serves the cached snapshot when offline and the fetch fails', async () => {
    const cachedRows = [row('1', 'Alice')];
    const key = buildObjectSetSnapshotKey('northwind', def, ['name']);
    await saveObjectSetSnapshot(key, {
      rows: cachedRows,
      fingerprint: fingerprintObjectSetRows(cachedRows),
      savedAt: 1700000000000,
    });

    vi.spyOn(objectsetsApi, 'loadObjectSet').mockRejectedValue(
      new Error('NetworkError'),
    );
    setOnLine(false);

    const { result } = renderHook(
      () =>
        useOfflineObjectSet({
          ontologyApiName: 'northwind',
          objectSet: def,
          select: ['name'],
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.isStale).toBe(true));
    expect(result.current.data?.data).toEqual(cachedRows);
  });

  it('reports a conflict when the server fingerprint differs from the cache on reconnect', async () => {
    const cachedRows = [row('1', 'Alice')];
    const key = buildObjectSetSnapshotKey('northwind', def, ['name']);
    await saveObjectSetSnapshot(key, {
      rows: cachedRows,
      fingerprint: fingerprintObjectSetRows(cachedRows),
      savedAt: 1700000000000,
    });

    const serverRows = [row('1', 'Alice2')];
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: serverRows,
      totalCount: '1',
    });

    const { result } = renderHook(
      () =>
        useOfflineObjectSet({
          ontologyApiName: 'northwind',
          objectSet: def,
          select: ['name'],
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.conflict).not.toBeNull());
    expect(result.current.conflict?.minePk).toEqual(['1']);
    expect(result.current.conflict?.serverPk).toEqual(['1']);
    // The displayed data defaults to the server view until the user picks otherwise.
    expect(result.current.data?.data).toEqual(serverRows);
  });

  it('keepMine swaps displayed data to the cached snapshot and clears the conflict', async () => {
    const cachedRows = [row('1', 'Alice')];
    const key = buildObjectSetSnapshotKey('northwind', def, ['name']);
    await saveObjectSetSnapshot(key, {
      rows: cachedRows,
      fingerprint: fingerprintObjectSetRows(cachedRows),
      savedAt: 1700000000000,
    });

    const serverRows = [row('1', 'Alice2')];
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: serverRows,
      totalCount: '1',
    });

    const { result } = renderHook(
      () =>
        useOfflineObjectSet({
          ontologyApiName: 'northwind',
          objectSet: def,
          select: ['name'],
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.conflict).not.toBeNull());

    await act(async () => {
      await result.current.keepMine();
    });

    expect(result.current.conflict).toBeNull();
    expect(result.current.data?.data).toEqual(cachedRows);
  });

  it('useServer overwrites the cache and clears the conflict', async () => {
    const cachedRows = [row('1', 'Alice')];
    const key = buildObjectSetSnapshotKey('northwind', def, ['name']);
    await saveObjectSetSnapshot(key, {
      rows: cachedRows,
      fingerprint: fingerprintObjectSetRows(cachedRows),
      savedAt: 1700000000000,
    });

    const serverRows = [row('1', 'Alice2')];
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: serverRows,
      totalCount: '1',
    });

    const { result } = renderHook(
      () =>
        useOfflineObjectSet({
          ontologyApiName: 'northwind',
          objectSet: def,
          select: ['name'],
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.conflict).not.toBeNull());

    await act(async () => {
      await result.current.useServer();
    });

    expect(result.current.conflict).toBeNull();
    expect(result.current.data?.data).toEqual(serverRows);

    const updated = await loadObjectSetSnapshot(key);
    expect(updated?.fingerprint).toBe(fingerprintObjectSetRows(serverRows));
  });

  it('does not flag a conflict on a clean first fetch (no prior cache)', async () => {
    const data = [row('1', 'Alice')];
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data,
      totalCount: '1',
    });

    const { result } = renderHook(
      () =>
        useOfflineObjectSet({
          ontologyApiName: 'northwind',
          objectSet: def,
          select: ['name'],
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.data?.data).toEqual(data));
    expect(result.current.conflict).toBeNull();
  });
});
