import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

const apiMocks = vi.hoisted(() => ({
  getQuiverData: vi.fn(),
}));
vi.mock('../../api/quiver', () => apiMocks);

import { useQuiverData } from '../useQuiverData';

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

describe('useQuiverData', () => {
  beforeEach(() => {
    apiMocks.getQuiverData.mockReset();
  });

  it('calls getQuiverData with the rid + window + step', async () => {
    apiMocks.getQuiverData.mockResolvedValue({
      rid: 'r1',
      from: '2026-01-01T00:00:00Z',
      to: '2026-01-02T00:00:00Z',
      step: '5m',
      series: [],
    });

    const { result } = renderHook(
      () =>
        useQuiverData({
          rid: 'r1',
          from: '2026-01-01T00:00:00Z',
          to: '2026-01-02T00:00:00Z',
          step: '5m',
          enabled: true,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(apiMocks.getQuiverData).toHaveBeenCalledWith('r1', {
      from: '2026-01-01T00:00:00Z',
      to: '2026-01-02T00:00:00Z',
      step: '5m',
    });
  });

  it('stays disabled when no rid or no step', () => {
    renderHook(
      () => useQuiverData({ rid: undefined, step: '5m', enabled: true }),
      { wrapper },
    );
    renderHook(() => useQuiverData({ rid: 'r1', step: '', enabled: true }), {
      wrapper,
    });
    expect(apiMocks.getQuiverData).not.toHaveBeenCalled();
  });

  it('respects the enabled flag', () => {
    renderHook(
      () => useQuiverData({ rid: 'r1', step: '5m', enabled: false }),
      { wrapper },
    );
    expect(apiMocks.getQuiverData).not.toHaveBeenCalled();
  });
});
