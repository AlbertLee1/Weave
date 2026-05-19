import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { TimeTravelToolbar } from '../TimeTravelToolbar';
import * as datasetsApi from '../../../api/datasets';

function renderToolbar() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <TimeTravelToolbar ontologyApiName="iotDemo" />
    </QueryClientProvider>,
  );
}

describe('TimeTravelToolbar empty transactions (dogfood #7)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('option text and hint guide the user to apply an action', async () => {
    vi.spyOn(datasetsApi, 'listDatasetHistory').mockResolvedValue({
      transactions: [],
      truncated: false,
    });

    renderToolbar();

    // Wait for the picker to render. The default option's label should
    // explain what to do when no transactions have been recorded.
    const picker = await screen.findByTestId('time-travel-picker');
    expect(picker.textContent ?? '').toMatch(/no transactions/i);
    expect(picker.textContent ?? '').toMatch(/apply an action/i);
  });

  it('warns when the transaction chain is capped to the latest 1000 rows', async () => {
    vi.spyOn(datasetsApi, 'listDatasetHistory').mockResolvedValue({
      transactions: [
        {
          txId: 'tx-1001',
          ontologyApiName: 'iotDemo',
          committedAt: '2026-05-19T08:00:00Z',
          editsCount: 2,
        },
      ],
      truncated: true,
    } as Awaited<ReturnType<typeof datasetsApi.listDatasetHistory>>);

    renderToolbar();

    await waitFor(() => {
      expect(screen.getByTestId('time-travel-picker').textContent ?? '').toMatch(
        /tx-1001/,
      );
    });
    const hint = screen.getByTestId('time-travel-hint');
    expect(hint.textContent ?? '').toMatch(/latest 1000 transactions/i);
  });
});
