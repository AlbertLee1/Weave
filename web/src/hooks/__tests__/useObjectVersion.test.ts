import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import { useObjectVersion } from '../useObjectVersion';
import * as objectsApi from '../../api/objects';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

describe('useObjectVersion', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetches totalVersions via getObjectHistory when enabled', async () => {
    const spy = vi.spyOn(objectsApi, 'getObjectHistory').mockResolvedValue({
      history: [],
      totalVersions: 4,
    });

    const { result } = renderHook(
      () =>
        useObjectVersion({
          ontologyApiName: 'default',
          objectType: 'Employee',
          primaryKey: '42',
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.version).toBe(4);
    expect(spy).toHaveBeenCalledWith({
      ontologyApiName: 'default',
      objectType: 'Employee',
      primaryKey: '42',
      limit: 1,
    });
  });

  it('is disabled when primaryKey is empty', async () => {
    const spy = vi.spyOn(objectsApi, 'getObjectHistory').mockResolvedValue({
      history: [],
      totalVersions: 1,
    });

    const { result } = renderHook(
      () =>
        useObjectVersion({
          ontologyApiName: 'default',
          objectType: 'Employee',
          primaryKey: '',
        }),
      { wrapper: makeWrapper() },
    );

    expect(result.current.fetchStatus).toBe('idle');
    expect(spy).not.toHaveBeenCalled();
    expect(result.current.version).toBeUndefined();
  });

  it('is disabled when objectType is empty', async () => {
    const spy = vi.spyOn(objectsApi, 'getObjectHistory').mockResolvedValue({
      history: [],
      totalVersions: 2,
    });

    const { result } = renderHook(
      () =>
        useObjectVersion({
          ontologyApiName: 'default',
          objectType: '',
          primaryKey: '1',
        }),
      { wrapper: makeWrapper() },
    );

    expect(result.current.fetchStatus).toBe('idle');
    expect(spy).not.toHaveBeenCalled();
  });

  it('refetch refreshes the cached version', async () => {
    const spy = vi
      .spyOn(objectsApi, 'getObjectHistory')
      .mockResolvedValueOnce({ history: [], totalVersions: 1 })
      .mockResolvedValueOnce({ history: [], totalVersions: 7 });

    const { result } = renderHook(
      () =>
        useObjectVersion({
          ontologyApiName: 'default',
          objectType: 'Employee',
          primaryKey: 'e1',
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.version).toBe(1));

    await result.current.refetch();
    await waitFor(() => expect(result.current.version).toBe(7));
    expect(spy).toHaveBeenCalledTimes(2);
  });
});
