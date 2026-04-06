import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import { useLoadObjectSet } from '../useObjectSets';
import * as objectsetsApi from '../../api/objectsets';
import type { ObjectSetDefinition } from '../../api/types';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

const baseObjectSet: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };

describe('useLoadObjectSet', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('calls loadObjectSet and returns data', async () => {
    const mockResponse = {
      data: [{ __rid: 'ri.1', __primaryKey: '1', __apiName: 'Employee' }],
      totalCount: '1',
    };
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue(mockResponse);

    const { result } = renderHook(
      () =>
        useLoadObjectSet({
          ontologyApiName: 'test',
          objectSet: baseObjectSet,
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data).toHaveLength(1);
    expect(objectsetsApi.loadObjectSet).toHaveBeenCalledWith('test', {
      objectSet: baseObjectSet,
    });
  });

  it('is disabled when enabled=false', async () => {
    const spy = vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: [],
      totalCount: '0',
    });

    const { result } = renderHook(
      () =>
        useLoadObjectSet({
          ontologyApiName: 'test',
          objectSet: baseObjectSet,
          enabled: false,
        }),
      { wrapper: makeWrapper() },
    );

    // Should remain idle
    expect(result.current.isPending).toBe(true);
    expect(result.current.fetchStatus).toBe('idle');
    expect(spy).not.toHaveBeenCalled();
  });

  it('is disabled when ontologyApiName is empty', async () => {
    const spy = vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: [],
      totalCount: '0',
    });

    const { result } = renderHook(
      () =>
        useLoadObjectSet({
          ontologyApiName: '',
          objectSet: baseObjectSet,
          enabled: true,
        }),
      { wrapper: makeWrapper() },
    );

    expect(result.current.fetchStatus).toBe('idle');
    expect(spy).not.toHaveBeenCalled();
  });
});
