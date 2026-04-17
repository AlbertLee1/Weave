import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import {
  filterPointsByRange,
  useTimeSeriesPoints,
} from '../useTimeSeries';
import * as timeseriesApi from '../../api/timeseries';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
}

describe('filterPointsByRange', () => {
  const now = Date.parse('2026-04-18T12:00:00Z');

  it('keeps only points within a 1h window', () => {
    const points = [
      { time: '2026-04-18T11:59:00Z', value: 1 },
      { time: '2026-04-18T10:00:00Z', value: 2 },
      { time: '2026-04-17T12:00:00Z', value: 3 },
    ];
    const out = filterPointsByRange(points, '1h', now);
    expect(out).toEqual([{ time: '2026-04-18T11:59:00Z', value: 1 }]);
  });

  it('filters by 24h', () => {
    const points = [
      { time: '2026-04-18T00:00:00Z', value: 1 },
      { time: '2026-04-10T00:00:00Z', value: 2 },
    ];
    const out = filterPointsByRange(points, '24h', now);
    expect(out).toHaveLength(1);
    expect(out[0].value).toBe(1);
  });

  it('filters by 30d', () => {
    const points = [
      { time: '2026-04-01T00:00:00Z', value: 1 },
      { time: '2026-01-01T00:00:00Z', value: 2 },
    ];
    const out = filterPointsByRange(points, '30d', now);
    expect(out).toHaveLength(1);
  });

  it('drops points with unparseable time', () => {
    const points = [
      { time: 'not-a-date', value: 1 },
      { time: '2026-04-18T11:00:00Z', value: 2 },
    ];
    const out = filterPointsByRange(points, '24h', now);
    expect(out).toEqual([{ time: '2026-04-18T11:00:00Z', value: 2 }]);
  });
});

describe('useTimeSeriesPoints', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetches points when all params are present', async () => {
    const spy = vi
      .spyOn(timeseriesApi, 'streamTimeSeriesPoints')
      .mockResolvedValue([
        { time: '2026-04-18T00:00:00Z', value: 21.0 },
        { time: '2026-04-18T01:00:00Z', value: 22.5 },
      ]);

    const { result } = renderHook(
      () =>
        useTimeSeriesPoints({
          ontologyApiName: 'northwind',
          objectType: 'Server',
          primaryKey: 's1',
          property: 'cpu',
        }),
      { wrapper: makeWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(2);
    expect(spy).toHaveBeenCalledWith({
      ontologyApiName: 'northwind',
      objectType: 'Server',
      primaryKey: 's1',
      property: 'cpu',
    });
  });

  it('is disabled when primaryKey is empty', () => {
    const spy = vi
      .spyOn(timeseriesApi, 'streamTimeSeriesPoints')
      .mockResolvedValue([]);

    const { result } = renderHook(
      () =>
        useTimeSeriesPoints({
          ontologyApiName: 'northwind',
          objectType: 'Server',
          primaryKey: '',
          property: 'cpu',
        }),
      { wrapper: makeWrapper() },
    );

    expect(result.current.fetchStatus).toBe('idle');
    expect(spy).not.toHaveBeenCalled();
  });

  it('respects enabled=false', () => {
    const spy = vi
      .spyOn(timeseriesApi, 'streamTimeSeriesPoints')
      .mockResolvedValue([]);

    const { result } = renderHook(
      () =>
        useTimeSeriesPoints({
          ontologyApiName: 'northwind',
          objectType: 'Server',
          primaryKey: 's1',
          property: 'cpu',
          enabled: false,
        }),
      { wrapper: makeWrapper() },
    );

    expect(result.current.fetchStatus).toBe('idle');
    expect(spy).not.toHaveBeenCalled();
  });
});
